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

package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclientwatch "k8s.io/client-go/rest/watch"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/metrics"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/utils"
)

func newProtobufWatchEncoder(w http.ResponseWriter) *restclientwatch.Encoder {
	pbSerializer := protobuf.NewSerializer(scheme.Scheme, scheme.Scheme)
	rawPBSerializer := protobuf.NewRawSerializer(scheme.Scheme, scheme.Scheme)
	framer := protobuf.LengthDelimitedFramer.NewFrameWriter(w)
	return restclientwatch.NewEncoder(streaming.NewEncoder(framer, rawPBSerializer), pbSerializer)
}

func writeProtobufWatchEvent(t *testing.T, enc *restclientwatch.Encoder, flusher http.Flusher, ev watch.Event) {
	t.Helper()
	if err := enc.Encode(&ev); err != nil {
		return
	}
	flusher.Flush()
}

func buildPod(name, ns, ip, rv string, createdAt time.Time) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			UID:               types.UID("uid-" + name),
			ResourceVersion:   rv,
			CreationTimestamp: metav1.NewTime(createdAt),
			Labels:            map[string]string{"target": "test-policy"},
			Annotations:       map[string]string{"payload": "trimmed"},
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Spec: corev1.PodSpec{
			NodeName:   "worker-1",
			Containers: []corev1.Container{{Name: "app", Image: "registry.k8s.io/pause:3.9"}},
		},
		Status: corev1.PodStatus{PodIP: ip},
	}
}

func buildPodBookmark(rv string, initialEnd bool) runtime.Object {
	var annotations map[string]string
	if initialEnd {
		annotations = map[string]string{metav1.InitialEventsAnnotationKey: "true"}
	}
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: rv,
			Annotations:     annotations,
		},
	}
}

func readHistogramCount(t *testing.T, h prometheus.Histogram) uint64 {
	t.Helper()
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		t.Fatalf("failed to read histogram: %v", err)
	}
	return m.GetHistogram().GetSampleCount()
}

func TestPodCreationReachabilityWatchListProtobufAndMaxTargets(t *testing.T) {
	var (
		nonWatchCalls atomic.Int32
		probedCount   atomic.Int32
		reqMu         sync.Mutex
		firstQuery    url.Values
		firstAccept   string
		eventsCh      = make(chan watch.Event, 16)
		watchClosed   = make(chan struct{})
		releaseProbes = make(chan struct{})
	)
	var closeOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			nonWatchCalls.Add(1)
			http.Error(w, "unexpected non-watch request", http.StatusInternalServerError)
			return
		}
		if r.URL.Path != "/api/v1/namespaces/target-ns/pods" {
			http.NotFound(w, r)
			return
		}

		reqMu.Lock()
		if firstQuery == nil {
			firstQuery = r.URL.Query()
			firstAccept = r.Header.Get("Accept")
		}
		reqMu.Unlock()

		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		pbCreatedAt := time.Now().Add(-5 * time.Second).Truncate(time.Second)
		enc := newProtobufWatchEncoder(w)
		// Pods streamed before the initial-events-end bookmark are tracked like new pods.
		writeProtobufWatchEvent(t, enc, flusher, watch.Event{
			Type:   watch.Added,
			Object: buildPod("preexisting-pod", "target-ns", "", "9", pbCreatedAt),
		})
		writeProtobufWatchEvent(t, enc, flusher, watch.Event{
			Type:   watch.Bookmark,
			Object: buildPodBookmark("10", true),
		})

		defer closeOnce.Do(func() { close(watchClosed) })
		for {
			select {
			case ev := <-eventsCh:
				writeProtobufWatchEvent(t, enc, flusher, ev)
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer srv.Close()

	cl, err := utils.NewK8sClientFromConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("NewK8sClientFromConfig failed: %v", err)
	}

	var tc *TestClient
	tc = &TestClient{
		BaseTestClientConfig: &utils.BaseTestClientConfig{
			HostConfig: &utils.HostConfig{
				HostNamespace: "host-ns",
				MetricsPort:   9154,
			},
			TargetConfig: &utils.TargetConfig{
				TargetNamespace:     "target-ns",
				TargetLabelSelector: "target=test-policy",
				TargetPort:          80,
				MaxTargets:          3,
			},
			InformerSyncTimeout: 5 * time.Second,
			K8sClient:           cl,
			ProbeFunc: func(*utils.TargetSpec) error {
				probedCount.Add(1)
				select {
				case <-releaseProbes:
					return nil
				case <-tc.StopCh:
					return errors.New("stopped")
				}
			},
		},
	}
	if err := tc.initialize(); err != nil {
		t.Fatalf("initialize() failed: %v", err)
	}
	defer tc.Stop()

	ipAssignedBefore := readHistogramCount(t, metrics.PodIPAddressAssignedLatency)
	reachabilityBefore := readHistogramCount(t, metrics.PodCreationReachabilityLatency)

	if err := tc.measurePodCreation(); err != nil {
		t.Fatalf("measurePodCreation() failed: %v", err)
	}

	reqMu.Lock()
	if firstQuery.Get("sendInitialEvents") != "true" ||
		firstQuery.Get("allowWatchBookmarks") != "true" ||
		firstQuery.Get("resourceVersionMatch") != string(metav1.ResourceVersionMatchNotOlderThan) ||
		firstQuery.Get("labelSelector") != "target=test-policy" {
		t.Fatalf("unexpected WatchList query parameters: %v", firstQuery)
	}
	if !strings.Contains(firstAccept, "application/vnd.kubernetes.protobuf") {
		t.Fatalf("expected protobuf Accept negotiation, got %q", firstAccept)
	}
	reqMu.Unlock()

	createdAt := time.Now().Add(-2 * time.Second).Truncate(time.Second)

	// The pre-existing pod from the initial WatchList and post-sync ADDED pods without PodIP are tracked and trimmed.
	eventsCh <- watch.Event{Type: watch.Added, Object: buildPod("pod-1", "target-ns", "", "12", createdAt)}
	eventsCh <- watch.Event{Type: watch.Added, Object: buildPod("pod-2", "target-ns", "", "13", createdAt)}
	eventsCh <- watch.Event{Type: watch.Added, Object: buildPod("pod-3", "target-ns", "", "14", createdAt)}

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		tc.createdPodsMap.Lock.RLock()
		defer tc.createdPodsMap.Lock.RUnlock()
		return len(tc.createdPodsMap.Mp) == 4, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for 4 ADDED pods to be tracked: %v", err)
	}
	if got := probedCount.Load(); got != 0 {
		t.Fatalf("probedCount before PodIP assignment = %d, want 0", got)
	}

	objs, err := tc.podInformer.GetIndexer().ByIndex(utils.NameIndex, "pod-1")
	if err != nil || len(objs) != 1 {
		t.Fatalf("expected pod-1 in indexer: objs=%v err=%v", objs, err)
	}
	cachedPod := objs[0].(*corev1.Pod)
	if cachedPod.Labels != nil || cachedPod.Annotations != nil || cachedPod.ManagedFields != nil || len(cachedPod.Spec.Containers) != 0 {
		t.Fatalf("expected trimmed Pod in cache, got %+v", cachedPod)
	}

	// Assign PodIPs to all 4 tracked pods; MaxTargets=3 caps probes at 3 and closes the watch while keeping active probes alive.
	eventsCh <- watch.Event{Type: watch.Modified, Object: buildPod("preexisting-pod", "target-ns", "192.0.2.99", "20", createdAt)}
	eventsCh <- watch.Event{Type: watch.Modified, Object: buildPod("pod-1", "target-ns", "192.0.2.1", "21", createdAt)}
	eventsCh <- watch.Event{Type: watch.Modified, Object: buildPod("pod-2", "target-ns", "192.0.2.2", "22", createdAt)}
	eventsCh <- watch.Event{Type: watch.Modified, Object: buildPod("pod-3", "target-ns", "192.0.2.3", "23", createdAt)}

	select {
	case <-watchClosed:
	case <-time.After(5 * time.Second):
		t.Fatal("expected pod watch stream to close once MaxTargets=3 pods had IPs assigned")
	}

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return probedCount.Load() == 3, nil
	})
	if err != nil {
		t.Fatalf("in-flight probes = %d, want 3: %v", probedCount.Load(), err)
	}

	close(releaseProbes)

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return readHistogramCount(t, metrics.PodCreationReachabilityLatency)-reachabilityBefore == 3, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for 3 PodCreationReachabilityLatency samples: %v", err)
	}

	if gotDelta := readHistogramCount(t, metrics.PodIPAddressAssignedLatency) - ipAssignedBefore; gotDelta != 3 {
		t.Fatalf("PodIPAddressAssignedLatency sample delta = %d, want 3", gotDelta)
	}
	if got := probedCount.Load(); got != 3 {
		t.Fatalf("total probes = %d, want 3", got)
	}
	if got := nonWatchCalls.Load(); got != 0 {
		t.Fatalf("non-watch calls = %d, want 0", got)
	}
}

// TestPodCreationKeepsMeasuringAfterSyncTimeout checks that a sync timeout does
// not stop the measurement: the informer and workers keep running, so pods that
// get an IP after a late sync still produce samples.
func TestPodCreationKeepsMeasuringAfterSyncTimeout(t *testing.T) {
	eventsCh := make(chan watch.Event, 4)
	releaseBookmark := make(chan struct{})
	probed := make(chan struct{})
	var probeOnce sync.Once

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("watch") != "true" {
			http.Error(w, "unexpected non-watch request", http.StatusInternalServerError)
			return
		}
		flusher := w.(http.Flusher)
		w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		enc := newProtobufWatchEncoder(w)

		select {
		case <-releaseBookmark:
		case <-r.Context().Done():
			return
		}
		writeProtobufWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: buildPodBookmark("10", true)})
		for {
			select {
			case ev := <-eventsCh:
				writeProtobufWatchEvent(t, enc, flusher, ev)
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer srv.Close()

	cl, err := utils.NewK8sClientFromConfig(&rest.Config{Host: srv.URL})
	if err != nil {
		t.Fatalf("NewK8sClientFromConfig failed: %v", err)
	}
	tc := &TestClient{
		BaseTestClientConfig: &utils.BaseTestClientConfig{
			HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
			TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
			InformerSyncTimeout: 100 * time.Millisecond,
			K8sClient:           cl,
			ProbeFunc: func(*utils.TargetSpec) error {
				probeOnce.Do(func() { close(probed) })
				return nil
			},
		},
	}
	if err := tc.initialize(); err != nil {
		t.Fatalf("initialize() failed: %v", err)
	}
	defer tc.Stop()

	reachabilityBefore := readHistogramCount(t, metrics.PodCreationReachabilityLatency)

	if err := tc.measurePodCreation(); err == nil {
		t.Fatal("measurePodCreation() succeeded, want a sync timeout error")
	}

	close(releaseBookmark)
	createdAt := time.Now().Add(-2 * time.Second).Truncate(time.Second)
	eventsCh <- watch.Event{Type: watch.Added, Object: buildPod("pod-1", "target-ns", "", "11", createdAt)}
	eventsCh <- watch.Event{Type: watch.Modified, Object: buildPod("pod-1", "target-ns", "192.0.2.1", "12", createdAt)}

	select {
	case <-probed:
	case <-time.After(5 * time.Second):
		t.Fatal("pod that got an IP after a late sync was never probed")
	}

	err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
		return readHistogramCount(t, metrics.PodCreationReachabilityLatency)-reachabilityBefore == 1, nil
	})
	if err != nil {
		t.Fatalf("timed out waiting for PodCreationReachabilityLatency sample after late sync: %v", err)
	}
}

func TestPodCreationInitialWatchListReachesMaxTargets(t *testing.T) {
	cases := []struct {
		name        string
		initialPods int
		maxTargets  int
	}{
		{name: "InitialPodsExceedMaxTargets", initialPods: 20, maxTargets: 3},
		{name: "InitialPodsEqualMaxTargets", initialPods: 3, maxTargets: 3},
	}
	for _, tcSpec := range cases {
		t.Run(tcSpec.name, func(t *testing.T) {
			var (
				nonWatchCalls atomic.Int32
				probedCount   atomic.Int32
				watchClosed   = make(chan struct{})
				closeOnce     sync.Once
			)
			createdAt := time.Now().Add(-3 * time.Second).Truncate(time.Second)

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("watch") != "true" {
					nonWatchCalls.Add(1)
					http.Error(w, "unexpected non-watch request", http.StatusInternalServerError)
					return
				}
				flusher := w.(http.Flusher)
				w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
				w.WriteHeader(http.StatusOK)
				flusher.Flush()
				defer closeOnce.Do(func() { close(watchClosed) })

				enc := newProtobufWatchEncoder(w)
				for i := 0; i < tcSpec.initialPods; i++ {
					name := "pod-" + strconv.Itoa(i)
					ip := "10.0.0." + strconv.Itoa(i+1)
					rv := strconv.Itoa(i + 1)
					writeProtobufWatchEvent(t, enc, flusher, watch.Event{
						Type:   watch.Added,
						Object: buildPod(name, "target-ns", ip, rv, createdAt),
					})
				}
				writeProtobufWatchEvent(t, enc, flusher, watch.Event{
					Type:   watch.Bookmark,
					Object: buildPodBookmark(strconv.Itoa(tcSpec.initialPods+1), true),
				})
				<-r.Context().Done()
			}))
			defer srv.Close()

			cl, err := utils.NewK8sClientFromConfig(&rest.Config{Host: srv.URL})
			if err != nil {
				t.Fatalf("NewK8sClientFromConfig failed: %v", err)
			}

			tc := &TestClient{
				BaseTestClientConfig: &utils.BaseTestClientConfig{
					HostConfig: &utils.HostConfig{
						HostNamespace: "host-ns",
						MetricsPort:   9154,
					},
					TargetConfig: &utils.TargetConfig{
						TargetNamespace:     "target-ns",
						TargetLabelSelector: "target=test-policy",
						TargetPort:          80,
						MaxTargets:          tcSpec.maxTargets,
					},
					InformerSyncTimeout: 5 * time.Second,
					K8sClient:           cl,
					ProbeFunc: func(*utils.TargetSpec) error {
						probedCount.Add(1)
						return nil
					},
				},
			}
			if err := tc.initialize(); err != nil {
				t.Fatalf("initialize() failed: %v", err)
			}
			defer tc.Stop()

			ipAssignedBefore := readHistogramCount(t, metrics.PodIPAddressAssignedLatency)
			reachabilityBefore := readHistogramCount(t, metrics.PodCreationReachabilityLatency)

			if err := tc.measurePodCreation(); err != nil {
				t.Fatalf("measurePodCreation() failed: %v", err)
			}

			select {
			case <-watchClosed:
			case <-time.After(5 * time.Second):
				t.Fatalf("expected pod watch stream to close once MaxTargets=%d was reached", tcSpec.maxTargets)
			}

			err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
				return readHistogramCount(t, metrics.PodCreationReachabilityLatency)-reachabilityBefore == uint64(tcSpec.maxTargets), nil
			})
			if err != nil {
				t.Fatalf("timed out waiting for %d PodCreationReachabilityLatency samples: %v", tcSpec.maxTargets, err)
			}

			if gotDelta := readHistogramCount(t, metrics.PodIPAddressAssignedLatency) - ipAssignedBefore; gotDelta != uint64(tcSpec.maxTargets) {
				t.Fatalf("PodIPAddressAssignedLatency sample delta = %d, want %d", gotDelta, tcSpec.maxTargets)
			}
			if got := probedCount.Load(); got != int32(tcSpec.maxTargets) {
				t.Fatalf("total probes = %d, want %d", got, tcSpec.maxTargets)
			}
			if got := nonWatchCalls.Load(); got != 0 {
				t.Fatalf("non-watch calls = %d, want 0", got)
			}
		})
	}
}
