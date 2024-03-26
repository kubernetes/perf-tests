/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/pkg/execservice"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/checker"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/workerqueue"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	serviceCreationLatencyName           = "ServiceCreationLatency"
	serviceCreationLatencyWorkers        = 10
	defaultServiceCreationLatencyTimeout = 10 * time.Minute
	defaultCheckInterval                 = 10 * time.Second
	pingBackoff                          = 1 * time.Second
	pingChecks                           = 3

	creatingPhase     = "creating"
	ipAssigningPhase  = "ipAssigning"
	reachabilityPhase = "reachability"
	deletingPhase     = "deleting"
	deletedPhase      = "deleted"

	ingressType = "ingress"
)

func init() {
	if err := measurement.Register(serviceCreationLatencyName, createServiceCreationLatencyMeasurement); err != nil {
		klog.Fatalf("cant register service %v", err)
	}
}

func createServiceCreationLatencyMeasurement() measurement.Measurement {
	return &serviceCreationLatencyMeasurement{
		selector:      util.NewObjectSelector(),
		queue:         workerqueue.NewWorkerQueue(serviceCreationLatencyWorkers),
		creationTimes: measurementutil.NewObjectTransitionTimes(serviceCreationLatencyName),
		pingCheckers:  checker.NewMap(),
	}
}

type serviceCreationLatencyMeasurement struct {
	selector      *util.ObjectSelector
	waitTimeout   time.Duration
	checkIngress  bool
	stopCh        chan struct{}
	isRunning     bool
	queue         workerqueue.Interface
	client        clientset.Interface
	creationTimes *measurementutil.ObjectTransitionTimes
	pingCheckers  checker.Map
	lock          sync.Mutex
}

// Execute executes service startup latency measurement actions.
// Services can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
// "start" action starts observation of the services.
// "waitForReady" waits until all services are reachable.
// "waitForDeletion" waits until all services are deleted
// "gather" returns service created latency summary.
// This measurement only works for services with ClusterIP, NodePort and LoadBalancer type.
func (s *serviceCreationLatencyMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	s.client = config.ClusterFramework.GetClientSets().GetClient()
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	if !config.ClusterLoaderConfig.ExecServiceConfig.Enable {
		return nil, fmt.Errorf("enable-exec-service flag not enabled")
	}

	switch action {
	case "start":
		if err := s.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		s.waitTimeout, err = util.GetDurationOrDefault(config.Params, "waitTimeout", defaultServiceCreationLatencyTimeout)
		if err != nil {
			return nil, err
		}
		s.checkIngress, err = util.GetBoolOrDefault(config.Params, "checkIngress", false)
		if err != nil {
			return nil, err
		}
		return nil, s.start()
	case "waitForReady":
		return nil, s.waitForReady()
	case "waitForDeletion":
		return nil, s.waitForDeletion()
	case "gather":
		return s.gather(config.Identifier)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// Dispose cleans up after the measurement.
func (s *serviceCreationLatencyMeasurement) Dispose() {
	if s.isRunning {
		s.isRunning = false
		close(s.stopCh)
	}
	s.queue.Stop()
	s.lock.Lock()
	defer s.lock.Unlock()
	s.pingCheckers.Dispose()
}

// String returns a string representation of the metric.
func (s *serviceCreationLatencyMeasurement) String() string {
	return serviceCreationLatencyName + ": " + s.selector.String()
}

func (s *serviceCreationLatencyMeasurement) start() error {
	if s.isRunning {
		klog.V(2).Infof("%s: service creation latency measurement already running", s)
		return nil
	}
	klog.V(2).Infof("%s: starting service creation latency measurement...", s)

	s.isRunning = true
	s.stopCh = make(chan struct{})

	svcInformer := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				s.selector.ApplySelectors(&options)
				return s.client.CoreV1().Services(s.selector.Namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				s.selector.ApplySelectors(&options)
				return s.client.CoreV1().Services(s.selector.Namespace).Watch(context.TODO(), options)
			},
		},
		func(oldObj, newObj interface{}) {
			f := func() {
				s.handleObject(oldObj, newObj)
			}
			s.queue.Add(&f)
		},
	)
	if err := informer.StartAndSync(svcInformer, s.stopCh, informerSyncTimeout); err != nil {
		return err
	}
	if s.checkIngress {
		ingressInformer := informer.NewInformer(
			&cache.ListWatch{
				ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
					s.selector.ApplySelectors(&options)
					return s.client.NetworkingV1().Ingresses(s.selector.Namespace).List(context.TODO(), options)
				},
				WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
					s.selector.ApplySelectors(&options)
					return s.client.NetworkingV1().Ingresses(s.selector.Namespace).Watch(context.TODO(), options)
				},
			},
			func(oldObj, newObj interface{}) {
				f := func() {
					s.handleIngressObject(oldObj, newObj)
				}
				s.queue.Add(&f)
			},
		)
		return informer.StartAndSync(ingressInformer, s.stopCh, informerSyncTimeout)
	}
	return nil
}

func (s *serviceCreationLatencyMeasurement) waitForReady() error {
	return wait.Poll(defaultCheckInterval, s.waitTimeout, func() (bool, error) {
		for _, svcType := range []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer, ingressType} {
			reachable := s.creationTimes.Count(phaseName(reachabilityPhase, svcType))
			created := s.creationTimes.Count(phaseName(creatingPhase, svcType))
			klog.V(2).Infof("%s type %s: %d created, %d reachable", s, svcType, created, reachable)
			if created != reachable {
				return false, nil
			}
		}
		return true, nil
	})
}

func (s *serviceCreationLatencyMeasurement) waitForDeletion() error {
	return wait.Poll(defaultCheckInterval, s.waitTimeout, func() (bool, error) {
		for _, svcType := range []corev1.ServiceType{corev1.ServiceTypeClusterIP, corev1.ServiceTypeNodePort, corev1.ServiceTypeLoadBalancer, ingressType} {
			deleted := s.creationTimes.Count(phaseName(deletedPhase, svcType))
			created := s.creationTimes.Count(phaseName(creatingPhase, svcType))
			klog.V(2).Infof("%s type %s: %d created, %d deleted", s, svcType, created, deleted)
			if created != deleted {
				return false, nil
			}
		}
		return true, nil
	})
}

var serviceCreationTransitions = map[string]measurementutil.Transition{
	"create_to_available_clusterip": {
		From: phaseName(creatingPhase, corev1.ServiceTypeClusterIP),
		To:   phaseName(reachabilityPhase, corev1.ServiceTypeClusterIP),
	},
	"create_to_available_nodeport": {
		From: phaseName(creatingPhase, corev1.ServiceTypeNodePort),
		To:   phaseName(reachabilityPhase, corev1.ServiceTypeNodePort),
	},
	"create_to_assigned_loadbalancer": {
		From: phaseName(creatingPhase, corev1.ServiceTypeLoadBalancer),
		To:   phaseName(ipAssigningPhase, corev1.ServiceTypeLoadBalancer),
	},
	"assigned_to_available_loadbalancer": {
		From: phaseName(ipAssigningPhase, corev1.ServiceTypeLoadBalancer),
		To:   phaseName(reachabilityPhase, corev1.ServiceTypeLoadBalancer),
	},
	"create_to_available_loadbalancer": {
		From: phaseName(creatingPhase, corev1.ServiceTypeLoadBalancer),
		To:   phaseName(reachabilityPhase, corev1.ServiceTypeLoadBalancer),
	},
	"delete_loadbalancer": {
		From: phaseName(deletingPhase, corev1.ServiceTypeLoadBalancer),
		To:   phaseName(deletedPhase, corev1.ServiceTypeLoadBalancer),
	},
	"create_to_assigned_ingress": {
		From: phaseName(creatingPhase, ingressType),
		To:   phaseName(ipAssigningPhase, ingressType),
	},
	"assigned_to_available_ingress": {
		From: phaseName(ipAssigningPhase, ingressType),
		To:   phaseName(reachabilityPhase, ingressType),
	},
	"create_to_available_ingress": {
		From: phaseName(creatingPhase, ingressType),
		To:   phaseName(reachabilityPhase, ingressType),
	},
	"delete_ingress": {
		From: phaseName(deletingPhase, ingressType),
		To:   phaseName(deletedPhase, ingressType),
	},
}

func (s *serviceCreationLatencyMeasurement) gather(identifier string) ([]measurement.Summary, error) {
	klog.V(2).Infof("%s: gathering service created latency measurement...", s)
	if !s.isRunning {
		return nil, fmt.Errorf("metric %s has not been started", s)
	}

	// NOTE: For ClusterIP or NodePort type of service, the cluster ip or node port is assigned as part of service creation API call, so the ipAssigning phase is no sense.
	serviceCreationLatency := s.creationTimes.CalculateTransitionsLatency(serviceCreationTransitions, measurementutil.MatchAll)

	content, err := util.PrettyPrintJSON(measurementutil.LatencyMapToPerfData(serviceCreationLatency))
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(fmt.Sprintf("%s_%s", serviceCreationLatencyName, identifier), "json", content)
	return []measurement.Summary{summary}, nil
}

func (s *serviceCreationLatencyMeasurement) handleObject(oldObj, newObj interface{}) {
	var oldService *corev1.Service
	var newService *corev1.Service
	var ok bool
	oldService, ok = oldObj.(*corev1.Service)
	if oldObj != nil && !ok {
		klog.Errorf("%s: uncastable old object: %v", s, oldObj)
		return
	}
	newService, ok = newObj.(*corev1.Service)
	if newObj != nil && !ok {
		klog.Errorf("%s: uncastable new object: %v", s, newObj)
		return
	}
	if isEqual := oldService != nil &&
		newService != nil &&
		equality.Semantic.DeepEqual(oldService.Spec, newService.Spec) &&
		equality.Semantic.DeepEqual(oldService.Status, newService.Status); isEqual {
		return
	}

	// TODO(#680): Make it thread-safe.
	if !s.isRunning {
		return
	}
	if newObj == nil {
		if err := s.deleteObject(oldService); err != nil {
			klog.Errorf("%s: delete checker error: %v", s, err)
		}
		return
	}
	if err := s.updateObject(newService); err != nil {
		klog.Errorf("%s: create checker error: %v", s, err)
	}
}

func (s *serviceCreationLatencyMeasurement) handleIngressObject(oldObj, newObj interface{}) {
	var oldIngress *networkingv1.Ingress
	var newIngress *networkingv1.Ingress
	var ok bool
	oldIngress, ok = oldObj.(*networkingv1.Ingress)
	if oldObj != nil && !ok {
		klog.Errorf("%s: uncastable old object: %v", s, oldObj)
		return
	}
	newIngress, ok = newObj.(*networkingv1.Ingress)
	if newIngress != nil && !ok {
		klog.Errorf("%s: uncastable new object: %v", s, newObj)
		return
	}
	if isEqual := oldIngress != nil &&
		newIngress != nil &&
		equality.Semantic.DeepEqual(oldIngress.Spec, newIngress.Spec) &&
		equality.Semantic.DeepEqual(oldIngress.Status, newIngress.Status); isEqual {
		return
	}

	if !s.isRunning {
		return
	}
	if newObj == nil {
		if err := s.deleteIngressObject(oldIngress); err != nil {
			klog.Errorf("%s: delete checker error: %v", s, err)
		}
		return
	}
	if err := s.updateIngressObject(newIngress); err != nil {
		klog.Errorf("%s: create checker error: %v", s, err)
	}
}

func (s *serviceCreationLatencyMeasurement) deleteObject(svc *corev1.Service) error {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(svc)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if svc.ObjectMeta.DeletionTimestamp == nil {
		klog.Warningf("DeletionTimestamp is nil for service: %v", key)
		return nil
	}
	s.creationTimes.Set(key, phaseName(deletingPhase, svc.Spec.Type), svc.ObjectMeta.DeletionTimestamp.Time)
	s.creationTimes.Set(key, phaseName(deletedPhase, svc.Spec.Type), time.Now())
	s.pingCheckers.DeleteAndStop(key)
	return nil
}

func (s *serviceCreationLatencyMeasurement) deleteIngressObject(ingress *networkingv1.Ingress) error {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(ingress)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	s.lock.Lock()
	defer s.lock.Unlock()
	if ingress.ObjectMeta.DeletionTimestamp == nil {
		klog.Warningf("DeletionTimestamp is nil for service: %v", key)
		return nil
	}
	s.creationTimes.Set(key, phaseName(deletingPhase, ingressType), ingress.ObjectMeta.DeletionTimestamp.Time)
	s.creationTimes.Set(key, phaseName(deletedPhase, ingressType), time.Now())
	s.pingCheckers.DeleteAndStop(key)
	return nil
}

func (s *serviceCreationLatencyMeasurement) updateObject(svc *corev1.Service) error {
	// This measurement only works for services with ClusterIP, NodePort and LoadBalancer type.
	if svc.Spec.Type != corev1.ServiceTypeClusterIP && svc.Spec.Type != corev1.ServiceTypeNodePort && svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(svc)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	if _, exists := s.creationTimes.Get(key, phaseName(creatingPhase, svc.Spec.Type)); !exists {
		s.creationTimes.Set(key, phaseName(creatingPhase, svc.Spec.Type), svc.CreationTimestamp.Time)
	}
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer && len(svc.Status.LoadBalancer.Ingress) < 1 {
		return nil
	}
	// NOTE: For ClusterIP or NodePort type of service, the cluster ip or node port is assigned as part of service creation API call, so the ipAssigning phase is no sense.
	if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
		if _, exists := s.creationTimes.Get(key, phaseName(ipAssigningPhase, svc.Spec.Type)); exists {
			return nil
		}
		s.creationTimes.Set(key, phaseName(ipAssigningPhase, svc.Spec.Type), time.Now())
	}
	pc := &pingChecker{
		callerName:    s.String(),
		svc:           svc,
		creationTimes: s.creationTimes,
		stopCh:        make(chan struct{}),
	}
	pc.run()
	s.lock.Lock()
	defer s.lock.Unlock()
	s.pingCheckers.Add(key, pc)

	return nil
}

func (s *serviceCreationLatencyMeasurement) updateIngressObject(ingress *networkingv1.Ingress) error {
	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(ingress)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	if _, exists := s.creationTimes.Get(key, phaseName(creatingPhase, ingressType)); !exists {
		s.creationTimes.Set(key, phaseName(creatingPhase, ingressType), ingress.CreationTimestamp.Time)
	}
	if len(ingress.Status.LoadBalancer.Ingress) < 1 {
		return nil
	}
	if _, exists := s.creationTimes.Get(key, phaseName(ipAssigningPhase, ingressType)); exists {
		return nil
	}
	s.creationTimes.Set(key, phaseName(ipAssigningPhase, ingressType), time.Now())
	pc := &pingChecker{
		callerName:    s.String(),
		ingress:       ingress,
		creationTimes: s.creationTimes,
		stopCh:        make(chan struct{}),
	}
	pc.run()
	s.lock.Lock()
	defer s.lock.Unlock()
	s.pingCheckers.Add(key, pc)

	return nil
}

func phaseName(phase string, serviceType corev1.ServiceType) string {
	return fmt.Sprintf("%s_%s", phase, serviceType)
}

type pingChecker struct {
	callerName    string
	svc           *corev1.Service
	ingress       *networkingv1.Ingress
	creationTimes *measurementutil.ObjectTransitionTimes
	stopCh        chan struct{}
}

func (p *pingChecker) run() {
	var key string
	var err error
	var svcType corev1.ServiceType
	if p.svc != nil {
		key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(p.svc)
		svcType = p.svc.Spec.Type
	} else {
		key, err = cache.DeletionHandlingMetaNamespaceKeyFunc(p.ingress)
		svcType = ingressType
	}
	if err != nil {
		klog.Errorf("%s: meta key created error: %v", p.callerName, err)
		return
	}
	success := 0
	for {
		select {
		case <-p.stopCh:
			return
		default:
			// TODO(#685): Make ping checks less communication heavy.
			pod, err := execservice.GetPod()
			if err != nil {
				klog.Warningf("call to execservice.GetPod() ended with error: %v", err)
				success = 0
				time.Sleep(pingBackoff)
				continue
			}
			addresses := p.collectAddresses(pod)
			for _, address := range addresses {
				command := fmt.Sprintf("curl %s", address)
				_, err = execservice.RunCommand(context.TODO(), pod, command)
				if err != nil {
					break
				}
			}
			if err != nil {
				success = 0
				time.Sleep(pingBackoff)
				continue
			}
			success++
			if success == pingChecks {
				p.creationTimes.Set(key, phaseName(reachabilityPhase, svcType), time.Now())
				return
			}
		}
	}
}

func (p *pingChecker) collectAddresses(pod *corev1.Pod) []string {
	var addresses []string
	if p.ingress != nil {
		for _, ing := range p.ingress.Status.LoadBalancer.Ingress {
			addresses = append(addresses, ing.IP)
		}
	} else {
		switch p.svc.Spec.Type {
		case corev1.ServiceTypeClusterIP:
			for _, ip := range p.svc.Spec.ClusterIPs {
				addresses = append(addresses, net.JoinHostPort(ip, fmt.Sprint(p.svc.Spec.Ports[0].Port)))
			}
		case corev1.ServiceTypeNodePort:
			addresses = []string{net.JoinHostPort(pod.Status.HostIP, fmt.Sprint(p.svc.Spec.Ports[0].NodePort))}
		case corev1.ServiceTypeLoadBalancer:
			for _, ingress := range p.svc.Status.LoadBalancer.Ingress {
				addresses = append(addresses, net.JoinHostPort(ingress.IP, fmt.Sprint(p.svc.Spec.Ports[0].Port)))
			}
		}
	}
	return addresses
}

func (p *pingChecker) Stop() {
	close(p.stopCh)
}
