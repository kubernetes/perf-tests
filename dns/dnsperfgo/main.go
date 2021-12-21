/*
Copyright 2021 The Kubernetes Authors.

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
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/net/context"
)

type Config struct {
	qps                                      int
	testDuration, idleDuration, queryTimeout time.Duration
	hostnameFile                             string
	queryClusterNames, logQueries            bool
	resultsLock                              sync.Mutex
}

type dnsClient struct {
	resultsLock   sync.Mutex
	stopChan      chan os.Signal
	metricsServer *http.Server
	config        *Config
	result        struct {
		errorCount   int
		timeoutCount int
		totalCount   int
	}
}

type LookupFunc func(string) ([]string, error)

func main() {
	config := Config{}
	flag.IntVar(&config.qps, "qps", 10, "The number of DNS queries per second to issue")
	flag.DurationVar(&config.testDuration, "duration", 30*time.Second, "The duration(in seconds) to run, before sleeping.")
	flag.DurationVar(&config.idleDuration, "idle-duration", 10*time.Second, "The duration(in seconds) to stop for between consecutive test runs. This simulates burst of queries followed by idle time.")
	flag.DurationVar(&config.queryTimeout, "timeout", 5*time.Second, "The timeout for the DNS query.")
	flag.StringVar(&config.hostnameFile, "inputfile", "", "Path to the file containing hostnames to lookup. Hostnames should be newline-separated.")
	flag.BoolVar(&config.queryClusterNames, "query-cluster-names", false, "Indicates whether the query names should be the service names in the cluster.")
	flag.BoolVar(&config.logQueries, "log-queries", false, "Indicates whether each query should be logged.")

	flag.Parse()
	log.Printf("Starting dnstest with config parameters - %+v", config)
	client := &dnsClient{config: &config, stopChan: make(chan os.Signal, 1)}
	signal.Notify(client.stopChan, syscall.SIGTERM)
	client.metricsServer = startMetricsServer(":9153")
	registerMetrics()
	client.run()
}

func hostnamesFromConfig(config *Config) []string {
	var hostnamesArr []string
	if config.hostnameFile != "" {
		contents, err := ioutil.ReadFile(config.hostnameFile)
		if err != nil {
			log.Fatalf("Failed to read input file %q, err - %v, Exiting.", config.hostnameFile, err)
		}
		for _, h := range strings.Split(string(contents), "\n") {
			h = strings.TrimSpace(h)
			if h != "" {
				hostnamesArr = append(hostnamesArr, h)
			}
		}
	}
	// TODO(prameshj) implement reading from k8s API server
	return hostnamesArr
}

func (c *dnsClient) run() {
	hostnames := hostnamesFromConfig(c.config)
	if len(hostnames) == 0 {
		log.Fatalf("No hostnames specified, Exiting.")
	}
	log.Printf("Got %d hostnames to lookup\n", len(hostnames))
	qpsSleepDuration := (1 * time.Second) / time.Duration(c.config.qps)
	ticker := time.NewTicker(c.config.testDuration)
	defer ticker.Stop()
	// result stores the dns query counts for logging purpose only.

	for {
		for _, h := range hostnames {
			select {
			case <-c.stopChan:
				c.logResults()
				log.Print("Exiting.")
				err := c.metricsServer.Shutdown(context.TODO())
				if err != nil {
					log.Printf("metricsServer Shutdown returned error - %v", err)
				}
				return
			case <-ticker.C:
				c.logResults()
				// Wait for the test to run for testDuration seconds before firing. Without this reset, the test will only run for (testDuration - idleDuration) seconds.
				ticker.Reset(c.config.testDuration + c.config.idleDuration)
				time.Sleep(c.config.idleDuration)
				log.Print("Restarting DNS lookups.")
			default:
				break
			}
			// Use nsLookup command rather than net.LookupHost because nslookup sends A and AAAA lookups in parallel(with the same source port) in Alpine base image.
			// Go program sends them with different source ports. The same source port behavior will trigger the DNS race conditions described in https://www.weave.works/blog/racy-conntrack-and-dns-lookup-timeouts.
			go c.runQuery(h, c.config.queryTimeout, nsLookup)
			time.Sleep(qpsSleepDuration)
		}
	}
}

func (c *dnsClient) logResults() {
	c.resultsLock.Lock()
	defer c.resultsLock.Unlock()
	log.Printf("Completed %d queries, %d errors, %d timeouts.\n", c.result.totalCount, c.result.errorCount, c.result.timeoutCount)

}

func (c *dnsClient) updateResults(timedOut bool, err error) {
	c.resultsLock.Lock()
	defer c.resultsLock.Unlock()
	if err != nil {
		c.result.errorCount++
		dnsErrorsCounter.Inc()
	}
	if timedOut {
		c.result.timeoutCount++
		dnsTimeoutsCounter.Inc()
	}
	c.result.totalCount++
	dnsLookupsCounter.Inc()
}

func (c *dnsClient) runQuery(name string, timeout time.Duration, lookupFunc LookupFunc) {
	timer := time.NewTimer(c.config.queryTimeout)
	defer timer.Stop()

	resultChan := make(chan error)
	go func(chan error) {
		startTime := time.Now()
		_, err := lookupFunc(name)
		latency := time.Since(startTime)
		dnsLatency.Observe(latency.Seconds())
		resultChan <- err
	}(resultChan)

	var err error
	var timedOut bool

	defer func() {
		if c.config.logQueries {
			log.Printf("DNS lookup of name %q, err - %v\n", name, err)
		}
		if err != nil {
			log.Printf("Failed DNS lookup of name %q, err - %v\n", name, err)
		}
		c.updateResults(timedOut, err)
	}()

	for {
		select {
		case err = <-resultChan:
			return
		case <-timer.C:
			timedOut = true
			err = fmt.Errorf("timed out after %v", timeout)
			return
		}
	}
}

// nslookup returns error for queries that result in NXDOMAIN as well.
func nsLookup(name string) ([]string, error) {
	cmd := exec.Command("nslookup", name)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var ip string
	var index int
	ips := make([]string, 0)
	outputStrings := strings.Split(string(out), "\n")
	for _, s := range outputStrings {
		if strings.Contains(s, "Address ") {
			// String will be of the form - "Address 1: 10.99.48.1 kubernetes.default.svc.cluster.local" or "Address 1: 40.76.4.15" on Alpine/BusyBox. Read in ip and host as a single variable.
			_, err := fmt.Sscanf(s, "Address %d: %s", &index, &ip)
			if err != nil {
				return nil, fmt.Errorf("failed to scan string %q, err - %w", s, err)
			}
			ips = append(ips, ip)
		}
	}

	return ips, nil
}
