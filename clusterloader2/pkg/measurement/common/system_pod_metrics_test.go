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
	"reflect"
	"testing"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
)

func Test_subtractInitialRestartCounts(t *testing.T) {
	tests := []struct {
		name        string
		metrics     *systemPodsMetrics
		initMetrics *systemPodsMetrics
		want        *systemPodsMetrics
	}{
		{
			name:        "same-pods-and-containers",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p1", "c1", 4),
			want:        generatePodMetrics("p1", "c1", 1),
		},
		{
			name:        "different-container-names",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p1", "c2", 4),
			want:        generatePodMetrics("p1", "c1", 5),
		},
		{
			name:        "different-pod-names",
			metrics:     generatePodMetrics("p1", "c1", 5),
			initMetrics: generatePodMetrics("p2", "c1", 4),
			want:        generatePodMetrics("p1", "c1", 5),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subtractInitialRestartCounts(tt.metrics, tt.initMetrics)
			if !reflect.DeepEqual(*tt.metrics, *tt.want) {
				t.Errorf("want %v, got %v", *tt.want, *tt.metrics)
			}
		})
	}
}

func Test_validateRestartCounts(t *testing.T) {
	tests := []struct {
		name    string
		metrics *systemPodsMetrics
		config  *measurement.Config
		wantErr bool
	}{
		{
			name:    "check-disabled",
			metrics: generatePodMetrics("p", "c", 1),
			config:  buildConfig(t, false, nil),
			wantErr: false,
		},
		{
			name:    "check-enabled-violation",
			metrics: generatePodMetrics("p", "c", 1),
			config:  buildConfig(t, true, nil),
			wantErr: true,
		},
		{
			name:    "check-enabled-ok",
			metrics: generatePodMetrics("p", "c", 0),
			config:  buildConfig(t, true, nil),
			wantErr: false,
		},
		{
			name:    "override-equal-to-actual-count",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"c": 3}),
			wantErr: false,
		},
		{
			name:    "override-default-used",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"default": 3}),
			wantErr: false,
		},
		{
			name:    "override-default-not-used",
			metrics: generatePodMetrics("p", "c", 3),
			config: buildConfig(t, true, map[string]int{
				"default": 5,
				"c":       0,
			}),
			wantErr: true,
		},
		{
			name:    "override-below-actual-count",
			metrics: generatePodMetrics("p", "c", 3),
			config:  buildConfig(t, true, map[string]int{"c": 2}),
			wantErr: true,
		},
		{
			name:    "override-for-different-container",
			metrics: generatePodMetrics("p", "c1", 3),
			config:  buildConfig(t, true, map[string]int{"c2": 4}),
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			overrides, err := getThresholdOverrides(tt.config)
			if err != nil {
				t.Fatalf("getThresholdOverrides() error = %v", err)
			}
			if err := validateRestartCounts(tt.metrics, tt.config, overrides); (err != nil) != tt.wantErr {
				t.Errorf("verifyViolations() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func generatePodMetrics(podName string, contName string, restartCount int32) *systemPodsMetrics {
	return &systemPodsMetrics{
		Pods: []podMetrics{
			{
				Name: podName,
				Containers: []containerMetrics{
					{
						Name:         contName,
						RestartCount: restartCount,
					},
				}},
		},
	}
}

func buildConfig(t *testing.T, checkEnabled bool, thresholdOverrides map[string]int) *measurement.Config {
	serializedOverrides, err := yaml.Marshal(thresholdOverrides)
	if err != nil {
		t.Fatal(err)
	}
	return &measurement.Config{
		Params: map[string]interface{}{
			"enableRestartCountCheck":        checkEnabled,
			"restartCountThresholdOverrides": string(serializedOverrides),
		},
	}
}

func Test_extractImageName(t *testing.T) {
	tests := []struct {
		name  string
		image string
		want  string
	}{
		{
			name:  "GCR image",
			image: "gcr.io/gke-release-staging/k8s-dns-node-cache:1.22.0",
			want:  "gke-release-staging/k8s-dns-node-cache",
		},
		{
			name:  "GCR image with custom domain",
			image: "us.gcr.io/gke-release-staging/calico/node:v3.26.3-gke.16",
			want:  "gke-release-staging/calico/node",
		},
		{
			name:  "Artifact Registry image",
			image: "us-central1-docker.pkg.dev/gke-release-staging/gke-release-staging/calico/node:v3.26.3-gke.16",
			want:  "gke-release-staging/gke-release-staging/calico/node",
		},
		{
			name:  "k8s.gcr.io image",
			image: "k8s.gcr.io/pause:3.6",
			want:  "pause",
		},
		{
			name:  "Custom registry image",
			image: "my-registry.com:5000/my-image:tag",
			want:  "my-image",
		},
		{
			name:  "Digest image",
			image: "gcr.io/gke-release-staging/anet/cni-writer@sha256:12345",
			want:  "gke-release-staging/anet/cni-writer",
		},
		{
			name:  "Simple image name",
			image: "busybox",
			want:  "busybox",
		},
		{
			name:  "Empty image",
			image: "",
			want:  "",
		},
		{
			name:  "GKE project without registry",
			image: "gke-release-staging/event-exporter:tag",
			want:  "gke-release-staging/event-exporter",
		},
		{
			name:  "GKE project without registry and tag",
			image: "gke-release/kube-proxy",
			want:  "gke-release/kube-proxy",
		},
		{
			name:  "AWS ECR image",
			image: "602401143452.dkr.ecr.us-west-2.amazonaws.com/eks/kube-proxy:v1.22.15-eksbuild.1",
			want:  "eks/kube-proxy",
		},
		{
			name:  "Docker Hub library image",
			image: "docker.io/library/busybox:latest",
			want:  "library/busybox",
		},
		{
			name:  "registry.k8s.io image",
			image: "registry.k8s.io/sig-storage/csi-provisioner:v3.0.0",
			want:  "sig-storage/csi-provisioner",
		},
		{
			name:  "Quay.io image",
			image: "quay.io/coreos/etcd:v3.5.0",
			want:  "coreos/etcd",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractImageName(tt.image); got != tt.want {
				t.Errorf("extractImageName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_extractMetrics(t *testing.T) {
	podList := &v1.PodList{
		Items: []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod1"},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "container1",
							Image: "gcr.io/gke-release-staging/k8s-dns-node-cache:1.22.0",
						},
						{
							Name:  "container2",
							Image: "gke-release-staging/event-exporter:v1.0.0",
						},
					},
				},
				Status: v1.PodStatus{
					ContainerStatuses: []v1.ContainerStatus{
						{
							Name:         "container1",
							Image:        "sha256:digest1",
							RestartCount: 1,
						},
						{
							Name:         "container2",
							Image:        "sha256:digest2",
							RestartCount: 2,
						},
					},
				},
			},
		},
	}

	want := &systemPodsMetrics{
		Pods: []podMetrics{
			{
				Name: "pod1",
				Containers: []containerMetrics{
					{
						Name:         "container1",
						Image:        "gke-release-staging/k8s-dns-node-cache",
						RestartCount: 1,
					},
					{
						Name:         "container2",
						Image:        "gke-release-staging/event-exporter",
						RestartCount: 2,
					},
				},
			},
		},
	}

	got := extractMetrics(podList)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("extractMetrics() = %v, want %v", got, want)
	}
}
