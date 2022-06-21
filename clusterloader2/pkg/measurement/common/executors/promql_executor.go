/*
Copyright 2021 The Kubernetes Authors.

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

package executors

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"gopkg.in/yaml.v2"
)

const (
	pathToPrometheusRules = "$GOPATH/src/k8s.io/perf-tests/clusterloader2/pkg/prometheus/manifests/prometheus-rules.yaml"
)

func toModelSample(s promql.Sample) *model.Sample {
	ls := make(model.Metric)
	for _, l := range s.Metric {
		ls[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}

	return &model.Sample{
		Value:     model.SampleValue(s.Point.V),
		Timestamp: model.Time(s.Point.T),
		Metric:    ls,
	}
}

type series struct {
	Series string `yaml:"series"`
	Values string `yaml:"values"`
}

type testSeries struct {
	InputSeries []series `yaml:"input_series"`
	Interval    string   `yaml:"interval"`
}

func (t *testSeries) seriesLoadingString() string {
	result := fmt.Sprintf("load %v\n", t.Interval)
	for _, is := range t.InputSeries {
		result += fmt.Sprintf("  %v %v\n", is.Series, is.Values)
	}
	return result
}

func loadFromFile(filename string) (*testSeries, error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	res := new(testSeries)
	err = yaml.Unmarshal(b, res)

	if err != nil {
		return nil, err
	}
	return res, nil
}

// PromqlExecutor executes queries against local prometheus query engine
type PromqlExecutor struct {
	ll       promql.LazyLoader
	interval time.Duration
}

// NewPromqlExecutor creates a new executor with time series and rules loaded from file
// Samples loaded from test file starts at time.Time.UTC(0,0)
func NewPromqlExecutor(timeSeriesFile string) (*PromqlExecutor, error) {
	//Load time series from file
	f, err := loadFromFile(timeSeriesFile)
	if err != nil {
		return nil, fmt.Errorf("could not parse time series file: %v", err)
	}

	interval, err := time.ParseDuration(f.Interval)
	if err != nil {
		return nil, fmt.Errorf("could not parse interval from time series file: %v", err)
	}

	ll, err := promql.NewLazyLoader(nil, f.seriesLoadingString())
	if err != nil {
		return nil, fmt.Errorf("could not initialize lazy loader: %v", err)
	}

	//Load rule groups
	opts := &rules.ManagerOptions{
		QueryFunc:  rules.EngineQueryFunc(ll.QueryEngine(), ll.Storage()),
		Appendable: ll.Storage(),
		Context:    context.Background(),
		NotifyFunc: func(ctx context.Context, expr string, alerts ...*rules.Alert) {},
		Logger:     nil,
	}
	m := rules.NewManager(opts)

	rulesFile, err := createRulesFile(os.ExpandEnv(pathToPrometheusRules))
	if err != nil {
		return nil, fmt.Errorf("could not create rules file: %v", err)
	}
	defer os.Remove(rulesFile.Name())
	groupsMap, ers := m.LoadGroups(interval, nil, rulesFile.Name())
	if ers != nil {
		return nil, fmt.Errorf("could not load rules file: %v", ers)
	}

	//Load data into ll
	ll.WithSamplesTill(time.Now(), func(e error) {
		if err != nil {
			err = e
		}
	})
	if err != nil {
		return nil, fmt.Errorf("could not load samples into lazyloader: %v", err)
	}

	// Evaluate rules after data was loaded
	// This assumes that no one will try to load time series longer than 6 hours
	maxt := time.Unix(0, 0).UTC().Add(time.Duration(6*60) * time.Minute)
	for ts := time.Unix(0, 0).UTC(); ts.Before(maxt) || ts.Equal(maxt); ts = ts.Add(interval) {
		for _, g := range groupsMap {
			g.Eval(context.Background(), ts)
		}
	}

	return &PromqlExecutor{ll: *ll, interval: interval}, nil
}

// Query queries prometheus mock engine with data loaded from file
// The start date for all queries is time.Time.UTC(0,0)
// Currently only queries that result in scalar are supported
func (p *PromqlExecutor) Query(query string, queryTime time.Time) ([]*model.Sample, error) {
	qe := p.ll.QueryEngine()
	q, err := qe.NewInstantQuery(p.ll.Queryable(), query, queryTime)
	if err != nil {
		fmt.Println(err)
		return nil, err
	}
	defer q.Close()
	res := q.Exec(p.ll.Context())

	switch v := res.Value.(type) {
	case promql.Vector:
		res := make([]*model.Sample, 0, len(v))
		for _, s := range v {
			res = append(res, toModelSample(s))
		}
		return res, nil
	case promql.Scalar:
		return nil, fmt.Errorf("query result is a scalar, expected vector. query: %s", query)
	default:
		return nil, fmt.Errorf("query result is not a vector. query: %s", query)
	}
}

func (p *PromqlExecutor) Close() {
	p.ll.Close()
}
