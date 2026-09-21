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

// Package heapprofile periodically dumps the clusterloader2 process heap
// profile into the report dir for diagnosing runner-pod OOMs. Profiles follow
// the same <name>_<RFC3339>.pprof naming convention as the KCP profiles
// collected by pkg/measurement/common/profile.go, so existing artifact tooling
// picks them up the same way.
package heapprofile

import (
	"os"
	"path"
	"runtime/pprof"
	"time"

	"k8s.io/klog/v2"
)

// Start launches periodic heap profiling.
func Start(reportDir string, interval time.Duration) {
	klog.Infof("Heap profiling enabled: writing to %q every %s", reportDir, interval)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			writeHeapProfile(reportDir, "periodic")
		}
	}()
}

func writeHeapProfile(reportDir, tag string) {
	ts := time.Now().Format(time.RFC3339)
	filePath := path.Join(reportDir, "ClusterLoader2_MemoryProfile_"+tag+"_"+ts+".pprof")
	f, err := os.Create(filePath)
	if err != nil {
		klog.Errorf("Cannot create heap profile %q: %v", filePath, err)
		return
	}
	defer f.Close()
	if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
		klog.Errorf("Cannot write heap profile %q: %v", filePath, err)
		return
	}
	klog.V(2).Infof("Wrote heap profile %q", filePath)
}
