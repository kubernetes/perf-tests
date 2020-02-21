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

package prometheus

import (
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
)

// gceNode is a struct storing gce node info
type gceNode struct {
	projectID string
	zone      string
	nodeName  string
}

func setUpWindowsNodeAndTemplate(k8sClient kubernetes.Interface, mapping map[string]interface{}) error {
	// Get the gce windows node info
	windowsNode, err := getGceWindowsNodeFromKubernetesService(k8sClient)
	if err != nil {
		return err
	}
	// Install the wmi exporter onto windows node
	if err := installWmiExporter(k8sClient, windowsNode, mapping); err != nil {
		return err
	}
	mapping["WINDOWS_NODE_NAME"] = windowsNode.nodeName
	mapping["PROMETHEUS_SCRAPE_WINDOWS_NODES"] = true
	return nil
}

func isWindowsNodeScrapingEnabled(mapping map[string]interface{}, clusterLoaderConfig *config.ClusterLoaderConfig) bool {
	if windowsNodeTest, exists := mapping["WINDOWS_NODE_TEST"]; exists && windowsNodeTest.(bool) && clusterLoaderConfig.ClusterConfig.Provider == "gce" {
		return true
	}
	return false
}

func installWmiExporter(k8sClient kubernetes.Interface, windowsNode *gceNode, mapping map[string]interface{}) error {
	wmiExporterURL, ok := mapping["WMI_EXPORTER_URL"]
	if !ok {
		return fmt.Errorf("missing setting up wmi exporter download url")
	}
	wmiExporterCollectors, ok := mapping["WMI_EXPORTER_ENABLED_COLLECTORS"]
	if !ok {
		return fmt.Errorf("missing setting up wmi exporter enabled collectors")
	}
	// Tried executing the invoke-webrequest cmd directly in gcloud ssh bash, but get this error:
	// 		invoke-webrequest : Win32 internal error "Access is denied" 0x5 occurred while reading the console output buffer.
	// 		Contact Microsoft Customer Support Services.
	// After wrap it up in script block, it works well, also support timeout setting.
	installWmiCmdTemplate := `Start-Job -Name InstallWmiExporter -ScriptBlock {invoke-webrequest -uri %s -outfile C:\wmi_exporter.msi; Start-Process msiexec.exe -Wait -ArgumentList '/I C:\wmi_exporter.msi ENABLED_COLLECTORS=%s LISTEN_PORT=5000 /quiet'}; Wait-Job -Timeout 300 -Name InstallWmiExporter`
	installWmiExporterCmd := fmt.Sprintf(installWmiCmdTemplate, wmiExporterURL, wmiExporterCollectors)
	powershellCmd := fmt.Sprintf(`powershell.exe -Command "%s"`, installWmiExporterCmd)
	if windowsNode == nil {
		return fmt.Errorf("no windows nodes available to install wmi exporter")
	}
	klog.Infof("Installing wmi exporter onto projectId: %s, zone: %s, nodeName: %s with cmd: %s", windowsNode.projectID, windowsNode.zone, windowsNode.nodeName, installWmiExporterCmd)
	cmd := exec.Command("gcloud", "compute", "ssh", "--project", windowsNode.projectID, "--zone", windowsNode.zone, windowsNode.nodeName, "--command", powershellCmd)
	err := cmd.Run()
	if err != nil {
		return err
	}
	return nil
}

func getGceWindowsNodeFromKubernetesService(k8sClient kubernetes.Interface) (*gceNode, error) {
	var nodeList *corev1.NodeList
	f := func() error {
		var err error
		nodeList, err = k8sClient.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: "kubernetes.io/os=windows"})
		return err
	}

	if err := client.RetryWithExponentialBackOff(client.RetryFunction(f)); err != nil {
		return nil, err
	}

	if len(nodeList.Items) == 0 {
		return nil, fmt.Errorf("no windows nodes available in kubernetes service")
	}
	// TODO: Modify to support multiple windows nodes soon.
	providerStrs := strings.Split(nodeList.Items[0].Spec.ProviderID, "/")
	if len(providerStrs) < 5 {
		return nil, fmt.Errorf("no valid gce provider ID available")
	}

	// Gce providerID format: gce://{projectID}/{zone}/{nodeName}
	return &gceNode{
		projectID: providerStrs[2],
		zone:      providerStrs[3],
		nodeName:  providerStrs[4],
	}, nil
}
