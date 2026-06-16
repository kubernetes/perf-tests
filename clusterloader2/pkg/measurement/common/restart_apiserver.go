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

package common

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/provider"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	restartApiserverName       = "RestartApiserver"
	restartManifestPath        = "/etc/kubernetes/manifests/kube-apiserver.manifest"
	restartBackupPath          = "/tmp/cl2-kube-apiserver.bak"
	restartDefaultDownSeconds  = 30
	restartDefaultReadyTimeout = 10 * time.Minute
	readyzPollInterval         = 2 * time.Second
	readyzCurlCmd              = `curl --connect-timeout 2 --silent --insecure --output /dev/null --write-out "%{http_code}" https://localhost:443/readyz`
)

func init() {
	if err := measurement.Register(restartApiserverName, createRestartApiserverMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", restartApiserverName, err)
	}
}

func createRestartApiserverMeasurement() measurement.Measurement {
	return &restartApiserverMeasurement{}
}

type restartApiserverMeasurement struct{}

func (r *restartApiserverMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	downSeconds, err := util.GetIntOrDefault(config.Params, "downSeconds", restartDefaultDownSeconds)
	if err != nil {
		return nil, err
	}
	readyTimeout, err := util.GetDurationOrDefault(config.Params, "readyTimeout", restartDefaultReadyTimeout)
	if err != nil {
		return nil, err
	}

	cc := config.ClusterFramework.GetClusterConfig()
	if !cc.Provider.Features().SupportSSHToMaster {
		return nil, fmt.Errorf("%s: provider %q does not support master SSH", r, cc.Provider.Name())
	}
	if len(cc.MasterIPs) < 1 {
		return nil, fmt.Errorf("%s: MasterIPs not configured", r)
	}

	masterIP := cc.MasterIPs[rand.Intn(len(cc.MasterIPs))]
	if err := r.restartOne(masterIP, cc.Provider, downSeconds, readyTimeout); err != nil {
		return nil, err
	}
	return nil, nil
}

func (r *restartApiserverMeasurement) restartOne(masterIP string, p provider.Provider, downSeconds int, readyTimeout time.Duration) error {
	klog.V(2).Infof("%s: restarting kube-apiserver on master %q (downSeconds=%d)", r, masterIP, downSeconds)

	cmd := fmt.Sprintf(
		`sudo mv %s %s && sleep %d && sudo mv %s %s`,
		restartManifestPath, restartBackupPath, downSeconds, restartBackupPath, restartManifestPath,
	)
	sshResult, sshErr := measurementutil.SSH(cmd, masterIP+":22", p)
	if sshErr != nil {
		return fmt.Errorf("ssh to %s failed: %v (stderr=%q, code=%d)", masterIP, sshErr, sshResult.Stderr, sshResult.Code)
	}
	if sshResult.Code != 0 {
		return fmt.Errorf("restart command exited %d on %s (stderr=%q)", sshResult.Code, masterIP, sshResult.Stderr)
	}

	if err := waitForReadyz(masterIP, p, readyTimeout); err != nil {
		return fmt.Errorf("readyz wait on %s: %v", masterIP, err)
	}
	klog.V(2).Infof("%s: kube-apiserver back online on %s", r, masterIP)
	return nil
}

func (r *restartApiserverMeasurement) Dispose() {}

func (r *restartApiserverMeasurement) String() string { return restartApiserverName }

func waitForReadyz(masterIP string, p provider.Provider, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), readyzPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		sshResult, err := measurementutil.SSH(readyzCurlCmd, masterIP+":22", p)
		if err != nil || sshResult.Code != 0 {
			return false, nil
		}
		return strings.TrimSpace(sshResult.Stdout) == "200", nil
	})
}
