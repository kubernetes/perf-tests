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

package prom

import (
	"time"
)

// Client provides interface for communicating with the Prometheus API.
type Client interface {
	// Query sends a GET request to Prometheus with the "query" field
	// in the URL's query string set using the provided arguments.
	Query(query string, queryTime time.Time) ([]byte, error)
}
