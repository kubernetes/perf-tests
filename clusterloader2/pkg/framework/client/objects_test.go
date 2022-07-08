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
	"errors"
	"fmt"
	"testing"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
