# Single-member defrag benchmark

Run with `--testconfig=testing/etcd/defrag.yaml --enable-prometheus-server=true`.
The scenario creates a fresh, single-member etcd cluster. Ten writers each target
100 direct etcd writes/second by default, with background defrag disabled.

The sequence is: 1m warm-up, 5m baseline, one `etcdctl defrag` Job, 2m recovery,
and 5m afterward. The Job has a 5m RPC timeout and no retries.

- `CL2_ETCD_DEFRAG_PHASE_DURATION` sets each before/after window (default `5m`;
  keep it longer than the 1m rate window).
- `CL2_ETCD_DEFRAG_ONCE=false` runs the same phases without defrag or its count
  assertion, for comparison.

Existing workload and storage settings apply. `CL2_ETCD_REPLICAS`,
`CL2_ETCD_DEFRAG_INTERVAL`, and `CL2_ETCD_BENCHMARK_DURATION` do not control this
scenario.

## Results

Compare Put throughput, server latency (rolling p99), errors, and database size
under the `BeforeDefrag`, `DefragWindow`, and `AfterDefrag` metric prefixes.
`DefragWindow` includes Job startup and recovery, so it is not the RPC duration.

`EtcdSingleDefragCount` must equal one for `EtcdSingleDefragDuration` to represent
the operation's backend duration in seconds. The Pod log records the client RPC
duration. Job failure, timeout, missing metrics, or an incorrect count fail the run.

Keep the Prometheus snapshot to inspect short stalls hidden by phase summaries.
