# PerfDash Phase 3 — Grafana on Thanos TSDB & Thanos Query

This directory contains the Phase 3 Grafana provisioning, dashboards, and local stack launcher for PerfDash on top of the Phase 2 Thanos TSDB + Thanos Query backend (`perfdash_metric`).

## Architecture

```
GCS logs bucket ──> perfdash (--thanos-mode=true) ──> Thanos TSDB blocks (/prometheus)
                         │                                      │
                         │                              thanos store (:10901)
                         │                                      │
                         └──────> legacy UI (:8080) <── thanos query (:10902) ──> Grafana (:3000)
```

1. **Thanos TSDB Block Writer (`ThanosIngester`)**: `perfdash --thanos-mode=true` parses Prow build artifacts from GCS and writes each build into an atomic Prometheus TSDB block under `--thanos-tsdb-dir` with Thanos external labels (`job`, `prow_job`, `build`).
2. **Thanos Store & Thanos Query**: `thanos store` serves the on-disk TSDB blocks over gRPC (`127.0.0.1:10901`), and `thanos query` exposes a stateless Prometheus-compatible HTTP API (`0.0.0.0:10902`).
3. **Grafana (`grafana/grafana:6.2.0`)**: Queries `perfdash`'s Prometheus metadata proxy (`http://127.0.0.1:8080/prometheus`, backed by `thanos-query` on `http://127.0.0.1:10902`) using the provisioned `Thanos` datasource and serves:
   - `dashboards/perfdash.json` (`uid: perfdash-grafana`): Single-chart PerfDash explorer with cascading `$job`, `$category`, `$metric`, `$percentile`, and dynamic item label filters (`$Resource`, `$Verb`, `$Scope`, `$Subresource`, `$PodName`, `$Operation`, `$TestName`, etc.), plotting cross-build percentile curves across runs.
   - `dashboards/regime-5k.json` (`uid: regime-5k-gce`): 5,000-node scale failure-anatomy and SLO regime board.

## Metric Schema (`perfdash_metric`)

| Label | Description | Example |
|---|---|---|
| `job` | `perfDashPrefix` configured on the Prow periodic job | `gce-5000Nodes`, `benchmark-write-throughput` |
| `prow_job` | Upstream Prow GCS job name | `ci-kubernetes-e2e-gce-scale-performance-5000` |
| `category` | PerfDash metric category | `APIServer`, `E2E`, `Scheduler`, `GenericMeasurements` |
| `metric` | PerfDash metric name | `LoadResponsiveness_PrometheusSimple`, `Latency` |
| `build` | Prow build ID string | `2103530432380276736` |
| `percentile` | Stream / percentile key from `DataItem.Data` | `Perc50`, `Perc90`, `Perc99`, `PutThroughput` |
| `unit` | Measurement unit from `DataItem.Unit` | `ms`, `s`, `B`, `MiB`, `cores`, `ops/s` |
| Dynamic labels | Item labels from `DataItem.Labels` (empty string stored as `__perfdash_empty__`) | `Resource`, `Subresource`, `Verb`, `Scope`, `PodName`, `Operation`, `TestName` |

## Kubernetes Deployment (`perfdash/deployment.yaml`)

Create or update the dashboard ConfigMap from the checked-in dashboard JSON files, then apply the deployment and service manifests:

```bash
kubectl -n perfdash create configmap grafana-dashboards \
  --from-file=perfdash.json=perfdash/grafana/dashboards/perfdash.json \
  --from-file=regime-5k.json=perfdash/grafana/dashboards/regime-5k.json \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f perfdash/deployment.yaml
kubectl apply -f perfdash/perfdash-service.yaml
```

## Local Stack Launcher

Run `./grafana/run-local.sh` from the `perfdash/` directory (or `docker compose -f grafana/docker-compose.yaml up -d`) to ingest Prow builds into `/tmp/perfdash-phase3/prometheus` and launch `thanos store`, `thanos query` (`:10902`), Grafana (`:3000`), and legacy `perfdash` (`:8080`) side-by-side.
