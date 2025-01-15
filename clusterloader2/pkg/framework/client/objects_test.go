/*
Copyright 2020 The Kubernetes Authors.

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
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
)

func TestIsResourceQuotaError(t *testing.T) {
	testcases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "resource-quota error",
			err:  apierrs.NewConflict(schema.GroupResource{Resource: "resourcequotas"}, "gke-resource-quotas", fmt.Errorf("please apply your changes to the latest version and try again")),
			want: true,
		},
		{
			name: "non-resource-quota api error",
			err:  apierrs.NewBadRequest("XXX"),
			want: false,
		},
		{
			name: "non-api error",
			err:  fmt.Errorf("other resourcequotas error"),
			want: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isResourceQuotaConflictError(tc.err); tc.want != got {
				t.Errorf("want: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestIsRetryableAPIError(t *testing.T) {
	testcases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "conflict resource-quota error",
			err:  apierrs.NewConflict(schema.GroupResource{Resource: "resourcequotas"}, "gke-resource-quotas", fmt.Errorf("please apply your changes to the latest version and try again")),
			want: true,
		},
		{
			name: "conflict non-resource-quota error",
			err:  apierrs.NewConflict(schema.GroupResource{Resource: "pods"}, "my-pod", fmt.Errorf("please apply your changes to the latest version and try again")),
			want: false,
		},
		{
			name: "non-conflict resource-quota error",
			err:  apierrs.NewAlreadyExists(schema.GroupResource{Resource: "resourcequotas"}, "gke-resource-quotas"),
			want: false,
		},
		{
			name: "other error",
			err:  fmt.Errorf("XXX"),
			want: false,
		},
		{
			name: "other resourcequotas error",
			err:  fmt.Errorf("other resourcequotas error"),
			want: false,
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryableAPIError(tc.err); tc.want != got {
				t.Errorf("want: %v, got: %v", tc.want, got)
			}
		})
	}
}

func TestIsRetryableNetError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "http2: client connection lost",
			err:  errors.New("http2: client connection lost"),
			want: true,
		},
		{
			name: "http2: some other error",
			err:  errors.New("http2: some other error"),
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsRetryableNetError(tt.err); got != tt.want {
				t.Errorf("IsRetryableNetError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestKindPluralization(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{
			kind: "Endpoints.v1.core",
			want: "endpoints.v1.core",
		},
		{
			kind: "Gateway.v1beta1.gateway.networking.k8s.io",
			want: "gateways.v1beta1.gateway.networking.k8s.io",
		},
		{
			kind: "Deployment.v1.apps",
			want: "deployments.v1.apps",
		},
	}
	for _, tt := range tests {
		name := fmt.Sprintf("Pluralization of '%s'", tt.kind)
		t.Run(name, func(t *testing.T) {
			gvk, _ := schema.ParseKindArg(tt.kind)
			want, _ := schema.ParseResourceArg(tt.want)
			if got := pluralResource(*gvk); !cmp.Equal(got, *want) {
				t.Errorf("pluralResource(%+v) = %+v, want %+v", *gvk, got, *want)
			}
		})
	}
}

func TestListEventsWithOptions(t *testing.T) {
	namespace := "default"
	event1 := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{
			Name:      "object1",
			Namespace: namespace,
		},
		Message: "Event 1 message",
	}
	event2 := &corev1.Event{
		InvolvedObject: corev1.ObjectReference{
			Name:      "object2",
			Namespace: namespace,
		},
		Message: "Event 2 message",
	}
	client := fake.NewSimpleClientset()
	client.CoreV1().Events(namespace).Create(context.TODO(), event1, metav1.CreateOptions{})
	client.CoreV1().Events(namespace).Create(context.TODO(), event2, metav1.CreateOptions{})

	events, err := ListEvents(client, namespace, "object1")
	if err != nil {
		t.Fatalf("Unexpected error from ListEvents()\n%v", err)
		return
	}
	if len(events.Items) != 1 {
		t.Fatalf("Expect 1 events, got %d", len(events.Items))
	}
	if events.Items[0].InvolvedObject.Name != "object1" {
		t.Errorf("Expect object1, got %q", events.Items[0].InvolvedObject.Name)
	}
}
