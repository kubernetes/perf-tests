# Clusterloader2 - experiment rollout

In this doc any change to the behavior of clusterloader2 that

    - enables new measurement
    - changes semantic of an existing measurement
    - changes how clusterloader2 setups cluster and run tests

is referred as an "experiment".

## Motivation

clusterloader2 is a tool used by all scalability, performance tests. Tests
compile clusterloader2 at [HEAD], thus introducing breaking changes to
clusterloader2 will stop scalability tests from passing at all. They have a
large blast radius: every PR to k/k needs to pass. Also while for smaller and
faster tests, breakages aren't that costly (unless they happen on the weekend,
see https://github.com/kubernetes/perf-tests/pull/586), they are expensive for
large, rarely run tests (e.g. [ci-kubernetes-e2e-gce-scale-performance]).

For this reason, all new changes/features added to clusterloader2 shall be gated
and rolled out gradually. This way, we can minimize blast radius of breaking
changes. We can even stop some kinds of them to happen at all, as they should be
caught at presubmit time and not allowed to merge at all.

### General principles

1. Allow at least 24h between changes to test configs to ensure the experiment
   is stable. We don't want to block PR to k/k because of a flaky experiment in
   clusterloader2.

1. Check with sig-scalability whether there is a regression in the test you want
   to enable experiment for. We don't want new features in clusterloader2 to
   interfere with regression' debugging.

1. For tests not listed below it's fine to enable experiments at your
   convenience.

### Step-by-step process

_Each step should be a separate PR_

1. Add a "knob" to turn on the future experiment. Usually this means adding a
   new environmental variable (PR to [test-infra]) or a new override file (PR to
   [perf-tests]). At this point the "knob" is not used by any code path.

   Since the knob is not used anywhere it's no-op and should be safe.

1. Enable experiment in [perf-tests presubmits],
   [ci-kubernetes-e2e-gci-gce-scalability] and [ci-kubernetes-kubemark-100-gce]

   Again since the knob is not used anywhere it's no-op and should be safe.

   Perf-test presubmit runs two jobs: [pull-perf-tests-clusterloader2] and
   [pull-perf-tests-clusterloader2-kubemark]. Primary role of those presubmits
   is to catch bugs in code from perf-tests, so we enable the experiment for
   both of those jobs first. Enabling experiment in
   [ci-kubernetes-e2e-gci-gce-scalability] should give you enough data points to
   determine whether experiments work, once we add new code path or config. It
   also runs frequent enough, so in case of problems you can revert quickly.
   Before enabling the experiment in [ci-kubernetes-e2e-gci-gce-scalability]
   make sure there is no ongoing regression affecting this test. If we are at
   the code freeze of thaw, you should wait with updating
   [ci-kubernetes-e2e-gci-gce-scalability] until the freeze is suspended.

1. Add a new code path or config that uses "knob" added in the first PR.

   We've already enable it in the first step, so the PR will be only merged if
   the new code path or configuration passes [perf-tests presubmits].

1. Enable experiment in [pull-kubernetes-e2e-gce-100-performance] and
   [pull-kubernetes-kubemark-e2e-gce-big]

   Changing presubmit definitions in test-infra has an ability to break k/k
   presubmits. PRs to test-infra don't trigger presubmit in k/k. Once you enable
   the experiment in the presubmit you need to watch next 3 runs after your PR
   is merged to detect breakages.

1. Enable experiment in [ci-kubernetes-e2e-gce-scale-performance] and rest of Kubemark
   tests ([ci-kubernetes-kubemark-500-gce], [ci-kubernetes-kubemark-gce-scale],
   and [ci-kubernetes-kubemark-high-density-100-gce])

   If feasible, please test experiment locally (it is outside of jobs run on
   Prow) first, as those tests run once a day and expensive. Double-check with
   sig-scalability that there is no ongoing regression in big clusters.

[head]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml#L121
[ci-kubernetes-e2e-gce-scale-performance]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml#L44
[ci-kubernetes-e2e-gci-gce-scalability]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-release-blocking-jobs.yaml#L98
[ci-kubernetes-kubemark-100-gce]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L258
[ci-kubernetes-kubemark-500-gce]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L307
[ci-kubernetes-kubemark-gce-scale]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L355
[ci-kubernetes-kubemark-high-density-100-gce]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L406
[perf-tests presubmits]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L267
[pull-kubernetes-e2e-gce-100-performance]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L3
[pull-kubernetes-kubemark-e2e-gce-big]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L149
[pull-perf-tests-clusterloader2-kubemark]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L317
[pull-perf-tests-clusterloader2]: https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-presubmit-jobs.yaml#L268
[test-infra]: https://github.com/kubernetes/test-infra
[perf-test]: https://github.com/kubernetes/perf-tests
