package parsers

import (
	"regexp"
)

func ParseIperfTCPBandwidth(output string) string {
	// Parses the output of iperf3 and grabs the group Mbits/sec from the output
	iperfTCPOutputRegexp := regexp.MustCompile("SUM.*\\s+(\\d+)\\sMbits/sec\\s+receiver")
	match := iperfTCPOutputRegexp.FindStringSubmatch(output)
	if len(match) > 1 {
		return match[1]
	}
	return "0"
}

func ParseIperfSctpBandwidth(output string) string {
	// Parses the output of iperf3 and grabs the group Mbits/sec from the output
	iperfSCTPOutputRegexp := regexp.MustCompile("SUM.*\\s+(\\d+)\\sMbits/sec\\s+receiver")
	match := iperfSCTPOutputRegexp.FindStringSubmatch(output)
	if len(match) > 1 {
		return match[1]
	}
	return "0"
}

func ParseIperfUDPBandwidth(output string) string {
	// Parses the output of iperf3 (UDP mode) and grabs the Mbits/sec from the output
	iperfUDPOutputRegexp := regexp.MustCompile("\\s+(\\S+)\\sMbits/sec\\s+\\S+\\s+ms\\s+")
	match := iperfUDPOutputRegexp.FindStringSubmatch(output)
	if len(match) > 1 {
		return match[1]
	}
	return "0"
}

func ParseNetperfBandwidth(output string) string {
	// Parses the output of netperf and grabs the Bbits/sec from the output
	netperfOutputRegexp := regexp.MustCompile("\\s+\\d+\\s+\\d+\\s+\\d+\\s+\\S+\\s+(\\S+)\\s+")
	match := netperfOutputRegexp.FindStringSubmatch(output)
	if len(match) > 1 {
		return match[1]
	}
	return "0"
}
