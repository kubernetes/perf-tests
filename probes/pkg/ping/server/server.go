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

package pingserver

import (
	"flag"
	"net/http"

	"k8s.io/klog"
)

var (
	pingServerBindAddress = flag.String("ping-server-bind-address", "", "The address to bind for the ping server")
)

// PingServerConfig configures the "ping-server" probe.
type PingServerConfig struct {
	pingServerBindAddress string
}

// NewDefaultPingServerConfig creates a default "ping-server" config.
func NewDefaultPingServerConfig() *PingServerConfig {
	if *pingServerBindAddress == "" {
		klog.Fatal("--ping-server-bind-address not set!")
	}
	return &PingServerConfig{
		pingServerBindAddress: *pingServerBindAddress,
	}
}

// Run runs the ping server.
func Run(config *PingServerConfig) {
	klog.Infof("Listening on %s \n", config.pingServerBindAddress)
	http.HandleFunc("/", pong)
	klog.Fatal(http.ListenAndServe(config.pingServerBindAddress, nil))
}

func pong(w http.ResponseWriter, r *http.Request) {
	klog.V(4).Infof("pong -> %s\n", r.RemoteAddr)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("pong"))
}
