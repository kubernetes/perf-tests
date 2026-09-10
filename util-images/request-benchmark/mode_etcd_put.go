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

package main

import (
	"context"
	"crypto/rand"
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

const (
	compactRevKey = "compact_rev_key"
)

//go:embed data/exemplar_pod.yaml
var exemplarPodYAML []byte

func runEtcdPut(args []string) error {
	fs := flag.NewFlagSet("etcd-put", flag.ExitOnError)
	klog.InitFlags(fs)

	endpointsStr := fs.String("endpoints", "http://localhost:2379", "Comma-separated list of etcd endpoints.")
	qps := fs.Float64("qps", 100, "The target QPS limit for all requests (0 or negative for unthrottled).")
	concurrency := fs.Int("concurrency", 10, "Number of concurrent writer goroutines.")
	keyCount := fs.Int("key-count", 10000, "Number of distinct keys to mutate.")
	keyPrefix := fs.String("key-prefix", "/registry/pods/default/pod-", "Prefix for target keys.")
	valSize := fs.Int("val-size", 256, "Total size of each value in bytes (for random payload-type).")
	payloadType := fs.String("payload-type", "pod", "Payload type: 'pod' for realistic serialized Kubernetes Pod proto, 'random' for random bytes.")
	compactInterval := fs.Duration("compact-interval", 150*time.Second, "Interval for background compaction simulating Kubernetes apiserver (0 to disable).")
	dialTimeout := fs.Duration("dial-timeout", 5*time.Second, "Timeout for connecting to etcd cluster.")
	requestTimeout := fs.Duration("request-timeout", 5*time.Second, "Timeout for each put request.")

	if err := fs.Parse(args); err != nil {
		return err
	}

	endpoints := strings.Split(*endpointsStr, ",")
	for i := range endpoints {
		endpoints[i] = strings.TrimSpace(endpoints[i])
	}

	if len(endpoints) == 0 || endpoints[0] == "" {
		return fmt.Errorf("--endpoints must not be empty")
	}
	if *concurrency <= 0 {
		return fmt.Errorf("--concurrency must be > 0")
	}
	if *keyCount <= 0 {
		return fmt.Errorf("--key-count must be > 0")
	}
	var basePod *corev1.Pod
	var randomValString string

	switch *payloadType {
	case "pod":
		if *valSize != 0 {
			return fmt.Errorf("--val-size not be set when payload-type is pod")
		}
		loaded, err := loadBasePod()
		if err != nil {
			return fmt.Errorf("failed to load base pod: %w", err)
		}
		basePod = loaded

		samplePod := basePod.DeepCopy()
		updatePodForIndex(samplePod, 0)
		sampleVal, err := serializePod(samplePod)
		if err != nil {
			return fmt.Errorf("failed to serialize sample pod: %w", err)
		}
		klog.Infof("Successfully loaded base Pod template (sample payload size: %d bytes)", len(sampleVal))
	case "random":
		if *valSize <= 0 {
			return fmt.Errorf("--val-size must be > 0 for random payload-type")
		}
		valueBytes := make([]byte, *valSize)
		if _, err := rand.Read(valueBytes); err != nil {
			return fmt.Errorf("failed to generate random value bytes: %w", err)
		}
		randomValString = string(valueBytes)
	default:
		return fmt.Errorf("--payload-type must be 'pod' or 'random'")
	}

	// Connect to etcd
	klog.Infof("Connecting to etcd endpoints: %v", endpoints)
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: *dialTimeout,
	})
	if err != nil {
		return fmt.Errorf("failed to create etcd client: %w", err)
	}
	defer cli.Close()

	var rateLimiter flowcontrol.RateLimiter
	if *qps > 0 {
		rateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*qps), int(*qps))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown on OS signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		klog.Infof("Received signal %v, shutting down etcd-put workload...", sig)
		cancel()
	}()

	// Start background compactor if interval is specified
	if *compactInterval > 0 {
		go runCompactor(ctx, cli, *compactInterval)
	}

	klog.Infof("Starting etcd-put benchmark: endpoints=%v, concurrency=%d, qps=%.1f, keyCount=%d, payloadType=%s, compactInterval=%v",
		endpoints, *concurrency, *qps, *keyCount, *payloadType, *compactInterval)

	var wg sync.WaitGroup
	counter := atomic.Uint64{}
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			var workerPod *corev1.Pod
			if *payloadType == "pod" {
				workerPod = basePod.DeepCopy()
			}

			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if rateLimiter != nil {
					if err := tryThrottle(ctx, rateLimiter); err != nil {
						if ctx.Err() != nil {
							return
						}
						klog.Warningf("Worker %d throttling error: %v", workerID, err)
						continue
					}
				}

				keyIdx := counter.Add(1) % uint64(*keyCount)
				key := fmt.Sprintf("%s%08d", *keyPrefix, keyIdx)

				var val string
				if *payloadType == "pod" {
					updatePodForIndex(workerPod, keyIdx)
					var err error
					val, err = serializePod(workerPod)
					if err != nil {
						klog.Errorf("Worker %d: failed to serialize pod: %v", workerID, err)
						continue
					}
				} else {
					val = randomValString
				}

				reqCtx, reqCancel := context.WithTimeout(ctx, *requestTimeout)
				_, putErr := cli.Put(reqCtx, key, val)
				reqCancel()

				if putErr != nil {
					if ctx.Err() != nil {
						return
					}
					klog.Warningf("Worker %d: Put key %s failed: %v", workerID, key, putErr)
				}
			}
		}(i)
	}

	wg.Wait()
	klog.Infof("etcd-put workload completed.")
	return nil
}

func loadBasePod() (*corev1.Pod, error) {
	var basePod corev1.Pod
	if err := yaml.Unmarshal(exemplarPodYAML, &basePod); err != nil {
		return nil, fmt.Errorf("failed to unmarshal exemplar pod YAML: %w", err)
	}
	return &basePod, nil
}

func updatePodForIndex(pod *corev1.Pod, i uint64) {
	podName := fmt.Sprintf("pod-%d", i)
	podIP := fmt.Sprintf("10.244.%d.%d", (i/250)%250+1, (i%250)+1)
	hostIP := fmt.Sprintf("10.40.0.%d", (i%10)+2)
	nodeName := fmt.Sprintf("benchmark-node-%d", i%100)

	pod.Name = podName
	pod.UID = types.UID(fmt.Sprintf("pod-uid-%08d", i))
	if pod.Labels != nil {
		pod.Labels["app.kubernetes.io/instance"] = podName
	}

	pod.Spec.NodeName = nodeName

	for idx := range pod.Spec.Volumes {
		if pod.Spec.Volumes[idx].ConfigMap != nil {
			pod.Spec.Volumes[idx].ConfigMap.Name = fmt.Sprintf("cm-%s", podName)
		}
		if pod.Spec.Volumes[idx].Secret != nil {
			pod.Spec.Volumes[idx].Secret.SecretName = fmt.Sprintf("secret-%s", podName)
		}
	}

	pod.Status.HostIP = hostIP
	if len(pod.Status.HostIPs) > 0 {
		pod.Status.HostIPs[0].IP = hostIP
	}
	pod.Status.PodIP = podIP
	if len(pod.Status.PodIPs) > 0 {
		pod.Status.PodIPs[0].IP = podIP
	}

	containerIDPrefix := fmt.Sprintf("containerd://%016x%016x", i, i+1)
	for idx := range pod.Status.InitContainerStatuses {
		pod.Status.InitContainerStatuses[idx].ContainerID = fmt.Sprintf("%s-init%d", containerIDPrefix, idx)
		if pod.Status.InitContainerStatuses[idx].State.Terminated != nil {
			pod.Status.InitContainerStatuses[idx].State.Terminated.ContainerID = fmt.Sprintf("%s-init%d", containerIDPrefix, idx)
		}
	}
	for idx := range pod.Status.ContainerStatuses {
		pod.Status.ContainerStatuses[idx].ContainerID = fmt.Sprintf("%s-%s", containerIDPrefix, pod.Status.ContainerStatuses[idx].Name)
	}
}

func serializePod(pod *corev1.Pod) (string, error) {
	protoBytes, err := pod.Marshal()
	if err != nil {
		return "", fmt.Errorf("failed to marshal pod to proto: %w", err)
	}
	// Prepend k8s\x00 (4-byte magic prefix used by Kubernetes apiserver for proto in etcd)
	k8sProtoBytes := append([]byte{0x6b, 0x38, 0x73, 0x00}, protoBytes...)
	return string(k8sProtoBytes), nil
}

func runCompactor(ctx context.Context, client *clientv3.Client, interval time.Duration) {
	klog.Infof("Starting background compactor simulating apiserver compaction with interval %v", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var compactTime int64
	var rev int64

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		newCompactTime, currentRev, compactRev, err := compact(ctx, client, compactTime, rev)
		if err != nil {
			klog.Errorf("Background compact failed: %v", err)
			continue
		}
		compactTime = newCompactTime
		rev = currentRev
		if compactRev != 0 {
			klog.Infof("Successfully compacted etcd to revision %d", compactRev)
		}
	}
}

func compact(ctx context.Context, client *clientv3.Client, expectVersion, rev int64) (currentVersion, currentRev, compactRev int64, err error) {
	resp, err := client.KV.Txn(ctx).If(
		clientv3.Compare(clientv3.Version(compactRevKey), "=", expectVersion),
	).Then(
		clientv3.OpPut(compactRevKey, strconv.FormatInt(rev, 10)),
	).Else(
		clientv3.OpGet(compactRevKey),
	).Commit()
	if err != nil {
		return expectVersion, rev, 0, err
	}

	currentRev = resp.Header.Revision

	if !resp.Succeeded {
		if len(resp.Responses) > 0 && len(resp.Responses[0].GetResponseRange().Kvs) > 0 {
			kv := resp.Responses[0].GetResponseRange().Kvs[0]
			currentVersion = kv.Version
			compactRev, err = strconv.ParseInt(string(kv.Value), 10, 64)
			if err != nil {
				return currentVersion, currentRev, 0, nil
			}
			return currentVersion, currentRev, compactRev, nil
		}
		return currentVersion, currentRev, 0, nil
	}
	currentVersion = expectVersion + 1

	if rev == 0 {
		// First interval: record current revision, do not compact on bootstrap
		return currentVersion, currentRev, 0, nil
	}

	if _, err = client.Compact(ctx, rev); err != nil {
		return currentVersion, currentRev, 0, err
	}
	klog.Infof("Compacted etcd store at revision %d", rev)
	return currentVersion, currentRev, rev, nil
}
