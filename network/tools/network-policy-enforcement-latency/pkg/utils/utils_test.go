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

package utils

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
)

func TestTrimPod(t *testing.T) {
	createdAt := metav1.NewTime(time.Unix(1700000000, 0).UTC())
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "target-pod-1",
			Namespace:         "target-ns",
			UID:               types.UID("uid-1"),
			ResourceVersion:   "42",
			CreationTimestamp: createdAt,
			Labels:            map[string]string{"app": "target"},
			Annotations:       map[string]string{"note": "strip"},
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "kubelet"}},
		},
		Spec: corev1.PodSpec{
			NodeName:   "node-1",
			Containers: []corev1.Container{{Name: "c", Image: "registry.k8s.io/pause:3.9"}},
		},
		Status: corev1.PodStatus{
			PodIP: "192.0.2.12",
			Conditions: []corev1.PodCondition{
				{Type: corev1.PodReady, Status: corev1.ConditionTrue},
			},
		},
	}

	out, err := TrimPod(pod)
	if err != nil {
		t.Fatalf("TrimPod returned error: %v", err)
	}
	trimmed, ok := out.(*corev1.Pod)
	if !ok {
		t.Fatalf("TrimPod returned %T, want *corev1.Pod", out)
	}
	if trimmed.Name != "target-pod-1" || trimmed.Namespace != "target-ns" || trimmed.UID != "uid-1" || trimmed.ResourceVersion != "42" || !trimmed.CreationTimestamp.Equal(&createdAt) {
		t.Fatalf("TrimPod lost required metadata: %+v", trimmed.ObjectMeta)
	}
	if trimmed.Status.PodIP != "192.0.2.12" {
		t.Fatalf("TrimPod lost PodIP: got %q, want %q", trimmed.Status.PodIP, "192.0.2.12")
	}
	if trimmed.Labels != nil || trimmed.Annotations != nil || trimmed.ManagedFields != nil || len(trimmed.Spec.Containers) != 0 || len(trimmed.Status.Conditions) != 0 {
		t.Fatalf("TrimPod retained stripped fields: %+v", trimmed)
	}

	out2, err := TrimPod(trimmed)
	if err != nil || out2.(*corev1.Pod).Status.PodIP != "192.0.2.12" {
		t.Fatalf("TrimPod is not idempotent: out=%+v err=%v", out2, err)
	}
	passthrough, err := TrimPod("tombstone")
	if err != nil || passthrough != "tombstone" {
		t.Fatalf("TrimPod altered non-Pod object: got %v err=%v", passthrough, err)
	}
}

func TestTrimNetworkPolicy(t *testing.T) {
	createdAt := metav1.NewTime(time.Unix(1700000100, 0).UTC())
	np := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "allow-egress",
			Namespace:         "host-ns",
			UID:               types.UID("np-uid-1"),
			ResourceVersion:   "99",
			CreationTimestamp: createdAt,
			Labels:            map[string]string{"role": "policy"},
			Annotations:       map[string]string{"note": "strip"},
			ManagedFields:     []metav1.ManagedFieldsEntry{{Manager: "controller"}},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PolicyTypes: []networkingv1.PolicyType{networkingv1.PolicyTypeEgress},
			Egress:      []networkingv1.NetworkPolicyEgressRule{{}},
		},
	}

	out, err := TrimNetworkPolicy(np)
	if err != nil {
		t.Fatalf("TrimNetworkPolicy returned error: %v", err)
	}
	trimmed, ok := out.(*networkingv1.NetworkPolicy)
	if !ok {
		t.Fatalf("TrimNetworkPolicy returned %T, want *networkingv1.NetworkPolicy", out)
	}
	if trimmed.Name != "allow-egress" || trimmed.Namespace != "host-ns" || trimmed.UID != "np-uid-1" || trimmed.ResourceVersion != "99" || !trimmed.CreationTimestamp.Equal(&createdAt) {
		t.Fatalf("TrimNetworkPolicy lost required metadata: %+v", trimmed.ObjectMeta)
	}
	if trimmed.Labels != nil || trimmed.Annotations != nil || trimmed.ManagedFields != nil || len(trimmed.Spec.Egress) != 0 || len(trimmed.Spec.PolicyTypes) != 0 {
		t.Fatalf("TrimNetworkPolicy retained stripped fields: %+v", trimmed)
	}

	passthrough, err := TrimNetworkPolicy("tombstone")
	if err != nil || passthrough != "tombstone" {
		t.Fatalf("TrimNetworkPolicy altered non-NetworkPolicy object: got %v err=%v", passthrough, err)
	}
}

func TestStopChannelBroadcastAndWaitUntilStopped(t *testing.T) {
	sigCh := make(chan os.Signal, 1)
	cfg := &BaseTestClientConfig{MainStopChan: sigCh}
	cfg.InitStopChannel()

	const listeners = 8
	var readyWg sync.WaitGroup
	var doneWg sync.WaitGroup
	readyWg.Add(listeners + 1)
	doneWg.Add(listeners + 1)

	for i := 0; i < listeners; i++ {
		go func() {
			readyWg.Done()
			<-cfg.StopCh
			doneWg.Done()
		}()
	}
	go func() {
		readyWg.Done()
		cfg.WaitUntilStopped()
		doneWg.Done()
	}()

	readyWg.Wait()
	sigCh <- syscall.SIGTERM

	done := make(chan struct{})
	go func() {
		doneWg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for StopCh broadcast and WaitUntilStopped to return")
	}

	cfg.Stop()
}

func TestRecordFirstSuccessfulRequestUntilStop(t *testing.T) {
	t.Run("SkipsNilAndEmptyIP", func(t *testing.T) {
		stopCh := make(chan struct{})
		called := false
		RecordFirstSuccessfulRequestUntilStop(
			&TargetSpec{Name: "no-ip", Port: 80},
			stopCh,
			func(*TargetSpec) error {
				called = true
				return nil
			},
			func(*TargetSpec, time.Time) {
				called = true
			},
		)
		if called {
			t.Fatal("expected empty-IP target to be skipped")
		}
	})

	t.Run("RetriesUntilFirstSuccess", func(t *testing.T) {
		stopCh := make(chan struct{})
		var attempts atomic.Int32
		var successCalls atomic.Int32
		RecordFirstSuccessfulRequestUntilStop(
			&TargetSpec{Name: "pod-1", IP: "192.0.2.1", Port: 80},
			stopCh,
			func(*TargetSpec) error {
				if attempts.Add(1) < 3 {
					return errors.New("denied")
				}
				return nil
			},
			func(*TargetSpec, time.Time) {
				successCalls.Add(1)
			},
		)
		if got := attempts.Load(); got != 3 {
			t.Fatalf("probe attempts = %d, want 3", got)
		}
		if got := successCalls.Load(); got != 1 {
			t.Fatalf("successFunc calls = %d, want 1", got)
		}
	})

	t.Run("ExitsWhenStopChannelCloses", func(t *testing.T) {
		stopCh := make(chan struct{})
		entered := make(chan struct{})
		var enterOnce sync.Once
		done := make(chan struct{})
		successCalled := false

		go func() {
			defer close(done)
			RecordFirstSuccessfulRequestUntilStop(
				&TargetSpec{Name: "pod-1", IP: "192.0.2.1", Port: 80},
				stopCh,
				func(*TargetSpec) error {
					enterOnce.Do(func() { close(entered) })
					<-stopCh
					return errors.New("canceled")
				},
				func(*TargetSpec, time.Time) {
					successCalled = true
				},
			)
		}()

		<-entered
		close(stopCh)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for probe loop to exit after stopCh closed")
		}
		if successCalled {
			t.Fatal("unexpected successFunc call after stopCh closed")
		}
	})
}

func TestNewK8sClientFromConfigNegotiatesProtobuf(t *testing.T) {
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","code":404}`))
	}))
	defer srv.Close()

	orig := &rest.Config{Host: srv.URL}
	cl, err := NewK8sClientFromConfig(orig)
	if err != nil {
		t.Fatalf("NewK8sClientFromConfig failed: %v", err)
	}
	if orig.AcceptContentTypes != "" || orig.ContentType != "" {
		t.Fatalf("NewK8sClientFromConfig mutated input rest.Config: %+v", orig.ContentConfig)
	}

	_, _ = cl.CoreV1().Pods("default").Get(t.Context(), "missing", metav1.GetOptions{})
	if !strings.Contains(gotAccept, protobufMediaType) || !strings.Contains(gotAccept, "application/json") {
		t.Fatalf("Accept header = %q, want protobuf with json fallback", gotAccept)
	}
}

func TestCurlCommand(t *testing.T) {
	cases := []struct {
		ip       string
		port     int
		expected []string
	}{
		{"192.0.2.1", 80, []string{"curl", "-g", "--connect-timeout", "1", "192.0.2.1:80"}},
		{"2001:db8::1", 8080, []string{"curl", "-g", "--connect-timeout", "1", "[2001:db8::1]:8080"}},
	}
	for _, c := range cases {
		if got := curlCommand(c.ip, c.port); !slices.Equal(got, c.expected) {
			t.Errorf("curlCommand(%q, %d) = %q, expected %q", c.ip, c.port, got, c.expected)
		}
	}
}

// TestNewCurlProbeIgnoresShellGlobs runs the real curl probe from a directory
// holding a file that a shell would match against an unquoted "[2001:db8::1]:<port>".
func TestNewCurlProbeIgnoresShellGlobs(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not installed")
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ln, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback not available: %v", err)
	}
	srv.Listener = ln
	srv.Start()
	defer srv.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	t.Chdir(t.TempDir())
	if err := os.WriteFile(fmt.Sprintf("1:%d", port), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	target := &TargetSpec{Name: "pod-1", IP: "::1", Port: port}
	if err := NewCurlProbe(target)(target); err != nil {
		t.Fatalf("curl probe to [::1]:%d failed: %v", port, err)
	}
}
