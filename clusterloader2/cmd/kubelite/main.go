/*
Copyright 2025 The Kubernetes Authors.

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

// Command kubelite simulates Kubernetes worker nodes (kubelet and kube-proxy)
// inside a single process to reproduce control-plane watch fanout, Go runtime
// scheduler contention, and write-path load without provisioning worker VMs:
//   - Two dedicated HTTP/2 TLS connections per node (impersonating
//     system:node:<name> and system:kube-proxy).
//   - 11 baseline watches per node plus refcounted per-pod ConfigMap/Secret
//     watches (matching kubelet watchBasedManager), draining non-pod watch
//     streams into io.Discard to avoid client-side informer cache duplication.
//   - 50%-jittered Lease heartbeats, periodic 4.9 KB NodeStatus patches, and
//     3-stage PodStatus transitions (Pending -> Started -> Ready) preceded by
//     GET /pods/{name} (matching kubelet status_manager).
package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	retrywatcher "k8s.io/client-go/tools/watch"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/component-helpers/apimachinery/lease"
	nodeutil "k8s.io/component-helpers/node/util"
	"k8s.io/klog/v2"
	cl2client "k8s.io/perf-tests/clusterloader2/pkg/framework/client"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
)

const (
	kubeletUserAgent   = "kubelet/v1.36.1 (linux/amd64) kubernetes/kubelite"
	kubeProxyUserAgent = "kube-proxy/v1.36.1 (linux/amd64) kubernetes/kubelite"
	simulatedLabelKey  = "kubelite.k8s.io/simulated"
	taintSimulatedKey  = "kubelite.io/simulated"
	pauseImageID       = "registry.k8s.io/pause@sha256:7031c1b283388d2c2e09b57badb803c05ebed362dc88d84b480cc47f72a21097"
)

var defaultNodeImages = func() []corev1.ContainerImage {
	imgs := make([]corev1.ContainerImage, 25)
	imgs[0] = corev1.ContainerImage{Names: []string{"registry.k8s.io/pause:3.9", pauseImageID}, SizeBytes: 321520}
	for i := 1; i < len(imgs); i++ {
		imgs[i] = corev1.ContainerImage{
			Names:     []string{fmt.Sprintf("registry.k8s.io/e2e-test-images/agnhost:2.%d", i), fmt.Sprintf("registry.k8s.io/e2e-test-images/agnhost@sha256:%064x", i+1000)},
			SizeBytes: int64(25000000 + i*1048576),
		}
	}
	return imgs
}()

type Config struct {
	Kubeconfig, NodePrefix, PVCNamespacePrefix                                 string
	NumNodes, RegisterConcurrency, PodWorkers, MaxInflightMut, PodStatusStages int
	LeaseInterval, NodeStatusInterval, PodStartupDelay, Jitter                 time.Duration
	TaintSimulated, CleanupOnExit                                              bool
}

type SimNode struct {
	Index                      int
	Name, InternalIP, Zone     string
	UID                        types.UID
	KubeletClient              *kubernetes.Clientset
	KubeletHTTP, KubeProxyHTTP *http.Client
	TransitionTime             metav1.Time
	Capacity                   corev1.ResourceList
	volMu                      sync.Mutex
	volRefCnt                  map[string]int
	volCancels                 map[string]context.CancelFunc
	podVols                    map[types.UID][]string
}

type impersonatingRoundTripper struct {
	base             http.RoundTripper
	bearer, user, ua string
	groups           []string
}

func (rt *impersonatingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	if rt.bearer != "" {
		req2.Header.Set("Authorization", "Bearer "+rt.bearer)
	}
	req2.Header.Set("Impersonate-User", rt.user)
	req2.Header["Impersonate-Group"] = rt.groups
	if req2.Header.Get("User-Agent") == "" {
		req2.Header.Set("User-Agent", rt.ua)
	}
	return rt.base.RoundTrip(req2)
}

type Simulator struct {
	cfg          Config
	baseURL      string
	isLoopback   bool
	adminClient  *kubernetes.Clientset
	tlsConfig    *tls.Config
	nodesByName  map[string]*SimNode
	nodes        []*SimNode
	podMutateSem chan struct{}
	podWorkCh    chan *corev1.Pod
	seenUIDs     sync.Map // types.UID -> int (1=started, 2=deleted)
}

func main() {
	var cfg Config
	flag.StringVar(&cfg.Kubeconfig, "kubeconfig", os.Getenv("KUBECONFIG"), "Path to kubeconfig")
	flag.IntVar(&cfg.NumNodes, "nodes", 5000, "Number of simulated nodes")
	flag.StringVar(&cfg.NodePrefix, "node-prefix", "kubelite-", "Simulated node name prefix")
	flag.StringVar(&cfg.PVCNamespacePrefix, "pvc-namespace-prefix", "test-", "Namespace prefix for auto-binding Pending PVCs")
	flag.IntVar(&cfg.RegisterConcurrency, "register-concurrency", 128, "Concurrency for initial node registration")
	flag.IntVar(&cfg.PodWorkers, "pod-workers", 512, "Concurrent workers actuating pod lifecycle transitions")
	flag.IntVar(&cfg.MaxInflightMut, "max-inflight-pod-mutations", 96, "Max concurrent in-flight pod status/event/delete mutations")
	flag.IntVar(&cfg.PodStatusStages, "pod-status-stages", 3, "Number of PodStatus PATCH stages per pod startup (1 or 3)")
	flag.DurationVar(&cfg.LeaseInterval, "lease-interval", 10*time.Second, "Per-node Lease heartbeat interval")
	flag.DurationVar(&cfg.NodeStatusInterval, "node-status-interval", 90*time.Second, "Per-node NodeStatus PATCH interval")
	flag.DurationVar(&cfg.PodStartupDelay, "pod-startup-delay", 150*time.Millisecond, "Minimum delay before patching PodStatus")
	flag.DurationVar(&cfg.Jitter, "pod-startup-jitter", 350*time.Millisecond, "Random startup jitter added to pod-startup-delay")
	flag.BoolVar(&cfg.TaintSimulated, "taint-simulated", true, "Taint simulated nodes with kubelite.io/simulated=true:NoSchedule")
	flag.BoolVar(&cfg.CleanupOnExit, "cleanup-on-exit", false, "Delete simulated nodes on SIGINT/SIGTERM")
	klog.InitFlags(nil)
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	sim, err := NewSimulator(cfg)
	if err != nil {
		klog.Fatalf("Failed to initialize kubelite: %v", err)
	}
	if err := sim.Run(ctx); err != nil && ctx.Err() == nil {
		klog.Fatalf("kubelite failed: %v", err)
	}
}

func NewSimulator(cfg Config) (*Simulator, error) {
	restCfg, err := clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %q: %w", cfg.Kubeconfig, err)
	}
	restCfg.QPS, restCfg.Burst = 2000, 4000
	restCfg.ContentType = k8sruntime.ContentTypeProtobuf
	restCfg.AcceptContentTypes = k8sruntime.ContentTypeProtobuf + "," + k8sruntime.ContentTypeJSON

	adminClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(restCfg.Host)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := rest.TLSConfigFor(restCfg)
	if err != nil {
		return nil, err
	}
	tlsCfg.NextProtos = []string{"h2", "http/1.1"}
	tlsCfg.ClientSessionCache = tls.NewLRUClientSessionCache(256)

	sim := &Simulator{
		cfg:          cfg,
		baseURL:      strings.TrimRight(restCfg.Host, "/"),
		isLoopback:   u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost",
		adminClient:  adminClient,
		tlsConfig:    tlsCfg,
		nodesByName:  make(map[string]*SimNode, cfg.NumNodes),
		nodes:        make([]*SimNode, cfg.NumNodes),
		podWorkCh:    make(chan *corev1.Pod, 131072),
		podMutateSem: make(chan struct{}, max(1, cfg.MaxInflightMut)),
	}
	capList := corev1.ResourceList{
		corev1.ResourceCPU:              resource.MustParse("4"),
		corev1.ResourceMemory:           resource.MustParse("16Gi"),
		corev1.ResourcePods:             resource.MustParse("110"),
		corev1.ResourceEphemeralStorage: resource.MustParse("100Gi"),
	}
	nodeRestCfg := rest.CopyConfig(restCfg)
	nodeRestCfg.RateLimiter = flowcontrol.NewFakeAlwaysRateLimiter()
	zones := []string{"us-east1-b", "us-east1-c", "us-east1-d"}

	for i := 0; i < cfg.NumNodes; i++ {
		name := fmt.Sprintf("%s%04d", cfg.NodePrefix, i)
		kubeletHTTP := sim.newNodeHTTPClient(i*2, "system:node:"+name, []string{"system:nodes", "system:authenticated"}, restCfg.BearerToken, kubeletUserAgent)
		kubeProxyHTTP := sim.newNodeHTTPClient(i*2+1, "system:kube-proxy", []string{"system:authenticated"}, restCfg.BearerToken, kubeProxyUserAgent)
		kubeletClient, err := kubernetes.NewForConfigAndClient(nodeRestCfg, kubeletHTTP)
		if err != nil {
			return nil, err
		}
		n := &SimNode{
			Index: i, Name: name, InternalIP: fmt.Sprintf("10.128.%d.%d", (i/250)+1, (i%250)+2),
			Zone: zones[i%len(zones)], KubeletClient: kubeletClient, KubeletHTTP: kubeletHTTP, KubeProxyHTTP: kubeProxyHTTP, Capacity: capList,
			volRefCnt: make(map[string]int), volCancels: make(map[string]context.CancelFunc), podVols: make(map[types.UID][]string),
		}
		sim.nodes[i], sim.nodesByName[name] = n, n
	}
	return sim, nil
}

func (s *Simulator) newNodeHTTPClient(connIdx int, user string, groups []string, bearer, ua string) *http.Client {
	dialer := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	if s.isLoopback {
		dialer.LocalAddr = &net.TCPAddr{IP: net.IPv4(127, 0, byte(1+(connIdx/200)), byte(1+(connIdx%200)))}
	}
	tr := &http.Transport{
		DialContext: dialer.DialContext, TLSClientConfig: s.tlsConfig.Clone(), ForceAttemptHTTP2: true,
		MaxIdleConnsPerHost: 2, IdleConnTimeout: 90 * time.Second, ReadBufferSize: 8192, WriteBufferSize: 8192,
	}
	return &http.Client{Transport: &impersonatingRoundTripper{base: tr, bearer: bearer, user: user, groups: groups, ua: ua}}
}

func (s *Simulator) ensureNodeWatchRBAC(ctx context.Context) {
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "kubelite-node-watches"},
		Rules: []rbacv1.PolicyRule{
			{APIGroups: []string{"storage.k8s.io"}, Resources: []string{"csinodes"}, Verbs: []string{"get", "list", "watch"}},
			{APIGroups: []string{""}, Resources: []string{"configmaps", "secrets"}, Verbs: []string{"get", "list", "watch"}},
		},
	}
	if _, err := s.adminClient.RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{}); apierrors.IsAlreadyExists(err) {
		_, _ = s.adminClient.RbacV1().ClusterRoles().Update(ctx, cr, metav1.UpdateOptions{})
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "kubelite-node-watches"},
		RoleRef:    rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "kubelite-node-watches"},
		Subjects:   []rbacv1.Subject{{APIGroup: "rbac.authorization.k8s.io", Kind: "Group", Name: "system:nodes"}},
	}
	if _, err := s.adminClient.RbacV1().ClusterRoleBindings().Create(ctx, crb, metav1.CreateOptions{}); apierrors.IsAlreadyExists(err) {
		_, _ = s.adminClient.RbacV1().ClusterRoleBindings().Update(ctx, crb, metav1.UpdateOptions{})
	}
}

func (s *Simulator) Run(ctx context.Context) error {
	start := time.Now()
	s.ensureNodeWatchRBAC(ctx)
	sem := make(chan struct{}, max(1, s.cfg.RegisterConcurrency))
	var wg sync.WaitGroup
	var firstErr atomic.Value
	for _, n := range s.nodes {
		wg.Add(1)
		sem <- struct{}{}
		go func(node *SimNode) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := s.registerSingleNode(ctx, node); err != nil {
				firstErr.CompareAndSwap(nil, err)
			}
		}(n)
	}
	wg.Wait()
	if v := firstErr.Load(); v != nil {
		return v.(error)
	}
	klog.Infof("Registered %d simulated nodes in %v", s.cfg.NumNodes, time.Since(start).Round(time.Millisecond))

	for w := 0; w < s.cfg.PodWorkers; w++ {
		go s.podActuatorWorker(ctx)
	}
	go s.watchAndBindPVCs(ctx)

	for _, n := range s.nodes {
		go s.runNodeLeaseController(ctx, n)
		go s.runNodeStatusLoop(ctx, n)
		s.startNodeWatches(ctx, n)
	}
	<-ctx.Done()
	if s.cfg.CleanupOnExit {
		cctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		_ = s.adminClient.CoreV1().Nodes().DeleteCollection(cctx, metav1.DeleteOptions{}, metav1.ListOptions{LabelSelector: simulatedLabelKey + "=true"})
	}
	return nil
}

func (s *Simulator) buildNode(n *SimNode, hb metav1.Time) *corev1.Node {
	cidr := fmt.Sprintf("10.%d.%d.0/24", 64+(n.Index/256), n.Index%256)
	var taints []corev1.Taint
	if s.cfg.TaintSimulated {
		taints = []corev1.Taint{{Key: taintSimulatedKey, Value: "true", Effect: corev1.TaintEffectNoSchedule}}
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: n.Name, UID: n.UID,
			Labels: map[string]string{
				"kubernetes.io/hostname": n.Name, "kubernetes.io/os": "linux", "kubernetes.io/arch": "amd64",
				"kubernetes.io/role": "node", "node-role.kubernetes.io/node": "", "topology.kubernetes.io/region": "us-east1",
				"topology.kubernetes.io/zone": n.Zone, "node.kubernetes.io/instance-type": "e2-medium",
				simulatedLabelKey: "true", taintSimulatedKey: "true",
			},
		},
		Spec: corev1.NodeSpec{PodCIDR: cidr, PodCIDRs: []string{cidr}, ProviderID: fmt.Sprintf("gce://kubelite/%s/%s", n.Zone, n.Name), Taints: taints},
		Status: corev1.NodeStatus{
			Capacity: n.Capacity, Allocatable: n.Capacity, Phase: corev1.NodeRunning,
			Addresses: []corev1.NodeAddress{{Type: corev1.NodeInternalIP, Address: n.InternalIP}, {Type: corev1.NodeHostName, Address: n.Name}},
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: corev1.ConditionTrue, LastHeartbeatTime: hb, LastTransitionTime: n.TransitionTime, Reason: "KubeletReady"},
				{Type: corev1.NodeMemoryPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: hb, LastTransitionTime: n.TransitionTime, Reason: "KubeletHasSufficientMemory"},
				{Type: corev1.NodeDiskPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: hb, LastTransitionTime: n.TransitionTime, Reason: "KubeletHasNoDiskPressure"},
				{Type: corev1.NodePIDPressure, Status: corev1.ConditionFalse, LastHeartbeatTime: hb, LastTransitionTime: n.TransitionTime, Reason: "KubeletHasSufficientPID"},
				{Type: corev1.NodeNetworkUnavailable, Status: corev1.ConditionFalse, LastHeartbeatTime: hb, LastTransitionTime: n.TransitionTime, Reason: "RouteCreated"},
			},
			NodeInfo: corev1.NodeSystemInfo{KubeletVersion: "v1.36.1", KubeProxyVersion: "v1.36.1", ContainerRuntimeVersion: "containerd://2.3.0", OperatingSystem: "linux", Architecture: "amd64"},
			Images:   defaultNodeImages,
		},
	}
}

func (s *Simulator) patchNodeStatus(n *SimNode, hb metav1.Time) error {
	newNode := s.buildNode(n, hb)
	baseNode := newNode.DeepCopy()
	baseNode.Status = corev1.NodeStatus{}
	_, _, err := nodeutil.PatchNodeStatus(n.KubeletClient.CoreV1(), types.NodeName(n.Name), baseNode, newNode)
	return err
}

func (s *Simulator) registerSingleNode(ctx context.Context, n *SimNode) error {
	now := metav1.Now()
	n.TransitionTime = now
	created, err := s.adminClient.CoreV1().Nodes().Create(ctx, s.buildNode(n, now), metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = s.adminClient.CoreV1().Nodes().Get(ctx, n.Name, metav1.GetOptions{})
	}
	if err != nil {
		return fmt.Errorf("creating node %s: %w", n.Name, err)
	}
	n.UID = created.UID
	return s.patchNodeStatus(n, now)
}

func (s *Simulator) runNodeLeaseController(ctx context.Context, n *SimNode) {
	time.Sleep(time.Duration(int64(n.Index) * int64(s.cfg.LeaseInterval) / int64(max(1, s.cfg.NumNodes))))
	lease.NewController(clock.RealClock{}, n.KubeletClient, n.Name, 40, nil, s.cfg.LeaseInterval, n.Name, corev1.NamespaceNodeLease,
		func(l *coordinationv1.Lease) error {
			if len(l.OwnerReferences) == 0 {
				l.OwnerReferences = []metav1.OwnerReference{{APIVersion: "v1", Kind: "Node", Name: n.Name, UID: n.UID}}
			}
			return nil
		}).Run(ctx)
}

func (s *Simulator) runNodeStatusLoop(ctx context.Context, n *SimNode) {
	if s.cfg.NodeStatusInterval <= 0 {
		return
	}
	time.Sleep(time.Duration(int64(n.Index) * int64(s.cfg.NodeStatusInterval) / int64(max(1, s.cfg.NumNodes))))
	wait.UntilWithContext(ctx, func(_ context.Context) { _ = s.patchNodeStatus(n, metav1.Now()) }, s.cfg.NodeStatusInterval)
}

func (s *Simulator) startNodeWatches(ctx context.Context, n *SimNode) {
	stagger := time.Duration(int64(n.Index) * int64(6*time.Second) / int64(max(1, s.cfg.NumNodes)))
	go s.runNodePodWatch(ctx, n, stagger)

	specs := []struct {
		client   *http.Client
		ua, path string
	}{
		{n.KubeletHTTP, kubeletUserAgent, "/api/v1/nodes?watch=true&fieldSelector=metadata.name%3D" + n.Name},
		{n.KubeProxyHTTP, kubeProxyUserAgent, "/api/v1/services?watch=true&labelSelector=%21service.kubernetes.io%2Fservice-proxy-name"},
		{n.KubeProxyHTTP, kubeProxyUserAgent, "/apis/discovery.k8s.io/v1/endpointslices?watch=true&labelSelector=%21service.kubernetes.io%2Fheadless"},
		{n.KubeletHTTP, kubeletUserAgent, "/api/v1/services?watch=true"},
		{n.KubeProxyHTTP, kubeProxyUserAgent, "/api/v1/nodes?watch=true&fieldSelector=metadata.name%3D" + n.Name},
		{n.KubeletHTTP, kubeletUserAgent, "/apis/storage.k8s.io/v1/csidrivers?watch=true"},
		{n.KubeletHTTP, kubeletUserAgent, "/apis/storage.k8s.io/v1/csinodes?watch=true&fieldSelector=metadata.name%3D" + n.Name},
		{n.KubeletHTTP, kubeletUserAgent, "/apis/node.k8s.io/v1/runtimeclasses?watch=true"},
		{n.KubeProxyHTTP, kubeProxyUserAgent, "/apis/networking.k8s.io/v1/servicecidrs?watch=true"},
		{n.KubeletHTTP, kubeletUserAgent, "/api/v1/namespaces/kube-system/configmaps?watch=true&fieldSelector=metadata.name%3Dkube-root-ca.crt"},
	}
	for i, sp := range specs {
		go s.runWatchStream(ctx, sp.client, stagger+time.Duration(i+1)*40*time.Millisecond, sp.ua, s.baseURL+sp.path)
	}
}

func (s *Simulator) runWatchStream(ctx context.Context, client *http.Client, delay time.Duration, ua, watchURL string) {
	if delay > 0 {
		time.Sleep(delay)
	}
	rv := "0"
	for ctx.Err() == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s&allowWatchBookmarks=true&resourceVersion=%s&timeoutSeconds=%d", watchURL, rv, 7200+rand.IntN(3600)), nil)
		if err != nil {
			return
		}
		req.Header.Set("Accept", k8sruntime.ContentTypeProtobuf)
		req.Header.Set("User-Agent", ua)
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if latest := s.fetchLatestRV(ctx, client, watchURL); latest != "" {
				rv = latest
			}
		} else if resp != nil {
			code := resp.StatusCode
			_ = resp.Body.Close()
			if code == http.StatusForbidden || code == http.StatusNotFound {
				return
			}
		}
		time.Sleep(time.Duration(500+rand.IntN(1500)) * time.Millisecond)
	}
}

func (s *Simulator) fetchLatestRV(ctx context.Context, client *http.Client, watchURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.Replace(watchURL, "watch=true", "limit=1&resourceVersion=0", 1), nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil || resp == nil {
		return ""
	}
	defer resp.Body.Close()
	var meta struct {
		Metadata metav1.ListMeta `json:"metadata"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&meta)
	return meta.Metadata.ResourceVersion
}

func extractPodVolumeWatchPaths(pod *corev1.Pod) []string {
	seen := make(map[string]struct{}, len(pod.Spec.Volumes)+2)
	add := func(res, name string) {
		if name != "" {
			seen[fmt.Sprintf("/api/v1/namespaces/%s/%s?watch=true&fieldSelector=metadata.name%%3D%s", pod.Namespace, res, name)] = struct{}{}
		}
	}
	for _, v := range pod.Spec.Volumes {
		if v.ConfigMap != nil {
			add("configmaps", v.ConfigMap.Name)
		}
		if v.Secret != nil {
			add("secrets", v.Secret.SecretName)
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.ConfigMap != nil {
					add("configmaps", src.ConfigMap.Name)
				}
				if src.Secret != nil {
					add("secrets", src.Secret.Name)
				}
			}
		}
	}
	paths := make([]string, 0, len(seen))
	for p := range seen {
		paths = append(paths, p)
	}
	return paths
}

func (s *Simulator) syncPodVolumeWatches(ctx context.Context, n *SimNode, pod *corev1.Pod, active bool) {
	n.volMu.Lock()
	defer n.volMu.Unlock()
	if active {
		if _, exists := n.podVols[pod.UID]; exists {
			return
		}
		paths := extractPodVolumeWatchPaths(pod)
		n.podVols[pod.UID] = paths
		for _, p := range paths {
			n.volRefCnt[p]++
			if n.volRefCnt[p] == 1 {
				wctx, cancel := context.WithCancel(ctx)
				n.volCancels[p] = cancel
				go s.runWatchStream(wctx, n.KubeletHTTP, time.Duration(rand.IntN(100))*time.Millisecond, kubeletUserAgent, s.baseURL+p)
			}
		}
		return
	}
	paths, exists := n.podVols[pod.UID]
	if !exists {
		return
	}
	delete(n.podVols, pod.UID)
	for _, p := range paths {
		n.volRefCnt[p]--
		if n.volRefCnt[p] <= 0 {
			delete(n.volRefCnt, p)
			if cancel := n.volCancels[p]; cancel != nil {
				cancel()
			}
			delete(n.volCancels, p)
		}
	}
}

func (s *Simulator) runNodePodWatch(ctx context.Context, n *SimNode, delay time.Duration) {
	time.Sleep(delay)
	fieldSel := "spec.nodeName=" + n.Name
	lw := &cache.ListWatch{
		WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector, opts.AllowWatchBookmarks = fieldSel, true
			return n.KubeletClient.CoreV1().Pods("").Watch(ctx, opts)
		},
	}
	for ctx.Err() == nil {
		list, err := n.KubeletClient.CoreV1().Pods("").List(ctx, metav1.ListOptions{FieldSelector: fieldSel, ResourceVersion: "0"})
		if err != nil || list.ResourceVersion == "" {
			time.Sleep(time.Second)
			continue
		}
		for i := range list.Items {
			s.enqueuePod(&list.Items[i])
		}
		if rw, err := retrywatcher.NewRetryWatcherWithContext(ctx, list.ResourceVersion, lw); err == nil {
			for ev := range rw.ResultChan() {
				if pod, ok := ev.Object.(*corev1.Pod); ok {
					if ev.Type == watch.Deleted {
						s.syncPodVolumeWatches(ctx, n, pod, false)
						s.seenUIDs.Delete(pod.UID)
					} else {
						s.enqueuePod(pod)
					}
				}
			}
		}
		time.Sleep(time.Second)
	}
}

func (s *Simulator) enqueuePod(pod *corev1.Pod) {
	if pod.Spec.NodeName == "" || s.nodesByName[pod.Spec.NodeName] == nil {
		return
	}
	if pod.DeletionTimestamp != nil {
		if prev, _ := s.seenUIDs.Swap(pod.UID, 2); prev != 2 {
			s.podWorkCh <- pod
		}
	} else if pod.Status.Phase == corev1.PodPending {
		if _, loaded := s.seenUIDs.LoadOrStore(pod.UID, 1); !loaded {
			s.podWorkCh <- pod
		}
	}
}

func (s *Simulator) podActuatorWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case pod := <-s.podWorkCh:
			n := s.nodesByName[pod.Spec.NodeName]
			if n == nil {
				continue
			}
			if pod.DeletionTimestamp != nil {
				s.syncPodVolumeWatches(ctx, n, pod, false)
				if s.cfg.PodStatusStages >= 3 {
					_ = s.patchPodStatusStage(ctx, n, pod, corev1.PodRunning, false, true, "", false)
				}
				_ = s.patchPodStatusStage(ctx, n, pod, corev1.PodRunning, false, false, "", true)
				_ = s.withMutateSem(ctx, func() error {
					return cl2client.RetryWithExponentialBackOff(cl2client.RetryFunction(func() error {
						return n.KubeletClient.CoreV1().Pods(pod.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
							GracePeriodSeconds: ptr.To(int64(0)), Preconditions: &metav1.Preconditions{UID: &pod.UID},
						})
					}, cl2client.Allow(apierrors.IsNotFound), cl2client.Retry(isRetryableErr)))
				})
				continue
			}

			s.syncPodVolumeWatches(ctx, n, pod, true)
			totalDelay := s.cfg.PodStartupDelay + time.Duration(rand.Int64N(max(1, int64(s.cfg.Jitter))))
			if s.cfg.PodStatusStages >= 3 {
				step := max(50*time.Millisecond, totalDelay/3)
				if !s.sleepAbortOnDelete(ctx, step, pod.UID) {
					continue
				}
				_ = s.patchPodStatusStage(ctx, n, pod, corev1.PodPending, false, false, "ContainerCreating", false)
				s.emitPodEvent(ctx, n, pod, "Pulled", "Container image already present on machine")

				if !s.sleepAbortOnDelete(ctx, step, pod.UID) {
					continue
				}
				_ = s.patchPodStatusStage(ctx, n, pod, corev1.PodRunning, false, true, "", false)
				s.emitPodEvent(ctx, n, pod, "Created", "Created container "+pod.Name)

				if !s.sleepAbortOnDelete(ctx, step, pod.UID) {
					continue
				}
			} else if !s.sleepAbortOnDelete(ctx, totalDelay, pod.UID) {
				continue
			}
			if err := s.patchPodStatusStage(ctx, n, pod, corev1.PodRunning, true, true, "", false); err != nil {
				s.seenUIDs.Delete(pod.UID)
				continue
			}
			s.emitPodEvent(ctx, n, pod, "Started", "Started container "+pod.Name)
		}
	}
}

func (s *Simulator) sleepAbortOnDelete(ctx context.Context, d time.Duration, uid types.UID) bool {
	time.Sleep(d)
	v, ok := s.seenUIDs.Load(uid)
	return ctx.Err() == nil && (!ok || v.(int) != 2)
}

func (s *Simulator) emitPodEvent(ctx context.Context, n *SimNode, pod *corev1.Pod, reason, msg string) {
	now := metav1.Now()
	ev := &corev1.Event{
		ObjectMeta:          metav1.ObjectMeta{Name: fmt.Sprintf("%s.%s.%x", pod.Name, strings.ToLower(reason), now.UnixNano()), Namespace: pod.Namespace},
		InvolvedObject:      corev1.ObjectReference{Kind: "Pod", Namespace: pod.Namespace, Name: pod.Name, UID: pod.UID, APIVersion: "v1", ResourceVersion: pod.ResourceVersion},
		Reason:              reason,
		Message:             msg,
		Source:              corev1.EventSource{Component: "kubelet", Host: n.Name},
		FirstTimestamp:      now,
		LastTimestamp:       now,
		Count:               1,
		Type:                corev1.EventTypeNormal,
		ReportingController: "kubelet",
		ReportingInstance:   n.Name,
	}
	_ = s.withMutateSem(ctx, func() error {
		_, err := n.KubeletClient.CoreV1().Events(pod.Namespace).Create(ctx, ev, metav1.CreateOptions{})
		return err
	})
}

func (s *Simulator) patchPodStatusStage(ctx context.Context, n *SimNode, pod *corev1.Pod, phase corev1.PodPhase, ready, started bool, waitingReason string, terminated bool) error {
	return cl2client.RetryWithExponentialBackOff(cl2client.RetryFunction(func() error {
		return s.withMutateSem(ctx, func() error {
			if latest, err := n.KubeletClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{}); err == nil && latest != nil {
				pod = latest
			} else if apierrors.IsNotFound(err) {
				return nil
			}
			if !terminated && pod.DeletionTimestamp != nil {
				return nil
			}
			now := metav1.Now()
			schedTime := now
			for _, c := range pod.Status.Conditions {
				if c.Type == corev1.PodScheduled && !c.LastTransitionTime.IsZero() {
					schedTime = c.LastTransitionTime
					break
				}
			}
			podIP := fmt.Sprintf("10.%d.%d.%d", 64+(n.Index/256), n.Index%256, 2+(int(pod.UID[0])%250))

			cStatuses := make([]corev1.ContainerStatus, len(pod.Spec.Containers))
			for i, c := range pod.Spec.Containers {
				cs := corev1.ContainerStatus{
					Name: c.Name, Image: c.Image, ImageID: pauseImageID,
					ContainerID: fmt.Sprintf("containerd://kubelite-%s-%d", pod.UID[:8], i),
					Ready:       ready, Started: &started, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}},
				}
				if waitingReason != "" {
					cs.ImageID, cs.ContainerID = "", ""
					cs.State = corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: waitingReason}}
				} else if terminated {
					cs.State = corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 0, Reason: "Completed", StartedAt: schedTime, FinishedAt: now}}
				}
				cStatuses[i] = cs
			}

			readyCond, readyReason := corev1.ConditionFalse, "ContainersNotReady"
			if ready {
				readyCond, readyReason = corev1.ConditionTrue, ""
			} else if phase == corev1.PodSucceeded {
				readyReason = "PodCompleted"
			}
			readyToStart := corev1.ConditionTrue
			if terminated {
				readyToStart = corev1.ConditionFalse
			}

			raw, _ := json.Marshal(map[string]any{"status": corev1.PodStatus{
				Phase: phase, HostIP: n.InternalIP, HostIPs: []corev1.HostIP{{IP: n.InternalIP}},
				PodIP: podIP, PodIPs: []corev1.PodIP{{IP: podIP}}, StartTime: &schedTime,
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReadyToStartContainers, Status: readyToStart, LastTransitionTime: now},
					{Type: corev1.PodInitialized, Status: corev1.ConditionTrue, LastTransitionTime: schedTime},
					{Type: corev1.PodReady, Status: readyCond, Reason: readyReason, LastTransitionTime: now},
					{Type: corev1.ContainersReady, Status: readyCond, Reason: readyReason, LastTransitionTime: now},
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: schedTime},
				},
				ContainerStatuses: cStatuses,
			}})

			_, err := n.KubeletClient.CoreV1().Pods(pod.Namespace).Patch(ctx, pod.Name, types.StrategicMergePatchType, raw, metav1.PatchOptions{}, "status")
			return err
		})
	}, cl2client.Allow(apierrors.IsNotFound), cl2client.Retry(isRetryableErr)))
}

func isRetryableErr(err error) bool {
	return apierrors.IsForbidden(err) || apierrors.IsConflict(err) || cl2client.IsRetryableAPIError(err) || cl2client.IsRetryableNetError(err)
}

func (s *Simulator) withMutateSem(ctx context.Context, fn func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.podMutateSem <- struct{}{}:
		defer func() { <-s.podMutateSem }()
		return fn()
	}
}

func (s *Simulator) watchAndBindPVCs(ctx context.Context) {
	lw := &cache.ListWatch{
		WatchFuncWithContext: func(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
			return s.adminClient.CoreV1().PersistentVolumeClaims("").Watch(ctx, opts)
		},
	}
	for ctx.Err() == nil {
		list, err := s.adminClient.CoreV1().PersistentVolumeClaims("").List(ctx, metav1.ListOptions{ResourceVersion: "0"})
		if err != nil || list.ResourceVersion == "" {
			time.Sleep(time.Second)
			continue
		}
		for i := range list.Items {
			s.maybeBindPVC(ctx, &list.Items[i])
		}
		if rw, err := retrywatcher.NewRetryWatcherWithContext(ctx, list.ResourceVersion, lw); err == nil {
			for ev := range rw.ResultChan() {
				if pvc, ok := ev.Object.(*corev1.PersistentVolumeClaim); ok {
					s.maybeBindPVC(ctx, pvc)
				}
			}
		}
		time.Sleep(time.Second)
	}
}

func (s *Simulator) maybeBindPVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) {
	if pvc.Status.Phase != corev1.ClaimPending || pvc.DeletionTimestamp != nil || !strings.HasPrefix(pvc.Namespace, s.cfg.PVCNamespacePrefix) {
		return
	}
	if _, loaded := s.seenUIDs.LoadOrStore(pvc.UID, 1); loaded {
		return
	}
	pvName := fmt.Sprintf("kubelite-pv-%s-%s", pvc.Namespace, pvc.Name)
	qty := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	if qty.IsZero() {
		qty = resource.MustParse("1Gi")
	}
	var scName string
	if pvc.Spec.StorageClassName != nil {
		scName = *pvc.Spec.StorageClassName
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: pvName},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{corev1.ResourceStorage: qty}, AccessModes: pvc.Spec.AccessModes,
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete, StorageClassName: scName,
			PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/" + pvName}},
			ClaimRef:               &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Namespace: pvc.Namespace, Name: pvc.Name, UID: pvc.UID},
		},
	}
	_, _ = s.adminClient.CoreV1().PersistentVolumes().Create(ctx, pv, metav1.CreateOptions{})
}
