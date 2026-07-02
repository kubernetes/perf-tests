---
name: establish-perf-baseline
description: Provides the workflow to find a "Last Known Good" (LKG) test run and calculate metric deltas to evaluate if an anomaly strongly indicates a genuine regression.
allowed-tools: Bash
---

# Goal
Provide the procedural knowledge required to quantify a performance regression (e.g., an SLO breach) by establishing a baseline from a recent, successful test run.

# Instructions

Absolute metric numbers (e.g., "Latency was 45 seconds" or "The API server processed 600 requests") are often insufficient in isolation. Before an agent can confidently classify a failure as a `CODE_REGRESSION`, it **MUST** establish a baseline.

## 1. Locate the "Last Known Good" (LKG) Run
To establish a baseline, you must find a recent run of the exact same test suite that completed successfully.
* **DO** check the GCS bucket history for the specific Prow job (e.g., `gs://kubernetes-ci-logs/logs/ci-kubernetes-e2e-gce-scale-performance-5000/`).
* **DO** inspect the `finished.json` of previous Build IDs.
* **DO** continue scanning backwards in time until you find a build where the `finished.json` payload indicates `"result": "SUCCESS"` and `"passed": true`. This is your LKG baseline.

## 2. Extract Baseline Metrics
Once the LKG build is identified, invoke the `download-ci-artifacts` and `parse-cl2-metrics` skills to retrieve the exact same metric payload that failed in the current run.
* *Example:* If the current run failed because `APIResponsivenessPrometheus_load_overall.json` showed a P99 latency of 41s for `LIST pods`, you MUST download `APIResponsivenessPrometheus_load_overall.json` from the LKG build and parse the `LIST pods` metrics.

## 3. Calculate the Delta (The Contextual Comparison)
Compare the metric from the failed run against the metric from the LKG baseline.
* **DO** explicitly calculate the delta (e.g., percentage increase, absolute difference, or multiplier).
* *Example Calculation:* "The LKG baseline shows a P99 latency of 5s and a request Count of 400. The failed run shows a P99 latency of 41s (an 8x degradation) and a request Count of 581 (a ~45% surge in volume)."

## 4. Evaluate Anomaly vs. Variance
Determine if the delta strongly indicates a genuine regression or expected variance.
* **DO NOT** flag minor variances (e.g., a 5% increase in latency) as the primary cause of a catastrophic test failure without further corroboration. 
* **DO** flag massive surges (e.g., 2x+ latency increases, >20% surges in raw request counts, or the sudden appearance of 10+ `SlowCount` violations) as highly probable regressions.
* **DO NOT** use categorical language (e.g., "this mathematically proves"). Use circumspect phrasing (e.g., "this strongly suggests a regression").

## 5. Report Baseline Delta
Whenever reporting a performance anomaly in a final triage journal or bug report, you **MUST** include the baseline comparison. 
* **DO** state the LKG Build ID used for the baseline.
* **DO** state the calculated delta that illustrates the regression.
* **DO NOT** use absolute claims like "Latency was terribly high" without appending "...compared to the baseline of X."
