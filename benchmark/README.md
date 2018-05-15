# Performance Comparison Tool

This tool enables comparing performance of a given kubemark test against
that of the corresponding real cluster test. The comparison is based on
the API request latencies and pod startup latency metrics. And it is
based on aggregate results of the tests over multiple runs. While using
this tool for kubemark performance is the prime usecase, this can in general
be used for comparing any two tests, making most sense when the tests run in
similar environments (i.e. with similar no. of nodes, e2e tests, etc).

For further details, here's the [design doc](https://github.com/kubernetes/perf-tests/blob/master/benchmark/docs/design.md)
for the tool.
