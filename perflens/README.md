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

---

## Normalized time

Ingest re-bases every sample of a run onto a common anchor of **2000-01-01T00:00:00Z**,
so the x-axis reads as elapsed time since run start instead of wall clock. Midnight is
deliberate: Grafana has no duration axis mode, so an axis labelled `00:00`, `00:30`,
`01:00` reads as elapsed time for free.

The offset is computed once per run, from the earliest `minTime` across its Prometheus
snapshot blocks, and every block of the run shifts by the same amount. Only the origin
moves, values and durations are untouched.

Consequences:

- `$run` is a comparison dimension, not a time selector. One time range works for every
  run, and the variable is multi-select so picking two runs overlays them.
- Dashboard time ranges are absolute and anchored to the constant, `2000-01-01T00:00:00Z`
  to `2000-01-01T02:00:00Z`. Never make them relative, `now-2h` points at today. Four
  measured runs span 88 to 111 minutes, so 2h fits them with room to spare. A longer run is
  truncated at the right edge with nothing to say data is missing, so widen the range by
  hand if a run overruns.
- The SLO block used to be stamped at ingest time, far from the metrics block of the same
  run. It now carries the run's offset and holds constant across the run's window.

Recover the original wall clock start from `perflens_run_start_timestamp_seconds`:

```bash
curl -s -G localhost:9090/api/v1/query \
  --data-urlencode 'query=perflens_run_start_timestamp_seconds' \
  --data-urlencode 'time=2000-01-01T01:00:00Z'
```

Normalization is on by default because the shipped dashboards assume the anchor window.
Pass `-normalize-time=false` to keep original wall clock timestamps.
