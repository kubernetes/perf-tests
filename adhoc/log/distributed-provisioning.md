Purpose
=======

The scale test tries to answer three different questions:
1. How quickly can a Kubernetes cluster provision volumes when using
   a CSI driver?
1. How well does [distributed
   tracing](https://github.com/kubernetes-csi/external-provisioner/pull/524)
   scale and which defaults should be used for the
   external-provisioner parameters?
1. What resource requirements do the sidecars have under load?
   This is relevant for https://github.com/kubernetes-csi/csi-driver-host-path/issues/47

Test setup
==========

ClusterLoader2
--------------

A [modified
version](https://github.com/kubernetes/perf-tests/pull/1530) of the
volume provisioning test was used. The key change was to test volume
provisioning without starting pods.

PMEM-CSI
--------

A [special version of
PMEM-CSI](https://github.com/pohly/pmem-csi/tree/scale-testing) was
prepared with support for both distributed and central
provisioning. This enables the comparison of both approaches while
using the same driver.

A fake backend was added to that driver which simulates volume
creation: each node pretends to have 1TB of storage and refuses to
create new volumes when the sum of all volume sizes on the node exceed
that. With this backend, PMEM-CSI runs without actual PMEM available
and volume operations are fast. This makes it possible to measure just
the communication overhead for volume provisioning.

Test script
-----------

A [test
script](https://github.com/pohly/pmem-csi/blob/scale-testing/hack/scale-test.sh)
in that special PMEM-CSI version runs through different scenarios:
- install PMEM-CSI as intended
- install ClusterLoader2
- test central provisioning
- test distributed provisioning with different external-provisioner parameters

The script chooses the number of volumes and volume size so that the
entire cluster gets filled up. This is important because it ensures
that "resource exhausted" errors on a node are handled gracefully. No
changes in the script are needed when running it against different
cluster sizes.

The [vertical pod
autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler)
gets installed and configured to produce recommendations for the
PMEM-CSI pods.

In an intermediate version of the script which then didn't end up
getting used for the final testing, overhead for kube-scheduler was
avoided by setting annotations as for late binding directly when
creating PVCs. This revealed a scalability problem in
external-provisioner ([see
below](#reducing-overhead-in-external-provisioner)). But because this
isn't how volumes are normally created, the actual testing then used
immediate binding without creating any pods.

The result of these tests are:
- junit.xml from ClusterLoad2 with timing information for volume
  creation and deletion
- Grafana dashboards which show performance of the cluster under
  load.

Test cluster
------------

Tests with 100 and 1000 nodes where run with the ["ad-hoc" Prow
jobs](https://github.com/kubernetes/perf-tests/tree/master/adhoc) via
[this PR](https://github.com/kubernetes/perf-tests/pull/1610).

Grafana dashboards were created manually by @mm4tt for some test runs.

Troubleshooting
===============

Several issues where encountered while trying to get the tests to run.

kubectl
-------

Inside the Prow environment, `kubectl` and `$KUBECTL` are both
available. The latter is a wrapper script with a relative path. Beware
that invoking it after changing the current directory fails. `kubectl`
works from any directory.

Downloading images from Docker
------------------------------

Bringing up PMEM-CSI on the 100 node cluster was okay. On the 1000
node cluster downloading from Docker was too slow, probably due to
throttling. As a workaround, the image required for the test was
re-published on GCR.

Prometheus
----------

On the 1000 node cluster, Prometheus did not come up. ClusterLoad2
got stuck with `Waiting for Prometheus stack to become healthy...`

Debugging this inside Prow was not possible due to time constraints
and the slow turnaround time for remote debugging without direct
access to the running cluster (add debug output, run test, wait for
cluster to be provisioned, check logs, try again).

The root cause was identified after getting direct access to a 1000
node cluster: there was no suitable node with enough RAM for the
Prometheus pods. This got fixed by changing the node configuration of
the cluster.

Metrics gathering
-----------------

Metrics gathering for `--provider=gce` relies on ssh access and failed
for the manually created 1000 node cluster. Tests had to be run
without it.

Results
=======

Reducing overhead in external-provisioner
-----------------------------------------

Analysing the Grafana dashboards showed a high number of GET
operations for nodes when using late binding. This got fixed in
[external-provisioner](https://github.com/kubernetes-csi/external-provisioner/pull/536)
and
[sig-storage-lib-external-provisioner](https://github.com/kubernetes-sigs/sig-storage-lib-external-provisioner/pull/100)
by using a node informer that external-provisioner had already anyway.

Volume provisioning rate
------------------------

With the default kube-controller-manager settings, rate limiting in
client-go restricts volume provisioning to around 4
volumes/second. For the following results, `qps=1000` and `burst=1000`
were used for kube-controller-manager.

When creating 3000 volumes for the 1000 node cluster, creating the
PVCs with a `qps=150` setting for ClusterLoad2 took roughly 20
seconds. Provisioning (by the CSI driver) and binding (by
kube-scheduler) then took around 100 additional seconds. This shows
that Kubernetes can provision 25 volumes/second.

Distributed vs. central provisioning
------------------------------------

[Delay parameters](https://github.com/kubernetes-csi/external-provisioner#distributed-provisioning) are crucial when using distributed
provisioning to avoid the thundering herd problem where all
provisioners wake up at the same time to process a new PVC without a
selected node.

Several combinations of parameters were compared in the end with 1000
nodes:
- central provisioning
- distributed provisioning with
  - initial delay 10 s, max delay 30 s
  - initial delay 20 s, max delay 30 s
  - initial delay 30 s, max delay 60 s

Other combinations had been tried earlier on the 100 node
cluster. Smaller initial delays were ruled out at that time because
too many provisioners were waking up for the same PVC.

| setup               | PVC created | PVC bound | rate         |
| --------------------|-------------|-----------|--------------|
| central             | 20 s        | 106 s     | 25 volumes/s |
| distributed 10s/30s | 23 s        | 112 s     | 22 volumes/s |
| distributed 20s/30s | 21 s        | 112 s     | 22 volumes/s |
| distributed 30s/60s | 23 s        | 110 s     | 22 volumes/s |

Distributed provisioning is slightly slower than central provisioning,
but the difference is small enough to make this approach viable. The
load on the apiserver is higher (seen when analysing Grafana
dashboards), which explains why the initial PVC creation step gets
slowed down a bit.

This is for immediate binding. With late binding, the thundering herd
problem does not exist and PVCs get provisioning immediately by a
single provisioner, therefore it is the recommended way of using
distributed provisioning.

Resource requirements
---------------------

VPA recommended for central provisioning:
```
    # Controller:
    recommendation:
      containerRecommendations:
      - containerName: pmem-driver
        lowerBound:
          cpu: 259m
          memory: "163091027"
        target:
          cpu: 813m
          memory: "511772986"
        uncappedTarget:
          cpu: 813m
          memory: "511772986"
        upperBound:
          cpu: 627984m
          memory: "395308076471"
      - containerName: external-provisioner
        lowerBound:
          cpu: 417m
          memory: 131072k
        target:
          cpu: 1388m
          memory: "323522422"
        uncappedTarget:
          cpu: 1388m
          memory: "323522422"
        upperBound:
          cpu: 1072130m
          memory: "249897962250"
    ...
    # Node:
    recommendation:
      containerRecommendations:
      - containerName: pmem-driver
        lowerBound:
          cpu: 12m
          memory: 131072k
        target:
          cpu: 12m
          memory: 131072k
        uncappedTarget:
          cpu: 12m
          memory: 131072k
        upperBound:
          cpu: 6611m
          memory: "14168573798"
      - containerName: driver-registrar
        lowerBound:
          cpu: 12m
          memory: 131072k
        target:
          cpu: 12m
          memory: 131072k
        uncappedTarget:
          cpu: 12m
          memory: 131072k
        upperBound:
          cpu: 6611m
          memory: 6911500k

```

And for distributed provisioning with initial delay 20 s, max delay 30 s:
```
    # Node:
    recommendation:
      containerRecommendations:
      - containerName: pmem-driver
        lowerBound:
          cpu: 9m
          memory: "87381333"
        target:
          cpu: 11m
          memory: "87381333"
        uncappedTarget:
          cpu: 11m
          memory: "87381333"
        upperBound:
          cpu: 790m
          memory: "826594339"
      - containerName: driver-registrar
        lowerBound:
          cpu: 9m
          memory: "87381333"
        target:
          cpu: 11m
          memory: "87381333"
        uncappedTarget:
          cpu: 11m
          memory: "87381333"
        upperBound:
          cpu: 790m
          memory: "826594339"
      - containerName: external-provisioner
        lowerBound:
          cpu: 109m
          memory: "87381333"
        target:
          cpu: 296m
          memory: "93633096"
        uncappedTarget:
          cpu: 296m
          memory: "93633096"
        upperBound:
          cpu: 27241m
          memory: "7893239268"
```
