# Metrics of interest when analyzing a scale run

The measurement JSONs cover the SLOs. Everything below comes from the Prometheus snapshot
(`--experimental-prometheus-snapshot-to-report-dir=true`) and the profiles collected
alongside it.

## What gets scraped

From `pkg/prometheus/manifests/master-ip/master-serviceMonitor.yaml`. All of it carries
`job="master"`, so pick a component by its port.

| component | port | interval | scraped when |
|---|---|---|---|
| kube-apiserver | 443 | 5s | always, 30s under `PROMETHEUS_SLOW_APISERVER` |
| kube-scheduler | 10259 | 5s | always |
| kube-controller-manager | 10257 | 5s | always |
| etcd | 2379, 2382 | 5s | `PROMETHEUS_SCRAPE_ETCD` |
| master kubelet and cAdvisor | 10250 | 5s | `PROMETHEUS_SCRAPE_MASTER_KUBELETS` |
| node-exporter | 9100 | 30s | `PROMETHEUS_SCRAPE_NODE_EXPORTER` |

## Per-process resource use

| series | what it tells you | notes |
|---|---|---|
| `rate(process_cpu_seconds_total[1m])` | cores consumed by the process | |
| `process_resident_memory_bytes` | RSS | the number to quote for memory |
| `go_memstats_heap_inuse_bytes`, `go_gc_heap_live_bytes` | heap in use and live heap | live heap is roughly `next_gc/2`, so it moves with GC timing |
| `rate(go_gc_heap_allocs_bytes_total[1m])` | allocation rate | a better regression signal than heap size, which GC timing smears |
| `go_goroutines` | goroutine count | |
| `container_memory_working_set_bytes` (cAdvisor) | per-container memory | excludes inactive page cache, so it badly understates mmap-heavy processes such as etcd. Prefer RSS |

## Whole machine

node-exporter, control-plane nodes only.

| series | what it tells you | notes |
|---|---|---|
| `sum(rate(node_cpu_seconds_total{mode!="idle"}[2m]))` | busy cores, and the `mode="idle"` counterpart | 30s scrape, so the rate window must cover several samples. Too wide a window extrapolates |
| `node_memory_MemAvailable_bytes` | host memory headroom | |
| `rate(node_disk_io_time_seconds_total[2m])` | disk saturation under etcd | |
| `rate(node_netstat_Tcp_{InSegs,OutSegs,RetransSegs}[2m])` | network volume and retransmits | |

Check headroom here before any hypothesis that assumes a resource was scarce.

## Request path

| series | what it tells you | notes |
|---|---|---|
| `apiserver_request_duration_seconds_bucket{subresource!~"log\|exec\|portforward\|attach\|proxy"}` | latency by verb and scope | the exclusion is not optional, long-lived streaming endpoints otherwise dominate the tail |
| `apiserver_request_total` | request volume, and the denominator for per-request normalization | |
| `apiserver_current_inflight_requests` | concurrency actually in flight | |
| `apiserver_longrunning_requests` | open watches and other long-running requests | |
| `apiserver_storage_objects` | object counts by resource | |
| `apiserver_flowcontrol_{current_executing_requests,rejected_requests_total,request_wait_duration_seconds}` | whether APF is admitting or holding | |
| `apiserver_flowcontrol_current_inqueue_requests` | queue depth per priority level | queueing and rejection move independently, so read this alongside the rejected counter rather than instead of it |
| `apiserver_request_total{verb="WATCHLIST"}` against `{verb="LIST"}` | whether reflectors are getting a watch-list or falling back | a rising LIST share for the same resource means watch-list is failing and clients are retrying as plain LISTs. The fallback reads are cheap, so this inflates request counts more than it costs |

## etcd

| series | what it tells you | notes |
|---|---|---|
| `etcd_request_duration_seconds` | storage latency as the apiserver sees it | despite the name this is emitted by the apiserver, not etcd, so it is on the apiserver target and includes the round trip. Different quantity from the disk metrics below |
| `etcd_disk_wal_fsync_duration_seconds` | write path latency | |
| `etcd_disk_backend_commit_duration_seconds` | commit latency | |
| `etcd_mvcc_db_total_size_in_bytes` | database size | |
| `etcd_server_leader_changes_seen_total` | leader churn | |

etcd shares the control-plane host with the apiserver, so a host-level effect appears in
both these and the apiserver series, and neither is then evidence about the other.

## Watch cache

Server side, labelled by `group` and `resource`.

| series | what it tells you | notes |
|---|---|---|
| `apiserver_terminated_watchers_total` | watchers the server closed as unresponsive | |
| `apiserver_init_events_total` | relist volume, which is what those terminations generate | |
| `apiserver_watch_cache_events_received_total` | events the cache ingested | |
| `apiserver_watch_cache_events_dispatched_total` | events it fanned out, so the ratio to received is fan-out width | |
| `apiserver_watch_events_total` | events delivered to clients | counts deliveries, so it scales with the number of watchers. Reading it as ingest turns a fan-out change into an apparent load change. Pair it with the received counter above |
| `apiserver_watch_cache_read_wait_seconds` | time a read waited for the cache to catch up | |
| `apiserver_watch_cache_resource_version` | how far the cache itself has advanced | |

## Informer lag

Client side. From `component-base/metrics/prometheus/clientgo/fifo`, subsystem `informer`,
labelled by `name`, `group`, `version`, `resource`.

| series | what it tells you | notes |
|---|---|---|
| `informer_queued_items` | DeltaFIFO depth |  |
| `informer_processing_latency_seconds` | time to process an event after it is popped | this is post-pop only, so queue wait is the depth above, not this |
| `informer_store_resource_version` | how far the informer's store has advanced | only the 15 least significant digits |

Differencing `informer_store_resource_version` against
`apiserver_watch_cache_resource_version` for the same resource shows how far a consumer
trails the server's own cache, which is the one direct read on delivery lag. Three things
make it easy to misread:

- an RV difference is not a duration. RV is a cluster-wide etcd counter, so the gap scales
  with total write volume, not with how late the consumer is.
- the watch cache gauge only advances when that resource sees an event, so a quiet resource
  reads as stale when nothing is wrong.
- the informer gauge is truncated, so compare like with like before subtracting.

`informer_store_resource_version` is exported by any client-go consumer, so the same
comparison works for kube-controller-manager and kube-scheduler against a given
apiserver's watch cache, not just for the apiserver's own loopback informers.

## Authorization

| series | what it tells you | notes |
|---|---|---|
| `apiserver_authorization_decisions_total{decision="deny"}` | denials attributed to an authorizer by `type` and `name` | |
| `apiserver_request_total{code="403"}` | denials as the client saw them | split by `resource` and `verb` before assuming which call is being denied. Denials during a pod-startup storm can be `POST serviceaccounts` token requests rather than the secret or configmap reads people expect |
| `node_authorizer_graph_actions_duration_seconds{operation}` | node authorizer graph maintenance | exponential buckets ending around 200ms, so a p99 above that is unresolvable. The `_count` is the more useful half, since inverting it on the cumulative pod curve estimates how far the graph trails |

The node authorizer is driven by the apiserver's own pod informer, so its staleness is the
informer lag above rather than anything in the authorizer itself.

## Go scheduler and locks

A deeper tier, for when the resource metrics above show headroom but latency is still bad.
Scoped to `{job="master",endpoint="apiserver"}`.

| series | what it tells you | notes |
|---|---|---|
| `rate(go_sync_mutex_wait_total_seconds_total[1m])` | mean goroutines blocked on a Go mutex | does not attribute to a lock, and includes runtime-internal locks |
| `rate(go_sched_latencies_seconds_sum[1m]) / rate(..._count[1m])` | mean time a runnable goroutine waits for a P | the exported histogram has few buckets and a low top finite edge, so a p99 taken from it pins to that edge |
| `go_sched_goroutines_{runnable,running}_goroutines` | work queued against work executing | `waiting` dominates the total, so overall goroutine count moves for unrelated reasons |
| `rate(go_cpu_classes_{user,gc_total,gc_mark_assist}_cpu_seconds_total[1m])` | Ps with work assigned, summing to GOMAXPROCS | not CPU consumed. Differs from `process_cpu_seconds_total`, and both are worth reporting |

## Profiles

CL2 collects CPU, memory and block profiles per control-plane component, as
`<host>_<component>_{CPU,Memory,Block}Profile_<test>_<timestamp>.pprof`. There is no mutex
profile and no execution trace, so the block profile is the only source for lock
attribution. It captures semacquire, so mutex waits are present, mixed with channel
blocking.

Profiles are cumulative since process start, so a single file says nothing about a run.
Diff a pair.
