/*
Copyright 2023 The Kubernetes Authors.

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

package networkpolicy

import (
	"context"
	"embed"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	measurementutil "k8s.io/perf-tests/clusterloader2/pkg/measurement/util"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

/*
The measurement tests network policy enforcement latency for two cases:
	1. Created network policies
Deploy the test clients (setup and run) with "testType" flag set to
"policy-creation" after creating the target pods.
	2. Created pods that are affected by network policies
Deploy the test clients (setup and run) with "testType" flag set to
"pod-creation", before creating the target pods.
Target pods are all pods that have the specified label:
{ net-pol-test: targetLabelValue }.
The test is set up by this measurement, by creating the required resources,
including the network policy enforcement latency test client pods that are
measuring the latencies and generating metrics for them.
https://github.com/kubernetes/perf-tests/tree/master/network/tools/network-policy-enforcement-latency
*/

const (
	networkPolicyEnforcementName = "NetworkPolicyEnforcement"
	kubernetesServiceName        = "kubernetes"
	netPolicyTestNamespace       = "net-policy-test"
	netPolicyTestClientName      = "np-test-client"
	policyCreationTest           = "policy-creation"
	podCreationTest              = "pod-creation"
	// denyLabelValue is used for network policies to allow connections only to
	// the pods with the specified label, effectively denying other connections,
	// as long as there isn't another network policy allowing it for other labels.
	denyLabelValue  = "deny-traffic"
	allowPolicyName = "allow-egress-to-target"
	denyPolicyName  = "deny-egress-to-target"

	serviceAccountFilePath              = "manifests/serviceaccount.yaml"
	clusterRoleFilePath                 = "manifests/clusterrole.yaml"
	clusterRoleBindingFilePath          = "manifests/clusterrolebinding.yaml"
	depTestClientPolicyCreationFilePath = "manifests/dep-test-client-policy-creation.yaml"
	depTestClientPodCreationFilePath    = "manifests/dep-test-client-pod-creation.yaml"
	policyEgressApiserverFilePath       = "manifests/policy-egress-allow-apiserver.yaml"
	policyEgressTargetPodsFilePath      = "manifests/policy-egress-allow-target-pods.yaml"
	policyLoadFilePath                  = "manifests/policy-load.yaml"

	defaultPolicyTargetLoadBaseName = "small-deployment"
	defaultPolicyLoadCount          = 1000
	defaultPolicyLoadQPS            = 10
)

//go:embed manifests
var manifestsFS embed.FS

func init() {
	klog.V(2).Infof("Registering %q", networkPolicyEnforcementName)
	if err := measurement.Register(networkPolicyEnforcementName, createNetworkPolicyEnforcementMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", networkPolicyEnforcementName, err)
	}
}

func createNetworkPolicyEnforcementMeasurement() measurement.Measurement {
	return &networkPolicyEnforcementMeasurement{}
}

type networkPolicyEnforcementMeasurement struct {
	k8sClient clientset.Interface
	framework *framework.Framework
	// testClientNamespace is the namespace of the test client pods.
	testClientNamespace string
	// targetLabelValue is the value for the label selector of target pods to
	// apply network policies on and measure the latency to become reachable.
	targetLabelValue string
	// targetNamespaces is a list of test namespace with "test-" prefix that are
	// used to specify namespaces of target pods for all test clients.
	targetNamespaces []string
	// baseline test does not create network policies. It is only used for pod
	// creation latency test, to compare pod creation reachability latency with
	// and without network policies.
	baseline bool
	// testClientNodeSelectorValue is value key for the node label on which the
	// test client pods should run.
	testClientNodeSelectorValue string
	isIPv6                      bool
}

// Execute - Available actions:
// 1. setup - Loads all the required data to execute run command. Should be
// called only once.
// 2. run - Runs a test measurement for the specified test type.
// 3. complete - Finishes and cleans up the specified test type.
func (nps *networkPolicyEnforcementMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}

	switch action {
	case "setup":
		return nil, nps.setup(config)
	case "run":
		return nil, nps.run(config)
	case "complete":
		return nil, nps.complete(config)
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

// setup initializes the measurement, creates a namespace, network policy egress
// allow to the kube-apiserver and permissions for the network policy
// enforcement test clients.
func (nps *networkPolicyEnforcementMeasurement) setup(config *measurement.Config) error {
	err := nps.initializeMeasurement(config)
	if err != nil {
		return fmt.Errorf("failed to initialize the measurement: %v", err)
	}

	if err := client.CreateNamespace(nps.k8sClient, nps.testClientNamespace); err != nil {
		return fmt.Errorf("error while creating namespace: %v", err)
	}

	// Create network policies for non-baseline test.
	if !nps.baseline {
		if err = nps.createPolicyAllowAPIServer(); err != nil {
			return err
		}

		// Create a policy that allows egress from pod creation test client pods to
		// target pods.
		podCreationAllowPolicyName := fmt.Sprintf("%s-pod-creation", allowPolicyName)
		if err = nps.createPolicyToTargetPods(podCreationAllowPolicyName, "", podCreationTest, true); err != nil {
			return err
		}

		// Create a policy that denies egress from policy creation test client pods
		// to target pods.
		policyCreationDenyPolicyName := fmt.Sprintf("%s-policy-creation", denyPolicyName)
		if err = nps.createPolicyToTargetPods(policyCreationDenyPolicyName, "", policyCreationTest, false); err != nil {
			return err
		}
	}

	return nps.createPermissionResources()
}

func (nps *networkPolicyEnforcementMeasurement) initializeMeasurement(config *measurement.Config) error {
	if nps.framework != nil {
		return fmt.Errorf("the %q is already started. Cannot start again", networkPolicyEnforcementName)
	}

	var err error
	if nps.targetLabelValue, err = util.GetString(config.Params, "targetLabelValue"); err != nil {
		return err
	}

	if nps.testClientNamespace, err = util.GetStringOrDefault(config.Params, "testClientNamespace", netPolicyTestNamespace); err != nil {
		return err
	}

	if nps.baseline, err = util.GetBoolOrDefault(config.Params, "baseline", false); err != nil {
		return err
	}

	if nps.testClientNodeSelectorValue, err = util.GetString(config.Params, "testClientNodeSelectorValue"); err != nil {
		return err
	}

	nps.framework = config.ClusterFramework
	nps.k8sClient = config.ClusterFramework.GetClientSets().GetClient()

	namespaceList, err := nps.k8sClient.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	// Target namespaces are only those that have a predefined test prefix.
	testNamespacePrefix := nps.framework.GetAutomanagedNamespacePrefix()
	for _, ns := range namespaceList.Items {
		if strings.HasPrefix(ns.GetName(), testNamespacePrefix) {
			nps.targetNamespaces = append(nps.targetNamespaces, ns.GetName())
		}
	}

	if len(nps.targetNamespaces) == 0 {
		return fmt.Errorf("cannot initialize the %q, no namespaces with prefix %q exist", networkPolicyEnforcementName, testNamespacePrefix)
	}

	return nil
}

// createPermissionResources creates ServiceAccount, ClusterRole and
// ClusterRoleBinding for the test client pods.
func (nps *networkPolicyEnforcementMeasurement) createPermissionResources() error {
	templateMap := map[string]interface{}{
		"Name":      netPolicyTestClientName,
		"Namespace": nps.testClientNamespace,
	}

	if err := nps.framework.ApplyTemplatedManifests(manifestsFS, serviceAccountFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating serviceaccount: %v", err)
	}

	if err := nps.framework.ApplyTemplatedManifests(manifestsFS, clusterRoleFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating clusterrole: %v", err)
	}

	if err := nps.framework.ApplyTemplatedManifests(manifestsFS, clusterRoleBindingFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating clusterrolebinding: %v", err)
	}

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) run(config *measurement.Config) error {
	if nps.framework == nil {
		return fmt.Errorf("the %q is not set up. Execute with the `setup` action before with the `run` action", networkPolicyEnforcementName)
	}

	targetPort, err := util.GetIntOrDefault(config.Params, "targetPort", 80)
	if err != nil {
		return err
	}

	maxTargets, err := util.GetIntOrDefault(config.Params, "maxTargets", 1000)
	if err != nil {
		return err
	}

	metricsPort, err := util.GetIntOrDefault(config.Params, "metricsPort", 9160)
	if err != nil {
		return err
	}

	testType, err := util.GetString(config.Params, "testType")
	if err != nil {
		return err
	}

	templateMap := map[string]interface{}{
		"Namespace":                   nps.testClientNamespace,
		"TestClientLabel":             netPolicyTestClientName,
		"TargetLabelSelector":         fmt.Sprintf("net-pol-test = %s", nps.targetLabelValue),
		"TargetPort":                  targetPort,
		"MetricsPort":                 metricsPort,
		"ServiceAccountName":          netPolicyTestClientName,
		"MaxTargets":                  maxTargets,
		"TestClientNodeSelectorValue": nps.testClientNodeSelectorValue,
	}

	switch testType {
	case policyCreationTest:
		err = nps.runPolicyCreationTest(templateMap, config)
	case podCreationTest:
		err = nps.runPodCreationTest(templateMap)
	default:
		err = fmt.Errorf("unknown testType is specified: %q", testType)
	}

	return err
}

func (nps *networkPolicyEnforcementMeasurement) runPodCreationTest(depTemplateMap map[string]interface{}) error {
	klog.V(2).Infof("Starting network policy enforcement latency measurement for pod creation")
	return nps.createTestClientDeployments(depTemplateMap, podCreationTest, depTestClientPodCreationFilePath)
}

func (nps *networkPolicyEnforcementMeasurement) runPolicyCreationTest(depTemplateMap map[string]interface{}, config *measurement.Config) error {
	klog.V(2).Infof("Starting network policy enforcement latency measurement for policy creation")
	if nps.baseline {
		klog.Warningf("Baseline flag is specified, which is only used for pod creation test, and means that no network policies should be created. Skipping policy creation test")
		return nil
	}

	if err := nps.createTestClientDeployments(depTemplateMap, policyCreationTest, depTestClientPolicyCreationFilePath); err != nil {
		return err
	}

	const (
		timeout      = 2 * time.Minute
		waitInterval = 5 * time.Second
	)
	ctx, cancel := context.WithTimeout(context.TODO(), timeout)
	defer cancel()

	desiredPodCount := len(nps.targetNamespaces)
	options := &measurementutil.WaitForPodOptions{
		DesiredPodCount:     func() int { return desiredPodCount },
		CallerName:          nps.String(),
		WaitForPodsInterval: waitInterval,
	}

	objectSelector := &util.ObjectSelector{
		Namespace:     nps.testClientNamespace,
		LabelSelector: fmt.Sprintf("type = %s", policyCreationTest),
	}

	podStore, err := measurementutil.NewPodStore(nps.k8sClient, objectSelector)
	if err != nil {
		return err
	}

	klog.V(2).Infof("Waiting for policy creation test client pods to be running")
	_, err = measurementutil.WaitForPods(ctx, podStore, options)
	if err != nil {
		klog.Warningf("Not all %d policy creation test client pods are running after %v", len(nps.targetNamespaces), timeout)
	}

	wg := sync.WaitGroup{}
	wg.Add(1)
	// Create load policies while allow policies are being created to take network
	// policy churn into account.
	go func() {
		nps.createLoadPolicies(config)
		wg.Done()
	}()

	nps.createAllowPoliciesForPolicyCreationLatency()
	wg.Wait()

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) createPolicyAllowAPIServer() error {
	// Get kube-apiserver addresses and ports to allow connections to it from the
	// test client pods. It's needed since network policies are denying connections
	// with all endpoints that are not in the specified Labels / CIDR range.
	// The kubernetes Service is single-stack in the cluster's primary IP family,
	// so all its endpoints share one address type, which also selects the IP
	// family of the load policies.
	var endpointSlices *discoveryv1.EndpointSliceList
	listEndpointSlices := func() error {
		var err error
		endpointSlices, err = nps.k8sClient.DiscoveryV1().EndpointSlices(corev1.NamespaceDefault).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", discoveryv1.LabelServiceName, kubernetesServiceName),
		})
		return err
	}
	if err := client.RetryWithExponentialBackOff(client.RetryFunction(listEndpointSlices)); err != nil {
		return fmt.Errorf("failed to list kube-apiserver EndpointSlices: %v", err)
	}

	addrCount := 0
	portCount := 0
	for _, slice := range endpointSlices.Items {
		for _, endpoint := range slice.Endpoints {
			addrCount += len(endpoint.Addresses)
		}
		portCount += len(slice.Ports)
	}

	kubeAPIServerCIDRs := make([]string, 0, addrCount)
	seenCIDRs := make(map[string]struct{}, addrCount)
	ports := make([]int32, 0, 2+portCount)
	ports = append(ports, 443, 80)
	seenPorts := make(map[int32]struct{}, 2+portCount)
	seenPorts[443] = struct{}{}
	seenPorts[80] = struct{}{}

	for _, slice := range endpointSlices.Items {
		if slice.AddressType != discoveryv1.AddressTypeIPv4 && slice.AddressType != discoveryv1.AddressTypeIPv6 {
			continue
		}
		for _, endpoint := range slice.Endpoints {
			if endpoint.Conditions.Ready != nil && !*endpoint.Conditions.Ready {
				continue
			}
			for _, addr := range endpoint.Addresses {
				cidr, _, err := getAPIServerCIDR(addr)
				if err != nil {
					return err
				}
				if len(kubeAPIServerCIDRs) == 0 {
					nps.isIPv6 = slice.AddressType == discoveryv1.AddressTypeIPv6
				}
				if _, ok := seenCIDRs[cidr]; !ok {
					seenCIDRs[cidr] = struct{}{}
					kubeAPIServerCIDRs = append(kubeAPIServerCIDRs, cidr)
				}
			}
		}
		for _, p := range slice.Ports {
			if p.Port == nil || *p.Port <= 0 || (p.Protocol != nil && *p.Protocol != corev1.ProtocolTCP) {
				continue
			}
			if _, ok := seenPorts[*p.Port]; !ok {
				seenPorts[*p.Port] = struct{}{}
				ports = append(ports, *p.Port)
			}
		}
	}
	if len(kubeAPIServerCIDRs) == 0 {
		return fmt.Errorf("kube-apiserver EndpointSlices do not have a ready IP address")
	}

	policyName := "allow-egress-apiserver"
	if policy, err := nps.k8sClient.NetworkingV1().NetworkPolicies(nps.testClientNamespace).Get(context.TODO(), policyName, metav1.GetOptions{}); err == nil && policy != nil {
		klog.Warningf("Attempting to create %q network policy, but it already exists", policyName)
		return nil
	}

	templateMap := map[string]interface{}{
		"Name":               policyName,
		"Namespace":          nps.testClientNamespace,
		"TestClientLabel":    netPolicyTestClientName,
		"Ports":              ports,
		"KubeAPIServerCIDRs": kubeAPIServerCIDRs,
	}

	if err := nps.framework.ApplyTemplatedManifests(manifestsFS, policyEgressApiserverFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating allow egress to apiserver network policy: %v", err)
	}

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) createPolicyToTargetPods(policyName, targetNamespace, testType string, allowForTargetPods bool) error {
	templateMap := map[string]interface{}{
		"Name":           policyName,
		"Namespace":      nps.testClientNamespace,
		"TypeLabelValue": testType,
	}

	if allowForTargetPods {
		templateMap["TargetLabelValue"] = nps.targetLabelValue
	} else {
		templateMap["TargetLabelValue"] = denyLabelValue
	}

	if len(targetNamespace) > 0 {
		templateMap["OnlyTargetNamespace"] = true
		templateMap["TargetNamespace"] = targetNamespace
	} else {
		templateMap["OnlyTargetNamespace"] = false
	}

	if err := nps.framework.ApplyTemplatedManifests(manifestsFS, policyEgressTargetPodsFilePath, templateMap); err != nil {
		return fmt.Errorf("error while creating allow egress to pods network policy: %v", err)
	}

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) createTestClientDeployments(templateMap map[string]interface{}, testType, deploymentFilePath string) error {
	klog.V(2).Infof("Creating test client deployments for measurement %q", networkPolicyEnforcementName)
	templateMap["TypeLabelValue"] = testType

	// Create a test client deployment for each test namespace.
	for i, ns := range nps.targetNamespaces {
		templateMap["Name"] = fmt.Sprintf("%s-%s-%d", testType, netPolicyTestClientName, i)
		templateMap["TargetNamespace"] = ns
		templateMap["AllowPolicyName"] = fmt.Sprintf("%s-%d", allowPolicyName, i)

		if err := nps.framework.ApplyTemplatedManifests(manifestsFS, deploymentFilePath, templateMap); err != nil {
			return fmt.Errorf("error while creating test client deployment: %v", err)
		}
	}

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) createLoadPolicies(config *measurement.Config) {
	policyLoadTargetBaseName, err := util.GetStringOrDefault(config.Params, "policyLoadTargetBaseName", defaultPolicyTargetLoadBaseName)
	if err != nil {
		klog.Errorf("Failed getting parameter policyLoadBaseName value, error: %v", err)
		return
	}

	policyLoadCount, err := util.GetIntOrDefault(config.Params, "policyLoadCount", defaultPolicyLoadCount)
	if err != nil {
		klog.Errorf("Failed getting parameter policyLoadBaseName value, error: %v", err)
		return
	}

	policyLoadQPS, err := util.GetIntOrDefault(config.Params, "policyLoadQPS", defaultPolicyLoadQPS)
	if err != nil {
		klog.Errorf("Failed getting parameter policyLoadQPS value, error: %v", err)
		return
	}

	policiesPerNs := policyLoadCount / len(nps.targetNamespaces)
	limiter := rate.NewLimiter(rate.Limit(policyLoadQPS), policyLoadQPS)

	for nsIdx, ns := range nps.targetNamespaces {
		baseCidr := getBaseCIDR(nsIdx, nps.isIPv6)

		for depIdx := 0; depIdx < policiesPerNs; depIdx++ {
			// This will be the same as "small-deployment-0".."small-deployment-50",
			// that is used in the ClusterLoader2 load test.
			// https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/testing/load
			podSelectorLabelValue := fmt.Sprintf("policy-load-%s-%d", policyLoadTargetBaseName, depIdx)
			templateMapForTargetPods := map[string]interface{}{
				"Name":                  fmt.Sprintf("%s-%d", podSelectorLabelValue, nsIdx),
				"Namespace":             ns,
				"PodSelectorLabelValue": podSelectorLabelValue,
				"CIDR":                  baseCidr,
			}

			err := limiter.Wait(context.TODO())
			if err != nil {
				klog.Errorf("limiter.Wait() returned an error: %v", err)
				return
			}

			if err := nps.framework.ApplyTemplatedManifests(manifestsFS, policyLoadFilePath, templateMapForTargetPods); err != nil {
				klog.Errorf("Error while creating load network policy for label selector 'name=%s': %v", podSelectorLabelValue, err)
			}
		}
	}
}

func (nps *networkPolicyEnforcementMeasurement) createAllowPoliciesForPolicyCreationLatency() {
	klog.V(2).Infof("Creating allow network policies for measurement %q", networkPolicyEnforcementName)

	for i, ns := range nps.targetNamespaces {
		policyName := fmt.Sprintf("%s-%d", allowPolicyName, i)
		err := nps.createPolicyToTargetPods(policyName, ns, policyCreationTest, true)
		if err != nil {
			klog.Errorf("Failed to create a network policy to allow traffic to namespace %q", ns)
		}
	}
}

// complete deletes test client deployments for the specified test mode.
func (nps *networkPolicyEnforcementMeasurement) complete(config *measurement.Config) error {
	testType, err := util.GetString(config.Params, "testType")
	if err != nil {
		return err
	}

	var typeLabelValue string
	switch testType {
	case policyCreationTest:
		typeLabelValue = policyCreationTest
	case podCreationTest:
		typeLabelValue = podCreationTest
	default:
		return fmt.Errorf("unknown testType is specified: %q", testType)
	}

	listOpts := metav1.ListOptions{LabelSelector: fmt.Sprintf("type=%s", typeLabelValue)}
	err = nps.k8sClient.AppsV1().Deployments(nps.testClientNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, listOpts)
	if err != nil {
		return fmt.Errorf("failed to complete %q test, error: %v", typeLabelValue, err)
	}

	return nil
}

func (nps *networkPolicyEnforcementMeasurement) deleteClusterRoleAndBinding() error {
	klog.V(2).Infof("Deleting ClusterRole and ClusterRoleBinding for measurement %q", networkPolicyEnforcementName)

	if err := nps.k8sClient.RbacV1().ClusterRoles().Delete(context.TODO(), netPolicyTestClientName, metav1.DeleteOptions{}); err != nil {
		return err
	}

	return nps.k8sClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), netPolicyTestClientName, metav1.DeleteOptions{})
}

func (nps *networkPolicyEnforcementMeasurement) cleanUp() error {
	if nps.k8sClient == nil {
		return fmt.Errorf("cleanup skipped - the measurement is not running")
	}

	if err := nps.deleteClusterRoleAndBinding(); err != nil {
		return err
	}

	klog.V(2).Infof("Deleting namespace %q for measurement %q", nps.testClientNamespace, networkPolicyEnforcementName)
	return nps.k8sClient.CoreV1().Namespaces().Delete(context.TODO(), nps.testClientNamespace, metav1.DeleteOptions{})
}

// String returns a string representation of the measurement.
func (nps *networkPolicyEnforcementMeasurement) String() string {
	return networkPolicyEnforcementName
}

// Dispose cleans up after the measurement.
func (nps *networkPolicyEnforcementMeasurement) Dispose() {
	if err := nps.cleanUp(); err != nil {
		klog.Errorf("Failed to clean up, error: %v", err)
	}
}

// getAPIServerCIDR returns the single-address CIDR of the kube-apiserver IP and
// whether the address is IPv6.
func getAPIServerCIDR(ip string) (string, bool, error) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return "", false, fmt.Errorf("invalid kube-apiserver IP address %q: %v", ip, err)
	}
	if addr.Zone() != "" {
		return "", false, fmt.Errorf("kube-apiserver IP address %q must not have a zone", ip)
	}
	addr = addr.Unmap()
	return netip.PrefixFrom(addr, addr.BitLen()).String(), addr.Is6(), nil
}

// getBaseCIDR returns the CIDR used by the load policies of a namespace.
func getBaseCIDR(nsIdx int, isIPv6 bool) string {
	octet := byte(nsIdx % 256)
	if isIPv6 {
		return netip.PrefixFrom(netip.AddrFrom16([16]byte{0xfd, 0x00, 0x00, 0x10, 0x00, 0x00, 0x00, octet}), 64).String()
	}
	return netip.PrefixFrom(netip.AddrFrom4([4]byte{10, 0, octet, 0}), 24).String()
}
