# Overview

This directory contains scripts used to run a DNS performance test in a
Kubernetes cluster. The performance script `run` benchmarks the performance of a
single DNS server instance with a synthetic query workload.

# Quick start

## Prerequisites

This assumes you have a working `kubectl` command Kubernetes cluster.  The
Python code depends on the `numpy` package, which is available as `python-numpy`
on Debian-based systems or with `pip install`.

## Running a performance test

For CoreDNS:

``` sh
$ ./run core-dns out
```
or
``` sh
$ mkdir out/                                        # output directory
$ python py/run_perf.py --dns-server coredns --params params/coredns/default.yaml --out-dir out  # run the perf test
```

For kube-dns:

``` sh
$ ./run kube-dns out
```
or
``` sh
$ mkdir out/                                        # output directory
$ python py/run_perf.py --dns-server kube-dns --params params/kubedns/default.yaml --out-dir out  # run the perf test
```

For node-local-dns:

``` sh
$ ./run node-local-dns out
```
or
``` sh
$ mkdir out/                                        # output directory
$ python py/run_perf.py --params params/nodelocaldns/default.yaml --out-dir out --nodecache-ip 169.254.20.10  # run the perf test
```


`run` will run a performance benchmark ranging over the parameters given in
`--params`. The included `default.yaml` run will take several hours to run
through all combinations. Each run will create a `run-<timestamp>` directory
under the output directory. `latest` symlink will point to the latest run
directory that was created.

### Benchmarking the cluster DNS

You can benchmark the existing cluster DNS by specifying the
`--use-cluster-dns` flag. (As opposed to the server referenced by
`--deployment-yaml`). Note: you should be aware that some noise may be
introduced if the client runs on the same pod as a DNS server.

Note: test parameters such as resource limits do not apply when testing the
cluster DNS as they cannot be changed. The run script will skip these
parameters when running in this mode. (See `params.Param.is_relevant()` for
details).

### Comparing cluster DNS and NodeLocal DNSCache

You can compare the performance of the existing cluster DNS with NodeLocal
DNSCache on a cluster that has NodeLocal DNSCache enabled.

You can run the following test to get the NodeLocal DNSCache data.

``` sh
$ mkdir out/
$ python py/run_perf.py --params params/nodelocaldns/default.yaml --out-dir out --nodecache-ip <listen-ip>
```

If you have configured NodeLocal DNSCache to listen on kube-dns service IP,
then use that same service ip as `<listen-ip>`. Otherwise, use the IP address
that NodeLocal DNSCache is listening on requests for. (169.254.20.10 or any
custom IP that you selected).

You can run the following test to get the clusterDNS data. Using the same params
as the nodelocaldns test makes the comparison easier.

``` sh
$ mkdir out/
$ python py/run_perf.py --params params/nodelocaldns/default.yaml --out-dir out --dns-ip <dns-service-ip>
```

If NodeLocal DNSCache is listening on the kube-dns service IP, use the IP
address of `kube-dns-upstream` service as `<dns-service-ip>` in this test.
This will be the service IP that node-local-dns pods use as upstream on a cache
miss.
Otherwise, use the kube-dns service IP as the `<dns-service-ip>`.

http://perf-dash.k8s.io/#/?jobname=node-local-dns%20benchmark shows the results
from periodic runs of NodeLocal DNSCache test.

http://perf-dash.k8s.io/#/?jobname=kube-dns%20benchmark shows the results from
periodic runs of the kube-dns test. This test runs on a cluster that uses kube-dns as cluster DNS.

The source for the scalability jobs is at: 
https://github.com/kubernetes/test-infra/blob/27a0743d7806eb0095188352841c2eadd46d2e9b/config/jobs/kubernetes/sig-scalability/sig-scalability-periodic-jobs.yaml#L414


## Analyzing results

Use the `ingest` script to parse the results of the runs into a sqlite3
database.

```sh
$ ./ingest --db out/db out/latest/*.out
```

The resulting metrics can then be queried using sqlite3. The schema of the
database can be shown using `sqlite3 out/db ".schema"`. To run sql queries, you
can use `sqlite3 out/db < my-query.sql` or `sqlite3 out/db "select * from runs"`
directly.

### Example queries

Maximum 99th percentile latency with dnsmasq caching disabled:

```sql
SELECT
  max(latency_99_percentile)
FROM
  results NATURAL JOIN runs -- equijoin on run_id, run_subid
WHERE
  dnsmasq_cache = 0;
```

Runs that have 95th percentile latency less than 20 ms:

```sql
SELECT
  run_id, run_subid, dnsmasq_cpu, kubedns_cpu, max_qps, query_file,
  '--',
  qps, latency_95_percentile
FROM
  results NATURAL JOIN runs
WHERE
  results.latency_95_percentile < 20 -- milliseconds
  AND results.run_id = runs.run_id
  AND results.run_subid = runs.run_subid
ORDER BY
  qps ASC;
```

Additional sql queries can be found in `sql/`.

# Monitoring

CoreDNS and kube-dns v1.5+ (image `k8s.gcr.io/kubedns-amd64:1.9`)
 can export [Prometheus](http://prometheus.io) metrics. A sample
prometheus pod that scrapes kube-dns metrics is defined in
`cluster/prometheus.yaml` and can be created using kubectl:

```yaml
$ kubectl create -f cluster/prometheus.yaml
```

Key metrics to look at are:

* `dnsmasq\_cache\_hits`, `dnsmasq\_cache\_misses` - number of DNS requests to the
  caching layer. Note: `dnsmasq\_cache\_hits + dnsmasq\_cache\_misses` = total DNS
  QPS.
* `skydns\_skydns\_request\_duration\_seconds\_count` - total number of requests
  served by the kube-dns component.

# Details

## Methodology

The questions we want to answer:

* What is the maximum queries per second (QPS) we can get from the Kubernetes
  DNS service given no limits?
* If we restrict CPU resources, what is the performance we can expect?
  (i.e. resource limits in the pod yaml).
* What are the SLOs (e.g. query latency) for a given setting that the
  user can expect? Alternate phrasing: what can we expect in realistic
  workloads that do not saturate the service?

The inclusion of `max_qps` vs attained `qps` is to answer the third question.
For example, if a user does not hit the maximum QPS possible from a given DNS
server pod, then what are the latencies that they should expect? Latency
increases with load and if a user's applications do not saturate the service,
they will attain better latencies.

## Parameters

The performance test harness tests all combinations of the parameters given in
the `--params` file. For example, the yaml file below will test all
combinations of `run_length_seconds`, `kubedns_cpu`, `dnsmasq_cpu`, ...,
`query_file`, resulting in `1 * 4 * 5 * 2 * 5 * 4 = 800` combinations.

``` yaml
# Number of seconds to run with a particular setting.
run_length_seconds: [60]
# cpu limit for kubedns, null means unlimited.
kubedns_cpu: [200, 250, 300, null]
# cpu limit for dnsmasq, null means unlimited.
dnsmasq_cpu: [100, 150, 200, 250, null]
# size of dnsmasq cache. Note: 10000 is the maximum. 0 to disable caching.
dnsmasq_cache: [0, 10000]
# Maximum QPS for dnsperf. dnsperf is self-pacing and will ramp request rate
# until requests are dropped. null means no limit.
max_qps: [500, 1000, 2000, 3000, null]
# File to take queries from. This is in dnsperf format.
query_file: ["nx-domain.txt", "outside.txt", "pod-ip.txt", "service.txt"]
```

## Results schema

``` sql
CREATE TABLE runs (
  run_id,
  run_subid,
  run_length_seconds,
  dnsmasq_cpu,
  dnsmasq_cache,
  kubedns_cpu,
  max_qps,
  query_file,
  primary key (run_id, run_subid)
);

CREATE TABLE results (
  run_id,
  run_subid,
  queries_sent,
  queries_completed,
  queries_lost,
  run_time,
  qps,
  avg_latency,
  min_latency,
  max_latency,
  stddev_latency,
  latency_50_percentile,          -- in milliseconds
  latency_95_percentile,
  latency_99_percentile,
  latency_99_5_percentile,
  primary key (run_id, run_subid)
);

CREATE TABLE histograms (
  run_id,
  run_subid,
  rtt_ms,
  rtt_ms_count
);
```

# Customizing and extending

## Using the cluster DNS server configuration

In Kubernetes 1.10 and earlier, `kube-dns` is installed by default using
[addon-manager](https://github.com/kubernetes/kubernetes/tree/master/cluster/addons).
The deployment configuration is located in `/etc/kubernetes/addons/dns`. You can
use the deployment yaml from this directory as the argument to
`--deployment-yaml` above, however, you will need to replace the `k8s-app:
kube-dns` label and replace it with `app: dns-perf-server` to avoid
clashing with the system DNS.

## Using a different DNS server

You can give different DNS server yaml to the runner via the `--deployment-yaml`
flag. Note: test parameters such as `kubedns_cpu` etc may no longer make sense,
so they should be removed from the `--params` file when the test is run.

## Adding new test parameters

To add a new test parameter to be explored, edit `py/params.py` and subclass the
appropriate `*Param` class and add the parameter to module variable
`PARAMETERS`. Each parameter instance implements the modification to the test
inputs (e.g. Kubernetes deployment yaml) necessary to set the value.

# Building the dnsperf image

See [image/README.md](image/README.md).
