package lib

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	api "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

func getLogsFromPod(c *kubernetes.Clientset, podName, testNamespace string) (*string, error) {
	var logData *string

	// Retry to get logs from the pod, as we are polling at intervals
	// and there might be intermittent network issues, a long retry time
	// is acceptable.
	err := retry.OnError(wait.Backoff{
		Steps:    5,
		Duration: 2 * time.Second,
		Factor:   2.0,
		Jitter:   100,
	}, func(err error) bool {
		return true
	}, func() error {
		body, err := c.CoreV1().Pods(testNamespace).GetLogs(podName, &api.PodLogOptions{}).DoRaw(context.Background())
		if err != nil {
			return err
		}
		data := string(body)
		logData = &data
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error reading logs from pod %s: %v", podName, err)
	}

	return logData, nil
}

func getDataFromPod(c *kubernetes.Clientset, podName, startMarker, endMarker, testNamespace string) (*string, error) {
	logData, err := getLogsFromPod(c, podName, testNamespace)
	if err != nil {
		return nil, err
	}
	index := strings.Index(*logData, startMarker)
	endIndex := strings.Index(*logData, endMarker)
	if index == -1 || endIndex == -1 {
		return nil, nil
	}
	data := string((*logData)[index+len(startMarker)+1 : endIndex])
	return &data, nil
}

func processRawData(rawData *string, testNamespace, tag, fileExtension string) (string, error) {
	t := time.Now().UTC()
	outputFileDirectory := fmt.Sprintf("results_%s-%s", testNamespace, tag)
	outputFilePrefix := fmt.Sprintf("%s-%s_%s.", testNamespace, tag, t.Format("20060102150405"))
	outputFilePath := fmt.Sprintf("%s/%s%s", outputFileDirectory, outputFilePrefix, fileExtension)
	fmt.Printf("Test concluded - Raw data written to %s\n", outputFilePath)
	if _, err := os.Stat(outputFileDirectory); os.IsNotExist(err) {
		err := os.Mkdir(outputFileDirectory, 0766)
		if err != nil {
			return "", err
		}
	}
	fd, err := os.OpenFile(outputFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return "", fmt.Errorf("ERROR writing output datafile: %s", err)
	}
	defer fd.Close()
	_, err = fd.WriteString(*rawData)
	if err != nil {
		return "", fmt.Errorf("error writing string: %s", err)
	}
	return outputFilePath, nil
}
