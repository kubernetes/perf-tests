/*
Copyright 2023 The Kubernetes Authors.

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

import "k8s.io/apimachinery/pkg/version"

func GetApiserverSLI(clusterVersion version.Info) string {
	if clusterVersion.Major == "1" && clusterVersion.Minor < "23" {
		return "apiserver_request_duration_seconds"
	}
	if clusterVersion.Major == "1" && clusterVersion.Minor < "26" {
		return "apiserver_request_slo_duration_seconds"
	}
	return "apiserver_request_sli_duration_seconds"
}

func GetApiserverLatency(clusterVersion version.Info) string {
	if clusterVersion.Major == "1" && clusterVersion.Minor < "23" {
		return "apiserver:apiserver_request_latency_1m:histogram_quantile"
	}
	if clusterVersion.Major == "1" && clusterVersion.Minor < "26" {
		return "apiserver:apiserver_request_slo_latency_1m:histogram_quantile"
	}
	return "apiserver:apiserver_request_sli_latency_1m:histogram_quantile"
}
