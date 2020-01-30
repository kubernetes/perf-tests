# How to add new measurement?

## 1. Implement a new measurement

TODO(oxddr)

## 2. Enable the new measurement in clusterloader2

TODO(oxddr)

## 3. Integration with PerfDash

Perfdash (performance dashboard) is a web UI that displays performance metrics.
Performance metrics are created based on various performance test we run (e.g.
sig-scalabiliry [periodic jobs], [release blocking jobs]). For more info read
https://github.com/kubernetes/perf-tests/blob/master/perfdash/README.md.

Intergation with PerfDash is three step process:

1. Sav
2. Label Prow job

### Save summary

2. Store results at [] https://gubernator.k8s.io/build/kubernetes-jenkins/logs/ (automatically done by kubetest)
3. Have summary in json format.

### Label Prow jobs

Perfdash support displaying measurement's summaries from sig-scalability
[periodic jobs] and [release blocking jobs], run by [prow].

The Prow job supported by the Perfdash is required to have two specific labels
set:

- `perfDashPrefix` - identifier and displayed name for given Prow job. This
  identifier needs to be unique.
- `perfDashJobType` - type of parser configs set that need to be used to parse
  the Prow job results. Currently defined sets are:
  - `performance` - performance test results
  - `benchmark` - generic go benchmarks
  - `dnsBenchmark` - dns benchmark results

If you add a new measurement to already existing Prow job, it's likely that it
has been already labeled.

### Create

Parsing process
Perfdash reads https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml and https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml and creates list of supported ci jobs.
For each job in the list, Perfdash downloads result from last 100 runs of given jobs.
Result are parsed based on perser sets configuration and then served at http://perf-dash.k8s.io/ .

Adding a new metric
Add missing parsers to https://github.com/kubernetes/perf-tests/blob/master/perfdash/parser.go .
Parser should converts json results to []DataItem (https://github.com/kubernetes/kubernetes/blob/fd0df59f5ba786cb25329e3a9d2793ad4227ed87/test/e2e/perftype/perftype.go#L26) and append it to testResult.Builds[build].
Define missing job type parser set in Perfdash config - https://github.com/kubernetes/perf-tests/blob/master/perfdash/config.go . The parser configs set will be used for ci jobs with matching perfDashJobType label. The label assignment to specific parser set is done at https://github.com/kubernetes/perf-tests/blob/bc241eb48b604826a1579230edd9ffb87c1b3330/perfdash/config.go#L213 .

The parser configs set structure:
TestDescriptions{
“<MetricGroup>”: {
“<MetricDisplayName>: []TestDescription{
//This is a list of parsers. Perfdash will iterate over list until it finds first parser that doesn’t return error (or until all listed parser are tried).
{
Name: “<TestName>”,
OutputFilePrefix: “<MetricName>”,
Parser: <Parser>
}
}
}
}
Where:
<MetricGroup> - groups metrics (e.g. if they are related to one specific component).
<MetricDisplayName> - the name that will be displayed in perfdash.
<TestName> - This is the same <TestName> as in metric json file naming convention.
<MetricName> - This is the same <MetricName> as in metric json file naming convention.
<Parser> - Is a parser method defined in parser.go file.

[Only if there was a change made to perfdash code]
Create new perfdash version.
The version of the perfdash should be bumped up (https://github.com/kubernetes/perf-tests/blob/master/perfdash/Makefile , https://github.com/kubernetes/perf-tests/blob/master/perfdash/deployment.yaml).
Create new perfdash image with command “make push”
Update used image in k8s-mungegithub project. Perfdash is hosted in mungegithub cluster.

Always remember to bump up the perfdash version, when you introduce any changes in its code!

[periodic jobs]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml
[release blocking jobs]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml
[prow]: https://prow.k8s.io
