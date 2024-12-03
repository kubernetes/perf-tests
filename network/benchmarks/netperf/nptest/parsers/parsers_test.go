package parsers_test

import (
	"encoding/json"
	"io"
	"os"
	"testing"

	"k8s.io/perf-tests/network/nptest/parsers"
)

func Test_ParseIperfTcpResults(t *testing.T) {
	tcp_json_file_name := "testdata/tcp.json"
	tcp_json_file, err := os.Open(tcp_json_file_name)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", tcp_json_file_name, err)
	}
	defer tcp_json_file.Close()

	tcp_output_file_name := "testdata/tcp_output.json"
	tcp_output_file, err := os.Open(tcp_output_file_name)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", tcp_output_file_name, err)
	}
	defer tcp_output_file.Close()

	tcp_json, err := io.ReadAll(tcp_json_file)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", tcp_json_file_name, err)
	}
	tcp_output_json, err := io.ReadAll(tcp_output_file)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", tcp_output_file_name, err)
	}

	var iperfOutput parsers.IperfTcpCommandOutput

	err = json.Unmarshal(tcp_json, &iperfOutput)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	var tcp_expected_output parsers.IperfTcpParsedResult
	var tcp_actual_output parsers.IperfTcpParsedResult

	tcp_parsed_fnc_output := parsers.ParseIperfTcpResults(string(tcp_json))
	_ = json.Unmarshal([]byte(tcp_parsed_fnc_output), &tcp_actual_output)
	_ = json.Unmarshal([]byte(tcp_output_json), &tcp_expected_output)

	if tcp_actual_output != tcp_expected_output {
		t.Fatalf("Expected %v, got %v", tcp_expected_output, tcp_actual_output)
	}

	tcp_bw, tcp_mss := parsers.ParseIperfTCPBandwidth(string(tcp_json))
	if tcp_bw != 14088.0 {
		t.Fatalf("Expected 14088.000000, got %f", tcp_bw)
	}
	if tcp_mss != 96 {
		t.Fatalf("Expected 96, got %d", tcp_mss)
	}
}

func Test_ParseIperfUdpResults(t *testing.T) {
	udp_json_file_name := "testdata/udp.json"
	udp_json_file, err := os.Open(udp_json_file_name)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", udp_json_file_name, err)
	}
	defer udp_json_file.Close()

	udp_output_file_name := "testdata/udp_output.json"
	udp_output_file, err := os.Open(udp_output_file_name)
	if err != nil {
		t.Fatalf("Failed to open file %s: %v", udp_output_file_name, err)
	}
	defer udp_output_file.Close()

	udp_json, err := io.ReadAll(udp_json_file)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", udp_json_file_name, err)
	}
	udp_output_json, err := io.ReadAll(udp_output_file)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", udp_output_file_name, err)
	}

	var iperfOutput parsers.IperfUdpCommandOutput

	err = json.Unmarshal(udp_json, &iperfOutput)
	if err != nil {
		t.Fatalf("Failed to unmarshal JSON: %v", err)
	}

	var udp_expected_output parsers.IperfUdpParsedResult
	var udp_actual_output parsers.IperfUdpParsedResult

	udp_parsed_fnc_output := parsers.ParseIperfUdpResults(string(udp_json))
	t.Logf("udp_parsed_fnc_output: %s", udp_parsed_fnc_output)
	_ = json.Unmarshal([]byte(udp_parsed_fnc_output), &udp_actual_output)
	_ = json.Unmarshal([]byte(udp_output_json), &udp_expected_output)

	if udp_actual_output != udp_expected_output {
		t.Fatalf("Expected %v, got %v", udp_expected_output, udp_actual_output)
	}

	udp_bw, udp_mss := parsers.ParseIperfUDPBandwidth(string(udp_json))
	if udp_bw != 1407.34 {
		t.Fatalf("Expected 1407.34, got %f", udp_bw)
	}
	if udp_mss != 0 {
		t.Fatalf("Expected 0, got %d", udp_mss)
	}
}
