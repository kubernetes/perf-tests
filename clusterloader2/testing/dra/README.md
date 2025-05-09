### Usage

In order to test the workload here, use the [Getting Started] (../../docs/GETTING_STARTED.md) guide
to set up a kind cluster for the test

1. Use the following env variable
      ```
      export CL2_PODS_PER_NODE=8
      export CL2_MODE=Indexed
      export CL2_NODES_PER_NAMESPACE=1
      export CL2_LOAD_TEST_THROUGHPUT=5
      export CL2_JOB_RUNNING_TIME=10s
      ```
2. In the main perf-test directory use this command to run the tests
    ```
    ./run-e2e.sh cluster-loader2 \
    --provider=kind \
    --kubeconfig=/root/.kube/config \
    --report-dir=/tmp/clusterloader2-results \
    --testconfig=testing/dra/config.yaml \
    --install-dra-test-driver=true \
    --nodes=1
    ```
