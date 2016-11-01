# Overview

This directory contains scripts used to run a dns performance test in a
Kubernetes cluster. The performance script `run` benchmarks the performance of a
single DNS server instance with a synthetic query workload.

# Quickstart

## Running a performance test

Assuming you have a working `kubectl` command:

``` sh
$ mkdir out/                                        # output directory
$ ./run --params params/default.yaml --out-dir out  # run the perf test
```

`run` will run a performance benchmark ranging over the parameters given in
`--params`. The included `default.yaml` run will take several hours to run
through all combinations. Each run will create a `run-<timestamp>` directory
under the output directory. `latest` symlink will point to the latest run
directory that was created.

### Benchmarking the cluster DNS

You can benchmark the existing cluster DNS by specifying the `--use-cluster-dns`
flag. (As opposed to the server referenced by `--deployment-yaml`). Note: you
should be aware that some noise may be introduced by the client running on the
same pod as a DNS server.

Note: some parameters are skipped if running with the cluster DNS as they do not
apply.

## Analyzing results

Use the `ingest` script to parse the results of the runs into a sqlite3
database.

```sh
$ ./ingest --db out/db out/latest/*.out
```

The resulting metrics can then be queried using sqlite3. The schema of the
database can be shown using `sqlite3 out/db ".schema"`. To run sql queries,
you can use `sqlite3 out/db < my-query.sql`.

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

Kubernetes kube-dns v1.5+ now exports [Prometheus](http://prometheus.io) metrics
by default. A sample prometheus pod that scrapes kube-dns metrics is defined in
`cluster/prometheus.yaml` and can be created using kubectl:

```yaml
$ kubectl create -f cluster/prometheus.yaml
```

Key metrics to look at are:

* dnsmasq\_cache\_hits, dnsmasq\_cache\_misses - number of dns requests to the
  caching layer. Note: dnsmasq\_cache\_hits + dnsmasq\_cache\_misses = total DNS
  QPS.
* skydns\_skydns\_request\_duration\_seconds\_count - total number of requests
  served by the kube-dns component.

# Methodology

The questions we want to answer:

* What is the maximum queries per second (QPS) we can get from the Kubernetes
  DNS service given no limits?
* If we restrict CPU resources, what is the peformance we can expect?
  (i.e. resource limits in the pod yaml).
* What are the SLOs (e.g. query latency) for a given setting that the
  user can expect? Alternate phrasing: what can we expect in realistic
  workloads that do not saturate the service?

The inclusion of `max_qps` vs attained `qps` is to answer the third question.
For example, if a user does not hit the maximum QPS possible from a given DNS
server pod, then what are the latencies that they should expect? Latency
increases with load and if a user's applications do not saturate the service,
they will attain better
latencies.

## Parameters

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

# Extending the perf test

## Using a different DNS server

You can give different DNS server yaml to the runner via the `--deployment-yaml`
flag. Note: test parameters such as `kubedns_cpu` etc may no longer make sense,
so they should be removed from the `--params` file when the test is run.

## Adding a new test parameter

To add a new test parameter to be explored, edit `py/params.py` and subclass the
appropriate `*Param` class and add the parameter to module variable
`PARAMETERS`. Each parameter instance implements the modification to the test
inputs (e.g. Kubernetes deployment yaml) necessary to set the value.

# Building the dnsperf image

See [image/README.md](image/README.md).
