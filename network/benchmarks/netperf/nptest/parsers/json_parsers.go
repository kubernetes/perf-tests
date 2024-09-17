package parsers

import (
	"encoding/json"
	"fmt"
	"strconv"
)

func ParseIperfThrouputTCPTest(output string) string {
	var iperfThroughput struct {
		End struct {
			Streams []struct {
				Sender struct {
					BitsPerSecond     float64 `json:"bits_per_second"`
					MeanRoundTripTime int     `json:"mean_rtt"`
					MinRoundTripTime  int     `json:"min_rtt"`
					MaxRoundTripTime  int     `json:"max_rtt"`
					Retransmits       int     `json:"retransmits"`
				} `json:"sender"`
			} `json:"streams"`
			CPUUtilizationPercent struct {
				HostTotal   float64 `json:"host_total"`
				RemoteTotal float64 `json:"remote_total"`
			} `json:"cpu_utilization_percent"`
		} `json:"end"`
	}

	fmt.Println("Parsing iperf output\n", output)
	fmt.Println("End of iperf output")

	err := json.Unmarshal([]byte(output), &iperfThroughput)
	if err != nil {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	if len(iperfThroughput.End.Streams) != 1 {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"Expected 1 stream, got " + strconv.Itoa(len(iperfThroughput.End.Streams)) + "\"}"
	}

	var outputResult struct {
		TotalThroughput   float64 `json:"total_throughput"`
		MeanRoundTripTime int     `json:"mean_rtt"`
		MinRoundTripTime  int     `json:"min_rtt"`
		MaxRoundTripTime  int     `json:"max_rtt"`
		Retransmits       int     `json:"retransmits"`
		CPUUtilization    struct {
			Host   float64 `json:"host"`
			Remote float64 `json:"remote"`
		} `json:"cpu_utilization"`
	}

	outputResult.TotalThroughput = iperfThroughput.End.Streams[0].Sender.BitsPerSecond / 1e6
	outputResult.MeanRoundTripTime = iperfThroughput.End.Streams[0].Sender.MeanRoundTripTime
	outputResult.MinRoundTripTime = iperfThroughput.End.Streams[0].Sender.MinRoundTripTime
	outputResult.MaxRoundTripTime = iperfThroughput.End.Streams[0].Sender.MaxRoundTripTime
	outputResult.CPUUtilization.Host = iperfThroughput.End.CPUUtilizationPercent.HostTotal
	outputResult.CPUUtilization.Remote = iperfThroughput.End.CPUUtilizationPercent.RemoteTotal

	parsedJson, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJson)
}

func ParseIperfThrouputUDPTest(output string) string {
	fmt.Println("Parsing iperf output\n", output)
	var iperfThroughput struct {
		End struct {
			Sum struct {
				BitsPerSecond float64 `json:"bits_per_second"`
				Jitter        float64 `json:"jitter_ms"`
				LostPackets   int     `json:"lost_packets"`
				TotalPackets  int     `json:"packets"`
				LostPercent   float64 `json:"lost_percent"`
			} `json:"sum"`
			CPUUtilizationPercent struct {
				HostTotal   float64 `json:"host_total"`
				RemoteTotal float64 `json:"remote_total"`
			} `json:"cpu_utilization_percent"`
		} `json:"end"`
	}

	err := json.Unmarshal([]byte(output), &iperfThroughput)
	if err != nil {
		return "{\"error\": \"Failed to parse JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	var outputResult struct {
		TotalThroughput float64 `json:"total_throughput"`
		Jitter          float64 `json:"jitter_ms"`
		LostPackets     int     `json:"lost_packets"`
		TotalPackets    int     `json:"total_packets"`
		LostPercent     float64 `json:"lost_percent"`
		CPUUtilization  struct {
			Host   float64 `json:"host"`
			Remote float64 `json:"remote"`
		} `json:"cpu_utilization"`
	}

	outputResult.TotalThroughput = iperfThroughput.End.Sum.BitsPerSecond / 1e6
	outputResult.Jitter = iperfThroughput.End.Sum.Jitter
	outputResult.LostPackets = iperfThroughput.End.Sum.LostPackets
	outputResult.TotalPackets = iperfThroughput.End.Sum.TotalPackets
	outputResult.LostPercent = iperfThroughput.End.Sum.LostPercent
	outputResult.CPUUtilization.Host = iperfThroughput.End.CPUUtilizationPercent.HostTotal
	outputResult.CPUUtilization.Remote = iperfThroughput.End.CPUUtilizationPercent.RemoteTotal

	parsedJson, err := json.Marshal(outputResult)
	if err != nil {
		return "{\"error\": \"Failed to marshal JSON output\", \"message\": \"" + err.Error() + "\"}"
	}

	return string(parsedJson)
}
