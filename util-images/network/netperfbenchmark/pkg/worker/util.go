/*
Copyright 2020 The Kubernetes Authors.

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

package worker

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"k8s.io/klog"
)

// Protocols supported.
const (
	ProtocolTCP  = "TCP"
	ProtocolUDP  = "UDP"
	ProtocolHTTP = "HTTP"
)

// Number of metrics for each protocol.
const (
	tcpMetricsCount  = 2
	udpMetricsCount  = 11
	httpMetricsCount = 12
)

// Iperf results vary from protocol,required for proper parsing.
const (
	zeroFormatTCP         = "0.0"
	fractionZeroFormatTCP = ".0"
	zeroFormatUDP         = "0.00"
	fractionZeroFormatUDP = ".00"
)

var (
	unitRegex        = regexp.MustCompile(`%|\[\s+|\]\s+|KBytes\s+|KBytes/sec\s*|sec\s+|pps\s*|ms\s+|/|\(|\)\s+`)
	multiSpaceRegex  = regexp.MustCompile(`\s+`)
	hyphenSpaceRegex = regexp.MustCompile(`\-\s+`)
)

// Function to be applied for each metric for aggregation.
var (
	iperfUDPFunction = []string{"", "", "", "Sum", "Sum", "Sum", "Sum", "Sum", "Avg", "Avg", "Min", "Max", "Avg", "Sum"}
	iperfTCPFunction = []string{"", "", "", "Sum", "Avg"}
)

// parseResult parses the response received for each protocol type.
func parseResult(protocol string, result []string, testDuration string) ([]float64, error) {
	switch protocol {
	case ProtocolTCP:
		return parseIperfResponse(result, testDuration, tcpMetricsCount, ProtocolTCP, zeroFormatTCP, fractionZeroFormatTCP, iperfTCPFunction)
	case ProtocolUDP:
		return parseIperfResponse(result, testDuration, udpMetricsCount, ProtocolUDP, zeroFormatUDP, fractionZeroFormatUDP, iperfUDPFunction)
	case ProtocolHTTP:
		return parseSiegeResponse(result)
	default:
		return nil, errors.New("invalid protocol: " + protocol)
	}
}

func parseIperfResponse(result []string, testDuration string, metricCount int, protocol, zeroFormat, fractionZeroFormat string, operators []string) ([]float64, error) {
	aggregatedResult := make([]float64, 0, metricCount)
	count := 0
	sessionID := make(map[string]bool)
	interval := zeroFormat + "-" + testDuration + fractionZeroFormat
	for _, line := range result {
		split := parseIperfLine(line)
		// iperf gives aggregated result in a row with "SUM", but many fields are not
		// getting aggregated hence not depending on iperf's aggregation.
		if len(split) < metricCount+1 || "SUM" == split[1] || split[2] != interval {
			continue
		}
		// iperf sometimes prints duplicate rows for same session id(for same duration), making sure
		// the row is taken for calculation only once.
		if _, ok := sessionID[split[1]]; ok {
			continue
		}
		sessionID[split[1]] = true
		// The first three and the last items are not metrics in iperf summary.
		for i := 3; i < len(split)-1; i++ {
			tmp, err := strconv.ParseFloat(split[i], 64)
			if err != nil {
				klog.Errorf("Error converting %v to float: %v", split[i], err)
				return nil, err
			}
			if len(aggregatedResult) < metricCount {
				aggregatedResult = append(aggregatedResult, tmp)
			} else {
				switch operators[i] {
				case "Sum":
					aggregatedResult[i-3] = tmp + aggregatedResult[i-3]
				case "Avg":
					aggregatedResult[i-3] = (float64(count)*tmp + aggregatedResult[i-3]) / (float64(1 + count))
				case "Min":
					aggregatedResult[i-3] = math.Min(tmp, aggregatedResult[i-3])
				case "Max":
					aggregatedResult[i-3] = math.Max(tmp, aggregatedResult[i-3])
				}
			}
			count++
		}
	}
	klog.Infof("Final output: %v", aggregatedResult)
	return aggregatedResult, nil
}

func parseIperfLine(line string) []string {
	noUnitsLine := unitRegex.ReplaceAllString(line, " ")
	noExtraSpacesLine := multiSpaceRegex.ReplaceAllString(noUnitsLine, " ")
	formattedString := hyphenSpaceRegex.ReplaceAllString(noExtraSpacesLine, "-")
	return strings.Split(formattedString, " ")
}

func parseSiegeResponse(result []string) ([]float64, error) {
	result, err := trimSiegeResponse(result)
	if err != nil {
		return nil, err
	}
	aggregatedResult := make([]float64, 0, httpMetricsCount)
	for _, op := range result {
		formattedString := multiSpaceRegex.ReplaceAllString(op, " ")
		splitOnColon := strings.Split(formattedString, ":")
		if len(splitOnColon) <= 1 {
			continue
		}
		splitOnSpace := strings.Split(splitOnColon[1], " ")
		if len(splitOnSpace) < 2 {
			continue
		}
		tmp, err := strconv.ParseFloat(splitOnSpace[1], 64)
		if err != nil {
			klog.Errorf("Error converting %v to float: %v", splitOnSpace[1], err)
			return nil, err
		}
		aggregatedResult = append(aggregatedResult, tmp)
	}
	klog.Infof("Final output: %v", aggregatedResult)
	return aggregatedResult, nil
}

func trimSiegeResponse(result []string) ([]string, error) {
	var beginIndex, endIndex int
	isBeginLine := func(index int) bool {
		return strings.HasPrefix(result[index], "Transactions:")
	}
	for beginIndex = 0; beginIndex < len(result) && !isBeginLine(beginIndex); beginIndex++ {
	}
	if beginIndex == len(result) {
		return nil, errors.New("unexpected Siege response: lack of Transactions: line")
	}
	isEndLine := func(index int) bool {
		return strings.HasPrefix(result[index], "Shortest transaction")
	}
	for endIndex = beginIndex + 1; endIndex < len(result) && !isEndLine(endIndex); endIndex++ {
	}
	if endIndex == len(result) {
		return nil, errors.New("unexpected Siege response: lack of Shortest transaction line")
	}
	return result[beginIndex:endIndex], nil
}

// GetValuesFromURL returns a map with values parsed from http request,for attributes specified in paramsToSearch.
func getValuesFromURL(request *http.Request, parametersToSearch []string) (map[string]string, error) {
	values := request.URL.Query()
	paramMap := make(map[string]string)
	for _, param := range parametersToSearch {
		val := values.Get(param)
		if val == "" {
			return nil, fmt.Errorf("missing URL parameter: %s", param)
		}
		paramMap[param] = val
	}
	return paramMap, nil
}

// populateTemplates populates template parameters with actual values from the http request object.
func populateTemplates(arguments []string, request *http.Request) ([]string, error) {
	for i, argument := range arguments {
		if idx := strings.Index(argument, "{"); idx == -1 {
			continue
		}
		parameter := argument[strings.Index(argument, "{")+1 : strings.Index(argument, "}")]
		valMap, err := getValuesFromURL(request, []string{parameter})
		if err != nil {
			return nil, err
		}
		arguments[i] = strings.Replace(argument, "{"+parameter+"}", valMap[parameter], 1)
		klog.Infof("Value after resolving template %v: %v", parameter, arguments[i])
	}
	return arguments, nil
}
