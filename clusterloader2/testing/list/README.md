## List load test

List load test perform the following steps:
- Create configmaps and secrets in a set of namespaces.
  - The namespaces used here are managed by CL2.
  - The number of namespaces is derived from `NODES_PER_NAMESPACE`. Namespaces will be 2, if there are no nodes.
  - The size of a single configmap and secret can be speficed using `CL2_LIST_CONFIG_MAP_BYTES` and `CL2_LIST_SECRET_BYTES` respectively.
  - The number of configmaps and secrets can be specified using `CL2_LIST_CONFIG_MAP_NUMBER` and `CL2_LIST_SECRET_NUMBER` respectively.
- Create RBAC rules to allow lister pods to access these configmaps and secrets
- Create lister pods using deployment in a separate namespace to list configmaps and secrets.
  - The namespace is created using `namespace.yaml` as a template, but its lifecycle is managed by CL2.
  - The number of replicas for the lister pods can be specified using `CL2_LIST_BENCHMARK_PODS`.
  - Each lister pod will maintain `CL2_LIST_CONCURRENCY` number of inflight list requests.
- Measurement uses [`APIResponsivenessPrometheusSimple`](https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/README.md) to meansure API latency for list configmaps and secrets calls.

The lister pods leverage https://github.com/kubernetes/perf-tests/tree/master/util-images/request-benchmark to create in-cluster list load.
