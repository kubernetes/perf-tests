package parsers

import (
	"encoding/json"
	"regexp"
	"strconv"
)

func ParseIperfTCPBandwidth(output string) (bw float64, mss int) {
	var iperfTcpoutput IperfTcpCommandOutput

	err := json.Unmarshal([]byte(output), &iperfTcpoutput)
	if err != nil {
		return 0, 0
	}

	bw = iperfTcpoutput.End.SumSent.BitsPerSecond / 1e6
	mss = iperfTcpoutput.Start.TcpMss

	return bw, mss
}

func ParseIperfUDPBandwidth(output string) (bw float64, mss int) {
	var iperfUdpOutput IperfUdpCommandOutput

	err := json.Unmarshal([]byte(output), &iperfUdpOutput)
	if err != nil {
		return 0, 0
	}

	return iperfUdpOutput.End.Sum.BitsPerSecond / 1e6, 0
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
