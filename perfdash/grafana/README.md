# PerfDash Grafana Provisioning & Dashboards

This directory contains the Grafana provisioning, dashboard definitions, and Docker Compose configuration for running Grafana on top of the PerfDash Thanos TSDB and Thanos Query backend (`perfdash_metric`).

## Architecture

```
GCS logs bucket ──> perfdash (--thanos-mode=true) ──> Thanos TSDB blocks (/prometheus)
                         │                                      │
                         │                              thanos store (:10901)
                         │                                      │
                         └──────> legacy UI (:8080) <── thanos query (:10902) ──> Grafana (:3000)
```

1. **Thanos TSDB Block Writer (`ThanosIngester`)**: `perfdash --thanos-mode=true` parses Prow build artifacts from GCS and writes each build into a Prometheus TSDB block under `--thanos-tsdb-dir` with Thanos external labels (`job`, `prow_job`).
2. **Thanos Store & Thanos Query**: `thanos store` serves the on-disk TSDB blocks over gRPC (`127.0.0.1:10901`), and `thanos query` exposes a stateless Prometheus-compatible HTTP API (`0.0.0.0:10902`).
3. **Grafana (`grafana/grafana:6.2.0`)**: Queries `perfdash`'s Prometheus metadata proxy (`http://127.0.0.1:8080/prometheus`, backed by `thanos-query` on `http://127.0.0.1:10902`) using the provisioned `Thanos` datasource and serves `dashboards/perfdash.json` (`uid: perfdash-grafana`) with cascading `$job`, `$category`, `$metric`, `$percentile`, and dynamic item label filters.

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
  --from-file=perfdash/grafana/dashboards/ \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -f perfdash/deployment.yaml
kubectl apply -f perfdash/perfdash-service.yaml
```

## Local Stack

Run `docker compose -f grafana/docker-compose.yaml up -d` from the `perfdash/` directory alongside `./perfdash --www --thanos-mode=true --thanos-tsdb-dir=/tmp/thanos/prometheus` to launch `thanos store`, `thanos query` (`:10902`), and Grafana (`:3000`) locally.
