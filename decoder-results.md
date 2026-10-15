# Decoder Test Results

## Summary

This is the baseline run using the default decoder. Latencies below are
server-observed `WATCHLIST` request latencies from the ClusterLoader2
`APIResponsivenessPrometheus` artifact; client pod logs were not available.

| Decoder | Serializer | Run | Pod replicas | Workers | Input queue | Output queue | Mode | Watch-list enabled | Server-observed requests | SlowCount | p50 latency | p90 latency | p99 latency | Notes |
|---|---|---:|---:|---:|---:|---:|---|---|---:|---:|---:|---:|---:|---|
| DefaultDecoder | protobuf | 1 | 10 | — | — | — | informer | yes | 308 | 0 | 8691.964285 ms | 9774.107142 ms | 16970 ms | Baseline |
| ConcurrentDecoderWithFixedWorkersAndOutputQueue | protobuf | 1 | — | 10 | 100 | 100 | informer | yes | 460 | 0 | 5585.492227 ms | 6693.333333 ms | 8986.666666 ms | Input and output queues; fixed workers |
| ConcurrentDecoderWithOutputQueue | protobuf | 1 | 10 | dynamic (max 10) | 100 | 100 | informer | yes | 460 | 0 | 5676.646706 ms | 7288.495575 ms | 8192 ms | Dynamically spawned goroutines; maximum 10 |

## Configuration details

### DefaultDecoder

| Setting | Value |
|---|---|
| Serializer | protobuf (`ContentType: proto`) |
| Mode | informer |
| Pod replicas | 10 |
| Watch-list enabled | yes |
| Decoder-specific tuning | default; no custom decoder settings reported |

### ConcurrentDecoderWithFixedWorkersAndOutputQueue

| Setting | Value |
|---|---|
| Serializer | protobuf |
| Workers | 10 |
| Input queue | 100 |
| Output queue | 100 |

### ConcurrentDecoderWithOutputQueue

| Setting | Value |
|---|---|
| Serializer | protobuf |
| Pod replicas | 10 |
| CPU per replica | 6 |
| Workers | Dynamic; maximum 10 |
| Input queue | 100 |
| Output queue | 100 |

## Run notes

| Decoder | Serializer | Run | Source files | Observations |
|---|---|---:|---|---|
| DefaultDecoder | protobuf | 1 | `artifacts/APIResponsivenessPrometheus_list-benchmark_2026-10-15T09:22:18Z.json`; `artifacts/generatedConfig_list-benchmark.yaml` | Server `WATCHLIST` latency: p50 8691.964285 ms, p90 9774.107142 ms, p99 16970 ms; 308 requests; `SlowCount` 0 |
| ConcurrentDecoderWithFixedWorkersAndOutputQueue | protobuf | 1 | `artifacts/APIResponsivenessPrometheus_list-benchmark_2026-10-15T11:54:24Z.json` | Server `WATCHLIST` latency: p50 5585.492227 ms, p90 6693.333333 ms, p99 8986.666666 ms; 460 requests; `SlowCount` 0 |
| ConcurrentDecoderWithOutputQueue | protobuf | 1 | `artifacts/APIResponsivenessPrometheus_list-benchmark_2026-10-16T07:02:36Z.json`; `artifacts/generatedConfig_list-benchmark.yaml` | Server `WATCHLIST` latency: p50 5676.646706 ms, p90 7288.495575 ms, p99 8192 ms; 460 requests; `SlowCount` 0 |

## Data limitations

The test-pod logs were not available for this run. Consequently, this report
contains only server-observed latencies from the ClusterLoader2 artifact and
does not include client-observed or end-to-end pod latency.

## Run source

- CI run: [pull-perf-tests-benchmark-watchlist-proto-informer](https://gcsweb.k8s.io/gcs/kubernetes-ci-logs/pr-logs/pull/perf-tests/4484/pull-perf-tests-benchmark-watchlist-proto-informer/2110652829642264576/)
- CI run: [pull-perf-tests-benchmark-watchlist-proto-informer](https://gcsweb.k8s.io/gcs/kubernetes-ci-logs/pr-logs/pull/perf-tests/4484/pull-perf-tests-benchmark-watchlist-proto-informer/2110690951696486400/)
- CI run: [pull-perf-tests-benchmark-watchlist-proto-informer](https://gcsweb.k8s.io/gcs/kubernetes-ci-logs/pr-logs/pull/perf-tests/4484/pull-perf-tests-benchmark-watchlist-proto-informer/2110980077914165248/)
