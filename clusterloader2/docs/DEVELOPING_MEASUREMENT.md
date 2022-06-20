# Developing measurement

## Prerequisite
It's strongly recommended to get familiar with [Getting started] tutorial.

Also, you can check out our [Design] of Clusterloader2.

## Introduction

All measurements are implemented [here][Measurements].
You can find current Measurement interface [here][Measurement interface].

Measurement interface consists of three methods:
```golang
Execute(config *Config) ([]Summary, error)
Dispose()
String() string
```
`Execute` method will be executed each time you specify your measurement in test config.
Usually measurement is specified two times in test config - once when we start measuring something and second time when we want to gather results.
Two distinguish these two cases, you need to specify parameter called `action` that takes values `start` and `gather`.
For example, for PodStartupLatency you specify action and some additional parameters when starting measurement:

```yaml
- Identifier: PodStartupLatency
  Method: PodStartupLatency
  Params:
    action: start
    labelSelector: group = test-pod
    threshold: 20s
```
And then later you can just gather results with:
```yaml
- Identifier: PodStartupLatency
  Method: PodStartupLatency
  Params:
    action: gather
```

`Dispose` method can be used to clean up after the measurement is no longer used.

`String()` method should return name of measurement.

## Implementing simple measurement

Let's start with implementing simple measurement that will measure maximum number of running pods.
First, let's start with specifying package and measurement name:

```golang
package common

const (
	maxRunningPodsMeasurementName = "MaxRunningPods"
)
```
Next, let's define structure and create constructor. We will need two integers for tracking current number of running pods and maximal number of running pods.
Our measurement will have two actions `start` and `gather` so also want to know when our measurement is in running state.
We will need also channel for stopping informers.
```golang
type maxRunningPodsMeasurement struct {
	maxRunningPods     int
	currentRunningPods int
	stopCh             chan struct{}
	isRunning          bool
	lock               sync.Mutex
}

func createMaxRunningPodsMeasurement() measurement.Measurement {
	return &maxRunningPodsMeasurement{}
}
```

Once we have it, we want to register our new measurement so it can be used in test config:
```golang
func init() {
	if err := measurement.Register(maxRunningPodsMeasurementName, createMaxRunningPodsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", maxRunningPodsMeasurementName, err)
	}
}
```

Next, let's implement `String` method:
```golang
func (*maxRunningPodsMeasurement) String() string {
	return maxRunningPodsMeasurementName
}
```
Next, we need to implement `Execute` method. We want to have two actions - `start` and `gather`:
```golang
func (s *maxRunningPodsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		return nil, s.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		return s.gather()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}
```

Now, let's focus on `start` method. First, we want to check if measurement is not already running. In this case we want to return error.
If measurement is not running then we want to start informer that will get information about all pods within cluster.

```golang
func (m *maxRunningPodsMeasurement) start(c clientset.Interface) error {
	if m.isRunning {
		return fmt.Errorf("%s: measurement already running", m)
	}
	klog.V(2).Infof("%s: starting max pod measurement...", m)
	m.isRunning = true
	m.stopCh = make(chan struct{})
	i := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.CoreV1().Pods("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.CoreV1().Pods("").Watch(context.TODO(), options)
			},
		},
		m.checkPod,
	)
	return informer.StartAndSync(i, m.stopCh, informerSyncTimeout)
}

```

Next step is implementing `checkPod` method. This method will be responsible for counting pods in Running state.
As arguments, we get old and new version of object.
There are two special cases.
First case when old version of object is nil - it means that object is added.
Second case when new version of object is nil - it means that object was deleted.

```golang
func MaxInt(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func (m *maxRunningPodsMeasurement) checkPod(oldObj, newObj interface{}) {
	func isPodRunning(obj interface{}) bool {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			klog.V(2).Warningf("Couldn't convert object to Pod")
			return false
		}
		return pod != nil && pod.Status.Phase == corev1.PodRunning
	}


	change := 0
	if isPodRunning(oldObj) {
		change--
	}
	if isPodRunning(newObj) {
		change++
	}
	m.currentRunningPods += change

	m.maxRunningPods = MaxInt(m.maxRunningPods, m.currentRunningPods)
	klog.V(2).Infof("Max: %d, current: %d", m.maxRunningPods, m.currentRunningPods)
}
```

Finally, we can implement `gather` method. The easiest way of creating summary is by creating structure with json annotation and then passing serialized json to `CreateSummary` function like this:
```golang
type runningPods struct {
	Max int `json:"max"`
}

func (m *maxRunningPodsMeasurement) gather() ([]measurement.Summary, error) {
	if !m.isRunning {
		return nil, fmt.Errorf("measurement %s has not been started", maxRunningPodsMeasurementName)
	}

	runningPods := &runningPods{Max: m.maxRunningPods}
	content, err := util.PrettyPrintJSON(runningPods)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(maxRunningPodsMeasurementName, "json", content)
	return []measurement.Summary{summary}, err
}
```

Last, but not least, lets implement `Dispose` method. In this method we want to close channel so informer will be closed as well.
```golang
func (s *maxRunningPodsMeasurement) Dispose() {
	s.stop()
}

func (m *maxRunningPodsMeasurement) stop() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
	}
}
```

Combaining all together we have full implementation:
```golang
package common

import (
	"context"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement/util/informer"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	maxRunningPodsMeasurementName = "MaxRunningPods"
)

type maxRunningPodsMeasurement struct {
	maxRunningPods     int
	currentRunningPods int
	stopCh             chan struct{}
	isRunning          bool
	lock               sync.Mutex
}

func createMaxRunningPodsMeasurement() measurement.Measurement {
	return &maxRunningPodsMeasurement{}
}

func init() {
	if err := measurement.Register(maxRunningPodsMeasurementName, createMaxRunningPodsMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", maxRunningPodsMeasurementName, err)
	}
}

func (s *maxRunningPodsMeasurement) Dispose() {
	s.stop()
}

func (*maxRunningPodsMeasurement) String() string {
	return maxRunningPodsMeasurementName
}

func (s *maxRunningPodsMeasurement) Execute(config *measurement.Config) ([]measurement.Summary, error) {
	action, err := util.GetString(config.Params, "action")
	if err != nil {
		return nil, err
	}
	switch action {
	case "start":
		return nil, s.start(config.ClusterFramework.GetClientSets().GetClient())
	case "gather":
		return s.gather()
	default:
		return nil, fmt.Errorf("unknown action %v", action)
	}
}

func (m *maxRunningPodsMeasurement) start(c clientset.Interface) error {
	if m.isRunning {
		return fmt.Errorf("%s: measurement already running", m)
	}
	klog.V(2).Infof("%s: starting max pod measurement...", m)
	m.isRunning = true
	m.stopCh = make(chan struct{})
	i := informer.NewInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return c.CoreV1().Pods("").List(context.TODO(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return c.CoreV1().Pods("").Watch(context.TODO(), options)
			},
		},
		m.checkPod,
	)
	return informer.StartAndSync(i, m.stopCh, informerSyncTimeout)
}

func (m *maxRunningPodsMeasurement) stop() {
	if m.isRunning {
		m.isRunning = false
		close(m.stopCh)
	}
}

func MaxInt(x, y int) int {
	if x < y {
		return y
	}
	return x
}

func (m *maxRunningPodsMeasurement) checkPod(oldObj, newObj interface{}) {
	func isPodRunning(obj interface{}) bool {
		pod, ok := obj.(*corev1.Pod)
		if !ok {
			klog.V(2).Warningf("Couldn't convert object to Pod")
			return false
		}
		return pod != nil && pod.Status.Phase == corev1.PodRunning
	}


	change := 0
	if isPodRunning(oldObj) {
		change--
	}
	if isPodRunning(newObj) {
		change++
	}
	m.lock.Lock()
	defer m.lock.Unlock()
	m.currentRunningPods += change

	m.maxRunningPods = MaxInt(m.maxRunningPods, m.currentRunningPods)
	klog.V(2).Infof("Max: %d, current: %d", m.maxRunningPods, m.currentRunningPods)
}

type runningPods struct {
	Max int `json:"max"`
}

func (m *maxRunningPodsMeasurement) gather() ([]measurement.Summary, error) {
	if !m.isRunning {
		return nil, fmt.Errorf("measurement %s has not been started", maxRunningPodsMeasurementName)
	}

	m.lock.Lock()
	defer m.lock.Unlock()
	runningPods := &runningPods{Max: m.maxRunningPods}
	content, err := util.PrettyPrintJSON(runningPods)
	if err != nil {
		return nil, err
	}
	summary := measurement.CreateSummary(maxRunningPodsMeasurementName, "json", content)
	return []measurement.Summary{summary}, err
}
```
## Trying new measurement

Once we have whole implementation, you can try it out. Easiest way is to modify [Getting started] example `config.yaml` by replacing `PodStartupLatency` with `MaxRunningPods`

## Enabling new measurement in tests

Once your measurement is implemented and tested, we need to add it to existing scalability test(s).
This process needs to be done very carefully, because any mistake can lead to blocking all PRs to kubernetes repository.
You can follow these instructions: [Rollout process]

## Prometheus based measurements

Sometimes you can implement your metric based on Prometheus metric. In this case, it consists of two steps:
- Gathering new metric (if it's not already gathered)
- Implementing measurement based on Prometheus metric

Let's start with two examples how you can gather more Prometheus metrics.

### Gathering Prometheus metric

You can check example PodMonitor and ServiceMonitor [here][Monitors]. Let's go through two examples.
PodMonitor for NodeLocalDNS:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  labels:
    k8s-app: node-local-dns-pods
  name: node-local-dns-pods
  namespace: monitoring
spec:
  podMetricsEndpoints:
    - interval: 10m
      port: metrics
  jobLabel: k8s-app
  selector:
    matchLabels:
      k8s-app: node-local-dns
  namespaceSelector:
    matchNames:
      - kube-system
```
Any monitor needs to be created within monitoring namespace.
Most important is specifying interval how often metric will be scraped (here we have once every 10 minutes).
The more pods you want to scrape the smaller frequency should be and/or Prometheus server should have more resources.
In this example we scrape up to 5k pods.

If your pods are already within service, you can use ServiceMonitor to scrape metrics:
```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  labels:
    k8s-app: my-service
  name: my-service
  namespace: monitoring
spec:
  endpoints:
    - interval: 60s
      port: http-metrics
  jobLabel: k8s-app
  namespaceSelector:
    matchNames:
      - kube-system
  selector:
    matchLabels:
      k8s-app: my-service
```

Also, instead of using port name, you can specify port number with targetPort.
You can check more options in [Prometheus operator doc].

### Implementing new measurement

In this example we will implement measurement that checks how many pods were scheduled using Prometheus metric.
Apiserver provides multiple metrics, including `apiserver_request_total`, which can be used to check how many pods have been binded to nodes.
We are interested in `apiserver_request_total{verb="POST", resource="pods", subresource="binding",code="201"`.

Just like before, let's start with defining package and metric name:
```golang
package common

const (
	schedulingThroughputPrometheusMeasurementName = "SchedulingThroughputPrometheus"
)
```

In case of measurement based on Prometheus metric, we only need to specify how results will be gathered.
You can check interface [here][Prometheus interface], which looks like this:

```golang
func CreatePrometheusMeasurement(gatherer Gatherer) measurement.Measurement

type Gatherer interface {
	Gather(executor QueryExecutor, startTime time.Time, config *measurement.Config) ([]measurement.Summary, error)
	IsEnabled(config *measurement.Config) bool
	String() string
}
```

So let's start with creating structure:
```golang
type schedulingThroughputGatherer struct{}
```

Now, we can register our new measurement:

```golang
func init() {
	create := func() measurement.Measurement { return CreatePrometheusMeasurement(&schedulingThroughputGatherer{}) }
	if err := measurement.Register(schedulingThroughputPrometheusMeasurementName, create); err != nil {
		klog.Fatalf("Cannot register %s: %v", schedulingThroughputMeasurementName, err)
	}
}
```

And let's define `IsEnabled` and `String` methods:

```golang
func (a *schedulingThroughputGatherer) String() string {
	return schedulingThroughputPrometheusMeasurementName
}

func (a *schedulingThroughputGatherer) IsEnabled(config *measurement.Config) bool {
	return true
}
```

Next we need to implement `Gather` method. We can start with Prometheus query. We want to check only maximal throughput of scheduling.
This query can look like this:
```golang
const (
	maxSchedulingThroughputQuery = `max_over_time(sum(irate(apiserver_request_total{verb="POST", resource="pods", subresource="binding",code="201"}[1m]))[%v:5s])`
)
```
We will need to provide only duration for which we want to compute it.

Now, let's define structure for our summary of results. As mentioned before, we will only gather maximal throughput of scheduling:
```golang
type schedulingThroughputPrometheus struct {
	Max float64 `json:"max"`
}
```
We can now implement method that will gather scheduling throughput data.
We need to compute duration for measurement, execute query, check of any error and return previously defined structure:
```golang
func (a *schedulingThroughputGatherer) getThroughputSummary(executor QueryExecutor, startTime time.Time, config *measurement.Config) (*schedulingThroughputPrometheus, error) {
	measurementEnd := time.Now()
	measurementDuration := measurementEnd.Sub(startTime)
	promDuration := measurementutil.ToPrometheusTime(measurementDuration)
	query := fmt.Sprintf(maxSchedulingThroughputQuery, promDuration)

	samples, err := executor.Query(query, measurementEnd)
	if err != nil {
		return nil, err
	}
	if len(samples) != 1 {
		return nil, fmt.Errorf("got unexpected number of samples: %d", len(samples))
	}

	maxSchedulingThroughput := samples[0].Value
	throughputSummary := &schedulingThroughputPrometheus{
		Max: float64(maxSchedulingThroughput),
	}

	return throughputSummary, nil
}
```

We can finally implement `Gather` method. We need to get result and convert it to `measurement.Summary` format.
```golang
func (a *schedulingThroughputGatherer) Gather(executor QueryExecutor, startTime time.Time, config *measurement.Config) ([]measurement.Summary, error) {
	throughputSummary, err := a.getThroughputSummary(executor, startTime, config)
	if err != nil {
		return nil, err
	}

	content, err := util.PrettyPrintJSON(throughputSummary)
	if err != nil {
		return nil, err
	}

	summaries := []measurement.Summary{
		measurement.CreateSummary(a.String(), "json", content),
	}

	return summaries, err
}
```

If we want to add `threshold` to our measurement so it fails if scheduling throughput is below it, we can add to `Gather` method this check:
```golang
	threshold, err := util.GetFloat64OrDefault(config.Params, "threshold", 0)
	if threshold > 0 && throughputSummary.Max < threshold {
		err = errors.NewMetricViolationError(
			"scheduler throughput_prometheus",
			fmt.Sprintf("actual throughput %f lower than threshold %f", throughputSummary.Max, threshold))
	}
```
You can check whole implementation [here][Scheduling throughput].

### Unit testing of Prometheus measurements

You can use our testing framework that runs a local Prometheus query executor to check whether your measurement works as intended in an end-to-end way without running Clusterloader2 on a live cluster.
This works by providing sample files containing time series as inputs for the measurement's underlying PromQL queries.
See an example of a [unit test][Container restarts test] written in this framework and the [input data][Test input data] used therein.

[Design]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/design.md
[Getting started]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/GETTING_STARTED.md
[Measurement interface]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/pkg/measurement/interface.go
[Measurements]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/pkg/measurement
[Monitors]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/pkg/prometheus/manifests/default
[Prometheus interface]: https://github.com/kubernetes/perf-tests/blob/1c21298a325633062d6069c01a3b27933d6dba93/clusterloader2/pkg/measurement/common/prometheus_measurement.go#L45
[Prometheus operator doc]: https://github.com/prometheus-operator/prometheus-operator/blob/master/Documentation/api.md
[Rollout process]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/experiments.md
[Scheduling throughput]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/pkg/measurement/common/scheduling_throughput_prometheus.go
[Container restarts test]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/pkg/measurement/common/container_restarts_test.go
[Test input data]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/pkg/measurement/common/testdata/container_restarts
