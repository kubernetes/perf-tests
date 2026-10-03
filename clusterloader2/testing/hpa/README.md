# HPA reconciliation baseline

Measures what it costs the control plane to keep a number of HorizontalPodAutoscalers reconciling in steady state.

The test exists to give [KEP-6003 (configurable HPA sync period)][kep] a measured starting point. Today every HPA reconciles on a single cluster-wide interval set by `--horizontal-pod-autoscaler-sync-period` (15s by default), and the KEP proposes letting each object pick its own. That multiplies an existing per-object cost, so the question to answer first is what that cost is now.

## What it measures

Per reconciliation the controller reads the target's `scale` subresource, queries one metrics API per metric spec, and writes HPA status whenever the recomputed status differs from the previous one. The measurements are grouped around that cost model:

- `HPAReconciliationRate` — reconciliations per second, split by outcome, plus the error and stale-sync-skip rates.
- `HPAEffectiveSyncPeriod` — the observed interval between two reconciliations of the same HPA, derived from the aggregate rate and the number of controlled objects. This is the aggregate form of the delay-ratio SLI the KEP proposes: it sits on the configured sync period while the controller keeps up and grows once it falls behind. Thresholded at `CL2_HPA_EXPECTED_SYNC_PERIOD_SECONDS * CL2_HPA_SYNC_PERIOD_TOLERANCE`.
- `HPAReconciliationDuration`, `HPAMetricComputationDuration` — percentiles, the inputs to sizing `--concurrent-horizontal-pod-autoscaler-syncs`.
- `HPAWorkqueueDepth`, `HPAWorkqueueLatency` — backlog on the HPA workqueue. The delaying queue holds an item until its delay expires before handing it to the queue proper, so these show real backlog rather than the sync period, and are the direct test of whether the queue saturates.
- `HPAAPIServerRequestRate` — scale reads, scale writes, HPA status writes and metrics API reads per second.
- `HPAControllerManagerCPU`, `HPAControllerManagerMemory`.
- `HPAEtcd*` — put throughput, backend commit and WAL fsync percentiles, and database size. Off by default, see below.
- `APIResponsivenessPrometheus` — the usual API call latency SLO, as a guard that the added load does not break it.

## Requirements

**A working `metrics.k8s.io`.** The HPAs use CPU utilization, so the cluster needs metrics-server. ClusterLoader2 does not install one — the manifests under `pkg/prometheus/manifests/exporters/metrics-server/` only add a workload and a ServiceMonitor for an existing install. GCE and GKE clusters have it as an addon; on kind you have to install it yourself with `--kubelet-insecure-tls`.

If metrics are unavailable the test fails at the "Wait for HPAs to become active" step with the list of HPAs still reporting `ScalingActive=False`, rather than producing a plausible-looking but meaningless baseline.

**Prometheus**, via `--enable-prometheus-server=true`. kube-controller-manager is scraped by default, so the HPA controller and workqueue metrics need no extra flags. etcd is not: pass `--prometheus-scrape-etcd=true` together with `CL2_HPA_ENABLE_ETCD_METRICS=true` to get the etcd measurements.

**Real kubelets.** Kubemark hollow nodes do not report real container CPU, so there is nothing for metrics-server to serve and the HPAs never become active.

## Running it

```
export CL2_HPA_COUNT=100
export CL2_HPA_STEADY_STATE_DURATION=10m

./run-e2e.sh cluster-loader2 \
  --provider=gce \
  --kubeconfig=${HOME}/.kube/config \
  --testconfig=testing/hpa/config.yaml \
  --report-dir=/tmp/clusterloader2-results \
  --enable-prometheus-server=true \
  --nodes=100
```

The load dimension is the number of HPAs, not the number of nodes. Each HPA drives a single-replica Deployment, so 1000 HPAs is roughly 1000 pods and fits on a small cluster.

## Parameters

- `CL2_HPA_COUNT` (100) — number of HPAs, rounded up to whole namespaces.
- `CL2_HPAS_PER_NAMESPACE` (50).
- `CL2_HPA_MIN_REPLICAS` (1), `CL2_HPA_MAX_REPLICAS` (1) — the default pins every HPA to one replica. The controller still runs the full metric computation and status update every cycle, but nothing scales, so pod churn does not confound the measurement. Widen the range to measure scaling behaviour instead of reconciliation cost.
- `CL2_HPA_TARGET_CPU_UTILIZATION` (50) — the pods consume half their CPU request, so the observed utilization sits on this target and the autoscaler settles rather than oscillating.
- `CL2_HPA_STEADY_STATE_DURATION` (10m) — the measurement window. It starts only once every HPA is active, so it covers steady state and not the object creation burst.
- `CL2_HPA_EXPECTED_SYNC_PERIOD_SECONDS` (15) — the sync period the cluster's controller-manager is running with. This *describes* the cluster, it does not configure it; see the limitation below.
- `CL2_HPA_SYNC_PERIOD_TOLERANCE` (1.5) — multiplier for the `HPAEffectiveSyncPeriod` threshold.
- `CL2_HPA_SYNC_PERIOD_SECONDS` (unset) — writes `spec.syncPeriodSeconds` on each HPA. Only meaningful once KEP-6003 lands; the field is pruned otherwise.
- `CL2_HPA_ENABLE_ETCD_METRICS` (false), `CL2_HPA_ENABLE_VIOLATIONS` (false).
- `CL2_HPA_CREATE_QPS` (20), `CL2_HPA_OPERATION_TIMEOUT` (15m).

## Limitations

**The global sync period cannot be varied from here.** `--horizontal-pod-autoscaler-sync-period` is a kube-controller-manager flag, so comparing 15s against a shorter period means two clusters brought up with different flags, with `CL2_HPA_EXPECTED_SYNC_PERIOD_SECONDS` set to match on each run. Once KEP-6003 lands this collapses into a single config with `CL2_HPA_SYNC_PERIOD_SECONDS`.

**Status writes are bounded by metrics-server, not by the sync period.** The HPA only writes status when the recomputed status differs, and `status.currentMetrics` can only change as fast as metrics-server refreshes (its resolution, ~15s). Reconciling faster than that does not produce proportionally more etcd writes here, so the write rate this test reports is a lower bound rather than the worst case. Measuring the worst case needs a metrics source that changes on every query — an external metrics adapter, which is also the workload KEP-6003 actually targets.

[kep]: https://github.com/kubernetes/enhancements/pull/6004
