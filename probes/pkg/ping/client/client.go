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

package pingclient

import (
	"flag"
	"net/http"
	"time"

	"k8s.io/klog"
)

var (
	pingServerAddress = flag.String("ping-server-address", "", "The address of the ping server")
	pingSleepDuration = flag.Duration("ping-sleep-duration", 1*time.Second, "Duration of the sleep between pings")
)

// Config configures the "ping-client" probe.
type Config struct {
	pingServerAddress string
	pingSleepDuration time.Duration
}

// NewDefaultPingClientConfig creates a default "ping-client" config.
func NewDefaultPingClientConfig() *Config {
	if *pingServerAddress == "" {
		klog.Fatal("--ping-server-address not set!")
	}
	return &Config{
		pingServerAddress: *pingServerAddress,
		pingSleepDuration: *pingSleepDuration,
	}
}

// Run runs the ping client probe that periodically pings the ping server and exports latency metric.
func Run(config *Config) {
	for {
		time.Sleep(config.pingSleepDuration)
		klog.V(4).Infof("ping -> %s...\n", config.pingServerAddress)
		startTime := time.Now()
		inClusterNetworkLatencyPingCount.Inc()
		if err := ping(config.pingServerAddress); err != nil {
			klog.Warningf("Got error: %v", err)
			inClusterNetworkLatencyError.Inc()
			continue
		}
		latency := time.Since(startTime)
		klog.V(4).Infof("Request took: %v\n", latency)
		inClusterNetworkLatency.Observe(latency.Seconds())
	}
}

func ping(serverAddress string) error {
	resp, err := http.Get("http://" + serverAddress)
	if resp != nil {
		resp.Body.Close()
	}
	return err
}

func merge(slices ...[]float64) []float64 {
	result := make([]float64, 1)
	for _, s := range slices {
		result = append(result, s...)
	}
	return result
}
