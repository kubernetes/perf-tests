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
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/perf-tests/clusterloader2/pkg/execservice"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/checker"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/workerqueue"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	serviceCreationLatencyName           = "ServiceCreationLatency"
	serviceCreationLatencyWorkers        = 10
	defaultServiceCreationLatencyTimeout = 10 * time.Minute
	defaultCheckInterval                 = 10 * time.Second
	pingBackoff                          = 1 * time.Second
	pingChecks                           = 10

	creatingPhase     = "creating"
	ipAssigningPhase  = "ipAssigning"
	reachabilityPhase = "reachability"
)

func init() {
	measurement.Register(serviceCreationLatencyName, createServiceCreationLatencyMeasurement)
}

func createServiceCreationLatencyMeasurement() measurement.Measurement {
	return &serviceCreationLatencyMeasurement{
		selector:      measurementutil.NewObjectSelector(),
		queue:         workerqueue.NewWorkerQueue(serviceCreationLatencyWorkers),
		creationTimes: measurementutil.NewObjectTransitionTimes(serviceCreationLatencyName),
		pingCheckers:  checker.NewCheckerMap(),
	}
}

type serviceCreationLatencyMeasurement struct {
	selector      *measurementutil.ObjectSelector
	waitTimeout   time.Duration
	stopCh        chan struct{}
	isRunning     bool
	queue         workerqueue.Interface
	client        clientset.Interface
	creationTimes *measurementutil.ObjectTransitionTimes
	pingCheckers  checker.CheckerMap
}

// Execute executes service startup latency measurement actions.
// Services can be specified by field and/or label selectors.
// If namespace is not passed by parameter, all-namespace scope is assumed.
// "start" action starts observation of the services.
// "waitForReady" waits until all services are reachable.
// "gather" returns service created latency summary.
// This measurement only works for services with LoadBalancer type.
func (s *serviceCreationLatencyMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	s.client = config.ClusterFramework.GetClientSets().GetClient()
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "start":
		if s.selector.Parse(config.Params); err != nil {
			return nil, err
		}
		s.waitTimeout, err = util.GetDurationOrDefault(config.Params, "waitTimeout", defaultServiceCreationLatencyTimeout)
		if err != nil {
			return nil, err
		}
		return nil, s.start()
	case "waitForReady":
		return nil, s.waitForReady()
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
	s.pingCheckers.Dispose()
}

// String returns a string representation of the metric.
func (s *serviceCreationLatencyMeasurement) String() string {
	return serviceCreationLatencyName + ": " + s.selector.String()
}

func (s *serviceCreationLatencyMeasurement) start() error {
	if s.isRunning {
		klog.Infof("%s: service creation latency measurement already running", s)
		return nil
	}
	klog.Infof("%s: starting service creation latency measurement...", s)

	s.isRunning = true
	s.stopCh = make(chan struct{})

	i := informer.NewInformer(
		s.client,
		"services",
		s.selector,
		func(oldObj, newObj interface{}) {
			f := func() {
				s.handleObject(oldObj, newObj)
			}
			s.queue.Add(&f)
		},
	)
	return informer.StartAndSync(i, s.stopCh, informerSyncTimeout)
}

func (s *serviceCreationLatencyMeasurement) waitForReady() error {
	return wait.Poll(defaultCheckInterval, s.waitTimeout, func() (bool, error) {
		reachable := s.creationTimes.Count(reachabilityPhase)
		ipAssigned := s.creationTimes.Count(ipAssigningPhase)
		created := s.creationTimes.Count(creatingPhase)
		klog.Infof("%s: %d created, %d ipAssigned, %d reachable", s, created, ipAssigned, reachable)
		return created == reachable, nil
	})
}

func (s *serviceCreationLatencyMeasurement) gather(identifier string) ([]measurement.Summary, error) {
	klog.Infof("%s: gathering service created latency measurement...", s)
	if !s.isRunning {
		return nil, fmt.Errorf("metric %s has not been started", s)
	}

	serviceCreationLatency := s.creationTimes.CalculateTransitionsLatency(map[string]measurementutil.Transition{
		"create_to_assigned": {
			From: creatingPhase,
			To:   ipAssigningPhase,
		},
		"assigned_to_available": {
			From: ipAssigningPhase,
			To:   reachabilityPhase,
		},
		"create_to_available": {
			From: creatingPhase,
			To:   reachabilityPhase,
		},
	})

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

func (s *serviceCreationLatencyMeasurement) deleteObject(svc *corev1.Service) error {
	key, err := runtimeobjects.CreateMetaNamespaceKey(svc)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	s.pingCheckers.DeleteAndStop(key)
	return nil
}

func (s *serviceCreationLatencyMeasurement) updateObject(svc *corev1.Service) error {
	// This measurement only works for services with LoadBalancer type.
	if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
		return nil
	}
	key, err := runtimeobjects.CreateMetaNamespaceKey(svc)
	if err != nil {
		return fmt.Errorf("meta key created error: %v", err)
	}
	if _, exists := s.creationTimes.Get(key, creatingPhase); !exists {
		s.creationTimes.Set(key, creatingPhase, svc.CreationTimestamp.Time)
	}
	if len(svc.Status.LoadBalancer.Ingress) < 1 {
		return nil
	}
	if _, exists := s.creationTimes.Get(key, ipAssigningPhase); exists {
		return nil
	}
	s.creationTimes.Set(key, ipAssigningPhase, time.Now())

	pc := &pingChecker{
		callerName:    s.String(),
		svc:           svc,
		creationTimes: s.creationTimes,
		stopCh:        make(chan struct{}),
	}
	pc.run()
	s.pingCheckers.Add(key, pc)

	return nil
}

type pingChecker struct {
	callerName    string
	svc           *corev1.Service
	creationTimes *measurementutil.ObjectTransitionTimes
	stopCh        chan struct{}
}

func (p *pingChecker) run() {
	key, err := runtimeobjects.CreateMetaNamespaceKey(p.svc)
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
			if _, exists := p.creationTimes.Get(key, reachabilityPhase); exists {
				return
			}
			// TODO(#679): Current implementation handles only load balancers.
			// TODO(#685): Make ping checks less communication heavy.
			_, err := execservice.RunCommand(
				fmt.Sprintf("curl %s:%d", p.svc.Status.LoadBalancer.Ingress[0].IP, p.svc.Spec.Ports[0].Port))
			if err != nil {
				success = 0
				time.Sleep(pingBackoff)
				continue
			}
			success++
			if success == pingChecks {
				p.creationTimes.Set(key, reachabilityPhase, time.Now())
			}
		}
	}
}

func (p *pingChecker) Stop() {
	close(p.stopCh)
}
