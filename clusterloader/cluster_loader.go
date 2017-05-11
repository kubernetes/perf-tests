/*
Copyright 2017 The Kubernetes Authors.

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
	"io/ioutil"
	"os"
	"regexp"
	"strconv"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/kubernetes/pkg/api/v1"
	clientset "k8s.io/kubernetes/pkg/client/clientset_generated/clientset"
	"k8s.io/kubernetes/test/e2e/framework"
	testutils "k8s.io/kubernetes/test/utils"
	clusterloaderframework "k8s.io/perf-tests/clusterloader/framework"
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
			tuning := clusterloaderframework.TuningSets(tuningSets).Get(p.Tuning)

			framework.Logf("Tuning set is: %+v", tuning)
			for j := 0; j < p.Number; j++ {
				// Create namespaces as defined in the config
				nsName := appendIntToString(p.Basename, j)
				ns, err := clusterloaderframework.CreateNSIfNotExists(f, nsName)
				if err != nil {
					framework.Failf("Error creating NS: %v", err)
				}
				// Keep track of all the namespaces we have created, not too useful currently
				namespaces = appendUnique(namespaces, ns)

				// Create templates as defined
				for _, template := range p.Templates {
					if err = createTemplate(template.Basename, ns, clusterloaderframework.MakePath(template.File), template.Number, tuning); err != nil {
						framework.Failf("Error creating template, %v", err)
					}
				}
				// RCs are a thing as well
				for _, RC := range p.RCs {
					config, err := RC.ParseConfig()
					if err != nil {
						framework.Failf("Error parsing config, %v", err)
					}
					label, err := RC.ConvertToLabelSet()
					if err != nil {
						framework.Failf("Error creating Labels, %v", err)
					}
					if err = clusterloaderframework.CreateRC(f, RC.Basename, ns.Name, label, config.Spec, RC.Number); err != nil {
						framework.Failf("Error creating RC, %v", err)
					}
				}
				// This is too familiar, create pods
				for _, pod := range p.Pods {
					config, err := pod.ParseConfig()
					if err != nil {
						framework.Failf("Error parsing config, %v", err)
					}
					label, err := pod.ConvertToLabelSet()
					if err != nil {
						framework.Failf("Error creating Labels, %v", err)
					}
					clusterloaderframework.CreatePods(f, pod.Basename, ns.Name, label, config.Spec, pod.Number, tuning)
				}
			}
			// Only sleeps for each new project defined in the config
			// need to move up to sleep for every copy
			// TODO: Consider if we want sleeps between each iteration
			if tuning != nil {
				tuning.Project.Delay()
			}
		}

		// Wait for pods to be running in all new namespaces
		for _, ns := range namespaces {
			// TODO If created namespace doesn't have a pod with matching label we will hang
			label := labels.SelectorFromSet(labels.Set(map[string]string{"purpose": "test"}))
			err := testutils.WaitForPodsWithLabelRunning(c, ns.Name, label)
			if err != nil {
				framework.Failf("Got %v when trying to wait for the pods to start", err)
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			framework.Logf("All pods running in namespace %s.", ns.Name)
		}
	})
})

// appendUnique appends new namespace pointers to a slice
func appendUnique(allNS []*v1.Namespace, newNS *v1.Namespace) []*v1.Namespace {
	for _, NS := range allNS {
		if NS.Name == newNS.Name {
			return allNS
		}
	}
	return append(allNS, newNS)
}

// createTemplate does regex substitution against the template file, then creates the template
func createTemplate(baseName string, ns *v1.Namespace, configPath string, numObjects int, tuning *clusterloaderframework.TuningSet) error {
	// Try to read the file
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		return err
	}

	// ${IDENTIFER} is what we're replacing in the file
	regex := regexp.MustCompile("\\${IDENTIFIER}")

	for i := 0; i < numObjects; i++ {
		result := regex.ReplaceAll(content, []byte(strconv.Itoa(i)))

		tmpfile, err := ioutil.TempFile("", "cl")
		if err != nil {
			return err
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.Write(result); err != nil {
			return err
		}
		if err := tmpfile.Close(); err != nil {
			return err
		}

		framework.RunKubectlOrDie("create", "-f", tmpfile.Name(), getNsCmdFlag(ns))
		framework.Logf("%d/%d : Created template %s", i+1, numObjects, baseName)

		// If there is a tuning set defined for this template
		if tuning != nil {
			if err := tuning.Templates.Delay(); err != nil {
				return err
			}
			if tuning.Templates.Stepping.StepSize != 0 && (i+1)%tuning.Templates.Stepping.StepSize == 0 {
				framework.Logf("We have created %d templates; sleep for %v", i+1, tuning.Templates.Stepping.Pause)
				if err := tuning.Templates.Pause(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func getNsCmdFlag(ns *v1.Namespace) string {
	return fmt.Sprintf("--namespace=%v", ns.Name)
}

func appendIntToString(s string, i int) string {
	return s + strconv.Itoa(i)
}
