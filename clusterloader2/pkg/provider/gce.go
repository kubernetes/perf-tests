/*
Copyright 2020 The Kubernetes Authors.

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

package provider

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
	sshutil "k8s.io/perf-tests/clusterloader2/pkg/util"
)

type GCEProvider struct {
	features Features
}

func NewGCEProvider(_ map[string]string) Provider {
	return &GCEProvider{
		features: Features{
			SupportWindowsNodeScraping:          true,
			SupportProbe:                        true,
			SupportSSHToMaster:                  true,
			SupportImagePreload:                 true,
			SupportSnapshotPrometheusDisk:       true,
			SupportNodeKiller:                   true,
			SupportEnablePrometheusServer:       true,
			SupportGrabMetricsFromKubelets:      true,
			SupportAccessAPIServerPprofEndpoint: true,
			SupportKubeStateMetrics:             true,
			SupportMetricsServerMetrics:         true,
			SupportResourceUsageMetering:        true,
			ShouldScrapeKubeProxy:               true,
		},
	}
}

func (p *GCEProvider) Name() string {
	return GCEName
}

func (p *GCEProvider) Features() *Features {
	return &p.features
}

func (p *GCEProvider) GetComponentProtocolAndPort(componentName string) (string, int, error) {
	return getComponentProtocolAndPort(componentName)
}

func (p *GCEProvider) GetConfig() Config {
	return Config{}
}

func (p *GCEProvider) RunSSHCommand(cmd, host string) (string, string, int, error) {
	// gce provider takes ssh key from GCE_SSH_KEY.
	r, err := sshutil.SSH(context.Background(), cmd, host, "gce")
	return r.Stdout, r.Stderr, r.Code, err
}

func (p *GCEProvider) Metadata(c clientset.Interface) (map[string]string, error) {
	nodes, err := client.ListNodes(c)
	if err != nil {
		return nil, err
	}

	var masterInstanceIDs []string
	for _, node := range nodes {
		if sshutil.IsControlPlaneNode(&node) {
			var project, zone string
			if node.Spec.ProviderID != "" {
				// providerID is in the format: gce://project-id/zone/instance-name
				// https://github.com/kubernetes/cloud-provider-gcp/blob/2e539007132d518d1356c0eab9e02ba8dee8a25d/providers/gce/gce_util.go#L259
				r, _ := regexp.Compile("gce://([^/]+)/([^/]+)/([^/]+)")
				matches := r.FindStringSubmatch(node.Spec.ProviderID)

				if len(matches) == 4 {
					project = matches[1]
					zone = matches[2]
				}
			}
			cmd := exec.Command("gcloud", "compute", "instances", "describe", "--format", "value(id)", "--zone", zone, node.Name, "--project", project)
			out, err := cmd.Output()
			if err != nil {
				var stderr string

				if ee, ok := err.(*exec.ExitError); ok {
					stderr = string(ee.Stderr)
				}
				return nil, fmt.Errorf("fetching instanceID for %q failed with: %v (stderr: %q)", node.Name, err, stderr)
			}
			instanceID := strings.TrimSpace(string(out))
			klog.Infof("Detected instanceID for %s/%s: %q", zone, node.Name, instanceID)
			masterInstanceIDs = append(masterInstanceIDs, instanceID)
		}
	}

	return map[string]string{"masterInstanceIDs": strings.Join(masterInstanceIDs, ",")}, nil
}

func (p *GCEProvider) GetManagedPrometheusClient() (prom.Client, error) {
	return prom.NewGCPManagedPrometheusClient()
}
