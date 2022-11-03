/*
Copyright 2018 The Kubernetes Authors.

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

package kubemark

import (
	"bufio"
	"fmt"
	"strings"

	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
)

// ResourceUsage represents resources used by the kubemark.
type ResourceUsage struct {
	Name                    string
	MemoryWorkingSetInBytes uint64
	CPUUsageInCores         float64
}

func getMasterUsageByPrefix(host string, provider provider.Provider, prefix string) (string, error) {
	sshResult, err := util.SSH(fmt.Sprintf("ps ax -o %%cpu,rss,command | tail -n +2 | grep %v | sed 's/\\s+/ /g'", prefix), host+":22", provider)
	if err != nil {
		return "", err
	}
	return sshResult.Stdout, nil
}

// GetKubemarkMasterComponentsResourceUsage returns resource usage of the kubemark components.
// TODO: figure out how to move this to kubemark directory (need to factor test SSH out of e2e framework)
func GetKubemarkMasterComponentsResourceUsage(host string, provider provider.Provider) map[string]*ResourceUsage {
	result := make(map[string]*ResourceUsage)
	// Get kubernetes component resource usage
	sshResult, err := getMasterUsageByPrefix(host, provider, "kube")
	if err != nil {
		klog.Errorf("error when trying to SSH to master machine. Skipping probe. %v", err)
		return nil
	}
	scanner := bufio.NewScanner(strings.NewReader(sshResult))
	for scanner.Scan() {
		var cpu float64
		var mem uint64
		var name string
		fmt.Sscanf(strings.TrimSpace(scanner.Text()), "%f %d /usr/local/bin/kube-%s", &cpu, &mem, &name)
		if name != "" {
			// Gatherer expects pod_name/container_name format
			fullName := name + "/" + name
			result[fullName] = &ResourceUsage{Name: fullName, MemoryWorkingSetInBytes: mem * 1024, CPUUsageInCores: cpu / 100}
		}
	}
	// Get etcd resource usage
	sshResult, err = getMasterUsageByPrefix(host, provider, "bin/etcd")
	if err != nil {
		klog.Errorf("error when trying to SSH to master machine. Skipping probe")
		return nil
	}
	scanner = bufio.NewScanner(strings.NewReader(sshResult))
	for scanner.Scan() {
		var cpu float64
		var mem uint64
		var etcdKind string
		fmt.Sscanf(strings.TrimSpace(scanner.Text()), "%f %d /usr/local/bin/etcd", &cpu, &mem)
		dataDirStart := strings.Index(scanner.Text(), "--data-dir")
		if dataDirStart < 0 {
			continue
		}
		fmt.Sscanf(scanner.Text()[dataDirStart:], "--data-dir /var/%s", &etcdKind)
		if etcdKind != "" {
			// Gatherer expects pod_name/container_name format
			fullName := "etcd/" + etcdKind
			result[fullName] = &ResourceUsage{Name: fullName, MemoryWorkingSetInBytes: mem * 1024, CPUUsageInCores: cpu / 100}
		}
	}
	return result
}
