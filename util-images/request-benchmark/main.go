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

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/flowcontrol"
)

const (
	HealthCheckRequests = 10
	NamespaceTmpl       = "%namespace%"
)

type ContentType string

const (
	JSONContentType  ContentType = "json"
	ProtoContentType ContentType = "proto"
	CBORContentType  ContentType = "cbor"
	YAMLContentType  ContentType = "yaml"
)

var (
	inflight    = flag.Int("inflight", 1, "Benchmark inflight (number of parallel requests being made to the apiserver")
	namespace   = flag.String("namespace", "", "Replace %namespace% in URI with provided namespace")
	URI         = flag.String("uri", "", "Request URI")
	verb        = flag.String("verb", "GET", "A verb to be used in requests.")
	qps         = flag.Float64("qps", -1, "The qps limit for all requests")
	contentType = ContentType("json")
)

func (c ContentType) String() string {
	return string(c)
}

func (c *ContentType) Set(value string) error {
	switch ContentType(value) {
	case JSONContentType, ProtoContentType, CBORContentType, YAMLContentType:
		*c = ContentType(value)
		return nil
	default:
		return fmt.Errorf("invalid content type: %s. Must be one of: [json, proto, cbor, yaml]", value)
	}
}

func init() {
	flag.Var(&contentType, "content-type", "Content type for requests (required). Valid values: [json, proto, cbor, yaml]")
	flag.Parse()
}

func getContentType(ct ContentType) (string, error) {
	switch ct {
	case JSONContentType:
		return "application/json", nil
	case ProtoContentType:
		return "application/vnd.kubernetes.protobuf", nil
	case CBORContentType:
		return "application/cbor", nil
	case YAMLContentType:
		return "application/yaml", nil
	default:
		return "", fmt.Errorf("unsupported content type: %s", ct)
	}
}

func main() {
	config, err := getConfig()
	if err != nil {
		panic(err)
	}
	config.QPS = float32(*qps)
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		panic(err)
	}
	ctx := context.Background()

	serverURL, _, err := rest.DefaultServerUrlFor(config)
	if err != nil {
		panic(err)
	}
	url, err := url.Parse(strings.ReplaceAll(*URI, NamespaceTmpl, *namespace))
	if err != nil {
		panic(err)
	}
	url.Host = serverURL.Host
	url.Scheme = serverURL.Scheme

	var rateLimiter flowcontrol.RateLimiter
	if *qps != -1 {
		rateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*qps), 10)
	}

	if err := healthCheck(ctx, client, *url, rateLimiter); err != nil {
		panic(err)
	}

	log.Printf("Sending requests to '%s' with inflight %d. Press Ctrl+C to stop...", url, *inflight)
	for i := 0; i < *inflight; i++ {
		go func() {
			for {
				sendRequest(ctx, client, *url, rateLimiter)
			}
		}()
	}

	select {} // block main thread from ending
}

func healthCheck(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter) error {
	for i := 0; i < HealthCheckRequests; i++ {
		if sendRequest(ctx, client, url, rateLimiter) {
			return nil
		}
	}
	return fmt.Errorf("could not successfully send a request to %s", url.String())
}

func sendRequest(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter) bool {
	req, err := http.NewRequestWithContext(ctx, *verb, url.String(), nil)
	if err != nil {
		log.Printf("Got error creating a request: %v\n", err)
		return false
	}

	contentType, err := getContentType(contentType)
	if err != nil {
		log.Printf("Invalid content type: %v", err)
		return false
	}
	req.Header.Set("Accept", contentType)

	err = tryThrottle(ctx, rateLimiter)
	if err != nil {
		log.Printf("Got error throttling a request: %v\n", err)
		return false
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Got error when sending a request: %v\n", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode > http.StatusPartialContent {
		log.Printf("Got bad status code: %v\n", resp.Status)
		return false
	}
	if resp.Header.Get("Content-Type") != contentType {
		log.Printf("Got bad content type: %q, expected %q\n", resp.Header.Get("Content-Type"), contentType)
		return false
	}
	written, err := io.Copy(io.Discard, resp.Body)
	if err != nil {
		log.Printf("Got error when reading response: %v\n", err)
		return false
	}
	log.Printf("Got response of %d bytes in %v", written, time.Since(start))
	return true
}

func getConfig() (*rest.Config, error) {
	if _, ok := os.LookupEnv("KUBERNETES_PORT"); ok {
		return rest.InClusterConfig()
	}

	if kubeconfig, ok := os.LookupEnv("KUBECONFIG"); ok {
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if home, ok := os.LookupEnv("HOME"); ok {
		kubeconfig := filepath.Join(home, ".kube", "config")
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	}

	return nil, fmt.Errorf("could not create client-go config")
}

func tryThrottle(ctx context.Context, rateLimiter flowcontrol.RateLimiter) error {
	if rateLimiter == nil {
		return nil
	}

	now := time.Now()
	err := rateLimiter.Wait(ctx)
	if err != nil {
		err = fmt.Errorf("client rate limiter Wait returned an error: %w", err)
	}
	latency := time.Since(now)

	if latency > time.Second {
		log.Printf("Waited for %v due to client-side throttling, not priority and fairness", latency)
	}

	return err
}
