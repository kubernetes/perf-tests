/*
Copyright 2018 The Kubernetes Authors.

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

package util

import (
	"bufio"
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport"
	"k8s.io/client-go/transport/spdy"
)

func ProxyRequestToPod(config *rest.Config, namespace, podName, scheme string, containerPort, localPort int32, path string) ([]byte, error) {
	mappings := []string{fmt.Sprintf("%d:%d", localPort, containerPort)}
	cancel, err := setupForwarding(config, namespace, podName, mappings)
	defer cancel()
	if err != nil {
		return nil, err
	}
	var query string
	if strings.Contains(path, "?") {
		elm := strings.SplitN(path, "?", 2)
		path = elm[0]
		query = elm[1]
	}
	reqURL := url.URL{Scheme: scheme, Path: path, RawQuery: query, Host: fmt.Sprintf("127.0.0.1:%d", localPort)}
	resp, err := sendRequest(config, reqURL.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func setupForwarding(config *rest.Config, namespace, podName string, mappings []string) (cancel func(), err error) {
	hostIP := strings.TrimLeft(config.Host, "https://")

	trans, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return noop, err
	}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: trans}, http.MethodPost, &url.URL{Scheme: "https", Path: fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", namespace, podName), Host: hostIP})

	var berr, bout bytes.Buffer
	buffErr := bufio.NewWriter(&berr)
	buffOut := bufio.NewWriter(&bout)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})

	fw, err := portforward.New(dialer, mappings, stopCh, readyCh, buffOut, buffErr)
	if err != nil {
		return noop, err
	}
	go func() {
		fmt.Print(fw.ForwardPorts())
	}()
	<-readyCh
	return func() {
		stopCh <- struct{}{}
	}, nil
}

func sendRequest(config *rest.Config, url string) (*http.Response, error) {
	tsConfig, err := config.TransportConfig()
	if err != nil {
		return nil, err
	}
	tsConfig.TLS.Insecure = true
	tsConfig.TLS.CAData = []byte{}
	tsConfig.TLS.CAFile = ""

	ts, err := transport.New(tsConfig)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: ts}
	return client.Get(url)
}

func noop() {}
