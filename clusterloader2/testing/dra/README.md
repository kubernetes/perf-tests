### Usage

In order to test the workload here, use the [Getting Started] (../../docs/GETTING_STARTED.md) guide
to set up a kind cluster for the test

#### Steady State DRA Test

This test scenario first fills the cluster to 90% utilization with long-running pods, then measures the performance of
constantly scheduling short-lived pods at a steady rate.

1. Use the following env variables:
```
export CL2_MODE=Indexed
export CL2_NODES_PER_NAMESPACE=1
export CL2_LOAD_TEST_THROUGHPUT=20   # Fast initial fill
export CL2_STEADY_STATE_QPS=5        # Controlled rate for measurement
export CL2_JOB_RUNNING_TIME=30s      # Short-lived pods runtime
export CL2_LONG_JOB_RUNNING_TIME=1h  # Long-running pods runtime (for cluster fill)
export CL2_GPUS_PER_NODE=8           # GPUs per node
export CL2_FILL_PERCENTAGE=90        # Cluster fill percentage
```

2. Run the test with:
```
# Make sure a Prometheus stack is deployed so that metric-based measurements work.

./run-e2e.sh cluster-loader2 \
--provider=kind \
--kubeconfig=/root/.kube/config \
--report-dir=/tmp/clusterloader2-results \
--testconfig=testing/dra/config.yaml \
--enable-prometheus-server=true \
--nodes=5
```

This test will:
1. Create ResourceClaimTemplates in each namespace
2. Fill the cluster to 90% utilization with long-running pods (each using 1 GPU)
3. Measure performance while continuously creating short-lived pods at a steady rate
4. Collect metrics on pod startup latency, job lifecycle latency, and scheduler metrics
