/*
Copyright The Kubernetes Authors.

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

package dnspropagation

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

// sampleTimes holds when one sampled pod reached each stage of DNS propagation.
// A zero time means that the stage has not been observed yet.
type sampleTimes struct {
	// ready is the lastTransitionTime of the pod's Ready condition. It has second resolution.
	ready time.Time
	// seen is when the prober's pod informer delivered the Ready pod.
	seen time.Time
	// endpoint is when the prober first saw the pod as a ready endpoint of the service.
	endpoint time.Time
	// resolved is when the first DNS lookup of the pod succeeded.
	resolved time.Time
}

// stageTracker records the stages of the sampled pods and exports the time spent in each stage.
type stageTracker struct {
	namespace string
	service   string

	mu      sync.Mutex
	samples map[string]*sampleTimes
}

func newStageTracker(pods map[string]struct{}) *stageTracker {
	t := &stageTracker{
		namespace: *namespace,
		service:   *service,
		samples:   make(map[string]*sampleTimes, len(pods)),
	}
	for pod := range pods {
		t.samples[pod] = &sampleTimes{}
	}
	return t
}

// podSeen records that the prober saw the pod Ready.
func (t *stageTracker) podSeen(pod string, ready, seen time.Time) {
	t.update(pod, func(s *sampleTimes) bool {
		if !s.seen.IsZero() {
			return false
		}
		s.ready, s.seen = ready, seen
		return true
	})
}

// endpointReady records that the pod became a ready endpoint of the service.
func (t *stageTracker) endpointReady(pod string, at time.Time) {
	s, ok := t.update(pod, func(s *sampleTimes) bool {
		if !s.endpoint.IsZero() {
			return false
		}
		s.endpoint = at
		return true
	})
	if ok && !s.resolved.IsZero() {
		logSample("DNS propagation endpoint seen after resolution", pod, s)
	}
}

// resolved records the first successful DNS lookup of the pod.
func (t *stageTracker) resolved(pod string, at time.Time) {
	s, ok := t.update(pod, func(s *sampleTimes) bool {
		if !s.resolved.IsZero() {
			return false
		}
		s.resolved = at
		return true
	})
	if ok {
		logSample("DNS propagation sample", pod, s)
	}
}

// update applies set to the pod's sample and exports the stages if set changed it.
func (t *stageTracker) update(pod string, set func(*sampleTimes) bool) (sampleTimes, bool) {
	t.mu.Lock()
	s, ok := t.samples[pod]
	if !ok || !set(s) {
		t.mu.Unlock()
		return sampleTimes{}, false
	}
	snapshot := *s
	t.mu.Unlock()

	t.exportStages(pod, snapshot)
	return snapshot, true
}

func (t *stageTracker) exportStages(pod string, s sampleTimes) {
	labels := prometheus.Labels{
		"namespace": t.namespace,
		"service":   t.service,
		"podName":   pod,
	}
	setStage(DNSPropagationPodSeenSeconds, labels, s.ready, s.seen)
	setStage(DNSPropagationEndpointSeconds, labels, s.ready, s.endpoint)
	setStage(DNSPropagationAfterEndpointSeconds, labels, s.endpoint, s.resolved)
	setStage(DNSPropagationLookupSeconds, labels, s.seen, s.resolved)
}

// setStage sets the gauge to end-start once both times are known.
func setStage(gauge *prometheus.GaugeVec, labels prometheus.Labels, start, end time.Time) {
	if start.IsZero() || end.IsZero() {
		return
	}
	gauge.With(labels).Set(end.Sub(start).Seconds())
}

// stageSeconds returns end-start in seconds, or nil if either time is unknown.
func stageSeconds(start, end time.Time) any {
	if start.IsZero() || end.IsZero() {
		return nil
	}
	return end.Sub(start).Seconds()
}

func logSample(msg, pod string, s sampleTimes) {
	latencyLogger.Info(msg,
		"pod", pod,
		"ready", s.ready,
		"seen", s.seen,
		"endpoint", s.endpoint,
		"resolved", s.resolved,
		"podSeenSeconds", stageSeconds(s.ready, s.seen),
		"endpointSeconds", stageSeconds(s.ready, s.endpoint),
		"afterEndpointSeconds", stageSeconds(s.endpoint, s.resolved),
		"lookupSeconds", stageSeconds(s.seen, s.resolved),
		"totalSeconds", stageSeconds(s.ready, s.resolved),
	)
}

// startEndpointSliceInformer records when the sampled pods become ready endpoints of the service.
// It keeps running after runProbe returns, so that endpoints seen after the lookup succeeded are
// still recorded.
func startEndpointSliceInformer(client kubernetes.Interface, tracker *stageTracker) {
	selector := discoveryv1.LabelServiceName + "=" + tracker.service
	factory := informers.NewSharedInformerFactoryWithOptions(
		client,
		0,
		informers.WithNamespace(tracker.namespace),
		informers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = selector
		}),
	)
	informer := factory.Discovery().V1().EndpointSlices().Informer()
	if err := informer.SetTransform(transformEndpointSlice); err != nil {
		klog.Fatalf("Failed to set EndpointSlice transform: %v", err)
	}

	handleSlice := func(obj interface{}) {
		slice, ok := obj.(*discoveryv1.EndpointSlice)
		if !ok {
			return
		}
		now := time.Now()
		for _, pod := range readyEndpointPods(slice) {
			tracker.endpointReady(pod, now)
		}
	}
	if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: handleSlice,
		UpdateFunc: func(oldObj, newObj interface{}) {
			handleSlice(newObj)
		},
	}); err != nil {
		klog.Fatalf("Failed to add EndpointSlice event handler: %v", err)
	}

	factory.Start(wait.NeverStop)
}

// readyEndpointPods returns the names of the pods that are ready endpoints in the slice.
func readyEndpointPods(slice *discoveryv1.EndpointSlice) []string {
	var pods []string
	for _, endpoint := range slice.Endpoints {
		if endpoint.TargetRef == nil || endpoint.TargetRef.Kind != "Pod" {
			continue
		}
		if endpoint.Conditions.Ready == nil || !*endpoint.Conditions.Ready {
			continue
		}
		pods = append(pods, endpoint.TargetRef.Name)
	}
	return pods
}

// transformEndpointSlice keeps only the fields that readyEndpointPods reads.
func transformEndpointSlice(obj interface{}) (interface{}, error) {
	slice, ok := obj.(*discoveryv1.EndpointSlice)
	if !ok {
		return obj, nil
	}
	stripped := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      slice.Name,
			Namespace: slice.Namespace,
		},
		Endpoints: make([]discoveryv1.Endpoint, 0, len(slice.Endpoints)),
	}
	for _, endpoint := range slice.Endpoints {
		e := discoveryv1.Endpoint{
			Conditions: discoveryv1.EndpointConditions{Ready: endpoint.Conditions.Ready},
		}
		if endpoint.TargetRef != nil {
			e.TargetRef = &v1.ObjectReference{Kind: endpoint.TargetRef.Kind, Name: endpoint.TargetRef.Name}
		}
		stripped.Endpoints = append(stripped.Endpoints, e)
	}
	return stripped, nil
}
