/*
Copyright 2018 The Kubernetes Authors.

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

package util

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
	prom "k8s.io/perf-tests/clusterloader2/pkg/prometheus/clients"
)

const (
	queryTimeout  = 15 * time.Minute
	queryInterval = 30 * time.Second
)

// ExtractMetricSamples unpacks metric blob into prometheus model structures.
func ExtractMetricSamples(metricsBlob string) ([]*model.Sample, error) {
	dec := expfmt.NewDecoder(strings.NewReader(metricsBlob), expfmt.FmtText)
	decoder := expfmt.SampleDecoder{
		Dec:  dec,
		Opts: &expfmt.DecodeOptions{},
	}

	var samples []*model.Sample
	for {
		var v model.Vector
		if err := decoder.Decode(&v); err != nil {
			if err == io.EOF {
				// Expected loop termination condition.
				return samples, nil
			}
			return nil, err
		}
		samples = append(samples, v...)
	}
}

// ExtractMetricSamples2 unpacks metric blob into prometheus model structures.
func ExtractMetricSamples2(response []byte) ([]*model.Sample, error) {
	var pqr promQueryResponse
	if err := json.Unmarshal(response, &pqr); err != nil {
		return nil, err
	}
	if pqr.Status != "success" {
		return nil, fmt.Errorf("non-success response status: %v", pqr.Status)
	}
	vector, ok := pqr.Data.v.(model.Vector)
	if !ok {
		return nil, fmt.Errorf("incorrect response type: %v", pqr.Data.v.Type())
	}
	return []*model.Sample(vector), nil
}

// promQueryResponse stores the response from the Prometheus server.
// This struct follows the format described in the Prometheus documentation:
// https://prometheus.io/docs/prometheus/latest/querying/api/#format-overview.
type promQueryResponse struct {
	Status    string           `json:"status"`
	Data      promResponseData `json:"data"`
	ErrorType string           `json:"errorType"`
	Error     string           `json:"error"`
	Warnings  []string         `json:"warnings"`
}

type promResponseData struct {
	v model.Value
}

// NewQueryExecutor creates instance of PrometheusQueryExecutor.
func NewQueryExecutor(pc prom.Client) *PrometheusQueryExecutor {
	return &PrometheusQueryExecutor{client: pc}
}

// PrometheusQueryExecutor executes queries against Prometheus.
type PrometheusQueryExecutor struct {
	client prom.Client
}

// Query executes given prometheus query at given point in time.
func (e *PrometheusQueryExecutor) Query(query string, queryTime time.Time) ([]*model.Sample, error) {
	if queryTime.IsZero() {
		return nil, fmt.Errorf("query time can't be zero")
	}

	var body []byte
	var queryErr error

	klog.V(2).Infof("Executing %q at %v", query, queryTime.Format(time.RFC3339))
	if err := wait.PollImmediate(queryInterval, queryTimeout, func() (bool, error) {
		body, queryErr = e.client.Query(query, queryTime)
		if queryErr != nil {
			return false, nil
		}
		return true, nil
	}); err != nil {
		if queryErr != nil {
			resp := "(empty)"
			if body != nil {
				resp = string(body)
			}
			return nil, fmt.Errorf("query error: %v [body: %s]", queryErr, resp)
		}
		return nil, fmt.Errorf("error: %v", err)
	}

	samples, err := ExtractMetricSamples2(body)
	if err != nil {
		return nil, fmt.Errorf("extracting error: %v", err)
	}

	var resultSamples []*model.Sample
	for _, sample := range samples {
		if !math.IsNaN(float64(sample.Value)) {
			resultSamples = append(resultSamples, sample)
		}
	}
	klog.V(4).Infof("Got %d samples", len(resultSamples))
	return resultSamples, nil
}

// UnmarshalJSON unmarshals json into promResponseData structure.
func (qr *promResponseData) UnmarshalJSON(b []byte) error {
	v := struct {
		Type   model.ValueType `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}

	err := json.Unmarshal(b, &v)
	if err != nil {
		return err
	}

	switch v.Type {
	case model.ValScalar:
		var sv model.Scalar
		err = json.Unmarshal(v.Result, &sv)
		qr.v = &sv
	case model.ValVector:
		var vv model.Vector
		err = json.Unmarshal(v.Result, &vv)
		qr.v = vv
	case model.ValMatrix:
		var mv model.Matrix
		err = json.Unmarshal(v.Result, &mv)
		qr.v = mv
	default:
		err = fmt.Errorf("unexpected value type %q", v.Type)
	}
	return err
}

// ToPrometheusTime returns prometheus string representation of given time.
func ToPrometheusTime(t time.Duration) string {
	if t < time.Minute {
		return fmt.Sprintf("%ds", int64(t)/int64(time.Second))
	}
	return fmt.Sprintf("%dm", int64(t)/int64(time.Minute))
}
