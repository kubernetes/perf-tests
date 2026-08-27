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
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/flags"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/runtimeobjects"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	manifest        = "manifests/daemonset.yaml"
	namespace       = "preload"
	daemonsetName   = "preload"
	pollingInterval = 5 * time.Second
)

var (
	images         []string
	pollingTimeout = 15 * time.Minute
	//go:embed manifests
	manifestsFS embed.FS
)

func InitFlags() {
	flags.StringSliceEnvVar(&images, "node-preload-images", "NODE_PRELOAD_IMAGES", []string{}, "List of images to preload on each node in the test cluster before executing tests")
	flags.DurationEnvVar(&pollingTimeout, "node-preload-images-timeout", "NODE_PRELOAD_IMAGES_TIMEOUT", 15*time.Minute, "Timeout for waiting for all nodes to preload images (e.g. 10m, 1h)")
}

type controller struct {
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

	nodeIndexer, err := measurementutil.NodeIndexerFactory.NodeIndexer(kclient)
	if err != nil {
		return fmt.Errorf("failed to get shared node indexer: %w", err)
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

	stopCh := make(chan struct{})
	defer close(stopCh)

	size, err := runtimeobjects.GetReplicasFromRuntimeObject(kclient, ds)
	if err != nil {
		return err
	}
	if err := size.Start(stopCh); err != nil {
		return err
	}
	nodeCounter, _ := size.(*runtimeobjects.NodeCounter)

	var clusterSize, doneCount int
	klog.V(2).Infof("Waiting for %d Node objects to be updated...", size.Replicas())
	if err := wait.Poll(pollingInterval, pollingTimeout, func() (bool, error) {
		clusterSize = size.Replicas()
		doneCount = 0
		for _, obj := range nodeIndexer.List() {
			node, ok := obj.(*v1.Node)
			if !ok {
				continue
			}

			if nodeCounter != nil {
				match, err := nodeCounter.ShouldRun(node)
				if err != nil || !match {
					continue
				}
			} else if !util.IsNodeSchedulableAndUntainted(node) {
				continue
			}

			if c.hasPreloadedImages(node) {
				doneCount++
			}
		}

		klog.V(3).Infof("%d out of %d nodes have pulled images", doneCount, clusterSize)
		return doneCount >= clusterSize, nil
	}); err != nil {
		klog.Errorf("%d out of %d nodes have pulled images", doneCount, clusterSize)
		return err
	}
	klog.V(2).Info("Waiting... done")

	klog.V(2).Infof("Deleting namespace %s...", namespace)
	if err := client.DeleteNamespace(kclient, namespace); err != nil {
		return err
	}
	if err := client.WaitForDeleteNamespace(kclient, namespace, client.DefaultNamespaceDeletionTimeout); err != nil {
		return err
	}
	return nil
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
