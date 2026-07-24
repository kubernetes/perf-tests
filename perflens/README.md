# PerfLens - Kubernetes Scalability Observability

**PerfLens** provides a minimal observability stack and single pane of glass for visualizing Kubernetes scale test results.

---

## Quickstart

1. **Start the PerfLens stack (Grafana + Thanos Store + Thanos Query)**:
   ```bash
   ./bin/perflens up
   ```

2. **Download scale run artifacts** (accepts Build ID or Prow URL):
   ```bash
   ./bin/perflens download <BUILD_ID_OR_URL>
   ```

3. **Ingest downloaded scale run metrics into Thanos TSDB**:
   ```bash
   ./bin/perflens ingest <BUILD_ID_OR_URL>
   ```

4. **View Dashboards**:
   - **SLO Verification Landing Page**: [http://localhost:3000/d/perflens-slo-landing](http://localhost:3000/d/perflens-slo-landing)
   - **API Server Metrics**: [http://localhost:3000/d/apiserver-metrics](http://localhost:3000/d/apiserver-metrics)
   - **Thanos Query UI**: [http://localhost:9090](http://localhost:9090)

---

## Command Reference

- `./bin/perflens up`: Start local services.
- `./bin/perflens down`: Stop local services.
- `./bin/perflens status`: View container status.
- `./bin/perflens download <BUILD_ID_OR_URL>`: Download scale run artifacts from GCS into `_artifacts/runs/<build_id>/`.
- `./bin/perflens ingest <BUILD_ID_OR_URL>`: Ingest local scale run metrics from `_artifacts/runs/<build_id>/` into Thanos TSDB.
