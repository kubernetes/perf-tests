/*
Copyright 2020 The Kubernetes Authors.

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
	goflag "flag"
	flag "github.com/spf13/pflag"
	"io/ioutil"
	"k8s.io/client-go/rest"
	"net"
	"os"
	"path/filepath"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	certutil "k8s.io/client-go/util/cert"
	flowcontrol "k8s.io/client-go/util/flowcontrol"
	"k8s.io/klog"
)

var (
	namespace = flag.String("namespace", "", "Namespace that is targeted with request flow.")
	tokenDirs = flag.StringSlice("access-token-dirs", nil, "Directories with access-token. A worker will be started per each dir/token")
	qps       = flag.Float64("qps-per-worker", 0.5, "QPS per worker")
)

func main() {
	initFlagsAndKlog()
	for i, tokenDir := range *tokenDirs {
		tokenFile := filepath.Join(tokenDir, "token")
		rootCAFile := filepath.Join(tokenDir, "ca.crt")
		config, err := newConfig(tokenFile, rootCAFile)
		if err != nil {
			klog.Fatal(err.Error())
		}
		client, err := kubernetes.NewForConfig(config)
		if err != nil {
			klog.Fatal(err.Error())
		}
		klog.Infof("Starting worker %d\n", i)
		go worker(i, client, *qps)
	}
	klog.Infof("Started %d workers \n", len(*tokenDirs))
	if len(*tokenDirs) > 0 {
		<-make(chan bool) // Block main routine.
	}
}

func initFlagsAndKlog() {
	flag.Set("alsologtostderr", "true")
	klogFlags := goflag.NewFlagSet("klog", goflag.ExitOnError)
	klog.InitFlags(klogFlags)
	flag.CommandLine.AddGoFlagSet(klogFlags)
	flag.Parse()
}

func makeRequest(id int, client kubernetes.Interface) {
	klog.V(4).Infof("Worker %v sends request\n", id)
	svcAccount, err := client.CoreV1().ServiceAccounts(*namespace).Get("default", metav1.GetOptions{ResourceVersion: "0"})
	if err != nil {
		klog.Warningf("Got error when getting default svcAccount: %v", err)
	} else {
		klog.V(4).Infof("Worker %v fetched %s svcAccount in namespace %s\n", id, svcAccount.Name, svcAccount.Namespace)
	}
}

func worker(id int, client kubernetes.Interface, qps float64) {
	duration := time.Duration(float64(int64(time.Second)) / qps)
	ticker := time.NewTicker(duration)
	for {
		go makeRequest(id, client)
		<-ticker.C
	}
}

func newConfig(tokenFile, rootCAFile string) (*rest.Config, error) {
	host, port := os.Getenv("KUBERNETES_SERVICE_HOST"), os.Getenv("KUBERNETES_SERVICE_PORT")
	if len(host) == 0 || len(port) == 0 {
		return nil, rest.ErrNotInCluster
	}
	token, err := ioutil.ReadFile(tokenFile)
	if err != nil {
		return nil, err
	}
	tlsClientConfig := rest.TLSClientConfig{}
	if _, err := certutil.NewPool(rootCAFile); err != nil {
		klog.Errorf("Expected to load root CA config from %s, but got err: %v", rootCAFile, err)
	} else {
		tlsClientConfig.CAFile = rootCAFile
	}
	return &rest.Config{
		Host:            "https://" + net.JoinHostPort(host, port),
		TLSClientConfig: tlsClientConfig,
		BearerToken:     string(token),
		BearerTokenFile: tokenFile,
		// We are already rate limiting in func worker - we do not need another limiter here
		RateLimiter: flowcontrol.NewFakeAlwaysRateLimiter(),
	}, nil
}
