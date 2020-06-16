# Clusterloader2 - tracking OOMs

In order to track pods OOMs (out of memory) during clusterloader2 tests
execution, TestMetrics has been augmented with `ClusterOOMsTracker` measurement.
This outputs an appropriate summary containing basic information regarding OOMs
inside the cluster like the PID of OOMing process or the name of a node where
the OOM happened.

Given that `ClusterOOMsTracker` is based on Kubernetes events and events are
best effort by their nature, the reported summary is not guaranteed to
accurately describe what really happened - some of the OOMs may be missed.

## Enabling OOMs tracking

Firstly, ensure that the `TestMetrics` measurement is added to `Starting
measurements` and `Collecting measurements` steps of your test config. Next,
add `clusterOOMsTrackerEnabled` parameter and set it to `true` in both steps
configuration.

Sample configuration in `Starting measurements` step:

```yaml
- name: Starting measurements
  measurements:
    ...
    - Identifier: TestMetrics
      Method: TestMetrics
      Params:
        action: start
        clusterOOMsTrackerEnabled: true
```

Sample configuration in `Collecting measurements` step:

```yaml
- name: Collecting measurements
  measurements:
    ...
    - Identifier: TestMetrics
      Method: TestMetrics
      Params:
        action: gather
        clusterOOMsTrackerEnabled: true
```

## Further debugging steps

`ClusterOOMsTracker` watches for events emitted by `node-problem-detector` when
an OOM occurs. Such events contain only a fraction of information that may be
useful for debugging - for more, check `systemd.log` files of an appropriate
node for the name of OOMing pod/container or the container's memory limit.
