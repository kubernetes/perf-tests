# PerfLens - Kubernetes Scalability Observability

**PerfLens** provides a minimal, native Go observability stack and single pane of glass for visualizing Kubernetes scale test results.

---

## Quickstart

1. **Start the PerfLens stack (Grafana + Thanos Store + Thanos Query)**:
   ```bash
   ./bin/perflens up
   ```

2. **Ingest Prow scale runs by Build ID**:
   ```bash
   ./bin/perflens ingest 2068741058296025088  # Ingest run
   ```

3. **View the SLO Verification Landing Page**:
   - Open Grafana: [http://localhost:3000/d/perflens-slo-landing](http://localhost:3000/d/perflens-slo-landing)
   - Open Thanos Query: [http://localhost:9090](http://localhost:9090)
