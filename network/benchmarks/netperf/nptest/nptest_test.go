/*
Copyright 2016 The Kubernetes Authors.

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

import "testing"

func TestParseQperfTCPLatency(t *testing.T) {
	input := `
tcp_bw:
	bw  =  5.07 GB/sec
tcp_lat:
	latency  =  15.6 us
`

	expected := "(bw = 5.07 GB/sec; latency = 15.6 us)"
	output := parseQperfTCPLatency(input)

	if output != expected {
		t.Fatalf("Expected: %s, Got: %s", expected, output)
	}

}
