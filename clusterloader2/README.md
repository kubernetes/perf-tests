# ClusterLoader

## Running ClusterLoader

To run ClusterLoader type:
```
go run cmd/clusterloader.go --kubeconfig=kubeConfig.yaml --testconfig=config.yaml
```
OR
```
./run-e2e.sh --testconfig=config.yaml
```
Flags kubeconfig and testconfig are necessary.

### Flags

#### Required

These flags are required for any test to be run.
 - kubeconfig - path to the kubeconfig file.
 - testconfig - path to the test config file. This flag can be used multiple times
if more than one test should be run.

#### Optional

 - nodes - number of nodes in the cluster.
If not provided, test will assign the number of schedulable cluster nodes.
 - report-dir - path to directory, where summaries files should be stored.
If not specified, summaries are printed to standard log.
 - provider - Cluster provider, options are: gce, gke, kubemark, aws, local, vsphere, skeleton
 - mastername - Name of the master node
 - masterip - DNS Name / IP of the master node
 - testoverrides - path to file with overrides.

## Tests

### Test definition

Test definition is an instantiation of this [api] (in json or yaml).
The motivation and description of the API can be found in [design doc].
Definitions of test as well as definitions of individual objects support templating.
Templates for test definition come with one predefined value - ```{{.Nodes}}```,
which represents the number of schedulable nodes in the cluster. \
Example of a test definition can be found here: [load test].

### Object template

Object template is similar to standard kubernetes object definition
with the only difference being templating mechanism.
Parameters can be passed from the test definition to the object template
using the ```templateFillMap``` map.
Two always available parameters are ```{{.Name}}``` and ```{{.Index}}```
which specifies object name and object replica index respectively. \
Example of a template can be found here: [load deployment template].

### Overrides

Overrides allow to inject new variables values to the template. \
Many tests define input parameters. Input parameter is a variable
that potentially will be provided by the test framework. Cause input parameters are optional,
each reference has to be opaqued with ```DefaultParam``` function that will
handle case if given variable doesn't exist. \
Example of overrides can be found here: [overrides]

#### Passing environment variables

Instead of using overrides in file, it is possible to depend on environment
variables. Only variables that start with `CL2_` prefix will be parsed and
available in script.

Environment variables can be used with `DefaultParam` function to provide sane
default values.

##### Setting variables in shell
```shell
export CL2_ACCESS_TOKENS_QPS=5
```

##### Usage from test definition
```yaml
{{$qpsPerToken := DefaultParam .CL2_ACCESS_TOKENS_QPS 0.1}}
```

## Measurement

Currently available measurements are:
- **APIResponsiveness** \
This measurement creates summary for latency and number for server api calls.
Api calls are divided by resource, subresource, verb and scope. \
This measurement verifies if [API call latencies SLO] is satisfied.
- **APIResponsivenessPrometheus** \
This measurement creates summary for latency and number for server api calls
based on the data collected by the prometheus server.
Api calls are divided by resource, subresource, verb and scope. \
This measurement verifies if [API call latencies SLO] is satisfied.
If prometheus server is not available, the measurement will be skipped.
- **CPUProfile** \
This measurement gathers the cpu usage profile provided by pprof for a given component.
- **EtcdMetrics** \
This measurement gathers a set of etcd metrics and its database size.
- **MemoryProfile** \
This measurement gathers the memory profile provided by pprof for a given component.
- **MetricsForE2E** \
The measurement gathers metrics from kube-apiserver, controller manager,
scheduler and optionally all kubelets.
- **PodStartupLatency** \
This measurement verifies if [pod startup SLO] is satisfied.
- **ResourceUsageSummary** \
This measurement collects the resource usage per component. During gather execution,
the collected data will be converted into summary presenting 90th, 99th and 100th usage percentile
for each observed component. \
Optionally resource constraints file can be provided to the measurement.
Resource constraints file specifies cpu and/or memory constraint for a given component.
If any of the constraint is violated, an error will be returned, causing test to fail.
- **SchedulingMetrics** \
This measurement gathers a set of scheduler metrics.
- **SchedulingThroughput** \
This measurement gathers scheduling throughput.
- **Timer** \
Timer allows for measuring latencies of certain parts of the test
(single timer allows for independent measurements of different actions).
- **WaitForControlledPodsRunning** \
This measurement works as a barrier that waits until specified controlling objects
(ReplicationController, ReplicaSet, Deployment, DaemonSet and Job) have all pods running.
Controlling objects can be specified by label selector, field selector and namespace.
In case of timeout test continues to run, with error (causing marking test as failed) being logged.
- **WaitForRunningPods** \
This is a barrier that waits until required number of pods are running.
Pods can be specified by label selector, field selector and namespace.
In case of timeout test continues to run, with error (causing marking test as failed) being logged.
- **Sleep** \
This is a barrier that waits until requested amount of the time passes.

## Vendor

Vendor is created using [govendor].

[api]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/api/types.go
[API call latencies SLO]: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/api_call_latency.md
[design doc]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/docs/design.md
[govendor]: https://github.com/kardianos/govendor
[load deployment template]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/load/deployment.yaml
[load test]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/load/config.yaml
[overrides]: https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/testing/density/5000_nodes/override.yaml
[pod startup SLO]: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/pod_startup_latency.md
