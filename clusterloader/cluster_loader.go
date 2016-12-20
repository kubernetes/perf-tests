/*
Copyright 2016 The Kubernetes Authors.

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

package clusterloader

import (
	"fmt"
	"path/filepath"
	"strconv"

	clusterloaderframework "github.com/kubernetes/perf-tests/clusterloader/framework"
	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/kubernetes/pkg/api/v1"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/clientset"
	"k8s.io/kubernetes/pkg/labels"
	"k8s.io/kubernetes/test/e2e/framework"
	testutils "k8s.io/kubernetes/test/utils"
)

var _ = framework.KubeDescribe("Cluster Loader [Feature:ManualPerformance]", func() {
	f := framework.NewDefaultFramework("cluster-loader")
	defer ginkgo.GinkgoRecover()

	var c clientset.Interface
	ginkgo.BeforeEach(func() {
		c = f.ClientSet
	})

	ginkgo.It(fmt.Sprintf("running config file"), func() {
		project := clusterloaderframework.ConfigContext.ClusterLoader.Projects
		tuningSets := clusterloaderframework.ConfigContext.ClusterLoader.TuningSets
		if len(project) < 1 {
			framework.Failf("invalid config file.\nFile: %v", project)
		}

		var namespaces []*v1.Namespace
		//totalPods := 0 // Keep track of how many pods for stepping
		// TODO sjug: add concurrency
		for _, p := range project {
			// Find tuning if we have it
			tuning := clusterloaderframework.GetTuningSet(tuningSets, p.Tuning)
			if tuning != nil {
				framework.Logf("Our tuning set is: %v", tuning)
			}
			for j := 0; j < p.Number; j++ {
				// Create namespaces as defined in Cluster Loader config
				nsName := appendIntToString(p.Basename, j)
				ns, err := f.CreateNamespace(nsName, nil)
				framework.ExpectNoError(err)
				framework.Logf("%d/%d : Created new namespace: %v", j+1, p.Number, nsName)
				namespaces = append(namespaces, ns)

				// Create templates as defined
				for _, template := range p.Templates {
					clusterloaderframework.CreateTemplate(template.Basename, ns, mkPath(template.File), template.Number, tuning)
				}
				// This is too familiar, create pods
				for _, pod := range p.Pods {
					// Parse Pod file into struct
					config := clusterloaderframework.ParsePods(mkPath(pod.File))
					// Check if environment variables are defined in CL config
					if pod.Parameters == (clusterloaderframework.ParameterConfigType{}) {
						framework.Logf("Pod environment variables will not be modified.")
					} else {
						// Override environment variables for Pod using ConfigMap
						configMapName := clusterloaderframework.InjectConfigMap(f, ns.Name, pod.Parameters, config)
						// Cleanup ConfigMap at some point after the Pods are created
						defer func() {
							_ = f.ClientSet.Core().ConfigMaps(ns.Name).Delete(configMapName, nil)
						}()
					}
					// TODO sjug: pass label via config
					labels := map[string]string{"purpose": "test"}
					clusterloaderframework.CreatePods(f, pod.Basename, ns.Name, labels, config.Spec, pod.Number, tuning)
				}
			}
		}

		// Wait for pods to be running
		for _, ns := range namespaces {
			label := labels.SelectorFromSet(labels.Set(map[string]string{"purpose": "test"}))
			err := testutils.WaitForPodsWithLabelRunning(c, ns.Name, label)
			if err != nil {
				framework.Failf("Got %v when trying to wait for the pods to start", err)
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			framework.Logf("All pods running in namespace %s.", ns.Name)
		}

		// Start sync WS
		//c := &clusterloaderframework.PodCount{Shutdown: make(chan bool)}
		//if err := clusterloaderframework.Server(c); err != nil {
		//	framework.Logf("HTTP server messed up: %v", err)
		//}
		//close(c.Shutdown)

	})
})

// mkPath returns fully qualfied file location as a string
func mkPath(file string) string {
	// Handle an empty filename.
	if file == "" {
		framework.Failf("No template file defined!")
	}
	return filepath.Join("content/", file)
}

// appendIntToString appends an integer i to string s
func appendIntToString(s string, i int) string {
	return s + strconv.Itoa(i)
}
