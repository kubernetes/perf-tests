/*
Copyright 2019 The Kubernetes Authors.

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
	"flag"
	"net"
	"time"

	"k8s.io/klog"
	"k8s.io/perf-tests/probes/dns-probe/pkg/metrics"
)

var probe dnsProbe

func init() {
	flag.StringVar(&probe.lookupURL, "url", "", "Name of a Service to lookup")
	flag.DurationVar(&probe.probingInterval, "probing-interval", 1*time.Second, "Interval between DNS lookups")
}

type dnsProbe struct {
	lookupURL       string
	probingInterval time.Duration
}

// RunDNSProber periodically does DNS lookups.
// Lookup's interval and URL are configurable via flags.
func RunDNSProber() {
	if probe.lookupURL == "" {
		klog.Fatal("--url has not been set")
	}
	probe.run()
}

func (p *dnsProbe) run() {
	klog.Infof("Starting dns-prober...")
	for {
		time.Sleep(p.probingInterval)
		klog.V(4).Infof("dns lookup %s", p.lookupURL)
		startTime := time.Now()
		metrics.InClusterDNSLookupCount.Inc()
		if err := p.lookup(); err != nil {
			klog.Warningf("got error: %v", err)
			metrics.InClusterDNSLookupError.Inc()
			continue
		}
		latency := time.Since(startTime)
		klog.V(4).Infof("dns lookup took %v", latency)
		metrics.InClusterDNSLatency.Observe(latency.Seconds())
	}
}

func (p *dnsProbe) lookup() error {
	_, err := net.LookupIP(p.lookupURL)
	if err != nil {
		return err
	}
	return nil
}
