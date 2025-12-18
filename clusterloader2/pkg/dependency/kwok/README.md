# KWOK DRA Dependency

This dependency provides fake Kubernetes nodes with Dynamic Resource Allocation (DRA) GPU resources using [KWOK (Kubernetes WithOut Kubelet)](https://kwok.sigs.k8s.io/).

## What it does

- Installs KWOK controller in `kwok-system` namespace
- Creates fake nodes with GPU resources exposed through DRA ResourceSlices
- Enables testing DRA workloads without real GPU hardware

## Configuration

Add the dependency to your ClusterLoader2 test configuration:

```yaml
# In your CL2 test config
dependencyConfigs:
- name: kwok-dra
  params:
    nodes: 4              # Number of fake nodes (default: 2)
    gpusPerNode: 16       # GPUs per node (default: 8)
    timeout: "10m"        # Setup timeout (default: 5m)
```

## Fake Resources Created

### Nodes
- **Name**: `kwok-node-0`, `kwok-node-1`, etc.
- **Resources**: 32 CPU, 256Gi memory, 110 pods
- **Labels**: `type=kwok`, `kubernetes.io/hostname=kwok-node-N`
- **Taints**: `kwok.x-k8s.io/node=fake:NoSchedule` (prevents real workloads)

### GPU Resources (DRA)
- **Driver**: `cl2-gpu.kwok.x-k8s.io`
- **API Version**: `resource.k8s.io/v1beta2`
- **ResourceSlices**: One per node with configurable GPU devices  
- **Device Names**: `gpu0`, `gpu1`, `gpu2`, etc.
- **Capacity**: Each device provides `1` GPU unit (`cl2-gpu.kwok.x-k8s.io/gpu`)
- **Device Attributes**: 
  - `gpu-type`: "kwok-gpu"
  - `memory`: "8Gi" 
  - `compute-capability`: "7.5"

## Example: Simple GPU Job

Create a job that requests fake GPUs using the provided example files:

### 1. GPU Job Template
See [`examples/kwok-gpu-job.yaml`](examples/kwok-gpu-job.yaml) - A ClusterLoader2 job template that:
- Uses the `job-type: short-lived` labels for KWOK completion simulation
- Includes proper tolerations for KWOK fake nodes
- References the GPU ResourceClaimTemplate
- Uses templating variables (`{{.Name}}`, `{{.Replicas}}`, etc.)

### 2. ResourceClaimTemplate 
See [`examples/kwok-gpu-resource-claim-template.yaml`](examples/kwok-gpu-resource-claim-template.yaml) - Defines:
- `v1beta2` ResourceClaimTemplate for requesting GPU devices
- References the `cl2-gpu.kwok.x-k8s.io` DeviceClass
- Created first in a separate step before jobs are created

## Running Tests with KWOK DRA

### Using the Main E2E Script

The `run-e2e.sh` script is the main entry point for running performance tests in the perf-tests repository.

```bash
# Basic usage from perf-tests root directory
./run-e2e.sh <tool-name> [options...]

# Run a ClusterLoader2 test with KWOK DRA dependency
./run-e2e.sh cluster-loader2 \
  --testconfig=pkg/dependency/kwok/examples/test-config.yaml \
  --provider=skeleton \
  --nodes=3 \
  --report-dir=/tmp/reports

# Quick test with different node counts
./run-e2e.sh cluster-loader2 \
  --testconfig=pkg/dependency/kwok/examples/test-config.yaml \
  --provider=skeleton \
  --nodes=5 \
  --report-dir=/tmp/reports

# View available test tools
./run-e2e.sh --help
```

### Quick Start

1. **Prerequisites**: Ensure you have a Kubernetes cluster running
2. **Environment**: Set `KUBECONFIG` or `~/.kube/config` pointing to your cluster  
3. **Run Test**: Execute the script with desired parameters
4. **Results**: Check the `--report-dir` for test results and metrics

### Available Test Tools

The `run-e2e.sh` script supports multiple performance testing tools:

- **`cluster-loader2`** - Kubernetes cluster performance and scale testing
- **`network-performance`** - Network performance benchmarks  
- **`kube-dns`** - DNS performance testing
- **`core-dns`** - CoreDNS performance testing
- **`node-local-dns`** - NodeLocalDNS performance testing

### Example ClusterLoader2 Test Config

Use the provided test configuration file:

```bash
# Copy the example config to your test directory
cp pkg/dependency/kwok/examples/test-config.yaml your-test-config.yaml

# Or reference it directly
./run-e2e.sh cluster-loader2 \
  --testconfig=pkg/dependency/kwok/examples/test-config.yaml \
  --provider=kind \
  --nodes=3 \
  --report-dir=/tmp/kwok-dra-test
```

The [`examples/test-config.yaml`](examples/test-config.yaml) includes:
- **KWOK DRA dependency** with 3 nodes and 8 GPUs per node  
- **ResourceClaimTemplate creation** step (must run before jobs)
- **10 GPU jobs** requesting fake GPU resources
- **QPS throttling** for controlled job creation

## Job Timing Configuration

To control how long simulated jobs run before completing:

```bash
# Set job duration to 10 seconds (10000ms)
export CL2_JOB_RUNNING_TIME_MS=10000

# Run your ClusterLoader2 test
./clusterloader2 --testconfig=test-config.yaml
```

This affects all jobs with `job-type: short-lived` labels running on KWOK nodes.

## Important Notes

1. **Tolerations Required**: Jobs must tolerate the `kwok.x-k8s.io/node=fake:NoSchedule` taint
2. **DeviceClass**: Uses the built-in `cl2-gpu.kwok.x-k8s.io` DeviceClass provided by the dependency
3. **Step Ordering**: ResourceClaimTemplate must be created before jobs that reference it
4. **Resource Dependencies**: Jobs depend on both DeviceClass (from dependency) and ResourceClaimTemplate (from first step)
5. **Job Labels**: Pods must have `job-type: short-lived` label for KWOK job completion simulation
6. **Job Completion**: Set `CL2_JOB_RUNNING_TIME_MS` environment variable to control simulated job duration (default: 30000ms)
7. **Device Attributes**: v1beta2 API provides rich device metadata (GPU type, memory, compute capability)  
8. **Enhanced Scheduling**: ResourceSlices include proper labels for improved resource discovery
9. **Fake Resources**: GPUs are simulated - no actual GPU operations occur  
10. **Cleanup**: The dependency automatically cleans up when tests complete

## Troubleshooting

### Common Issues

- **Nodes not ready**: Check KWOK controller logs in `kwok-system` namespace
- **Jobs not scheduling**: Verify tolerations and DeviceClass configuration
- **Timeout errors**: Increase the `timeout` parameter in dependency config
- **ResourceClaimTemplate not found**: Ensure the ResourceClaimTemplate step runs before job creation

### Testing the Setup

```bash
# Test KWOK DRA dependency with example config
./run-e2e.sh cluster-loader2 \
  --testconfig=clusterloader2/pkg/dependency/kwok/examples/test-config.yaml \
  --provider=kind \
  --nodes=3 \
  --report-dir=/tmp/kwok-test

# Check KWOK nodes are created
kubectl get nodes -l type=kwok

# Verify ResourceSlices are available  
kubectl get resourceslices

# Check DeviceClass is installed
kubectl get deviceclasses cl2-gpu.kwok.x-k8s.io
```

### Debug Mode

Enable verbose logging by setting environment variables:

```bash
export KLOG_V=2
./run-e2e.sh cluster-loader2 --testconfig=... --v=2
```

## See Also

- [KWOK Documentation](https://kwok.sigs.k8s.io/)
- [Kubernetes DRA Documentation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/)
- [ClusterLoader2 Documentation](../../../docs/)