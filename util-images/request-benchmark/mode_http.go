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

package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
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

func runHTTP(args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	inflight := fs.Int("inflight", 1, "Benchmark inflight (number of parallel requests being made to the apiserver")
	namespace := fs.String("namespace", "", "Replace %namespace% in URI with provided namespace")
	URI := fs.String("uri", "", "Request URI")
	verb := fs.String("verb", "GET", "A verb to be used in requests.")
	qps := fs.Float64("qps", -1, "The qps limit for all requests")
	watchList := fs.Bool("watch-list", false, "Use watch-list requests and restart after the initial-events-end bookmark.")
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
	acceptContentType, err := getContentType(contentType)
	if err != nil {
		return err
	}
	config.AcceptContentTypes = acceptContentType
	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
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
	if *watchList {
		if *verb != http.MethodGet {
			return fmt.Errorf("--watch-list requires --verb=GET")
		}
		*url = watchListURL(*url)
	}

	var rateLimiter flowcontrol.RateLimiter
	if *qps != -1 {
		rateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(*qps), 10)
	}

	send := func() bool { return sendRequest(ctx, client, *url, rateLimiter, *verb, acceptContentType) }
	if *watchList {
		send = func() bool {
			return sendWatchListRequest(ctx, client, *url, rateLimiter, acceptContentType)
		}
	}
	if err := healthCheck(send, url.String()); err != nil {
		return err
	}
	log.Printf("Sending requests to '%s' with inflight %d. Press Ctrl+C to stop...", url, *inflight)
	for i := 0; i < *inflight; i++ {
		go func() {
			for {
				send()
			}
		}()
	}

	select {} // block main thread from ending
}

func watchListURL(url url.URL) url.URL {
	query := url.Query()
	query.Set("watch", "true")
	query.Set("sendInitialEvents", "true")
	query.Set("allowWatchBookmarks", "true")
	query.Set("resourceVersionMatch", string(metav1.ResourceVersionMatchNotOlderThan))
	url.RawQuery = query.Encode()
	return url
}

func sendWatchListRequest(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter, acceptContentType string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url.String(), nil)
	if err != nil {
		log.Printf("Got error creating a watch-list request: %v", err)
		return false
	}
	req.Header.Set("Accept", acceptContentType)

	if err := tryThrottle(ctx, rateLimiter); err != nil {
		log.Printf("Got error throttling a watch-list request: %v", err)
		return false
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Got error when sending a watch-list request: %v", err)
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("Got bad status code for watch-list request: %v", resp.Status)
		return false
	}
	mediaType, params, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil {
		log.Printf("Got invalid Content-Type for watch-list request: %v", err)
		return false
	}
	expectedMediaType, _, _ := mime.ParseMediaType(acceptContentType)
	if mediaType != expectedMediaType {
		log.Printf("Got bad content type for watch-list request: %q, expected %q", mediaType, expectedMediaType)
		return false
	}
	if err := consumeWatchList(resp.Body, mediaType, params); err != nil {
		log.Printf("Watch-list request failed: %v", err)
		return false
	}
	log.Printf("Watch-list stream completed in %v", time.Since(start))
	return true
}

func consumeWatchList(body io.ReadCloser, mediaType string, params map[string]string) error {
	negotiator := runtime.NewClientNegotiator(scheme.Codecs.WithoutConversion(), schema.GroupVersion{})
	_, serializer, framer, err := negotiator.StreamDecoder(mediaType, params)
	if err != nil {
		return err
	}
	decoder := streaming.NewDecoder(framer.NewFrameReader(body), serializer)
	defer decoder.Close()
	// Decode only watch envelopes, leaving resource objects in event.Object.Raw. Unlike
	// RESTClient.Watch, this avoids decoding each object into its typed representation.
	for {
		var event metav1.WatchEvent
		decoded, _, err := decoder.Decode(nil, &event)
		if err != nil {
			return err
		}
		if decoded != &event {
			return fmt.Errorf("unable to decode to metav1.WatchEvent")
		}
		if event.Type == string(watch.Bookmark) && hasInitialEventsEnd(event.Object.Raw) {
			return nil
		}
	}
}

func hasInitialEventsEnd(object []byte) bool {
	return bytes.Contains(object, []byte(metav1.InitialEventsAnnotationKey)) && bytes.Contains(object, []byte("true"))
}

func healthCheck(send func() bool, requestURL string) error {
	for i := 0; i < HealthCheckRequests; i++ {
		if send() {
			return nil
		}
	}
	return fmt.Errorf("could not successfully send a request to %s", requestURL)
}

func sendRequest(ctx context.Context, client *http.Client, url url.URL, rateLimiter flowcontrol.RateLimiter, verb, acceptContentType string) bool {
	req, err := http.NewRequestWithContext(ctx, verb, url.String(), nil)
	if err != nil {
		log.Printf("Got error creating a request: %v\n", err)
		return false
	}

	req.Header.Set("Accept", acceptContentType)

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
	expectedMediaType, _, _ := mime.ParseMediaType(acceptContentType)
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
