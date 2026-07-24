---
name: parse-cl2-metrics
description: Identifies which ClusterLoader2 artifacts to download and provides the JSON paths/commands to parse the resulting local files (e.g. APIResponsiveness, PodStartupLatency).
allowed-tools: Bash
---

# Goal
Provide deterministic rules for identifying *what* ClusterLoader2 artifacts an agent should request the `download-ci-artifacts` skill to fetch, and how to parse those files once they are downloaded locally.

# Instructions

## 1. Interaction with Downloader Skill
* **DO** use this skill to determine the exact upstream GCS file paths or patterns needed for your investigation.
* **DO** explicitly instruct the `download-ci-artifacts` skill to fetch these specific files. (e.g., "Invoke the `download-ci-artifacts` skill to download `artifacts/junit.xml` and `artifacts/metrics/APIResponsivenessPrometheus_load_overall.json`").
* **DO NOT** attempt to download the files yourself. Always delegate to the `download-ci-artifacts` skill.

## 2. Establish Ground Truth (`junit.xml`)
* **Target Artifact:** `artifacts/junit.xml` or `artifacts/junit_runner.xml`
* **Parsing Rule:** Once downloaded locally by the downloader skill, use fast text-processing tools like `grep` to extract `<testsuite>` and `<failure>` XML tags.
* **DO NOT** rely on the exit code of `build-log.txt` (which often reflects infrastructure teardown failures rather than the core test workload).

## 3. Navigate Metric Topology
When asked to evaluate SLO breaches or performance metrics, request the structured JSON summaries located in the upstream `artifacts/metrics/` directory. Once downloaded:
* **API Responsiveness:** Parse `APIResponsivenessPrometheus_load_overall.json` or `APIResponsivenessPrometheus_simple_load*.json` using `jq`. Extract the `Perc50`, `Perc90`, `Perc99`, `Count`, and `SlowCount` values.
* **Pod Startup:** To evaluate pod startup regressions, parse `PodStartupLatency_*.json` using `jq`.
* **Resource Usage:** To evaluate node-level CPU/Memory consumption, parse `ResourceUsageSummary_*.json` using `jq`.

## 4. Handle Cumulative Prometheus Snapshots
* **Target Artifact:** `artifacts/metrics/MetricsForE2E_*.json`
* **Parsing Rule:** Understand that these contain "instant vector snapshots" (cumulative counters at the end of the test). Use `jq` to compare absolute lifetime totals (e.g., total garbage collection CPU seconds vs. total process CPU seconds).
* **DO NOT** attempt to use `MetricsForE2E_*.json` to prove what happened during a specific, isolated temporal window (e.g., a 30-second spike). 

## 5. Handle Control Plane Logs (Warning)
* **Target Artifact:** `artifacts/control-plane-*/kube-apiserver.log` or `etcd.log`
* **WARNING:** These logs are often 10GB+. If you instruct the `download-ci-artifacts` skill to fetch them, ensure you have sufficient local disk space, as that skill uses `gcloud storage cp` to download the entire file locally.
* **Parsing Rule:** Once downloaded, use `grep -E "LEVEL=ERROR|context deadline exceeded|HTTP 429|panic"` to extract anomalies.

## 6. Report Anomalies
* **DO** output the exact parsed metric payload or error signature. 
* **DO** include the explicit file path that the data was extracted from so the user can verify the finding.
