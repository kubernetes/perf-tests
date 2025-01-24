package parsers_test

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"k8s.io/perf-tests/network/nptest/parsers"
)

func Test_ParseIperfTCPResults(t *testing.T) {
	tcpJSONFileName := "testdata/tcp.json"
	tcpJSONFile, err := os.Open(tcpJSONFileName)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", tcpJSONFileName, err)
	}
	defer tcpJSONFile.Close()

	tcpOutputFileName := "testdata/tcp_output.json"
	tcpOutputFile, err := os.Open(tcpOutputFileName)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", tcpOutputFileName, err)
	}
	defer tcpOutputFile.Close()

	tcpJSON, err := io.ReadAll(tcpJSONFile)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", tcpJSONFileName, err)
	}
	tcpOutputJSON, err := io.ReadAll(tcpOutputFile)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", tcpOutputFileName, err)
	}

	var iperfOutput parsers.IperfTCPCommandOutput

	err = json.Unmarshal(tcpJSON, &iperfOutput)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	var tcpExpectedOutput parsers.IperfTCPParsedResult
	var tcpActualOutput parsers.IperfTCPParsedResult

	tcpParsedFncOutput := parsers.ParseIperfTCPResults(string(tcpJSON))
	_ = json.Unmarshal([]byte(tcpParsedFncOutput), &tcpActualOutput)
	_ = json.Unmarshal([]byte(tcpOutputJSON), &tcpExpectedOutput)

	if tcpActualOutput != tcpExpectedOutput {
		t.Fatalf("Expected %v, got %v", tcpExpectedOutput, tcpActualOutput)
	}

	tcpBw, tcpMSS := parsers.ParseIperfTCPBandwidth(string(tcpJSON))
	if tcpBw != 14088.0 {
		t.Fatalf("Expected 14088.000000, got %f", tcpBw)
	}
	if tcpMSS != 96 {
		t.Fatalf("Expected 96, got %d", tcpMSS)
	}
}

func Test_ParseIperfUDPResults(t *testing.T) {
	udpJSONFileName := "testdata/udp.json"
	udpJSONFile, err := os.Open(udpJSONFileName)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", udpJSONFileName, err)
	}
	defer udpJSONFile.Close()

	udpOutputFileName := "testdata/udp_output.json"
	udpOutputFile, err := os.Open(udpOutputFileName)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", udpOutputFileName, err)
	}
	defer udpOutputFile.Close()

	udpJSON, err := io.ReadAll(udpJSONFile)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", udpJSONFileName, err)
	}
	udpOutputJSON, err := io.ReadAll(udpOutputFile)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", udpOutputFileName, err)
	}

	var iperfOutput parsers.IperfUDPCommandOutput

	err = json.Unmarshal(udpJSON, &iperfOutput)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	var udpExpectedOutput parsers.IperfUDPParsedResult
	var udpActualOutput parsers.IperfUDPParsedResult

	udpParsedFncOutput := parsers.ParseIperfUDPResults(string(udpJSON))
	t.Logf("udp_parsed_fnc_output: %s", udpParsedFncOutput)
	_ = json.Unmarshal([]byte(udpParsedFncOutput), &udpActualOutput)
	_ = json.Unmarshal([]byte(udpOutputJSON), &udpExpectedOutput)

	if udpActualOutput != udpExpectedOutput {
		t.Fatalf("Expected %v, got %v", udpExpectedOutput, udpActualOutput)
	}

	udpBw, udpMSS := parsers.ParseIperfUDPBandwidth(string(udpJSON))
	if udpBw != 1407.34 {
		t.Fatalf("Expected 1407.34, got %f", udpBw)
	}
	if udpMSS != 0 {
		t.Fatalf("Expected 0, got %d", udpMSS)
	}
}
