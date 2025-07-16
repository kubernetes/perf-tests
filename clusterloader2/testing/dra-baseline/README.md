(8000 m – 80 m) / 8 ≈ 990 m CPU
```

##### 2  Run the test

```bash
# Ensure a Prometheus stack is running so metric-based measurements succeed.

./run-e2e.sh cluster-loader2 \
  --provider=kind \
  --kubeconfig=$HOME/.kube/config \
  --report-dir=/tmp/clusterloader2-results \
  --testconfig=testing/dra-baseline/config.yaml \
  --enable-prometheus-server=true \
  --nodes=1        # adjust to match your cluster size
```

##### What the test does

1. Calculates per-pod CPU from node capacity and `CL2_PODS_PER_NODE`.
2. Fills each node to ~90 % CPU utilisation with long-running Jobs.
3. Waits until all fill pods are running, then gathers startup & scheduler metrics.
4. Resets metrics and runs short-lived Jobs (churn) that consume the remaining capacity.
5. Gathers the same metrics for the churn phase.

Collected measurements include PodStartupLatency and Prometheus-based scheduler metrics, allowing direct comparison to the DRA test (`testing/dra/config.yaml`).
```

This mirrors the structure and tone of `testing/dra/README.md` while documenting the CPU-only baseline test and its new tunable parameters.

### Usage

Follow the **Getting Started** guide at `clusterloader2/docs/GETTING_STARTED.md`
to bring up a kind cluster suitable for ClusterLoader² tests.

#### Steady-State CPU Baseline Test

This scenario saturates each worker node to ≈ 90 % of its *effective* CPU
capacity with long-running pods and then measures scheduler performance while
continuously creating short-lived pods that consume the remaining 10 %.

Unlike the original `testing/dra/` test, **no Device Resource Allocation
(ResourceClaims) are used**—each pod simply requests CPU and memory.  
This provides a clean baseline for comparing DRA overhead.

---

##### 1  Environment variables

```bash
export CL2_MODE=Indexed                # Job completion mode (Indexed/NonIndexed)
export CL2_NODES_PER_NAMESPACE=1       # 1 namespace per node
export CL2_PODS_PER_NODE=8             # target pods per node
export CL2_NODE_AVAILABLE_MILLICORES=8000   # node allocatable CPU
export CL2_SYSTEM_USED_MILLICORES=80        # CPU already used by system pods
export CL2_FILL_PERCENTAGE=90          # % of capacity for long-running pods
export CL2_LOAD_TEST_THROUGHPUT=20     # QPS for the fast fill phase
export CL2_STEADY_STATE_QPS=5          # QPS for steady-state churn
export CL2_LONG_JOB_RUNNING_TIME=1h    # runtime of long-running pods
export CL2_JOB_RUNNING_TIME=30s        # runtime of short-lived pods
export CL2_POD_MEMORY=128Mi            # memory request per pod
```

With the defaults above, each pod will request 