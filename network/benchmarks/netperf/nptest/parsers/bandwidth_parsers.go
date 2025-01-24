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

package parsers

import (
	"encoding/json"
	"regexp"
	"strconv"
)

func ParseIperfTCPBandwidth(output string) (bw float64, mss int) {
	var iperfTCPoutput IperfTCPCommandOutput

	err := json.Unmarshal([]byte(output), &iperfTCPoutput)
	if err != nil {
		return 0, 0
	}

	bw = iperfTCPoutput.End.SumSent.BitsPerSecond / 1e6
	mss = iperfTCPoutput.Start.TCPMss

	return bw, mss
}

func ParseIperfUDPBandwidth(output string) (bw float64, mss int) {
	var iperfUDPOutput IperfUDPCommandOutput

	err := json.Unmarshal([]byte(output), &iperfUDPOutput)
	if err != nil {
		return 0, 0
	}

	return iperfUDPOutput.End.Sum.BitsPerSecond / 1e6, 0
}

func ParseNetperfBandwidth(output string) (bw float64, mss int) {
	// Parses the output of netperf and grabs the Bbits/sec from the output
	netperfOutputRegexp := regexp.MustCompile("\\s+\\d+\\s+\\d+\\s+\\d+\\s+\\S+\\s+(\\S+)\\s+")
	match := netperfOutputRegexp.FindStringSubmatch(output)
	if len(match) > 1 {
		floatVal, _ := strconv.ParseFloat(match[1], 64)
		return floatVal, 0
	}
	return 0, 0
}
