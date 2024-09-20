package lib

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	api "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

func getLogsFromPod(c *kubernetes.Clientset, podName, testNamespace string) (*string, error) {
	body, err := c.CoreV1().Pods(testNamespace).GetLogs(podName, &api.PodLogOptions{Timestamps: false}).DoRaw(context.Background())
	if err != nil {
		return nil, fmt.Errorf("error (%s) reading logs from pod %s", err, podName)
	}
	logData := string(body)
	return &logData, nil
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

func processRawData(rawData *string, testNamespace, tag, fileExtension string) error {
	t := time.Now().UTC()
	outputFileDirectory := fmt.Sprintf("results_%s-%s", testNamespace, tag)
	outputFilePrefix := fmt.Sprintf("%s-%s_%s.", testNamespace, tag, t.Format("20060102150405"))
	outputFilePath := fmt.Sprintf("%s/%s%s", outputFileDirectory, outputFilePrefix, fileExtension)
	fmt.Printf("Test concluded - Raw data written to %s\n", outputFilePath)
	if _, err := os.Stat(outputFileDirectory); os.IsNotExist(err) {
		err := os.Mkdir(outputFileDirectory, 0766)
		if err != nil {
			return err
		}
	}
	fd, err := os.OpenFile(outputFilePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return fmt.Errorf("ERROR writing output datafile: %s", err)
	}
	defer fd.Close()
	_, err = fd.WriteString(*rawData)
	if err != nil {
		return fmt.Errorf("error writing string: %s", err)
	}
	return nil
}
