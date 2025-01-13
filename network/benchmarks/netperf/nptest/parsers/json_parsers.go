package parsers

import (
	"encoding/json"
	"math/bits"
)

func ParseIperfTcpResults(output string) string {
	var iperfOutput IperfTcpCommandOutput

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

	var outputResult IperfTcpParsedResult
	outputResult.TestInfo = IperfTestInfo{
		Protocol: iperfOutput.Start.TestStart.Protocol,
		Streams:  iperfOutput.Start.TestStart.NumStreams,
		BlkSize:  iperfOutput.Start.TestStart.BlkSize,
		Duration: iperfOutput.Start.TestStart.Duration,
		Mss:      iperfOutput.Start.TcpMss,
	}
	outputResult.TotalThroughput = iperfOutput.End.SumSent.BitsPerSecond / 1e6
	outputResult.MeanRoundTripTime = float64(sumMeanRtt) / float64(len(iperfOutput.End.Streams))
	outputResult.MinRoundTripTime = minRtt
	outputResult.MaxRoundTripTime = maxRtt
	outputResult.Retransmits = iperfOutput.End.SumSent.Retransmits
	outputResult.CPUUtilization = iperfOutput.End.CPUUtilizationPercent

	parsedJson, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJson)
}

func ParseIperfUdpResults(output string) string {
	var iperfOutput IperfUdpCommandOutput

	err := json.Unmarshal([]byte(output), &iperfOutput)
	if err != nil {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	// Calculate the total out of order packets
	var totalOutOfOrderPackets int
	for _, stream := range iperfOutput.End.Streams {
		totalOutOfOrderPackets += stream.Udp.OutOfOrderPackets
	}

	var outputResult IperfUdpParsedResult
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

	parsedJson, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJson)
}
