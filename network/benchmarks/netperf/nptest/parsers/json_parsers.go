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
	"math/bits"
)

func ParseIperfTCPResults(output string) string {
	var iperfOutput IperfTCPCommandOutput

	err := json.Unmarshal([]byte(output), &iperfOutput)
	if err != nil {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	// Calculate the min, max and mean rtts by aggregating the streams
	var sumMeanRtt uint
	var minRtt uint = 1<<bits.UintSize - 1
	var maxRtt uint

	for _, stream := range iperfOutput.End.Streams {
		sumMeanRtt += stream.Sender.MeanRtt
		minRtt = min(minRtt, stream.Sender.MinRtt)
		maxRtt = max(maxRtt, stream.Sender.MaxRtt)
	}

	var outputResult IperfTCPParsedResult
	outputResult.TestInfo = IperfTestInfo{
		Protocol: iperfOutput.Start.TestStart.Protocol,
		Streams:  iperfOutput.Start.TestStart.NumStreams,
		BlkSize:  iperfOutput.Start.TestStart.BlkSize,
		Duration: iperfOutput.Start.TestStart.Duration,
		Mss:      iperfOutput.Start.TCPMss,
	}
	outputResult.TotalThroughput = iperfOutput.End.SumSent.BitsPerSecond / 1e6
	outputResult.MeanRoundTripTime = float64(sumMeanRtt) / float64(len(iperfOutput.End.Streams))
	outputResult.MinRoundTripTime = minRtt
	outputResult.MaxRoundTripTime = maxRtt
	outputResult.Retransmits = iperfOutput.End.SumSent.Retransmits
	outputResult.CPUUtilization = iperfOutput.End.CPUUtilizationPercent

	parsedJSON, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJSON)
}

func ParseIperfUDPResults(output string) string {
	var iperfOutput IperfUDPCommandOutput

	err := json.Unmarshal([]byte(output), &iperfOutput)
	if err != nil {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	// Calculate the total out of order packets
	var totalOutOfOrderPackets int
	for _, stream := range iperfOutput.End.Streams {
		totalOutOfOrderPackets += stream.UDP.OutOfOrderPackets
	}

	var outputResult IperfUDPParsedResult
	outputResult.TestInfo = IperfTestInfo{
		Protocol: iperfOutput.Start.TestStart.Protocol,
		Streams:  iperfOutput.Start.TestStart.NumStreams,
		BlkSize:  iperfOutput.Start.TestStart.BlkSize,
		Duration: iperfOutput.Start.TestStart.Duration,
	}
	outputResult.TotalThroughput = iperfOutput.End.Sum.BitsPerSecond / 1e6
	outputResult.Jitter = iperfOutput.End.Sum.Jitter
	outputResult.LostPackets = iperfOutput.End.Sum.LostPackets
	outputResult.TotalPackets = iperfOutput.End.Sum.Packets
	outputResult.LostPercent = iperfOutput.End.Sum.LostPercent
	outputResult.TotalOutOfOrderPackets = totalOutOfOrderPackets
	outputResult.CPUUtilization = iperfOutput.End.CPUUtilizationPercent

	parsedJSON, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJSON)
}
