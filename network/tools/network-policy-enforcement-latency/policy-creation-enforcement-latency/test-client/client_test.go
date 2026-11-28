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
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclientwatch "k8s.io/client-go/rest/watch"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/metrics"
	"k8s.io/perf-tests/network/tools/network-policy-enforcement-latency/pkg/utils"
)

type recordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Accept string
}

func newProtobufWatchEncoder(w http.ResponseWriter) *restclientwatch.Encoder {
	pbSerializer := protobuf.NewSerializer(scheme.Scheme, scheme.Scheme)
	rawPBSerializer := protobuf.NewRawSerializer(scheme.Scheme, scheme.Scheme)
	framer := protobuf.LengthDelimitedFramer.NewFrameWriter(w)
	return restclientwatch.NewEncoder(streaming.NewEncoder(framer, rawPBSerializer), pbSerializer)
}

func writeWatchEvent(t *testing.T, enc *restclientwatch.Encoder, flusher http.Flusher, ev watch.Event) {
	t.Helper()
	if err := enc.Encode(&ev); err != nil {
		return
	}
	flusher.Flush()
}

func makePodObj(name, ns, ip, rv string, createdAt time.Time) *corev1.Pod {
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			UID:               types.UID("uid-" + name),
			ResourceVersion:   rv,
			CreationTimestamp: metav1.NewTime(createdAt),
			Labels:            map[string]string{"target": "test-policy"},
			Annotations:       map[string]string{"large": "annotation"},
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Status: corev1.PodStatus{PodIP: ip},
	}
}

func makePodBookmark(rv string, initialEnd bool) *corev1.Pod {
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

func makePolicyObj(name, ns, rv string, createdAt time.Time) *networkingv1.NetworkPolicy {
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         ns,
			UID:               types.UID("uid-" + name),
			ResourceVersion:   rv,
			CreationTimestamp: metav1.NewTime(createdAt),
			Annotations:       map[string]string{"large": "annotation"},
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "controller"}},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
		},
	}
}

func makePolicyBookmark(rv string, initialEnd bool) *networkingv1.NetworkPolicy {
	var annotations map[string]string
	if initialEnd {
		annotations = map[string]string{metav1.InitialEventsAnnotationKey: "true"}
	}
	return &networkingv1.NetworkPolicy{
		TypeMeta: metav1.TypeMeta{APIVersion: "networking.k8s.io/v1", Kind: "NetworkPolicy"},
		ObjectMeta: metav1.ObjectMeta{
			ResourceVersion: rv,
			Annotations:     annotations,
		},
	}
}

func newTestK8sClient(t *testing.T, serverURL string) *clientset.Clientset {
	t.Helper()
	cl, err := utils.NewK8sClientFromConfig(&rest.Config{Host: serverURL})
	if err != nil {
		t.Fatalf("NewK8sClientFromConfig failed: %v", err)
	}
	return cl
}

func histogramStats(t *testing.T, h prometheus.Histogram) (uint64, float64) {
	t.Helper()
	var m dto.Metric
	if err := h.Write(&m); err != nil {
		t.Fatalf("failed to read histogram metric: %v", err)
	}
	return m.GetHistogram().GetSampleCount(), m.GetHistogram().GetSampleSum()
}

func assertWatchListQuery(t *testing.T, req recordedRequest, wantLabelSelector, wantFieldSelector string) {
	t.Helper()
	if req.Method != http.MethodGet {
		t.Errorf("unexpected method %q for %s", req.Method, req.Path)
	}
	if req.Query.Get("watch") != "true" {
		t.Errorf("expected watch=true on %s, got query %v", req.Path, req.Query)
	}
	if req.Query.Get("sendInitialEvents") != "true" {
		t.Errorf("expected sendInitialEvents=true on %s, got query %v", req.Path, req.Query)
	}
	if req.Query.Get("allowWatchBookmarks") != "true" {
		t.Errorf("expected allowWatchBookmarks=true on %s, got query %v", req.Path, req.Query)
	}
	if req.Query.Get("resourceVersionMatch") != string(metav1.ResourceVersionMatchNotOlderThan) {
		t.Errorf("expected resourceVersionMatch=NotOlderThan on %s, got query %v", req.Path, req.Query)
	}
	if req.Query.Get("labelSelector") != wantLabelSelector {
		t.Errorf("labelSelector on %s = %q, want %q", req.Path, req.Query.Get("labelSelector"), wantLabelSelector)
	}
	if req.Query.Get("fieldSelector") != wantFieldSelector {
		t.Errorf("fieldSelector on %s = %q, want %q", req.Path, req.Query.Get("fieldSelector"), wantFieldSelector)
	}
	if !strings.Contains(req.Accept, "application/vnd.kubernetes.protobuf") {
		t.Errorf("expected Accept header to negotiate protobuf on %s, got %q", req.Path, req.Accept)
	}
}

func TestPolicyCreationEnforcementLatencyWatchList(t *testing.T) {
	t.Run("WatchListProtobufSyncMaxTargetsBookmarkReconnectAndShutdown", func(t *testing.T) {
		var (
			reqMu          sync.Mutex
			requests       = make([]recordedRequest, 0, 4)
			nonWatchCalls  atomic.Int32
			probedCount    atomic.Int32
			probedNamesMu  sync.Mutex
			probedNames    = make(map[string]int, 3)
			podWatchOpened = make(chan struct{})
			npWatchOpened  = make(chan struct{})
			releaseNPInit  = make(chan struct{})
			npEventsCh     = make(chan watch.Event, 8)
			closeNPFirst   = make(chan struct{})
			npReconnectReq = make(chan recordedRequest, 1)
			handlersDoneWg sync.WaitGroup
		)

		var podWatchOnce, npWatchOnce sync.Once
		var npConnCount atomic.Int32

		podCreatedAt := time.Now().Add(-10 * time.Second).Truncate(time.Second)
		policyCreatedAt := time.Now().Add(-2 * time.Second).Truncate(time.Second)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlersDoneWg.Add(1)
			defer handlersDoneWg.Done()

			rec := recordedRequest{
				Method: r.Method,
				Path:   r.URL.Path,
				Query:  r.URL.Query(),
				Accept: r.Header.Get("Accept"),
			}
			reqMu.Lock()
			requests = append(requests, rec)
			reqMu.Unlock()

			if rec.Query.Get("watch") != "true" {
				nonWatchCalls.Add(1)
				http.Error(w, fmt.Sprintf("unexpected non-watch request: %s?%s", r.URL.Path, r.URL.RawQuery), http.StatusInternalServerError)
				return
			}

			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				// Unsorted stream: sorted first 3 are pod-01 (IP), pod-02 (empty IP -> skipped), pod-03 (IP); pod-04 exceeds MaxTargets=3.
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-03", "target-ns", "192.0.2.3", "11", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-01", "target-ns", "192.0.2.1", "12", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-02", "target-ns", "", "13", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-04", "target-ns", "192.0.2.4", "14", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("100", true)})
				podWatchOnce.Do(func() { close(podWatchOpened) })
				<-r.Context().Done()

			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				if npConnCount.Add(1) == 1 {
					npWatchOnce.Do(func() { close(npWatchOpened) })
					select {
					case <-releaseNPInit:
					case <-r.Context().Done():
						return
					}
					writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("200", true)})
					for {
						select {
						case ev := <-npEventsCh:
							writeWatchEvent(t, enc, flusher, ev)
						case <-closeNPFirst:
							return
						case <-r.Context().Done():
							return
						}
					}
				}
				select {
				case npReconnectReq <- rec:
				default:
				}
				<-r.Context().Done()

			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		sigCh := make(chan os.Signal, 1)
		policySyncedInCache := make(chan struct{})
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
				MainStopChan:        sigCh,
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
				ProbeFunc: func(target *utils.TargetSpec) error {
					probedCount.Add(1)
					probedNamesMu.Lock()
					probedNames[target.Name]++
					probedNamesMu.Unlock()

					// Deny until the allow policy is in the cache, as the dataplane would.
					select {
					case <-policySyncedInCache:
						return nil
					case <-tc.StopCh:
						return errors.New("stopped")
					case <-time.After(5 * time.Millisecond):
						return errors.New("denied")
					}
				},
			},
			allowPolicyName: "allow-egress",
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		countBefore, sumBefore := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)

		measureErrCh := make(chan error, 1)
		go func() {
			measureErrCh <- tc.measureNetPolicyCreation()
		}()

		<-podWatchOpened
		<-npWatchOpened

		if got := probedCount.Load(); got != 0 {
			t.Fatalf("probes started before NetworkPolicy WatchList bookmark sync: got %d, want 0", got)
		}

		close(releaseNPInit)

		err := wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
			probedNamesMu.Lock()
			defer probedNamesMu.Unlock()
			return probedNames["pod-01"] >= 2 && probedNames["pod-03"] >= 2, nil
		})
		if err != nil {
			t.Fatalf("timed out waiting for pod-01 and pod-03 probes: %v", err)
		}

		cachedPodObj, exists, err := tc.podInformer.GetStore().GetByKey("target-ns/pod-01")
		if err != nil || !exists {
			t.Fatalf("expected target-ns/pod-01 in podInformer cache: exists=%v err=%v", exists, err)
		}
		cachedPod := cachedPodObj.(*corev1.Pod)
		if cachedPod.Labels != nil || cachedPod.Annotations != nil || cachedPod.ManagedFields != nil || cachedPod.Status.PodIP != "192.0.2.1" {
			t.Fatalf("expected trimmed Pod in cache, got %+v", cachedPod)
		}

		npEventsCh <- watch.Event{
			Type:   watch.Added,
			Object: makePolicyObj("allow-egress", "host-ns", "205", policyCreatedAt),
		}
		select {
		case <-tc.allowPolicyReadyCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for allowPolicyReadyCh to close on ADDED event")
		}

		// Advance bookmark RV to 250, drop the first NetworkPolicy watch, and verify reconnect resumes at RV=250 without LIST.
		npEventsCh <- watch.Event{
			Type:   watch.Bookmark,
			Object: makePolicyBookmark("250", false),
		}
		err = wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
			return tc.policyInformer.LastSyncResourceVersion() == "250", nil
		})
		if err != nil {
			t.Fatalf("LastSyncResourceVersion = %q, want 250: %v", tc.policyInformer.LastSyncResourceVersion(), err)
		}

		close(closeNPFirst)

		var reconnectReq recordedRequest
		select {
		case reconnectReq = <-npReconnectReq:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for NetworkPolicy watch reconnect")
		}
		if reconnectReq.Query.Get("watch") != "true" || reconnectReq.Query.Get("resourceVersion") != "250" || reconnectReq.Query.Get("sendInitialEvents") != "" {
			t.Fatalf("unexpected reconnect query: %v", reconnectReq.Query)
		}

		close(policySyncedInCache)

		select {
		case err := <-measureErrCh:
			if err != nil {
				t.Fatalf("measureNetPolicyCreation() failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for measureNetPolicyCreation() to finish")
		}

		countAfter, sumAfter := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)
		if gotDelta := countAfter - countBefore; gotDelta != 2 {
			t.Fatalf("PolicyEnforceLatencyPolicyCreation sample delta = %d, want 2", gotDelta)
		}
		if sumDelta := sumAfter - sumBefore; sumDelta < 4.0 {
			t.Fatalf("PolicyEnforceLatencyPolicyCreation sample sum delta = %v, want >= 4.0s (2 pods * >= 2s)", sumDelta)
		}

		probedNamesMu.Lock()
		if _, hasPod02 := probedNames["pod-02"]; hasPod02 {
			t.Errorf("empty-IP pod-02 must not be probed: %v", probedNames)
		}
		if _, hasPod04 := probedNames["pod-04"]; hasPod04 {
			t.Errorf("pod-04 beyond MaxTargets=3 must not be probed: %v", probedNames)
		}
		probedNamesMu.Unlock()

		// Both watches must close once the measurement finishes, while the client keeps running for metrics scraping.
		watchesClosedCh := make(chan struct{})
		go func() {
			handlersDoneWg.Wait()
			close(watchesClosedCh)
		}()
		select {
		case <-watchesClosedCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for watch streams to close after the measurement finished")
		}
		if !utils.ChannelIsOpen(tc.StopCh) {
			t.Fatal("StopCh closed when the measurement finished, want it open until SIGTERM")
		}

		sigCh <- syscall.SIGTERM
		select {
		case <-tc.StopCh:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for StopCh to close after SIGTERM")
		}

		if got := nonWatchCalls.Load(); got != 0 {
			t.Fatalf("non-watch calls = %d, want 0", got)
		}

		reqMu.Lock()
		defer reqMu.Unlock()
		var sawPodWatchInit, sawNPWatchInit bool
		for _, r := range requests {
			switch r.Path {
			case "/api/v1/namespaces/target-ns/pods":
				sawPodWatchInit = true
				assertWatchListQuery(t, r, "target=test-policy", "")
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				if !sawNPWatchInit {
					sawNPWatchInit = true
					assertWatchListQuery(t, r, "", "metadata.name=allow-egress")
				}
			}
		}
		if !sawPodWatchInit || !sawNPWatchInit {
			t.Fatalf("missing initial WatchList requests: sawPod=%v sawNP=%v", sawPodWatchInit, sawNPWatchInit)
		}
	})

	t.Run("PreservesSampleWhenProbeSucceedsBeforeAllowPolicyWatchEvent", func(t *testing.T) {
		var nonWatchCalls, probeAttempts atomic.Int32
		npEventsCh := make(chan watch.Event, 2)
		probeSucceeded := make(chan struct{})
		var probeOnce sync.Once

		podCreatedAt := time.Now().Add(-10 * time.Second).Truncate(time.Second)
		policyCreatedAt := time.Now().Add(-3 * time.Second).Truncate(time.Second)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("watch") != "true" {
				nonWatchCalls.Add(1)
				http.Error(w, "unexpected non-watch call", http.StatusInternalServerError)
				return
			}
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-01", "target-ns", "192.0.2.1", "10", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("10", true)})
				<-r.Context().Done()
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("20", true)})
				for {
					select {
					case ev := <-npEventsCh:
						writeWatchEvent(t, enc, flusher, ev)
					case <-r.Context().Done():
						return
					}
				}
			}
		}))
		defer srv.Close()

		tc := &TestClient{
			BaseTestClientConfig: &utils.BaseTestClientConfig{
				HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
				TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
				ProbeFunc: func(*utils.TargetSpec) error {
					if probeAttempts.Add(1) == 1 {
						return errors.New("denied")
					}
					probeOnce.Do(func() { close(probeSucceeded) })
					return nil
				},
			},
			allowPolicyName:        "allow-egress",
			allowPolicyWaitTimeout: 5 * time.Second,
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		countBefore, sumBefore := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)

		doneCh := make(chan error, 1)
		go func() {
			doneCh <- tc.measureNetPolicyCreation()
		}()

		// Deliver ADDED allow-egress strictly after ProbeFunc has already succeeded and returned.
		<-probeSucceeded
		npEventsCh <- watch.Event{
			Type:   watch.Added,
			Object: makePolicyObj("allow-egress", "host-ns", "21", policyCreatedAt),
		}

		select {
		case err := <-doneCh:
			if err != nil {
				t.Fatalf("measureNetPolicyCreation() failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for measureNetPolicyCreation() to finish after delayed allow policy event")
		}

		countAfter, sumAfter := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)
		if gotDelta := countAfter - countBefore; gotDelta != 1 {
			t.Fatalf("sample count delta = %d, want 1", gotDelta)
		}
		if sumDelta := sumAfter - sumBefore; sumDelta < 3.0 {
			t.Fatalf("sample sum delta = %v, want >= 3.0s", sumDelta)
		}
		if got := nonWatchCalls.Load(); got != 0 {
			t.Fatalf("non-watch calls = %d, want 0", got)
		}
	})

	t.Run("DropsSampleWithoutGetWhenAllowPolicyNeverArrives", func(t *testing.T) {
		var nonWatchCalls, probeAttempts atomic.Int32
		podCreatedAt := time.Now().Add(-5 * time.Second).Truncate(time.Second)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("watch") != "true" {
				nonWatchCalls.Add(1)
				http.Error(w, "unexpected non-watch call", http.StatusInternalServerError)
				return
			}
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-01", "target-ns", "192.0.2.1", "10", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("10", true)})
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("20", true)})
			}
			<-r.Context().Done()
		}))
		defer srv.Close()

		tc := &TestClient{
			BaseTestClientConfig: &utils.BaseTestClientConfig{
				HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
				TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
				ProbeFunc: func(*utils.TargetSpec) error {
					if probeAttempts.Add(1) == 1 {
						return errors.New("denied")
					}
					return nil
				},
			},
			allowPolicyName:        "allow-egress",
			allowPolicyWaitTimeout: 25 * time.Millisecond,
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		countBefore, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)
		if err := tc.measureNetPolicyCreation(); err != nil {
			t.Fatalf("measureNetPolicyCreation() failed: %v", err)
		}
		countAfter, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)

		if countAfter != countBefore {
			t.Fatalf("expected ungrounded sample to be dropped, got delta %d", countAfter-countBefore)
		}
		if got := nonWatchCalls.Load(); got != 0 {
			t.Fatalf("non-watch calls = %d, want 0", got)
		}
	})

	t.Run("ReturnsErrorWhenZeroTargetPodsMatched", func(t *testing.T) {
		var activeWatches atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("watch") != "true" {
				http.Error(w, "unexpected non-watch call", http.StatusInternalServerError)
				return
			}
			activeWatches.Add(1)
			defer activeWatches.Add(-1)
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("10", true)})
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("20", true)})
			}
			<-r.Context().Done()
		}))
		defer srv.Close()

		tc := &TestClient{
			BaseTestClientConfig: &utils.BaseTestClientConfig{
				HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
				TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
			},
			allowPolicyName: "allow-egress",
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		if err := tc.measureNetPolicyCreation(); err == nil {
			t.Fatal("expected error when zero target pods exist after WatchList sync, got nil")
		}
		err := wait.PollUntilContextTimeout(context.Background(), 5*time.Millisecond, 5*time.Second, true, func(context.Context) (bool, error) {
			return activeWatches.Load() == 0, nil
		})
		if err != nil {
			t.Fatalf("watch streams still open after measureNetPolicyCreation() failed: %d", activeWatches.Load())
		}
	})

	t.Run("DropsSampleWhenFirstProbeSucceeds", func(t *testing.T) {
		podCreatedAt := time.Now().Add(-10 * time.Second).Truncate(time.Second)
		policyCreatedAt := time.Now().Add(-5 * time.Second).Truncate(time.Second)

		// The allow policy already exists, so traffic is never denied.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-01", "target-ns", "192.0.2.1", "10", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("10", true)})
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePolicyObj("allow-egress", "host-ns", "19", policyCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("20", true)})
			}
			<-r.Context().Done()
		}))
		defer srv.Close()

		tc := &TestClient{
			BaseTestClientConfig: &utils.BaseTestClientConfig{
				HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
				TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
				ProbeFunc: func(*utils.TargetSpec) error {
					return nil
				},
			},
			allowPolicyName: "allow-egress",
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		countBefore, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)
		if err := tc.measureNetPolicyCreation(); err != nil {
			t.Fatalf("measureNetPolicyCreation() failed: %v", err)
		}
		if countAfter, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation); countAfter != countBefore {
			t.Fatalf("expected sample without a denied probe to be dropped, got delta %d", countAfter-countBefore)
		}
	})

	t.Run("DropsSampleReachedBeforeAllowPolicyCreation", func(t *testing.T) {
		var probeAttempts atomic.Int32
		podCreatedAt := time.Now().Add(-10 * time.Second).Truncate(time.Second)
		// A creation timestamp after the probe success means the target was reachable before the allow policy existed.
		policyCreatedAt := time.Now().Add(5 * time.Second).Truncate(time.Second)
		probeSucceeded := make(chan struct{})
		var probeOnce sync.Once
		npEventsCh := make(chan watch.Event, 1)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			flusher := w.(http.Flusher)
			w.Header().Set("Content-Type", "application/vnd.kubernetes.protobuf;stream=watch")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()
			enc := newProtobufWatchEncoder(w)

			switch r.URL.Path {
			case "/api/v1/namespaces/target-ns/pods":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Added, Object: makePodObj("pod-01", "target-ns", "192.0.2.1", "10", podCreatedAt)})
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePodBookmark("10", true)})
			case "/apis/networking.k8s.io/v1/namespaces/host-ns/networkpolicies":
				writeWatchEvent(t, enc, flusher, watch.Event{Type: watch.Bookmark, Object: makePolicyBookmark("20", true)})
				select {
				case ev := <-npEventsCh:
					writeWatchEvent(t, enc, flusher, ev)
				case <-r.Context().Done():
					return
				}
			}
			<-r.Context().Done()
		}))
		defer srv.Close()

		tc := &TestClient{
			BaseTestClientConfig: &utils.BaseTestClientConfig{
				HostConfig:          &utils.HostConfig{HostNamespace: "host-ns", MetricsPort: 9154},
				TargetConfig:        &utils.TargetConfig{TargetNamespace: "target-ns", TargetLabelSelector: "target=test-policy", TargetPort: 80, MaxTargets: 5},
				InformerSyncTimeout: 5 * time.Second,
				K8sClient:           newTestK8sClient(t, srv.URL),
				ProbeFunc: func(*utils.TargetSpec) error {
					if probeAttempts.Add(1) == 1 {
						return errors.New("denied")
					}
					probeOnce.Do(func() { close(probeSucceeded) })
					return nil
				},
			},
			allowPolicyName:        "allow-egress",
			allowPolicyWaitTimeout: 5 * time.Second,
		}
		if err := tc.initialize(); err != nil {
			t.Fatalf("initialize() failed: %v", err)
		}
		defer tc.Stop()

		countBefore, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation)
		doneCh := make(chan error, 1)
		go func() {
			doneCh <- tc.measureNetPolicyCreation()
		}()

		<-probeSucceeded
		npEventsCh <- watch.Event{Type: watch.Added, Object: makePolicyObj("allow-egress", "host-ns", "21", policyCreatedAt)}

		select {
		case err := <-doneCh:
			if err != nil {
				t.Fatalf("measureNetPolicyCreation() failed: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for measureNetPolicyCreation() to finish")
		}
		if countAfter, _ := histogramStats(t, metrics.PolicyEnforceLatencyPolicyCreation); countAfter != countBefore {
			t.Fatalf("expected negative-latency sample to be dropped, got delta %d", countAfter-countBefore)
		}
	})
}
