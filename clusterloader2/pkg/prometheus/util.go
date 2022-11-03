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

package prometheus

import (
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"os"
	"regexp"

	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const allTargets = -1

type targetsResponse struct {
	Data targetsData `json:"data"`
}

type targetsData struct {
	ActiveTargets []Target `json:"activeTargets"`
}

// Target represents a Prometheus target object.
type Target struct {
	Labels map[string]string `json:"labels"`
	Health string            `json:"health"`
}

// CheckAllTargetsReady returns true iff there is at least minActiveTargets matching the selector and
// all of them are ready.
func CheckAllTargetsReady(k8sClient kubernetes.Interface, selector func(Target) bool, minActiveTargets int) (bool, error) {
	return CheckTargetsReady(k8sClient, selector, minActiveTargets, allTargets)
}

// CheckTargetsReady returns true iff there is at least minActiveTargets matching the selector and
// at least minReadyTargets of them are ready.
func CheckTargetsReady(k8sClient kubernetes.Interface, selector func(Target) bool, minActiveTargets, minReadyTargets int) (bool, error) {
	raw, err := k8sClient.CoreV1().
		Services(namespace).
		ProxyGet("http", "prometheus-k8s", "9090", "api/v1/targets", nil /*params*/).
		DoRaw(context.TODO())
	if err != nil {
		response := "(empty)"
		if raw != nil {
			response = string(raw)
		}
		// This might happen if prometheus server is temporary down, log error but don't return it.
		klog.Warningf("error while calling prometheus api: %v, response: %v", err, response)
		return false, nil
	}
	var response targetsResponse
	if err := json.Unmarshal(raw, &response); err != nil {
		return false, err // This shouldn't happen, return error.
	}
	nReady, nTotal := 0, 0
	var exampleNotReadyTarget Target
	for _, t := range response.Data.ActiveTargets {
		if !selector(t) {
			continue
		}
		nTotal++
		if t.Health == "up" {
			nReady++
			continue
		}
		exampleNotReadyTarget = t
	}
	if nTotal < minActiveTargets {
		klog.V(2).Infof("Not enough active targets (%d), expected at least (%d), waiting for more to become active...",
			nTotal, minActiveTargets)
		return false, nil
	}
	if minReadyTargets == allTargets {
		minReadyTargets = nTotal
	}
	if nReady < minReadyTargets {
		klog.V(2).Infof("%d/%d targets are ready, example not ready target: %v", nReady, minReadyTargets, exampleNotReadyTarget)
		return false, nil
	}
	klog.V(2).Infof("All %d expected targets are ready", minReadyTargets)
	return true, nil
}

type snapshotResponse struct {
	Data snapshotData `json:"data"`
}

type snapshotData struct {
	Name string `json:"name"`
}

func makeSnapshot(k8sClient kubernetes.Interface, config *restclient.Config, filePath string) error {
	raw, err := k8sClient.CoreV1().RESTClient().Post().
		Namespace(namespace).
		Resource("services").
		SubResource("proxy").
		Name(net.JoinSchemeNamePort("http", "prometheus-k8s", "9090")).
		Suffix("api/v1/admin/tsdb/snapshot").
		DoRaw(context.TODO())
	if err != nil {
		return err
	}
	var response snapshotResponse

	if err := json.Unmarshal(raw, &response); err != nil {
		return err
	}

	klog.V(2).Infof("Snapshot made: %v", response)

	svc, err := k8sClient.CoreV1().Services(namespace).Get(context.TODO(), "prometheus-k8s", metav1.GetOptions{})
	labelSelector := labels.Set(svc.Spec.Selector).AsSelector().String()
	pods, err := k8sClient.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{LabelSelector: labelSelector})

	if len(pods.Items) != 1 {
		return fmt.Errorf("unexpected number of pods in prometheus service: %d", len(pods.Items))
	}

	pod := pods.Items[0]
	return copyFromPod(k8sClient, config, "/prometheus/snapshots/"+response.Data.Name, filePath, pod.Name, namespace)
}

const snapshotNamePattern = `^(?:[a-z](?:[-a-z0-9]{0,61}[a-z0-9])?)$`

var re = regexp.MustCompile(snapshotNamePattern)

// VerifySnapshotName verifies if snapshot name statisfies snapshot name regex.
func VerifySnapshotName(name string) error {
	if re.MatchString(name) {
		return nil
	}
	return fmt.Errorf("disk name doesn't match %v", snapshotNamePattern)
}

func copyFromPod(k8sClient kubernetes.Interface, config *restclient.Config, srcPath, destPath string, podName, namespace string) error {
	cmdArr := []string{"tar", "cfz", "-", srcPath}
	outStream, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer outStream.Close()
	req := k8sClient.CoreV1().RESTClient().
		Post().
		Namespace(namespace).
		Resource("pods").
		Name(podName).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: "prometheus",
			Command:   cmdArr,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdout: outStream,
		Stderr: os.Stderr,
		Tty:    false,
	})
}
