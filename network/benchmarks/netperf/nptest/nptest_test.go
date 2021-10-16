package main

import "testing"

func TestParseQperfTCPLatency(t *testing.T) {
	input := `
tcp_bw:
	bw  =  5.07 GB/sec
tcp_lat:
	latency  =  15.6 us
`

	expected :="(bw = 5.07 GB/sec; latency = 15.6 us)"
	output := parseQperfTCPLatency(input)

	if output != expected {
		t.Fatalf("Expected: %s, Got: %s", expected, output)
	}

}
