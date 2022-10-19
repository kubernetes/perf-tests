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
	"log"
	"strings"
)

const (
	HealthCheckRequests = 10
	NamespaceTmpl       = "%namespace%"
)

var (
	inflight  = flag.Int("inflight", 0, "Benchmark inflight (number of parallel requests being made to the apiserver")
	namespace = flag.String("namespace", "", "Replace %namespace% in URI with provided namespace")
	URI       = flag.String("uri", "", "Request URI")
)

func init() {
	flag.Parse()
}

func main() {
	client, err := GetClient()
	if err != nil {
		panic(err)
	}

	newURI := strings.ReplaceAll(*URI, NamespaceTmpl, *namespace)
	URI = &newURI

	if err := healthCheck(client); err != nil {
		panic(err)
	}

	log.Printf("Sending requests to '%s' with inflight %d. Press Ctrl+C to stop...", *URI, *inflight)
	for i := 0; i < *inflight; i++ {
		go func() {
			for {
				sendRequest(client)
			}
		}()
	}

	select {} // block main thread from ending
}

func healthCheck(client *Client) error {
	for i := 0; i < HealthCheckRequests; i++ {
		if sendRequest(client) {
			return nil
		}
	}
	return fmt.Errorf("could not successfully send a request to %s", *URI)
}

func sendRequest(client *Client) bool {
	request := client.RESTClient().Get().RequestURI(*URI)
	response := client.TimedRequest(context.Background(), request)

	if err := response.Error; err != nil {
		log.Printf("Got error after sending a request: %v\n%s", err, string(response.Raw))
		return false
	}

	log.Printf("Got response of %d bytes in %v", len(response.Raw), response.Duration)
	return true
}
