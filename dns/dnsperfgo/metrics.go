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
	"log"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	dnsErrorsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dns_errors_total",
			Help: "Count of DNS resolution errors. Includes dns timeouts.",
		},
	)
	dnsTimeoutsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dns_timeouts_total",
			Help: "Count of DNS queries that timed out.",
		},
	)
	dnsLookupsCounter = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "dns_lookups_total",
			Help: "Count of DNS queries that were sent. Includes error and successful queries.",
		},
	)
	dnsLatency = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "dns_lookup_latency",
			Help: "latency(in seconds) distribution of DNS lookups.",
		},
	)
)

var register sync.Once

func registerMetrics() {
	register.Do(func() {
		prometheus.MustRegister(dnsErrorsCounter)
		prometheus.MustRegister(dnsTimeoutsCounter)
		prometheus.MustRegister(dnsLookupsCounter)
		prometheus.MustRegister(dnsLatency)
	})
}

func startMetricsServer(listenAddr string) *http.Server {
	http.Handle("/metrics", promhttp.Handler())
	http.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server := &http.Server{Addr: listenAddr}
	go func(server *http.Server) {
		log.Printf("Starting HTTP server on %q.", listenAddr)
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("Metrics server failed to start, err - %v", err)
		}
	}(server)
	return server
}
