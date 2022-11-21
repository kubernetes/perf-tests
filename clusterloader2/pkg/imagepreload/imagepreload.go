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

package imagepreload

import (
	"context"
	"embed"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
)

const (
	informerTimeout = time.Minute
	manifest        = "manifests/daemonset.yaml"
	namespace       = "preload"
	daemonsetName   = "preload"
	pollingInterval = 5 * time.Second
	pollingTimeout  = 15 * time.Minute
)

var (
	images []string
	//go:embed manifests
	manifestsFS embed.FS
)

func init() {
	flags.StringSliceEnvVar(&images, "node-preload-images", "NODE_PRELOAD_IMAGES", []string{}, "List of images to preload on each node in the test cluster before executing tests")
}

type controller struct {
	// lock for controlling access to doneNodes in PreloadImages
	lock sync.Mutex

	config          *config.ClusterLoaderConfig
	framework       *framework.Framework
	templateMapping map[string]interface{}
	images          []string
}

// Setup ensures every node in cluster preloads given list of images before starting tests.
// It does it by creating a Daemonset that call "docker pull" and awaits for Node object to be updated.
// As a side-effect of the image preloading, size of Node objects is increased.
//
// Preloading is skipped in kubemark or if no images have been specified.
func Setup(conf *config.ClusterLoaderConfig, f *framework.Framework) error {
	mapping, err := config.GetMapping(conf, nil)
	if err != nil {
		return err
	}

	ctl := &controller{
		config:          conf,
		framework:       f,
		templateMapping: mapping,
		images:          images,
	}
	return ctl.PreloadImages()
}

func (c *controller) PreloadImages() error {
	if len(images) == 0 {
		klog.Warning("No images specified. Skipping image preloading")
		return nil
	}
	if !c.config.ClusterConfig.Provider.Features().SupportImagePreload {
		klog.Warningf("Image preloading is disabled in provider: %s", c.config.ClusterConfig.Provider.Name())
		return nil
	}

	kclient := c.framework.GetClientSets().GetClient()

	doneNodes := make(map[string]struct{})
	stopCh := make(chan struct{})
	defer close(stopCh)

	nodeInformer := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return kclient.CoreV1().Nodes().List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return kclient.CoreV1().Nodes().Watch(context.TODO(), options)
			},
		},
		func(old, new interface{}) { c.checkNode(doneNodes, old, new) })
	if err := informer.StartAndSync(nodeInformer, stopCh, informerTimeout); err != nil {
		return err
	}

	klog.V(2).Infof("Creating namespace %s...", namespace)
	if err := client.CreateNamespace(kclient, namespace); err != nil {
		return err
	}

	klog.V(2).Info("Creating daemonset to preload images...")
	c.templateMapping["Images"] = c.images
	if err := c.framework.ApplyTemplatedManifests(manifestsFS, manifest, c.templateMapping); err != nil {
		return err
	}

	klog.V(2).Infof("Getting %s/%s deamonset size...", namespace, daemonsetName)
	ds, err := kclient.AppsV1().DaemonSets(namespace).Get(context.TODO(), daemonsetName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	size, err := runtimeobjects.GetReplicasFromRuntimeObject(kclient, ds)
	if err != nil {
		return err
	}
	if err := size.Start(stopCh); err != nil {
		return err
	}

	var clusterSize, doneCount int
	klog.V(2).Infof("Waiting for %d Node objects to be updated...", size.Replicas())
	if err := wait.Poll(pollingInterval, pollingTimeout, func() (bool, error) {
		clusterSize = size.Replicas()
		doneCount = c.countDone(doneNodes)
		klog.V(3).Infof("%d out of %d nodes have pulled images", doneCount, clusterSize)
		return doneCount == clusterSize, nil
	}); err != nil {
		klog.Errorf("%d out of %d nodes have pulled images", doneCount, clusterSize)
		return err
	}
	klog.V(2).Info("Waiting... done")

	klog.V(2).Infof("Deleting namespace %s...", namespace)
	if err := client.DeleteNamespace(kclient, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(kclient, namespace); err != nil {
		return err
	}
	return nil
}

func (c *controller) checkNode(set map[string]struct{}, old, new interface{}) {
	if new != nil {
		node := new.(*v1.Node)
		preloaded := c.hasPreloadedImages(node)
		c.markDone(set, node.Name, preloaded)
		return
	}
	if old != nil {
		node := old.(*v1.Node)
		c.markDone(set, node.Name, false)
		return
	}

}

func (c *controller) markDone(set map[string]struct{}, node string, done bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if done {
		set[node] = struct{}{}
	} else {
		delete(set, node)
	}
}

func (c *controller) countDone(set map[string]struct{}) int {
	c.lock.Lock()
	defer c.lock.Unlock()
	return len(set)
}

func (c *controller) hasPreloadedImages(node *v1.Node) bool {
	nodeImages := make([]string, 0, 20)
	for _, nodeImg := range node.Status.Images {
		nodeImages = append(nodeImages, nodeImg.Names...)
	}

	for _, img := range c.images {
		found := false
		for _, nodeImg := range nodeImages {
			found = strings.HasPrefix(nodeImg, img)
			if found {
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
