/*
Copyright 2022 The Kubernetes Authors.

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

package dns

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

/*
The DNS Performance test for K8s Hostnames creates the required permissions for
DNS client pods to get K8s hostnames (from services and endpoints). Then DNS
client deployment is created that uses dnsperfgo image
(https://github.com/kubernetes/perf-tests/tree/master/dns/dnsperfgo) to query
the hostnames within the cluster and generate Prometheus metrics for number of
errors, timeouts and their latencies. Outside this measurement, the metrics are
scraped and results verified to not cross the specified thresholds.
*/

const (
	dnsPerfK8sHostnamesMeasureName = "DNSPerformanceK8sHostnames"
	dnsPerfTestNamespace           = "dns-perf-test"
	dnsPerfTestPermissionsName     = "dns-test-client"
	manifestPathPrefix             = "./pkg/measurement/common/dns/manifests"
)

var (
	serviceAccountFilePath     = filepath.Join(manifestPathPrefix, "serviceaccount.yaml")
	clusterRoleFilePath        = filepath.Join(manifestPathPrefix, "clusterrole.yaml")
	clusterRoleBindingFilePath = filepath.Join(manifestPathPrefix, "clusterrolebinding.yaml")
	clientDeploymentFilePath   = filepath.Join(manifestPathPrefix, "dns-client.yaml")
)

func init() {
	klog.Info("Registering measurement: DNS Performance for K8s Hostnames")
	if err := measurement.Register(dnsPerfK8sHostnamesMeasureName, createDNSPerfK8sHostnamesMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", dnsPerfK8sHostnamesMeasureName, err)
	}
}

func createDNSPerfK8sHostnamesMeasurement() measurement.Measurement {
	return &dnsPerfK8sHostnamesMeasurement{}
}

type dnsPerfK8sHostnamesMeasurement struct {
	k8sClient clientset.Interface
	framework *framework.Framework
	// testClientNamespace is the new namespace where the dns clients are going to
	// be deployed. It's cleaned up when the test finishes.
	testClientNamespace string
	// podReplicas is the number of DNS client pods to be deployed.
	podReplicas int
	// qpsPerClient is the number of DNS queries each DNS client should send per
	// second.
	qpsPerClient int
	// testDurationMinutes is the duration in minutes for DNS client pods to run.
	testDurationMinutes int
}

func (m *dnsPerfK8sHostnamesMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	err := m.initializeMeasurement(config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize the measurement: %v", err)
	}

	if err = client.CreateNamespace(m.k8sClient, m.testClientNamespace); err != nil {
		return nil, fmt.Errorf("error while creating namespace: %v", err)
	}

	if err = m.createDNSClientPermissions(); err != nil {
		return nil, fmt.Errorf("failed to create dns client permission resources: %v", err)
	}

	if err = m.createDNSClientDeployment(); err != nil {
		return nil, fmt.Errorf("failed to create DNS client deployment: %v", err)
	}

	// Keep running the dns clients for the specified duration.
	runDuration := time.Duration(m.testDurationMinutes) * time.Minute
	klog.Infof("DNS tests are going to run for %v", runDuration)
	time.Sleep(runDuration)

	return nil, m.cleanUp()
}

func (m *dnsPerfK8sHostnamesMeasurement) initializeMeasurement(config *measurement.Config) error {
	if m.framework != nil {
		klog.Warningf("The %q was already started, but running it again", dnsPerfK8sHostnamesMeasureName)
	}

	var err error
	if m.testClientNamespace, err = util.GetStringOrDefault(config.Params, "testNamespace", dnsPerfTestNamespace); err != nil {
		return err
	}

	if m.podReplicas, err = util.GetIntOrDefault(config.Params, "podReplicas", 10); err != nil {
		return err
	}

	if m.qpsPerClient, err = util.GetIntOrDefault(config.Params, "qpsPerClient", 10); err != nil {
		return err
	}

	if m.testDurationMinutes, err = util.GetIntOrDefault(config.Params, "testDurationMinutes", 10); err != nil {
		return err
	}

	m.framework = config.ClusterFramework
	m.k8sClient = config.ClusterFramework.GetClientSets().GetClient()

	return nil
}

// createDNSClientPermissions creates ServiceAccount, ClusterRole and
// ClusterRoleBinding for the test client pods to access Services and Endpoints.
func (m *dnsPerfK8sHostnamesMeasurement) createDNSClientPermissions() error {
	templateMap := map[string]interface{}{
		"Name":      dnsPerfTestPermissionsName,
		"Namespace": m.testClientNamespace,
	}

	if err := m.framework.ApplyTemplatedManifests(serviceAccountFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating serviceaccount: %v", err)
	}

	if err := m.framework.ApplyTemplatedManifests(clusterRoleFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating clusterrole: %v", err)
	}

	if err := m.framework.ApplyTemplatedManifests(clusterRoleBindingFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating clusterrolebinding: %v", err)
	}

	return nil
}

func (m *dnsPerfK8sHostnamesMeasurement) createDNSClientDeployment() error {
	templateMap := map[string]interface{}{
		"Namespace":          m.testClientNamespace,
		"PodReplicas":        m.podReplicas,
		"QPSPerClient":       m.qpsPerClient,
		"ServiceAccountName": dnsPerfTestPermissionsName,
	}

	return m.framework.ApplyTemplatedManifests(clientDeploymentFilePath, templateMap)
}

func (m *dnsPerfK8sHostnamesMeasurement) deleteDNSClientPermissions() error {
	klog.Infof("Deleting DNS client permission resources for measurement %q", dnsPerfK8sHostnamesMeasureName)

	if err := m.k8sClient.CoreV1().ServiceAccounts(m.testClientNamespace).Delete(context.TODO(), dnsPerfTestPermissionsName, metav1.DeleteOptions{}); err != nil {
		return err
	}

	if err := m.k8sClient.RbacV1().ClusterRoles().Delete(context.TODO(), dnsPerfTestPermissionsName, metav1.DeleteOptions{}); err != nil {
		return err
	}

	return m.k8sClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), dnsPerfTestPermissionsName, metav1.DeleteOptions{})
}

func (m *dnsPerfK8sHostnamesMeasurement) cleanUp() error {
	if m.framework == nil {
		klog.Warning("Cleanup skipped. The measurement is not running")
		return nil
	}

	if err := m.deleteDNSClientPermissions(); err != nil {
		return err
	}

	klog.Infof("Deleting namespace %q for measurement %q", m.testClientNamespace, dnsPerfK8sHostnamesMeasureName)
	return m.k8sClient.CoreV1().Namespaces().Delete(context.TODO(), m.testClientNamespace, metav1.DeleteOptions{})
}

// String returns a string representation of the measurement.
func (m *dnsPerfK8sHostnamesMeasurement) String() string {
	return dnsPerfK8sHostnamesMeasureName
}

// Dispose cleans up after the measurement.
func (m *dnsPerfK8sHostnamesMeasurement) Dispose() {
	if err := m.cleanUp(); err != nil {
		klog.Infof("Cleanup failed: %v", err)
	}
}
