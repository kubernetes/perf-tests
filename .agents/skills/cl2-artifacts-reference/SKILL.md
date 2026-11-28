---
name: cl2-artifacts-reference
description: Reference guide for ClusterLoader2 CI artifacts and their GCS paths.
---
# Goal
Describe the artifacts produced by a ClusterLoader2 scalability test run.
Use this reference to understand what each file is and where it lives
in the artifact tree.

# Reference
## 1. Artifact Directory Structure
- **Root**: `artifacts/` under the CI job run's output path.
- **Subdirectories**:
  - `control-plane-*/` — control-plane node logs.
  - `nodes-*/` — a subset of worker node logs (not all nodes).
  - `cluster-info/` — resource YAML dumps (cluster-scoped at root, namespaced under per-namespace subdirectories).
  - `addons-*/` — addons node logs.
  - `otlp/` — OpenTelemetry traces.

## 2. Test Results
- `junit.xml` — CL2 test results with pass/fail per step and measurement violation messages.
- `junit_runner.xml` — test harness results covering cluster lifecycle phases (Up, PreTestCmd, Test, Down).
