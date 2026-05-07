/*
Copyright 2026 The Kubernetes Authors.

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
	"net/http"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	restartApiserverName = "RestartApiserver"
	// Glob because the manifest filename differs across providers:
	// kubeadm uses kube-apiserver.yaml; kube-up GCE/kubemark and kops use
	// kube-apiserver.manifest.
	restartManifestGlob        = "/etc/kubernetes/manifests/kube-apiserver*"
	restartAsidePath           = "/tmp/cl2-kube-apiserver.aside"
	restartDefaultDownSeconds  = 30
	restartDefaultReadyTimeout = 10 * time.Minute
	readyzPollInterval         = 2 * time.Second
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

// Execute restarts kube-apiserver by sshing to the master, moving the static
// pod manifest aside, sleeping, and moving it back. Then waits for /readyz to
// return 200 again before returning. Skips with a warning when the provider
// does not support master SSH (kind, managed control planes).
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
		klog.Warningf("%s: skipping - master SSH not supported by provider %s", r, cc.Provider.Name())
		return nil, nil
	}
	if len(cc.MasterIPs) < 1 {
		klog.Warningf("%s: skipping - MasterIPs not configured", r)
		return nil, nil
	}

	masterIP := cc.MasterIPs[0]
	klog.V(2).Infof("%s: restarting kube-apiserver on master %q (downSeconds=%d)", r, masterIP, downSeconds)

	manifestDir := filepath.Dir(restartManifestGlob)
	cmd := fmt.Sprintf(
		`m=$(sudo sh -c "ls -1 %s 2>/dev/null" | head -n1); `+
			`if [ -z "$m" ]; then `+
			`echo "no kube-apiserver manifest matched %s" >&2; `+
			`sudo ls -la %s >&2 || true; `+
			`exit 2; `+
			`fi; `+
			`sudo mv "$m" %s && sleep %d && sudo mv %s "$m"`,
		restartManifestGlob, restartManifestGlob, manifestDir, restartAsidePath, downSeconds, restartAsidePath,
	)
	sshResult, sshErr := measurementutil.SSH(cmd, masterIP+":22", cc.Provider)
	if sshErr != nil {
		return nil, fmt.Errorf("ssh to %s failed: %v (stderr=%q, code=%d)", masterIP, sshErr, sshResult.Stderr, sshResult.Code)
	}
	if sshResult.Code != 0 {
		return nil, fmt.Errorf("restart command exited %d on %s (stderr=%q)", sshResult.Code, masterIP, sshResult.Stderr)
	}

	if err := waitForReadyz(config.ClusterFramework.GetClientSets().GetClient(), readyTimeout); err != nil {
		return nil, err
	}
	klog.V(2).Infof("%s: kube-apiserver back online", r)
	return nil, nil
}

func (r *restartApiserverMeasurement) Dispose() {}

func (r *restartApiserverMeasurement) String() string { return restartApiserverName }

// waitForReadyz polls /readyz until it returns 200 or the timeout elapses.
// Each attempt is bounded by readyzPollInterval so a hung connect during
// apiserver boot can't consume the whole timeout budget.
func waitForReadyz(c clientset.Interface, timeout time.Duration) error {
	return wait.PollUntilContextTimeout(context.Background(), readyzPollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		attemptCtx, cancel := context.WithTimeout(ctx, readyzPollInterval)
		defer cancel()
		result := c.CoreV1().RESTClient().Get().AbsPath("/readyz").Do(attemptCtx)
		if result.Error() != nil {
			return false, nil
		}
		var statusCode int
		result.StatusCode(&statusCode)
		return statusCode == http.StatusOK, nil
	})
}
