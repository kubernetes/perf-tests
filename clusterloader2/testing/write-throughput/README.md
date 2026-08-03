# Write Throughput & Background Watch Benchmark

This ClusterLoader2 test scenario evaluates Kubernetes `kube-apiserver` write throughput and responsiveness under heavy concurrent Pod mutations (`PATCH`) while background watches are continuously streaming `Pod` events.

## Running Tests Locally with Kind

**Prerequisites:**

* **Hardware:** To ensure smooth operation, it's recommended to have at least 4 CPU cores and 16GB of RAM free.
* **Docker:** Required for building and loading the `request-benchmark` container image locally.
* **Kind:** Install Kind if you haven't already. (See: [https://kind.sigs.k8s.io/](https://kind.sigs.k8s.io/))

**Steps:**

1. **Create the Kind Cluster:**
   * Execute the following command:
       ```bash
       make cluster
       ```
2. **Build & Load Local `request-benchmark` Image:**
   * Build the image and load it into the Kind cluster:
       ```bash
       make load-image
       ```
3. **Run the Test:**
   * Execute the following command:
       ```bash
       make test
       ```

4. **Check results:**
   * See the `report` directory for API responsiveness histograms and resource usage metrics.
5. **Delete cluster before running another test:**
   * Execute the following command:
       ```bash
       make clean
       ```
