---
name: debug-watchlist-latency
description: >
  Debug watchlist or list latency from a scale-performance-5000 test run.
  Triggers: "watchlist latency", "scale-performance-5000",
  "watch_list_duration_seconds", GCS artifact URLs from perf-tests
  scale-performance jobs, watch cache performance, goroutine scheduling
  contention, APF throttling during scale tests.
argument-hint: "<gcs-artifact-url>"
arguments: [url]
allowed-tools: Bash Read WebFetch
---

# Debug Watchlist Latency at Scale

Systematically debug WATCHLIST and LIST latency from a `scale-performance-5000` test run.
Given a GCS artifact URL, walk through watchlist pod logs, Prometheus metrics, apiserver
logs, and pprof profiles to identify the root cause of latency regressions.

Reference issue: https://github.com/kubernetes/kubernetes/issues/138670

The test creates a real 5000-node cluster and runs approximately 165K pods (this number
may change across test revisions — check the actual test configuration for the current
value). The high pod count drives significant watch event churn, especially during
scale-up and scale-down phases, which is where latency issues typically surface.

## Locate Test Artifacts

Resolve the GCS URL `$url` into browsable artifact paths. Use `gsutil ls` to enumerate:

```
$url/
  artifacts/
    <master-name>/                    # e.g. ci-master-scale-master
      kube-apiserver-audit-logs/      # audit logs per apiserver instance
      kube-apiserver-logs/            # container logs
    prometheus-metrics/               # Prometheus snapshot or scraped metrics JSON
    pprof/                            # CPU, goroutine, heap, block profiles
    resource-usage/                   # resource usage CSVs
```

1. Run `gsutil ls $url/artifacts/` to enumerate artifact directories.
2. Locate watchlist pod logs (`informer-watch-list-on-0`, `informer-watch-list-off-0`) from both worker and master nodes.
3. Identify apiserver log directories. Multiple apiserver instances may exist — check each.
4. Check for Prometheus snapshot vs metrics JSON.
5. Check for pprof profiles (may be per-apiserver).

If the URL points to a Prow job, the artifacts are under `$url/artifacts/`. If it's a direct
GCS path to an artifact, adjust accordingly.

## Test Topology: Worker Node vs Master Node Pods

The scale-performance-5000 test runs watchlist test pods from **two locations**:

- **Worker node pod** — runs on a regular worker node, connects to the apiserver over
  the network. LIST uses compression; WATCHLIST currently does not support compression
  (this may change in the future). Logs are in artifacts as `informer-watch-list-on-0`
  (WATCHLIST enabled) and `informer-watch-list-off-0` (WATCHLIST disabled, falls back
  to LIST).

- **Master node pod** — runs on the control-plane node itself, connects to the apiserver
  via **localhost**, does **not** use compression. This eliminates network hops, TLS
  overhead, and compression cost from the measurement.

**Why this matters**: comparing latency between the two locations is the fastest way to
rule out networking as a factor. If both worker-node and master-node pods show the same
high latency, the problem is likely not network-related and warrants further
investigation (apiserver internals, Go runtime, etc.).

Always check both sets of logs early in the investigation — it saves significant time
before diving into apiserver internals.

## General Rules

**Download everything to local disk.** All analyzed files (logs, Prometheus snapshots,
pprof profiles) must be downloaded to local disk before analysis. These files are large —
streaming and grepping directly from GCS does not work well.

**Always provide executable PromQL queries.** When referencing a Prometheus metric,
always print the full PromQL query that the user can copy-paste into their local
Prometheus UI. Include Prometheus UI URLs with the query and time range pre-filled
when possible.

Create a local working directory for the investigation:

```bash
mkdir -p /tmp/watchlist-debug/<run-id>
cd /tmp/watchlist-debug/<run-id>
```

## Debugging Workflow

### Step 1: Download watchlist pod logs and plot client-side latency

Download logs from the watchlist test pods to local disk:

```bash
gsutil cp $url/artifacts/**/informer-watch-list-on-0*.log .
gsutil cp $url/artifacts/**/informer-watch-list-off-0*.log .
```

Download logs from both worker-node and master-node pods (see Test Topology above).

Parse the logs and plot WATCHLIST vs LIST latency on the same graph over time. The
`informer-watch-list-on-0` pods use WATCHLIST; the `informer-watch-list-off-0` pods
have WATCHLIST disabled and use regular LIST. Plotting both together gives an immediate visual comparison
of the two code paths as observed by the client (including network transfer,
decompression, and decoding).

### Step 2: Download Prometheus snapshot and check server-side SLI

Download the Prometheus snapshot to local disk and start a local Prometheus instance
to query it:

```bash
gsutil -m cp -r $url/artifacts/prometheus-metrics/ .
# Extract and start local Prometheus pointing at the snapshot
```

Query the server-side P99 latency for both WATCHLIST and LIST for pods:

```promql
histogram_quantile(0.99, rate(apiserver_watch_list_duration_seconds_bucket{job="master", endpoint="apiserver", resource="pods"}[2m]))
histogram_quantile(0.99, rate(apiserver_request_duration_seconds_bucket{job="master", endpoint="apiserver", resource="pods", verb="LIST"}[2m]))
```

Print the full Prometheus UI URLs with queries and time range pre-filled so the user
can open them in a browser and visually compare with the client-side graph from Step 1.
The URL format is:

```
http://localhost:9090/graph?g0.expr=<url-encoded-query>&g0.tab=0&g0.range_input=<duration>&g0.end_input=<end-time>
```

**How to interpret**: compare all four curves — client-side WATCHLIST vs LIST (from
Step 1) and server-side WATCHLIST vs LIST (from Prometheus).

- If the server-side and client-side graphs match for a given verb, then client-side
  processing (network transfer, decompression, decoding) is negligible — the bottleneck
  is entirely inside the apiserver.
- If they diverge (client latency significantly higher than server latency), investigate
  client-side overhead: network bandwidth, compression/decompression cost, or client
  decoding time.
- If both WATCHLIST and LIST follow the same latency pattern, that is a strong signal
  that something systemic inside the apiserver is affecting both code paths (e.g.,
  goroutine scheduling contention, CPU saturation, GC pressure). Focus on shared
  infrastructure rather than WATCHLIST-specific or LIST-specific code. For example,
  in a prior investigation LIST latency increased first and WATCHLIST followed the
  same pattern — this ruled out a WATCHLIST-specific regression and pointed to a
  shared bottleneck (goroutine scheduling contention during churn).
- If only WATCHLIST is slow → focus on watch setup and event streaming path.
- If only LIST is slow → focus on cache list or etcd list path.
- If both worker-node and master-node are slow → not a network issue (see Test Topology).

### Step 4: Check event volume

Query `apiserver_watch_events_total` by resource. During scale-up creating ~165K pods,
expect hundreds of thousands of events for pods. High event counts correlate with contention.

Check `apiserver_watch_events_sizes` for event size distribution. Large objects (>16KB)
increase serialization cost per event, multiplied by watcher count.

### Step 5: Check pprof across test phases and Go runtime metrics

Collect pprof CPU profiles from different test phases (idle, scale-up, steady state).
Compare them. If the WatchServer or LIST handler shows increased time spent in Go
runtime functions (`selectgo`, `gopark`, `runtime.mcall`, `runtime.schedule`) during
high-latency phases, that points to Go runtime contention.

If pprof suggests runtime contention, check the following metrics to confirm:

Go scheduler P99 latency:
```promql
histogram_quantile(0.99, rate(go_sched_latencies_seconds_bucket{job="master"}[2m]))
```

Mutex wait time rate:
```promql
rate(go_sync_mutex_wait_total_seconds_total{job="master"}[2m])
```

Running vs runnable goroutines:
```promql
go_sched_goroutines_goroutines{job="master", state="running"}
go_sched_goroutines_goroutines{job="master", state="runnable"}
```

Total goroutine count:
```promql
go_goroutines{job="master"}
```

CPU utilization:
```promql
rate(process_cpu_seconds_total{job="master"}[1m])
```

Compare running goroutines against GOMAXPROCS (typically 96 cores in the 5000-node test).
If running goroutines ≈ GOMAXPROCS and runnable count is high, the Go scheduler cannot
keep up — goroutines are ready to run but have no CPU to run on.

### Step 6: Correlate with cluster activity timeline

The test runs through distinct phases that are visible as patterns in the latency and
event-rate graphs. Identify the phases by looking at the graphs from Steps 1-2 and
cross-referencing with the test code (the ClusterLoader2 test config defines the
sequence of operations, timing, and pod counts). Typical phases:

1. **Idle** — few pods, low event rate, baseline latency
2. **Scale-up** — creating ~165K pods, high event churn, peak latency
3. **Steady state** — all pods running, event rate drops
4. **Scale-down** — deleting pods, second churn spike

The exact phase boundaries and durations depend on the test configuration — read the
test code to confirm rather than guessing from timestamps alone.

The key question is whether latency is worse only during specific phases (e.g., scale-up)
or consistently bad across all phases. Phase-correlated latency points to churn-driven
contention — the apiserver struggles only when event rate is high. Latency that is
uniformly bad regardless of phase suggests a structural problem (misconfiguration,
resource limits, baseline overhead) rather than a load-dependent one.

### Step 7: Check APF throttling

- `apiserver_flowcontrol_rejected_requests_total` — any non-zero rate means requests are rejected
- `apiserver_flowcontrol_dispatched_requests_total` — distribution by priority level
- `apiserver_flowcontrol_nominal_limit_seats` — configured concurrency per priority level

If WATCHLIST requests land in a priority level with low concurrency, they may be queued.
Check which FlowSchema matches WATCHLIST requests.

### Step 8: Check watch cache health

- `apiserver_watch_cache_terminated_watchers_total` — watchers closed for being too slow.
  Increasing count causes client reconnection storms.
- `apiserver_watch_cache_read_wait_seconds` — time waiting for cache to become fresh.
  High values mean the cache is falling behind.
- `apiserver_watch_cache_capacity` — ring buffer capacity. If small relative to event rate,
  watchers with old resource versions cannot be served from cache.
- `apiserver_watch_cache_events_dispatched_total` / `apiserver_watch_cache_events_received_total` —
  ratio indicates fan-out factor (dispatched >> received means many watchers multiplying work)
- `apiserver_cache_list_fetched_objects_total` / `apiserver_cache_list_returned_objects_total` —
  ratio indicates filtering efficiency

### Step 9: Analyze pprof profiles

**CPU profile** (`/debug/pprof/profile`):
- Hot paths in `dispatchEvent`, `nonblockingAdd`, `add` (cache_watcher.go)
- `CacheEncode` vs fresh `Encode` time
- `selectgo` and `gopark` time = goroutine scheduling overhead
- `runtime.mcall`, `runtime.schedule` = scheduler contention

**Goroutine profile** (`/debug/pprof/goroutine`):
- Count goroutines blocked in `select` in the watch handler event loop
- Count goroutines in `dispatchEvent` or `add`

**Execution trace** (`/debug/pprof/trace`):
- Use `go tool trace` to visualize goroutine scheduling latency
- Look for long periods where goroutines are runnable but not running

### Step 10: Formulate hypothesis

Based on findings, match to a root cause pattern below and suggest next steps.

## Root Cause Patterns

### Pattern 1: Goroutine scheduling contention

**Signature**: Running goroutines ≈ GOMAXPROCS (96). High runnable count.
Occurs during scale-up/down churn phases.

**Mechanism**: `dispatchEvent` (cacher.go:978) fans out to thousands of watchers via
`nonblockingAdd` then `add` for blocked ones. When event rate is high and watcher count
is large, the Go scheduler cannot keep up. Per-event cost increases 3-6x vs idle.

**Observed data points** (from prior investigation):
- Idle (10K pods): ~0.170ms/event, Running=12, P99 sched latency ≈0.2ms
- Scale-up (160K pods): ~0.625ms/event, Running=95-96, P99 sched latency ≈90-100ms

**Next steps**: Examine pprof for `selectgo`/`gopark` dominance. Consider watch event
batching, reducing watcher count, or serialization caching improvements.

### Pattern 2: Slow watcher termination

**Signature**: `apiserver_watch_cache_terminated_watchers_total` increasing.

**Mechanism**: In cache_watcher.go, watchers that cannot consume events within the
dispatch timeout budget are terminated. Terminated watchers reconnect, creating more load.

**Next steps**: Identify which resources have terminated watchers. Check client-side
logs for reconnection storms. Consider increasing channel buffer size.

### Pattern 3: APF throttling

**Signature**: `apiserver_flowcontrol_rejected_requests_total` rate > 0.
High `apiserver_request_duration_seconds` with APF queue time dominating.

**Mechanism**: WATCHLIST requests queued or rejected when their priority level is saturated.

**Next steps**: Check which priority level handles WATCHLIST requests. Consider tuning
APF configuration or exempting WATCHLIST from flow control.

### Pattern 4: Watch cache capacity issues

**Signature**: `apiserver_watch_cache_capacity` small relative to event rate.
`apiserver_watch_cache_read_wait_seconds` high. Frequent re-initializations.

**Mechanism**: Ring buffer too small means watchers with older resource versions cannot
be served from cache. They must re-list, adding load.

**Next steps**: Check `--watch-cache-sizes` flag. Increase capacity for high-churn resources.

### Pattern 5: Large event serialization cost

**Signature**: `apiserver_watch_events_sizes` skewed toward large buckets.
CPU profile shows hot path in `Encode` or `CacheEncode`.

**Mechanism**: Large objects (ConfigMaps, Secrets, CRDs) cause high per-event serialization
cost, multiplied by watcher count. `CachingObject` avoids re-serialization across watchers
using the same format, but mixed protobuf/JSON clients negate this.

**Next steps**: Identify large objects. Consider server-side field selection or object trim.

## Metrics Reference

### Server-side watch/list metrics

| Metric | Type | Key Labels |
|--------|------|------------|
| `apiserver_watch_list_duration_seconds` | Histogram | group, version, resource, scope |
| `apiserver_request_duration_seconds` | Histogram | verb, group, version, resource, scope |
| `apiserver_request_sli_duration_seconds` | Histogram | verb, group, version, resource, scope |
| `apiserver_watch_events_total` | Counter | group, version, resource |
| `apiserver_watch_events_sizes` | Histogram | group, version, resource |
| `apiserver_longrunning_requests` | Gauge | verb, group, version, resource, scope |

### Watch cache metrics

| Metric | Type | Key Labels |
|--------|------|------------|
| `apiserver_watch_cache_events_received_total` | Counter | group, resource |
| `apiserver_watch_cache_events_dispatched_total` | Counter | group, resource |
| `apiserver_watch_cache_terminated_watchers_total` | Counter | group, resource |
| `apiserver_watch_cache_read_wait_seconds` | Histogram | group, resource |
| `apiserver_watch_cache_capacity` | Gauge | group, resource |
| `apiserver_watch_cache_initializations_total` | Counter | group, resource |
| `apiserver_cache_list_total` | Counter | group, resource, index |
| `apiserver_cache_list_fetched_objects_total` | Counter | group, resource, index |
| `apiserver_cache_list_returned_objects_total` | Counter | group, resource |

### APF metrics

| Metric | Type | Key Labels |
|--------|------|------------|
| `apiserver_flowcontrol_dispatched_requests_total` | Counter | priority_level, flow_schema |
| `apiserver_flowcontrol_rejected_requests_total` | Counter | priority_level, flow_schema, reason |
| `apiserver_flowcontrol_nominal_limit_seats` | Gauge | priority_level |

### Runtime metrics

| Metric | Type | Notes |
|--------|------|-------|
| `go_goroutines` | Gauge | Total goroutine count |
| `go_sched_goroutines_goroutines` | Gauge | Label `state` = runnable, running |
| `go_sched_latencies_seconds` | Histogram | Go scheduler latency, high P99 = scheduling delay |
| `go_sync_mutex_wait_total_seconds_total` | Counter | Total time waiting on mutexes |
| `process_cpu_seconds_total` | Counter | Rate gives CPU utilization |

## Code Pointers

| Component | File | Key locations |
|-----------|------|---------------|
| Event dispatch | `staging/src/k8s.io/apiserver/pkg/storage/cacher/cacher.go` | `dispatchEvents()`:886, `dispatchEvent()`:978 |
| Cache watcher | `staging/src/k8s.io/apiserver/pkg/storage/cacher/cache_watcher.go` | `nonblockingAdd()`, `add()`, termination logic |
| Watch handler | `staging/src/k8s.io/apiserver/pkg/endpoints/handlers/watch.go` | Event loop, latency recording |
| List/WatchList | `staging/src/k8s.io/apiserver/pkg/endpoints/handlers/get.go` | `ListResource()`:169 |
| Server metrics | `staging/src/k8s.io/apiserver/pkg/endpoints/metrics/metrics.go` | `watch_list_duration_seconds`:289 |
| Cache metrics | `staging/src/k8s.io/apiserver/pkg/storage/cacher/metrics/metrics.go` | All watch cache counters and gauges |

## Comparing Two Runs

To compare two test runs (e.g., before/after a patch, or to find when a regression
started), invoke the skill with two GCS URLs:

```
/debug-watchlist-latency <baseline-url> <comparison-url>
```

### Setup

Download artifacts from both runs into separate directories:

```bash
mkdir -p /tmp/watchlist-debug/baseline /tmp/watchlist-debug/comparison
```

Run two local Prometheus instances on different ports, each pointing at its own snapshot:

```bash
# Terminal 1: baseline on port 9090
prometheus --storage.tsdb.path=baseline/prometheus-metrics --web.listen-address=:9090
# Terminal 2: comparison on port 9091
prometheus --storage.tsdb.path=comparison/prometheus-metrics --web.listen-address=:9091
```

### What to compare

1. **Client-side latency graphs** — overlay the watchlist pod latency plots from both
   runs on the same chart. Look for shifts in latency across all phases or only specific
   phases.

2. **Server-side SLI** — run the same PromQL queries against both Prometheus instances
   and compare. For example:
   ```
   # Baseline (port 9090)
   http://localhost:9090/graph?g0.expr=histogram_quantile(0.99,+rate(apiserver_watch_list_duration_seconds_bucket{job="master",+endpoint="apiserver",+resource="pods"}[2m]))&g0.tab=0
   # Comparison (port 9091)
   http://localhost:9091/graph?g0.expr=histogram_quantile(0.99,+rate(apiserver_watch_list_duration_seconds_bucket{job="master",+endpoint="apiserver",+resource="pods"}[2m]))&g0.tab=0
   ```

3. **Go runtime metrics** — compare scheduler latency, goroutine counts, and mutex
   wait times between the two runs using the same queries on both ports.

4. **pprof diff** — use `go tool pprof -diff_base` to compare CPU profiles:
   ```bash
   go tool pprof -diff_base baseline/cpu.prof comparison/cpu.prof
   ```
   This highlights functions that got hotter or cooler between runs.

## Usage

### Single run analysis

```
/debug-watchlist-latency <gcs-artifact-url>
```

Walks through the full debugging workflow for one test run.

### Comparing two runs

```
/debug-watchlist-latency <baseline-url> <comparison-url>
```

Downloads both runs and produces side-by-side comparisons of client-side latency,
server-side metrics, and pprof profiles.

### Auto-invocation

Claude will also invoke this skill automatically when it detects relevant context in the
conversation — mentions of watchlist latency, scale-performance-5000, or GCS artifact
URLs from scale test jobs.

## Tips

- Compare worker-node vs master-node pod latency first (see Test Topology section above).
  This is the fastest way to rule out networking.
- Multiple apiserver instances run in the 5000-node test. Check each instance separately;
  load distribution may be uneven.
- Event volume during scale-up can reach 500K+ events in a few minutes for pods alone.
- `CachingObject` in cacher.go avoids re-serialization across watchers using the same format,
  but mixed protobuf/JSON watchers negate this optimization.
- Per-event cost is a useful derived metric: total WATCHLIST time / event count.
  Idle baseline ~0.17ms/event; >0.5ms/event indicates contention.
