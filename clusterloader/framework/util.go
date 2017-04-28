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

package framework

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"time"
	"unicode"

	"github.com/fatih/structs"

	"k8s.io/kubernetes/pkg/api/v1"
	metav1 "k8s.io/kubernetes/pkg/apis/meta/v1"
	"k8s.io/kubernetes/test/e2e/framework"
)

const maxRetries = 5

// ParsePods unmarshalls the json file defined in the CL config into a struct
func ParsePods(jsonFile string) (configStruct v1.Pod) {
	configFile, err := ioutil.ReadFile(jsonFile)
	if err != nil {
		framework.Failf("Cant read config file. Error: %v", err)
	}

	err = json.Unmarshal(configFile, &configStruct)
	if err != nil {
		framework.Failf("Unable to unmarshal pod config. Error: %v", err)
	}

	framework.Logf("The loaded config file is: %+v", configStruct.Spec.Containers)
	return
}

// CreatePods creates pods in user defined namspaces with user configurable tuning sets
func CreatePods(f *framework.Framework, appName string, ns string, labels map[string]string, spec v1.PodSpec, maxCount int, tuning *TuningSetType) {
	for i := 0; i < maxCount; i++ {
		framework.Logf("%v/%v : Creating pod", i+1, maxCount)
		// Retry on pod creation failure
		for retryCount := 0; retryCount < maxRetries; retryCount++ {
			_, err := f.ClientSet.Core().Pods(ns).Create(&v1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name:      fmt.Sprintf(appName+"-pod-%v", i),
					Namespace: ns,
					Labels:    labels,
				},
				Spec: spec,
			})
			if err == nil {
				break
			}
			framework.ExpectNoError(err)
		}
		if tuning != nil {
			// If a rate limit has been defined we wait for N ms between creation
			if tuning.Pods.RateLimit.Delay != 0 {
				framework.Logf("Sleeping %d ms between podcreation.", tuning.Pods.RateLimit.Delay)
				time.Sleep(tuning.Pods.RateLimit.Delay * time.Millisecond)
			}
			// If a stepping tuningset has been defined in the config, we wait for the step of pods to be created, and pause
			if tuning.Pods.Stepping.StepSize != 0 && (i+1)%tuning.Pods.Stepping.StepSize == 0 {
				verifyRunning := f.NewClusterVerification(
					&v1.Namespace{
						ObjectMeta: v1.ObjectMeta{
							Name: ns,
						},
						Status: v1.NamespaceStatus{},
					},
					framework.PodStateVerification{
						Selectors:   labels,
						ValidPhases: []v1.PodPhase{v1.PodRunning},
					},
				)

				pods, err := verifyRunning.WaitFor(i+1, tuning.Pods.Stepping.Timeout*time.Second)
				if err != nil {
					framework.Failf("Error in wait... %v", err)
				} else if len(pods) < i+1 {
					framework.Failf("Only got %v out of %v", len(pods), i+1)
				}

				framework.Logf("We have created %d pods and are now sleeping for %d seconds", i+1, tuning.Pods.Stepping.Pause)
				time.Sleep(tuning.Pods.Stepping.Pause * time.Second)
			}
		}
	}
}

// GetTuningSet matches the name of the tuning set defined in the project and returns a pointer to the set
func GetTuningSet(tuningSets []TuningSetType, podTuning string) (tuning *TuningSetType) {
	if podTuning != "" {
		// Iterate through defined tuningSets
		for _, ts := range tuningSets {
			// If we have a matching tuningSet keep it
			if ts.Name == podTuning {
				tuning = &ts
				return
			}
		}
		framework.Failf("No pod tuning found for: %s", podTuning)
	}
	return nil
}

// Server is the webservice that will syncronize the start and stop of Pods
func Server(c *PodCount) error {
	http.HandleFunc("/start", handleStart(startHandler, c))
	http.HandleFunc("/stop", handleStop(stopHandler, c))

	server := &http.Server{Addr: ":8081", Handler: nil}

	ln, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}

	go server.Serve(ln)
	fmt.Println("Listening on port", server.Addr)
	select {
	case <-c.Shutdown:
		fmt.Println("Shutdown server")
		ln.Close()
		return err
	}
}

func handleStart(fn http.HandlerFunc, c *PodCount) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.Started++
		fn(w, r)
		fmt.Printf("Started pods: %d, Stopped pods: %d\n", c.Started, c.Stopped)
	}
}

func startHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Hello")
}

func handleStop(fn http.HandlerFunc, c *PodCount) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c.Stopped++
		fn(w, r)
		fmt.Printf("Started pods: %d, Stopped pods: %d\n", c.Started, c.Stopped)
		if c.Stopped == c.Started && c.Stopped > 0 {
			c.Shutdown <- true
		}
	}
}

func stopHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "Goodbye")
}

// firstLowercase conversts the first letter of a string to lowercase.
func firstLowercase(s string) string {
	a := []rune(s)
	a[0] = unicode.ToLower(a[0])
	return string(a)
}

// convertVariables takes our loaded struct and converts it into a map[string]string.
func convertVariables(params ParameterConfigType) map[string]string {
	m := structs.Map(params)
	values := make(map[string]string)
	for k, v := range m {
		k = firstLowercase(k)
		if v != 0 && v != "" {
			if _, ok := v.(int); ok {
				values[k] = strconv.Itoa(v.(int))
			} else {
				values[k] = v.(string)
			}
		}
	}
	return values
}

// InjectConfigMap modifies the pod struct and replaces the environment variables.
func InjectConfigMap(f *framework.Framework, ns string, vars ParameterConfigType, config v1.Pod) string {
	configMapName := ns + "-configmap"
	freshConfigVars := convertVariables(vars)
	dirtyConfigVars := getClusterData(f, freshConfigVars)
	configMap := newConfigMap(ns, configMapName, dirtyConfigVars)
	framework.Logf("Creating configMap %v in namespace %v", configMap.Name, ns)
	var err error
	if configMap, err = f.ClientSet.Core().ConfigMaps(ns).Create(configMap); err != nil {
		framework.Failf("Unable to create test configMap %s: %v", configMap.Name, err)
	}

	for i, envVar := range config.Spec.Containers[0].Env {
		if _, ok := dirtyConfigVars[envVar.Name]; ok {
			framework.Logf("Found match to replace: %+v", envVar)
			config.Spec.Containers[0].Env[i] = v1.EnvVar{
				Name: envVar.Name,
				ValueFrom: &v1.EnvVarSource{
					ConfigMapKeyRef: &v1.ConfigMapKeySelector{
						LocalObjectReference: v1.LocalObjectReference{
							Name: configMapName,
						},
						Key: envVar.Name,
					},
				},
			}
		} else {
			framework.Logf("Environment variable %v is not defined in Pod file, skipping.", envVar.Name)
		}
	}
	return configMapName
}

// getClusterData will return map containing updated strings based on custer data
func getClusterData(f *framework.Framework, config map[string]string) map[string]string {
	newConfig := make(map[string]string)
	for k, v := range config {
		if k == "routerIP" {
			// TODO sjug: make localhost func
			//v = localhost(f)
			v = "127.0.0.1"
		} else if k == "targetHost" {
			// getEndpointsWithLabel will not return single string
			v = concatenateIP(getEndpointsWithLabel(f, config["match"]))
		}
		newConfig[k] = v
	}
	return newConfig
}

func concatenateIP(endpointInfo []ServiceInfo) (ip string) {
	for i := range endpointInfo {
		ip += endpointInfo[i].IP + ","
	}
	return
}

func getEndpointsWithLabel(f *framework.Framework, label string) (endpointInfo []ServiceInfo) {
	selector := v1.ListOptions{LabelSelector: label}
	endpoints, err := f.ClientSet.Core().Endpoints("").List(selector)
	if err != nil {
		panic(err.Error())
	}
	for _, v := range endpoints.Items {
		if len(v.Subsets) > 0 {
			for _, ep := range v.Subsets[0].Addresses {
				end := ServiceInfo{v.ObjectMeta.Name, ep.IP, v.Subsets[0].Ports[0].Port}
				fmt.Printf("For endpoint \"%s\", the IP is %v, the port is %d\n", end.Name, end.IP, end.Port)
				endpointInfo = append(endpointInfo, end)
			}
		}
	}

	return
}

func getPodDetailsWithLabel(f *framework.Framework, label string) (podInfo []ServiceInfo) {
	selector := v1.ListOptions{LabelSelector: label}
	pods, err := f.ClientSet.Core().Pods("").List(selector)
	if err != nil {
		panic(err.Error())
	}
	for _, v := range pods.Items {
		pod, err := f.ClientSet.Core().Pods(v.ObjectMeta.Namespace).Get(v.ObjectMeta.Name, metav1.GetOptions{})
		if err != nil {
			panic(err.Error())
		}
		info := ServiceInfo{pod.Name, pod.Status.PodIP, pod.Spec.Containers[0].Ports[0].ContainerPort}
		fmt.Printf("For pod \"%s\", the IP is %v, the port is %d\n", info.Name, info.IP, info.Port)
		podInfo = append(podInfo, info)
	}

	return
}

func newConfigMap(ns string, name string, vars map[string]string) *v1.ConfigMap {
	return &v1.ConfigMap{
		ObjectMeta: v1.ObjectMeta{
			Namespace: ns,
			Name:      name,
		},
		Data: vars,
	}
}

// CreateTemplate does regex substitution against the template file, then creates the template
func CreateTemplate(baseName string, ns *v1.Namespace, configPath string, numObjects int, tuning *TuningSetType) {
	// Try to read the file
	content, err := ioutil.ReadFile(configPath)
	if err != nil {
		framework.Failf("Error reading file: %s", err)
	}

	// ${IDENTIFER} is what we're replacing in the file
	regex, err := regexp.Compile("\\${IDENTIFIER}")
	if err != nil {
		framework.Failf("Error compiling regex: %v", err)
	}

	for i := 0; i < numObjects; i++ {
		result := regex.ReplaceAll(content, []byte(strconv.Itoa(i)))

		tmpfile, err := ioutil.TempFile("", "cl")
		if err != nil {
			framework.Failf("Error creating new tempfile: %v", err)
		}
		defer os.Remove(tmpfile.Name())

		if _, err := tmpfile.Write(result); err != nil {
			framework.Failf("Error writing to tempfile: %v", err)
		}
		if err := tmpfile.Close(); err != nil {
			framework.Failf("Error closing tempfile: %v", err)
		}

		framework.RunKubectlOrDie("create", "-f", tmpfile.Name(), getNsCmdFlag(ns))
		framework.Logf("%d/%d : Created template %s", i+1, numObjects, baseName)

		// If there is a tuning set defined for this template
		if tuning != nil {
			if tuning.Templates.RateLimit.Delay != 0 {
				framework.Logf("Sleeping %d ms between template creation.", tuning.Templates.RateLimit.Delay)
				time.Sleep(time.Duration(tuning.Templates.RateLimit.Delay) * time.Millisecond)
			}
			if tuning.Templates.Stepping.StepSize != 0 && (i+1)%tuning.Templates.Stepping.StepSize == 0 {
				framework.Logf("We have created %d templates and are now sleeping for %d seconds", i+1, tuning.Templates.Stepping.Pause)
				time.Sleep(time.Duration(tuning.Templates.Stepping.Pause) * time.Second)
			}
		}
	}
}

func getNsCmdFlag(ns *v1.Namespace) string {
	return fmt.Sprintf("--namespace=%v", ns.Name)
}
