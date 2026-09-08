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
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog/v2"
)

func runEtcdPut(args []string) error {
	fs := flag.NewFlagSet("etcd-put", flag.ExitOnError)
	klog.InitFlags(fs)

	endpointsStr := fs.String("endpoints", "http://localhost:2379", "Comma-separated list of etcd endpoints.")
	qps := fs.Float64("qps", 100, "The target QPS limit for all requests (0 or negative for unthrottled).")
	concurrency := fs.Int("concurrency", 10, "Number of concurrent writer goroutines.")
	keyCount := fs.Int("key-count", 10000, "Number of distinct keys to mutate.")
	keyPrefix := fs.String("key-prefix", "bench-key-", "Prefix for target keys.")
	keySize := fs.Int("key-size", 64, "Total size of each key in bytes.")
	valSize := fs.Int("val-size", 256, "Total size of each value in bytes.")
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
	if *valSize <= 0 {
		return fmt.Errorf("--val-size must be > 0")
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

	// Pre-generate a value payload buffer
	valueBytes := make([]byte, *valSize)
	if _, err := rand.Read(valueBytes); err != nil {
		return fmt.Errorf("failed to generate random value bytes: %w", err)
	}
	valString := string(valueBytes)

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

	klog.Infof("Starting etcd-put benchmark: endpoints=%v, concurrency=%d, qps=%.1f, keyCount=%d, keySize=%d, valSize=%d",
		endpoints, *concurrency, *qps, *keyCount, *keySize, *valSize)

	var wg sync.WaitGroup
	counter := atomic.Uint64{}
	for i := 0; i < *concurrency; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
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
				key := fmt.Sprintf("%s%010d", *keyPrefix, counter.Add(1)%uint64(*keyCount))

				reqCtx, reqCancel := context.WithTimeout(ctx, *requestTimeout)
				_, putErr := cli.Put(reqCtx, key, valString)
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
