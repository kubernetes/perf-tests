---
name: scale-run-metrics
description: Reference for the Prometheus series and profiles worth querying out of a ClusterLoader2 scale run, and what each one actually measures. Use when analyzing or comparing scale runs, reading a prometheus_snapshot.tar, or deciding which series answers a question about apiserver, etcd, watch cache, informer lag or authorization behaviour.
---

# scale-run-metrics

Read [references/metrics.md](references/metrics.md). It catalogs the Prometheus series and
profiles from a scale run by area: what gets scraped, per-process and whole-machine
resource use, the request path, etcd, the watch cache, informer lag, authorization, and the
Go scheduler and lock series.

Several series are easy to misread, so each carries its caveat next to the metric name.
