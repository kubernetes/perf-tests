# ClusterLoader

ClusterLoader2 (CL2) is a "bring your own YAML" Kubernetes load testing tool
and the official K8s scalability and performance testing framework.

CL2 tests are written in YAML using a semi-declarative paradigm.
A test defines a set of desired states for a cluster
(e.g. I want to run 10k pods, 2k cluster-ip services, or 5 daemon-sets)
and specifies how quickly each state should be reached (the "pod throughput").
CL2 also defines which performance characteristics
to measure (see the [Measurements list](#Measurement) for details).
Last but not least, CL2 provides extra observability for the cluster
during the tests with [Prometheus](#prometheus-metrics).

The CL2 test API is described [here][API].

## Getting started

See the [Getting started] guide if you are a new user of ClusterLoader.

### Flags

#### Required

These flags are required for any test to be run.
 - `kubeconfig` - path to the kubeconfig file.
 - `testconfig` - path to the test config file. This flag can be used multiple times
if more than one test should be run.
 - `provider` - Cluster provider. Options are: gce, gke, kind, kubemark, aws, local, vsphere, skeleton

#### Optional

 - `nodes` - number of nodes in the cluster.
If not provided, test will assign the number of schedulable cluster nodes.
 - `report-dir` - path to a directory where summary files should be stored.
If not specified, summaries are printed to the standard log.
 - `mastername` - Name of the master node.
 - `masterip` - DNS Name or IP of the master node.
 - `testoverrides` - path to file with overrides.
 - `kubelet-port` - TCP port of the kubelet to use (*default: 10250*).

## Tests

### Test definition

A test definition is an instantiation of this [API] (in JSON or YAML).
The motivation and description of the API can be found in the [design doc].
The test definition as well as the individual object definitions support templating.
Templates for a test definition come with the predefined value ```{{.Nodes}}``` which represents the number of schedulable nodes in the cluster.

An example of a test definition can be found here: [load test].

### Modules

ClusterLoader2 supports the modularization of test configs via the [Module API](https://github.com/kubernetes/perf-tests/blob/1bbb8bd493e5ce6370b0e18f3deaf821f3f28fd0/clusterloader2/api/types.go#L77).
With the Module API, you can divide a single test config file into multiple
module files. A module can be parameterized and used multiple times by
the test or by other modules. This is a convenient way to avoid copy-and-pasting
and maintaining long, unreadable test configs<sup id="a1">[1](#f1)</sup>.

[TODO(mm4tt)]: <> (Point to the load config based on modules here once we migrate it)

### Object template

An object template is similar to a standard Kubernetes object definition,
the only difference being its templating mechanism.
Parameters can be passed from the test definition to the object template
using the ```templateFillMap``` map.
Two always-available parameters are ```{{.Name}}``` and ```{{.Index}}```
which specify object name and object replica index respectively.

An example of a template can be found here: [load deployment template].

### Overrides

Overrides allow for injecting new variable values into the template.
Many tests define input parameters: variables
that potentially will be provided by the test framework. Since input parameters are optional,
each reference must use the ```DefaultParam``` function to
handle the case where the given variable doesn't exist.

An example of overrides can be found here: [overrides]

#### Passing environment variables

Instead of hard-coding overrides in a file, it is possible to depend on environment
variables. Only variables that start with the prefix `CL2_` will be available in the
template.

Environment variables can be used with the `DefaultParam` function to provide sane
default values.

##### Set a variable in the shell
```shell
export CL2_ACCESS_TOKENS_QPS=5
```

##### Reference the variable in the test definition
```yaml
{{$qpsPerToken := DefaultParam .CL2_ACCESS_TOKENS_QPS 0.1}}
```

## Measurement

Currently available measurements are:
- **APIAvailabilityMeasurement** \
This measurement collects information about the availability of a cluster's control plane. \
There are two slightly different ways this is measured:
  - cluster-level availability, where we periodically issue an API call to `/readyz`,
  - host-level availability, where we periodically poll each of the control plane's host `/readyz` endpoint.
    - this requires the [exec service](https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/pkg/execservice) to be enabled.
- **APIResponsivenessPrometheusSimple** \
This measurement creates percentiles of latency and number for server api calls based on the data collected by the prometheus server. 
Api calls are divided by resource, subresource, verb and scope. \
This measurement verifies if [API call latencies SLO] is satisfied.
If a prometheus server is not available, the measurement will be skipped.
- **APIResponsivenessPrometheus** \
This measurement creates a summary for latency and number for server API calls
based on the data collected by the prometheus server.
API calls are divided by resource, subresource, verb and scope. \
This measurement verifies if [API call latencies SLO] is satisfied.
If a prometheus server is not available, the measurement will be skipped.
- **CPUProfile** \
This measurement gathers the CPU usage profile provided by pprof for a given component.
- **EtcdMetrics** \
This measurement gathers a set of etcd metrics and its database size.
- **MemoryProfile** \
This measurement gathers the memory profile provided by pprof for a given component.
- **MetricsForE2E** \
The measurement gathers metrics from kube-apiserver, controller manager,
scheduler, and optionally all kubelets.
- **PodPeriodicCommand** \
This measurement continually runs commands on an interval in pods targeted
with a label selector. The output from each command is collected, allowing for
information to be polled throughout the duration of the measurement, such as
CPU and memory profiles.
- **PodStartupLatency** \
This measurement verifies if [pod startup SLO] is satisfied.
- **ResourceUsageSummary** \
This measurement collects the resource usage per component. During gather execution,
the collected data will be converted into summary presenting 90th, 99th and 100th usage percentile
for each observed component. \
Optionally, a resource constraints file can be provided to the measurement.
A resource constraints file specifies CPU and/or memory constraints for a given component.
If any of the constraints is violated, an error will be returned and the test will fail.
- **SchedulingMetrics** \
This measurement gathers a set of scheduler metrics.
- **SchedulingThroughput** \
This measurement gathers scheduling throughput.
- **Timer** \
Timer allows for measuring the latencies of certain parts of the test
(a single timer allows for independent measurements of different actions).
- **WaitForControlledPodsRunning** \
This measurement works as a barrier that waits until the specified controlling objects
(ReplicationController, ReplicaSet, Deployment, DaemonSet and Job) have all pods running.
Controlling objects can be specified by label selector, field selector and namespace.
In case of a timeout, the test continues to run, with an error being logged and the test marked as failed.
- **WaitForRunningPods** \
This is a barrier that waits until the required number of pods are running.
Pods can be specified by label selector, field selector and namespace.
In case of a timeout, the test continues to run, with an error being logged and the test marked as failed.
- **Sleep** \
This is a barrier that waits until a requested amount of the time passes.
- **WaitForGenericK8sObjects** \
This is a barrier that waits until required number of k8s objects fulfill the required conditions.
Those conditions can be specified as a list of requirements of `Type=Status` format, e.g.: `NodeReady=True`.
In case of a timeout, the test continues to run, with an error being logged and the test marked as failed.

## Prometheus metrics

There are two ways of scraping metrics from pods within a cluster:
- **ServiceMonitor** \
Allows scraping metrics from all pods in service. Here is an example: [Service monitor]
- **PodMonitor** \
Allows scraping metrics from all pods with a specific label. Here is an example: [Pod monitor]

## Vendor

Vendoring of required packages is handled with [Go modules].

---

<sup><b id="f1">1.</b> As an example and anti-pattern see the 900 line [load test config.yaml](https://github.com/kubernetes/perf-tests/blob/92cc27ff529ae3702c87e8f154ea62f3f2d8e837/clusterloader2/testing/load/config.yaml) we ended up maintaining at some point. [↩](#a1)</sup>


[API]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/api/types.go
[API call latencies SLO]: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/api_call_latency.md
[exec service]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/pkg/execservice
[design doc]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/design.md
[Go modules]: https://blog.golang.org/using-go-modules
[Getting started]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/GETTING_STARTED.md
[load deployment template]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/load/deployment.yaml
[load test]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/load/config.yaml
[overrides]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/density/scheduler/pod-affinity/overrides.yaml
[pod startup SLO]: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/pod_startup_latency.md
[Service monitor]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/pkg/prometheus/manifests/default/prometheus-serviceMonitorKubeProxy.yaml
[Pod monitor]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/pkg/prometheus/manifests/default/prometheus-podMonitorNodeLocalDNS.yaml
