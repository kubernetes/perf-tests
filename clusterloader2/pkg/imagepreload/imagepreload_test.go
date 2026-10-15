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

package imagepreload

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHasPreloadedImages(t *testing.T) {
	tests := []struct {
		name       string
		images     []string
		nodeImages []v1.ContainerImage
		want       bool
	}{
		{
			name: "no images requested",
			want: true,
		},
		{
			name:   "all requested images present",
			images: []string{"registry.example/pause:3.10", "registry.example/test:v1"},
			nodeImages: []v1.ContainerImage{
				{Names: []string{"registry.example/pause:3.10@sha256:abc"}},
				{Names: []string{"registry.example/test:v1", "registry.example/test@sha256:def"}},
			},
			want: true,
		},
		{
			name:   "requested image missing",
			images: []string{"registry.example/pause:3.10", "registry.example/test:v2"},
			nodeImages: []v1.ContainerImage{
				{Names: []string{"registry.example/pause:3.10"}},
				{Names: []string{"registry.example/test:v1"}},
			},
			want: false,
		},
		{
			name:       "node has no images",
			images:     []string{"registry.example/pause:3.10"},
			nodeImages: []v1.ContainerImage{},
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &controller{images: tt.images}
			node := &v1.Node{Status: v1.NodeStatus{Images: tt.nodeImages}}

			if got := controller.hasPreloadedImages(node); got != tt.want {
				t.Errorf("hasPreloadedImages() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestCheckNode(t *testing.T) {
	controller := &controller{images: []string{"registry.example/test:v1"}}
	doneNodes := make(map[string]struct{})
	preloadedNode := nodeWithImages("worker-1", "registry.example/test:v1")

	controller.checkNode(doneNodes, nil, preloadedNode)
	if got := controller.countDone(doneNodes); got != 1 {
		t.Fatalf("countDone() after add = %d, want 1", got)
	}

	notPreloadedNode := nodeWithImages("worker-1", "registry.example/other:v1")
	controller.checkNode(doneNodes, preloadedNode, notPreloadedNode)
	if got := controller.countDone(doneNodes); got != 0 {
		t.Fatalf("countDone() after update = %d, want 0", got)
	}

	controller.checkNode(doneNodes, nil, preloadedNode)
	controller.checkNode(doneNodes, preloadedNode, nil)
	if got := controller.countDone(doneNodes); got != 0 {
		t.Errorf("countDone() after delete = %d, want 0", got)
	}
}

func nodeWithImages(name string, images ...string) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: v1.NodeStatus{
			Images: []v1.ContainerImage{{Names: images}},
		},
	}
}
