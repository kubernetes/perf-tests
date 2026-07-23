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
	"mime"
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
	JSONContentType       ContentType = "json"
	ProtoContentType      ContentType = "proto"
	CBORContentType       ContentType = "cbor"
	YAMLContentType       ContentType = "yaml"
	TableContentType      ContentType = "table"
	JSONPrettyContentType ContentType = "json-pretty"

	tableAccept = "application/json;as=Table;v=v1;g=meta.k8s.io"
)

func (c ContentType) String() string {
	return string(c)
}

func (c *ContentType) Set(value string) error {
	switch ContentType(value) {
	case JSONContentType, ProtoContentType, CBORContentType, YAMLContentType, TableContentType, JSONPrettyContentType:
		*c = ContentType(value)
		return nil
	default:
		return fmt.Errorf("invalid content type: %s. Must be one of: [json, proto, cbor, yaml, table, json-pretty]", value)
	}
}

func getContentType(ct ContentType) (string, error) {
	switch ct {
	case JSONContentType, JSONPrettyContentType:
		return "application/json", nil
	case ProtoContentType:
		return "application/vnd.kubernetes.protobuf", nil
	case CBORContentType:
		return "application/cbor", nil
	case YAMLContentType:
		return "application/yaml", nil
	case TableContentType:
		return tableAccept, nil
	default:
		return "", fmt.Errorf("unsupported content type: %s", ct)
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(os.Stderr, `Usage: request-benchmark <subcommand> [flags]

Subcommands:
  http      Send HTTP requests to the apiserver (default when no subcommand given)
  informer  Start informers and measure sync time

Run 'request-benchmark <subcommand> --help' for subcommand-specific flags.
`)
		os.Exit(0)
	}
	mode := "http"
	// if the first arg doesn't start with "-" it's a subcommand name
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	switch mode {
	case "http":
		if err := runHTTP(args); err != nil {
			log.Fatal(err)
		}
	case "informer":
		if err := runInformer(args); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown subcommand %q, valid: http, informer", mode)
	}
}

func runHTTP(args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	inflight := fs.Int("inflight", 1, "Benchmark inflight (number of parallel requests being made to the apiserver")
	namespace := fs.String("namespace", "", "Replace %namespace% in URI with provided namespace")
	URI := fs.String("uri", "", "Request URI")
	verb := fs.String("verb", "GET", "A verb to be used in requests.")
	qps := fs.Float64("qps", -1, "The qps limit for all requests")
	contentType := ContentType("json")
	fs.Var(&contentType, "content-type", "Content type for requests (required). Valid values: [json, proto, cbor, yaml, table, json-pretty]")
	if err := fs.Parse(args); err != nil {
		return err
	}

	config, err := getConfig()
	if err != nil {
		return err
	}
	config.QPS = float32(*qps)
	client, err := rest.HTTPClientFor(config)
	if err != nil {
		return err
	}
	ctx := context.Background()

	serverURL, _, err := rest.DefaultServerUrlFor(config)
	if err != nil {
		return err
	}
	url, err := url.Parse(strings.ReplaceAll(*URI, NamespaceTmpl, *namespace))
	if err != nil {
		return err
	}
	url.Host = serverURL.Host
	url.Scheme = serverURL.Scheme

	if contentType == JSONPrettyContentType {
		q := url.Query()
		q.Set("pretty", "true")
		url.RawQuery = q.Encode()
	}

	var rateLimiter flowcontrol.RateLimiter
	if *qps != -1 {
		rateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*qps), 10)
	}

	if err := healthCheck(ctx, client, *url, rateLimiter, *verb, contentType); err != nil {
		return err
	}

	log.Printf("Sending requests to '%s' with inflight %d. Press Ctrl+C to stop...", url, *inflight)
	for i := 0; i < *inflight; i++ {
		go func() {
			for {
				sendRequest(ctx, client, *url, rateLimiter, *verb, contentType)
			}
		}()
	}

	select {} // block main thread from ending
}

func healthCheck(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter, verb string, ct ContentType) error {
	for i := 0; i < HealthCheckRequests; i++ {
		if sendRequest(ctx, client, url, rateLimiter, verb, ct) {
			return nil
		}
	}
	return fmt.Errorf("could not successfully send a request to %s", url.String())
}

func sendRequest(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter, verb string, ct ContentType) bool {
	req, err := http.NewRequestWithContext(ctx, verb, url.String(), nil)
	if err != nil {
		log.Printf("Got error creating a request: %v\n", err)
		return false
	}

	contentType, err := getContentType(ct)
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
	respContentType := resp.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(respContentType)
	if err != nil {
		log.Printf("Got invalid Content-Type header %q: %v\n", respContentType, err)
		return false
	}
	expectedMediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType != expectedMediaType {
		log.Printf("Got bad content type: %q, expected %q\n", mediaType, expectedMediaType)
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
