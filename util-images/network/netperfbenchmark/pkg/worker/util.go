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
	"bufio"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
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

var (
	unitRegex        = regexp.MustCompile(`%|\[\s+|\]\s+|KBytes\s+|KBytes/sec\s*|sec\s+|pps\s*|ms\s+|/|\(|\)\s+`)
	multiSpaceRegex  = regexp.MustCompile(`\s+`)
	hyphenSpaceRegex = regexp.MustCompile(`\-\s+`)
	intervalRegex    = regexp.MustCompile(`\d+\.\d+\-\s*\d+\.\d+`)
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
		return parseIperfResponse(result, testDuration, tcpMetricsCount, ProtocolTCP, iperfTCPFunction)
	case ProtocolUDP:
		return parseIperfResponse(result, testDuration, udpMetricsCount, ProtocolUDP, iperfUDPFunction)
	case ProtocolHTTP:
		return parseSiegeResponse(result)
	default:
		return nil, errors.New("invalid protocol: " + protocol)
	}
}

func parseIperfResponse(result []string, testDuration string, metricCount int, protocol string, operators []string) ([]float64, error) {
	aggregatedResult := make([]float64, 0, metricCount)
	count := 0
	sessionID := make(map[string]bool)
	for _, line := range result {
		klog.V(4).Info(line)
		split := parseIperfLine(line)
		// iperf gives aggregated result in a row with "SUM", but many fields are not
		// getting aggregated hence not depending on iperf's aggregation.
		if len(split) < metricCount+1 || "SUM" == split[1] || !intervalRegex.MatchString(split[2]) {
			continue
		}
		// iperf sometimes prints duplicate rows for same session id(for same duration), making sure
		// the row is taken for calculation only once.
		if _, isDuplicate := sessionID[split[1]]; isDuplicate {
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

func extractParameters(input resourceProperties) map[string]string {
	result := map[string]string{}
	result["duration"] = strconv.FormatInt(input.Duration, 10)
	result["numberOfClients"] = strconv.FormatInt(input.NumberOfClients, 10)
	result["serverPodIP"] = input.ServerPodIP
	return result
}

// populateTemplates populates template parameters with actual values from the http request object.
func populateTemplates(arguments []string, requestProperties map[string]string) ([]string, error) {
	for i, argument := range arguments {
		if idx := strings.Index(argument, "{"); idx == -1 {
			continue
		}
		parameter := argument[strings.Index(argument, "{")+1 : strings.Index(argument, "}")]
		value, isPresent := requestProperties[parameter]
		if !isPresent {
			return nil, fmt.Errorf("property %v not present in request", parameter)
		}
		arguments[i] = strings.Replace(argument, "{"+parameter+"}", value, 1)
		klog.Infof("Value after resolving template %v: %v", parameter, arguments[i])
	}
	return arguments, nil
}

func getInformer(labelSelector, namespace string, k8sClient dynamic.Interface, gvr schema.GroupVersionResource) (cache.SharedInformer, error) {
	optionsModifier := func(options *metav1.ListOptions) {
		options.LabelSelector = labelSelector
	}
	tweakListOptions := dynamicinformer.TweakListOptionsFunc(optionsModifier)
	dynamicInformerFactory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(k8sClient, 0, namespace, tweakListOptions)
	informer := dynamicInformerFactory.ForResource(gvr).Informer()
	return informer, nil
}

func getDynamicClient() (dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed fetching cluster config: %s", err)
	}
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed getting dynamic client: %s", err)
	}
	return dynamicClient, nil
}

func executeCommand(commandString string, arguments []string) ([]string, error) {
	command := exec.Command(commandString, arguments...)
	out, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errorOut, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	multiOut := io.MultiReader(out, errorOut)
	if err := command.Start(); err != nil {
		return nil, err
	}
	return scanOutput(multiOut)
}

func scanOutput(out io.Reader) ([]string, error) {
	var result []string
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		if line := scanner.Text(); len(line) > 0 {
			result = append(result, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func makeErrorStatus(err error) map[string]interface{} {
	return map[string]interface{}{"error": err}
}

func makeSuccessStatus(metrics []float64, delay float64) map[string]interface{} {
	return map[string]interface{}{"metrics": metrics, "workerDelay": delay}
}
