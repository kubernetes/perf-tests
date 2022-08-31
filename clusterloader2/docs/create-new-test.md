# Create a new performance test

This document is intended for K8s developers that need to test their feature
using ClusterLoader2 with resulting test config being added to the CI tool.

## Select and copy the baseline test job config

All of the performance test definitions lie at the [test-infra] repository. All
them are written as [ProwJob]s. There are two test categories being used there:

-   **periodic** which is a test that runs at given cron interval
-   **presubmit** which may be used as a presubmit for GitHub pull requests

And there are three scales at which these are run: 100, 500 and 5000 nodes. Once
it's known what is to be achieved it's good to start with a test that is the
closest to it. Here are a few good example starting points, all running
[load-test][]:

Presubmits:

-   100 nodes: [pull-kubernetes-e2e-gce-100-performance]
-   5000 nodes: [pull-kubernetes-e2e-gce-scale-performance-manual]

Periodics:

-   100 nodes: [ci-kubernetes-e2e-gci-gce-scalability]
-   5000 nodes: [ci-kubernetes-e2e-gce-scale-performance]

We propose to start with 100 node tests as they are less costly and much
quicker, shortening debugging cycle. After that it's possible to switch to 5k
node scale but since running such a test is quite expensive we recommend using
[ci-kubernetes-kubemark-gce-scale] as the baseline instead ([kubemark] is a tool
used for simulating a large cluster on small number of nodes).

There are also other test scenarios aside from [load-test] that can be used,
such as [l4ilb] or [access-tokens]. They can be found at the [testing
directory].

New scalability tests with ambiguous ownership should be added to
[sig-scalability-experimental-periodic-jobs.yaml].

## Copy and modify the selected baseline test

Copy the test config to another location within that file. After that it's
mandatory to edit the following fields:

-   `name` must be unique among all configs.
-   If testgrid metadata is defined in `annotations`, change it so that the
    baseline and copied job results are shown in separate views (changing
    `testgrid-tab-name` should be enough).
-   If a periodic:
    -   If the same `gcp-project` or `gcp-project-type` is used then change
        `cron` so that the baseline and copied jobs won't collide.
    -   If defined, change perfdash `tags` so that test metrics are shown in
        separate views.
    -   In `spec.containers[0].args` set `--cluster` flag to a unique value.

### Developing a ProwJob

Before preparing the final version of the developed [ProwJob], it's good to run
a few tests manually on own resources to make sure that everything works fine. A
useful tool here is [Phaino] which allows submitting a ProwJob from local
workstation (no need to setup Prow). Please remember to make sure that the
ProwJob's `cluster` exists and set the GCP project via `--gcp-project` flag
(instead of `--gcp-project-type` boskos pool, unless Boskos is already set up).

## Adjust the modified baseline test

ClusterLoader2 test configuration consists of a group of templatized YAML files.
You can find the high level description of the used config language in the
project [README].

If needed, modify the `CL2_` variables used with specific test in order to tune
its behaviour. It's always an option to create a new test if needed.

It's also possible to modify values used for other kubekins arguments. For
reference of their behaviour visit [Kubetest source code].

  [test-infra]: https://github.com/kubernetes/test-infra/tree/d189c05b6f770a5bbc4224452e223a28d8ac7c57/config/jobs/kubernetes/sig-scalability
  [ProwJob]: https://github.com/kubernetes/test-infra/blob/d189c05b6f770a5bbc4224452e223a28d8ac7c57/prow/jobs.md
  [load-test]: https://github.com/kubernetes/perf-tests/tree/f1d31ce5e28a6ab9eace149d71b2ff22f524a0aa/clusterloader2/testing/load
  [pull-kubernetes-e2e-gce-100-performance]: https://github.com/kubernetes/test-infra/blob/0ad2c03e7ef5fc974b5209cc7fea1ce1fe8bf4b2/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L5
  [pull-kubernetes-e2e-gce-scale-performance-manual]: https://github.com/kubernetes/test-infra/blob/0ad2c03e7ef5fc974b5209cc7fea1ce1fe8bf4b2/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L223
  [ci-kubernetes-e2e-gci-gce-scalability]: https://github.com/kubernetes/test-infra/blob/d189c05b6f770a5bbc4224452e223a28d8ac7c57/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml#L152
  [ci-kubernetes-e2e-gce-scale-performance]: https://github.com/kubernetes/test-infra/blob/d189c05b6f770a5bbc4224452e223a28d8ac7c57/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml#L59
  [ci-kubernetes-kubemark-gce-scale]: https://github.com/kubernetes/test-infra/blob/d189c05b6f770a5bbc4224452e223a28d8ac7c57/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L438
  [kubemark]: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-scalability/kubemark-guide.md
  [l4ilb]: https://github.com/kubernetes/perf-tests/tree/f1d31ce5e28a6ab9eace149d71b2ff22f524a0aa/clusterloader2/testing/l4ilb
  [access-tokens]: https://github.com/kubernetes/perf-tests/tree/f1d31ce5e28a6ab9eace149d71b2ff22f524a0aa/clusterloader2/testing/access-tokens
  [testing directory]: https://github.com/kubernetes/perf-tests/tree/f1d31ce5e28a6ab9eace149d71b2ff22f524a0aa/clusterloader2/testing
  [sig-scalability-experimental-periodic-jobs.yaml]: https://github.com/kubernetes/test-infra/blob/d189c05b6f770a5bbc4224452e223a28d8ac7c57/config/jobs/kubernetes/sig-scalability/sig-scalability-experimental-periodic-jobs.yaml
  [Phaino]: https://github.com/kubernetes/test-infra/tree/ef78d789f06559f8362106fe3717498672406061/prow/cmd/phaino
  [README]: https://github.com/kubernetes/perf-tests/blob/764074f2612da6673fb806f15120a3f3629d1416/clusterloader2/README.md
  [Kubetest source code]: https://github.com/kubernetes/test-infra/blob/6e76216e7438c6556522fae964e7f050ecb654d1/kubetest/main.go#L134
