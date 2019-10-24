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
	"strings"
	"sync"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
)

const (
	informerTimeout = time.Minute
	manifest        = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/imagepreload/manifests/daemonset.yaml"
	namespace       = "preload"
	daemonsetName   = "preload"
	pollingInterval = 5 * time.Second
	// TODO(oxddr): verify whether 5 minutes is a sufficient timeout
	pollingTimeout = 5 * time.Minute
)

var images []string

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
	mapping, err := config.GetMapping(conf)
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
	if c.config.ClusterConfig.Provider == "kubemark" {
		klog.Warning("Image preloading is disabled in kubemark")
		return nil
	}

	kclient := c.framework.GetClientSets().GetClient()

	doneNodes := make(map[string]bool)
	stopCh := make(chan struct{})
	defer close(stopCh)

	nodeInformer := informer.NewInformer(
		kclient,
		"nodes",
		util.NewObjectSelector(),
		func(old, new interface{}) { c.checkNode(new, doneNodes) })
	if err := informer.StartAndSync(nodeInformer, stopCh, informerTimeout); err != nil {
		return err
	}

	klog.Infof("Creating namespace %s...", namespace)
	if err := client.CreateNamespace(kclient, namespace); err != nil {
		return err
	}

	klog.Info("Creating daemonset to preload images...")
	c.templateMapping["Images"] = c.images
	if err := c.framework.ApplyTemplatedManifests(manifest, c.templateMapping); err != nil {
		return err
	}

	klog.Infof("Getting %s/%s deamonset size...", namespace, daemonsetName)
	ds, err := kclient.AppsV1().DaemonSets(namespace).Get(daemonsetName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	size, err := runtimeobjects.GetReplicasFromRuntimeObject(kclient, ds)
	if err != nil {
		return err
	}
	clusterSize := int(size)

	klog.Infof("Waiting for %d Node objects to be updated...", clusterSize)
	if err := wait.Poll(pollingInterval, pollingTimeout, func() (bool, error) {
		klog.Infof("%d out of %d nodes have pulled images", len(doneNodes), clusterSize)
		return len(doneNodes) == clusterSize, nil
	}); err != nil {
		return nil
	}
	klog.Info("Waiting... done")

	klog.Infof("Deleting namespace %s...", namespace)
	if err := client.DeleteNamespace(kclient, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(kclient, namespace); err != nil {
		return err
	}
	return nil
}

func (c *controller) checkNode(obj interface{}, done map[string]bool) {
	if obj == nil {
		return
	}

	node := obj.(*v1.Node)
	if c.hasPreloadedImages(node) {
		c.lock.Lock()
		defer c.lock.Unlock()
		done[node.Name] = true
	}
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
