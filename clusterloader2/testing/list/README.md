# List load test

List load test perform the following steps:
- Create configmaps.
  - The namespaces used here are managed by CL2.
  - The size and number of configmaps can be specified using `CL2_LIST_CONFIG_MAP_BYTES` and `CL2_LIST_CONFIG_MAP_NUMBER`.
- Create RBAC rules to allow lister pods to access these configmaps
- Create lister pods using deployment in a separate namespace to list configmaps and secrets.
  - The namespace is created using `namespace.yaml` as a template, but its lifecycle is managed by CL2.
  - The number of replicas for the lister pods can be specified using `CL2_LIST_BENCHMARK_PODS`.
  - Each lister pod will maintain `CL2_LIST_CONCURRENCY` number of inflight list requests.
- Measurement uses [`APIResponsivenessPrometheusSimple`](https://github.com/kubernetes/perf-tests/blob/master/clusterloader2/README.md) to meansure API latency for list configmaps and secrets calls.

The lister pods leverage https://github.com/kubernetes/perf-tests/tree/master/util-images/request-benchmark to create in-cluster list load.

## Running Tests Locally with Kind

**Prerequisites:**

* **Hardware:** To ensure smooth operation, it's recommended to have at least 4 CPU cores and 16GB of RAM free.
* **Kind:** Install Kind if you haven't already. (See: [https://kind.sigs.k8s.io/](https://kind.sigs.k8s.io/))

**Steps:**

1.  **Create the Kind Cluster:**
    * Execute the following command:
        ```bash
        kind create cluster --config ./kind.yaml
        ```
2.  **Export Kubeconfig:**
    * Run:
        ```bash
        kind export kubeconfig
        ```
3.  **Wait for Nodes:**
    * Allow approximately 30 seconds for the cluster node to become schedulable.
4.  **Run the Test:**
    * Execute the following command:
        ```bash
        export CL2_ENABLE_CONTAINER_RESOURCES_MEASUREMENT=true
        export CL2_PROMETHEUS_TOLERATE_MASTER=true
        go run ../../cmd/clusterloader.go --provider kind -v=4 \
         --testconfig ./config.yaml \
         --kubeconfig $HOME/.kube/config \
         --enable-prometheus-server=true \
         --prometheus-scrape-kube-proxy=false \
         --prometheus-apiserver-scrape-port=6443 \
         --prometheus-scrape-master-kubelets \
         --report-dir=report
        ```

5.  **Check results:**
    * See the `report` directory for report.
5.  **Delete cluster before running another test:**
    * Execute the following command:
        ```bash
        kind delete cluster
        ```

**Local Development and Testing:**

Kind's speed makes it ideal for rapid Kubernetes development and benchmarking. To build and test local changes:

1.  **Build a Node Image:**
    * From the `kubernetes/kubernetes` directory, run:
        ```bash
        kind build node-image
        ```
2.  **Create Cluster with Custom Image:**
    * When creating the Kind cluster, add the `--image kindest/node:latest` flag to the `kind create cluster` command:
        ```bash
        kind create cluster --config ./kind.yaml --image kindest/node:latest
        ```
