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

package networkpolicy

import (
	"context"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/perf-tests/clusterloader2/pkg/framework"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/utils/ptr"
)

var networkPolicyGVR = schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"}

func TestGetAPIServerCIDR(t *testing.T) {
	cases := []struct {
		ip         string
		expected   string
		expectIPv6 bool
		expectErr  bool
	}{
		{ip: "192.0.2.1", expected: "192.0.2.1/32"},
		{ip: "2001:db8::1", expected: "2001:db8::1/128", expectIPv6: true},
		{ip: "::ffff:192.0.2.1", expected: "192.0.2.1/32"},
		{ip: "fe80::1%eth0", expectErr: true},
		{ip: "", expectErr: true},
		{ip: "not-an-ip", expectErr: true},
	}
	for _, c := range cases {
		got, isIPv6, err := getAPIServerCIDR(c.ip)
		if c.expectErr {
			if err == nil {
				t.Errorf("getAPIServerCIDR(%q) = %q, expected an error", c.ip, got)
			}
			continue
		}
		if err != nil || got != c.expected || isIPv6 != c.expectIPv6 {
			t.Errorf("getAPIServerCIDR(%q) = (%q, %t, %v), expected (%q, %t, nil)", c.ip, got, isIPv6, err, c.expected, c.expectIPv6)
		}
	}
}

func TestCreatePolicyAllowAPIServerUsesAllEndpointSlices(t *testing.T) {
	notReady := false
	cases := []struct {
		name        string
		slices      []*discoveryv1.EndpointSlice
		expectCIDRs []string
		expectPorts []int64
		expectIPv6  bool
		expectErr   bool
	}{
		{
			name: "ha-ipv6-multiple-slices-and-port-6443",
			slices: []*discoveryv1.EndpointSlice{
				apiServerEndpointSlice("kubernetes-a", discoveryv1.AddressTypeIPv6,
					[]discoveryv1.Endpoint{readyEndpoint("2001:db8::1"), readyEndpoint("2001:db8::2")},
					tcpPort(6443), tcpPort(443), udpPort(53)),
				apiServerEndpointSlice("kubernetes-b", discoveryv1.AddressTypeIPv6,
					[]discoveryv1.Endpoint{readyEndpoint("2001:db8::2"), readyEndpoint("2001:db8::3")},
					discoveryv1.EndpointPort{Port: ptr.To[int32](8443)}),
			},
			expectCIDRs: []string{"2001:db8::1/128", "2001:db8::2/128", "2001:db8::3/128"},
			expectPorts: []int64{443, 80, 6443, 8443},
			expectIPv6:  true,
		},
		{
			name: "ipv4-empty-slice-and-not-ready-endpoint",
			slices: []*discoveryv1.EndpointSlice{
				apiServerEndpointSlice("kubernetes-a", discoveryv1.AddressTypeIPv4, nil),
				apiServerEndpointSlice("kubernetes-b", discoveryv1.AddressTypeIPv4,
					[]discoveryv1.Endpoint{
						readyEndpoint("192.0.2.2"),
						{Addresses: []string{"192.0.2.3"}, Conditions: discoveryv1.EndpointConditions{Ready: &notReady}},
						readyEndpoint("192.0.2.4"),
					},
					tcpPort(6443)),
			},
			expectCIDRs: []string{"192.0.2.2/32", "192.0.2.4/32"},
			expectPorts: []int64{443, 80, 6443},
		},
		{
			name: "only-fqdn-slice",
			slices: []*discoveryv1.EndpointSlice{
				apiServerEndpointSlice("kubernetes-a", discoveryv1.AddressTypeFQDN,
					[]discoveryv1.Endpoint{readyEndpoint("apiserver.example.com")}, tcpPort(6443)),
			},
			expectErr: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv("")
			for _, slice := range c.slices {
				if _, err := env.clientset.DiscoveryV1().EndpointSlices(corev1.NamespaceDefault).Create(context.TODO(), slice, metav1.CreateOptions{}); err != nil {
					t.Fatalf("failed to create EndpointSlice: %v", err)
				}
			}

			nps := &networkPolicyEnforcementMeasurement{}
			_, err := nps.Execute(setupConfig(env.framework))
			if c.expectErr {
				if err == nil {
					t.Fatalf("expected setup error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			if nps.isIPv6 != c.expectIPv6 {
				t.Errorf("isIPv6 = %t, expected %t", nps.isIPv6, c.expectIPv6)
			}

			gotCIDRs := env.ipBlockCIDRs(t, netPolicyTestNamespace)
			slices.Sort(gotCIDRs)
			if !slices.Equal(gotCIDRs, c.expectCIDRs) {
				t.Errorf("allow-egress-apiserver CIDRs = %v, expected %v", gotCIDRs, c.expectCIDRs)
			}

			npObj, err := env.dynamic.Resource(networkPolicyGVR).Namespace(netPolicyTestNamespace).Get(context.TODO(), "allow-egress-apiserver", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get allow-egress-apiserver: %v", err)
			}
			egress, _, _ := unstructured.NestedSlice(npObj.Object, "spec", "egress")
			if len(egress) != 1 {
				t.Fatalf("egress rules = %d, expected 1", len(egress))
			}
			rawPorts, _, _ := unstructured.NestedSlice(egress[0].(map[string]interface{}), "ports")
			gotPorts := make([]int64, 0, len(rawPorts))
			for _, rawPort := range rawPorts {
				pm := rawPort.(map[string]interface{})
				port, _, _ := unstructured.NestedInt64(pm, "port")
				if protocol, _, _ := unstructured.NestedString(pm, "protocol"); protocol != string(corev1.ProtocolTCP) {
					t.Errorf("port %d protocol = %q, expected %q", port, protocol, corev1.ProtocolTCP)
				}
				gotPorts = append(gotPorts, port)
			}
			slices.Sort(gotPorts)
			expectPorts := slices.Sorted(slices.Values(c.expectPorts))
			if !slices.Equal(gotPorts, expectPorts) {
				t.Errorf("allow-egress-apiserver ports = %v, expected %v", gotPorts, expectPorts)
			}
		})
	}
}

func apiServerEndpointSlice(name string, addressType discoveryv1.AddressType, endpoints []discoveryv1.Endpoint, ports ...discoveryv1.EndpointPort) *discoveryv1.EndpointSlice {
	return &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: corev1.NamespaceDefault,
			Labels:    map[string]string{discoveryv1.LabelServiceName: kubernetesServiceName},
		},
		AddressType: addressType,
		Endpoints:   endpoints,
		Ports:       ports,
	}
}

func readyEndpoint(addr string) discoveryv1.Endpoint {
	return discoveryv1.Endpoint{Addresses: []string{addr}, Conditions: discoveryv1.EndpointConditions{Ready: ptr.To(true)}}
}

func tcpPort(port int32) discoveryv1.EndpointPort {
	return discoveryv1.EndpointPort{Port: ptr.To(port), Protocol: ptr.To(corev1.ProtocolTCP)}
}

func udpPort(port int32) discoveryv1.EndpointPort {
	return discoveryv1.EndpointPort{Port: ptr.To(port), Protocol: ptr.To(corev1.ProtocolUDP)}
}

func TestGetBaseCIDR(t *testing.T) {
	cases := []struct {
		nsIdx    int
		isIPv6   bool
		expected string
	}{
		{0, false, "10.0.0.0/24"},
		{255, false, "10.0.255.0/24"},
		{256, false, "10.0.0.0/24"},
		{0, true, "fd00:10::/64"},
		{10, true, "fd00:10:0:a::/64"},
		{255, true, "fd00:10:0:ff::/64"},
	}
	for _, c := range cases {
		if got := getBaseCIDR(c.nsIdx, c.isIPv6); got != c.expected {
			t.Errorf("getBaseCIDR(%d, %t) = %q, expected %q", c.nsIdx, c.isIPv6, got, c.expected)
		}
	}
}

type testEnv struct {
	clientset *fake.Clientset
	dynamic   *dynamicfake.FakeDynamicClient
	framework *framework.Framework
}

// newTestEnv returns a fake cluster with two test namespaces and, when
// apiServerIP is set, a kubernetes EndpointSlice with that address.
func newTestEnv(apiServerIP string) *testEnv {
	objects := []runtime.Object{
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns-1"}},
		&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "test-ns-2"}},
	}
	if apiServerIP != "" {
		addressType := discoveryv1.AddressTypeIPv4
		if strings.Contains(apiServerIP, ":") {
			addressType = discoveryv1.AddressTypeIPv6
		}
		objects = append(objects, apiServerEndpointSlice(kubernetesServiceName, addressType,
			[]discoveryv1.Endpoint{readyEndpoint(apiServerIP)}, tcpPort(443)))
	}
	clientset := fake.NewSimpleClientset(objects...)
	dynamicClient := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(runtime.NewScheme(), map[schema.GroupVersionResource]string{
		networkPolicyGVR: "NetworkPolicyList",
		{Group: "", Version: "v1", Resource: "serviceaccounts"}:                              "ServiceAccountList",
		{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles"}:        "ClusterRoleList",
		{Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings"}: "ClusterRoleBindingList",
	})
	f := framework.NewFrameworkFromClients(framework.NewMultiClientSetFromClients(clientset), framework.NewMultiDynamicClientFromClients(dynamicClient))
	f.SetAutomanagedNamespacePrefix("test-ns")
	return &testEnv{clientset: clientset, dynamic: dynamicClient, framework: f}
}

func (e *testEnv) ipBlockCIDRs(t *testing.T, namespace string) []string {
	t.Helper()
	list, err := e.dynamic.Resource(networkPolicyGVR).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list network policies in %q: %v", namespace, err)
	}
	var cidrs []string
	for _, item := range list.Items {
		egress, _, _ := unstructured.NestedSlice(item.Object, "spec", "egress")
		for _, rule := range egress {
			peers, _, _ := unstructured.NestedSlice(rule.(map[string]interface{}), "to")
			for _, peer := range peers {
				if cidr, ok, _ := unstructured.NestedString(peer.(map[string]interface{}), "ipBlock", "cidr"); ok {
					cidrs = append(cidrs, cidr)
				}
			}
		}
	}
	return cidrs
}

func setupConfig(f *framework.Framework) *measurement.Config {
	return &measurement.Config{
		ClusterFramework: f,
		Params: map[string]interface{}{
			"action":                      "setup",
			"targetLabelValue":            "enforcement-latency",
			"testClientNodeSelectorValue": "net-policy-client",
		},
	}
}

func TestSetupSelectsIPFamilyFromAPIServerAddress(t *testing.T) {
	cases := []struct {
		name               string
		apiServerIP        string
		existingPolicy     bool
		expectIPv6         bool
		expectAPIServer    string
		expectLoadPolicies string
	}{
		{
			name:               "192.0.2.2",
			apiServerIP:        "192.0.2.2",
			expectAPIServer:    "192.0.2.2/32",
			expectLoadPolicies: "10.0.0.0/24",
		},
		{
			name:               "2001:db8::2",
			apiServerIP:        "2001:db8::2",
			expectIPv6:         true,
			expectAPIServer:    "2001:db8::2/128",
			expectLoadPolicies: "fd00:10::/64",
		},
		{
			name:               "2001:db8::2 with existing allow-egress-apiserver",
			apiServerIP:        "2001:db8::2",
			existingPolicy:     true,
			expectIPv6:         true,
			expectLoadPolicies: "fd00:10::/64",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(c.apiServerIP)
			if c.existingPolicy {
				_, err := env.clientset.NetworkingV1().NetworkPolicies(netPolicyTestNamespace).Create(
					context.TODO(),
					&networkingv1.NetworkPolicy{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "allow-egress-apiserver",
							Namespace: netPolicyTestNamespace,
						},
					},
					metav1.CreateOptions{},
				)
				if err != nil {
					t.Fatalf("failed to create pre-existing allow-egress-apiserver policy: %v", err)
				}
			}
			nps := &networkPolicyEnforcementMeasurement{}
			if _, err := nps.Execute(setupConfig(env.framework)); err != nil {
				t.Fatalf("setup failed: %v", err)
			}
			if nps.isIPv6 != c.expectIPv6 {
				t.Errorf("isIPv6 = %t, expected %t", nps.isIPv6, c.expectIPv6)
			}
			if c.existingPolicy {
				if got := env.ipBlockCIDRs(t, netPolicyTestNamespace); len(got) != 0 {
					t.Errorf("allow-egress-apiserver CIDRs = %q, expected [] when policy already exists", got)
				}
			} else if got := env.ipBlockCIDRs(t, netPolicyTestNamespace); len(got) != 1 || got[0] != c.expectAPIServer {
				t.Errorf("allow-egress-apiserver CIDRs = %q, expected [%q]", got, c.expectAPIServer)
			}

			nps.createLoadPolicies(&measurement.Config{Params: map[string]interface{}{"policyLoadCount": 2, "policyLoadQPS": 100}})
			if got := env.ipBlockCIDRs(t, nps.targetNamespaces[0]); len(got) != 1 || got[0] != c.expectLoadPolicies {
				t.Errorf("load policy CIDRs in %q = %q, expected [%q]", nps.targetNamespaces[0], got, c.expectLoadPolicies)
			}
		})
	}
}

func TestSetupRetriesEndpointSliceList(t *testing.T) {
	env := newTestEnv("2001:db8::2")
	calls := 0
	env.clientset.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls == 1 {
			return true, nil, apierrors.NewTooManyRequests("throttled", 0)
		}
		return false, nil, nil
	})

	nps := &networkPolicyEnforcementMeasurement{}
	if _, err := nps.Execute(setupConfig(env.framework)); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if calls != 2 {
		t.Errorf("EndpointSlice LIST calls = %d, expected 2", calls)
	}
	if !nps.isIPv6 {
		t.Errorf("isIPv6 = false after a retried EndpointSlice LIST, expected true")
	}
}

func TestBaselineSetupSkipsEndpointSliceList(t *testing.T) {
	env := newTestEnv("192.0.2.2")
	calls := 0
	env.clientset.PrependReactor("list", "endpointslices", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		return false, nil, nil
	})

	config := setupConfig(env.framework)
	config.Params["baseline"] = true
	nps := &networkPolicyEnforcementMeasurement{}
	if _, err := nps.Execute(config); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if calls != 0 {
		t.Errorf("EndpointSlice LIST calls = %d, expected 0", calls)
	}
}
