# DRA Extended Resources Scale Test

## Overview

This test validates the performance and scalability of Kubernetes' DRA Extended Resources feature (KEP-5004). It measures how well the scheduler handles extended resource requests that are backed by Dynamic Resource Allocation (DRA), allowing applications to use familiar extended resource syntax while benefiting from DRA's dynamic allocation capabilities.

## What This Test Does

This test scenario mirrors the structure of the regular DRA test (`testing/dra/config.yaml`) but uses **extended resources syntax** instead of explicit ResourceClaims:

1. **Setup Phase**: Creates a DeviceClass with an `extendedResourceName` field that maps DRA devices to traditional extended resources
2. **Fill Phase**: Fills the cluster to 90% utilization with long-running pods that request extended resources (e.g., `example.com/gpu: 1`)
3. **Measurement Phase**: Measures performance while continuously scheduling short-lived pods at a steady rate using the same extended resource requests
4. **Metrics Collection**: Collects detailed metrics on:
   - Pod startup latency
   - Job lifecycle latency  
   - Scheduler performance metrics
   - DRA-specific metrics (PrepareResources/UnprepareResources latencies)
   - Extended resource allocation metrics

## Key Differences from Regular DRA Test

- **No ResourceClaimTemplates**: Uses DeviceClass with `extendedResourceName` instead
- **Extended Resource Syntax**: Pods request `example.com/gpu: 1` in `resources.limits` instead of using `resourceClaims`
- **Transparent DRA**: The scheduler automatically creates ResourceClaims behind the scenes
- **Backward Compatibility**: Tests that existing extended resource workloads work with DRA

## Prerequisites

1. **Feature Gate**: Ensure `DRAExtendedResource=true` is enabled on:
   - kube-apiserver
   - kube-scheduler  
   - kubelet

2. **DRA Driver**: A DRA driver must be running (installed automatically by the test)

3. **Prometheus**: Required for metric-based measurements

## Usage

### Environment Variables

```bash
export CL2_MODE=Indexed                    # Job completion mode
export CL2_NODES_PER_NAMESPACE=1           # Namespaces per node
export CL2_LOAD_TEST_THROUGHPUT=20         # Fast initial fill rate
export CL2_STEADY_STATE_QPS=5              # Controlled rate for measurement
export CL2_JOB_RUNNING_TIME=30s            # Short-lived pods runtime
export CL2_LONG_JOB_RUNNING_TIME=1h        # Long-running pods runtime
export CL2_GPUS_PER_NODE=8                 # Extended resources per node
export CL2_FILL_PERCENTAGE=90              # Cluster fill percentage
export CL2_EXTENDED_RESOURCE_NAME="example.com/gpu"  # Extended resource name
```

### Run the Test

```bash
# Make sure a Prometheus stack is deployed
./run-e2e.sh cluster-loader2 \
--provider=kind \
--kubeconfig=/root/.kube/config \
--report-dir=/tmp/clusterloader2-results \
--testconfig=testing/dra-extended-resources/config.yaml \
--enable-prometheus-server=true \
--nodes=5
```

## Test Flow

### 1. DeviceClass Creation
Creates a DeviceClass that maps DRA devices to extended resources:
```yaml
apiVersion: resource.k8s.io/v1beta2
kind: DeviceClass
metadata:
  name: gpu-extended-resource
spec:
  selectors:
  - cel:
      expression: device.driver == 'gpu.example.com' && device.attributes['gpu.example.com'].type == 'gpu'
  extendedResourceName: example.com/gpu
```

### 2. Cluster Fill (90% utilization)
- Creates long-running Jobs with pods requesting `example.com/gpu: 1`
- Each pod gets a single extended resource unit
- Scheduler automatically creates ResourceClaims behind the scenes
- Fills cluster to specified percentage (default 90%)

### 3. Steady State Churn
- Creates short-lived Jobs at a controlled rate
- Uses remaining 10% of cluster capacity
- Measures scheduler performance under steady load
- Tests both pod creation and cleanup performance

### 4. Metrics Collection
Collects comprehensive metrics including:
- **Standard Metrics**: Pod startup latency, scheduling throughput
- **DRA Metrics**: PrepareResources/UnprepareResources latencies
- **Extended Resource Metrics**: Claim creation and allocation rates
- **Comparison Data**: Allows comparison with regular DRA and baseline tests

## Key Metrics

### Pod Startup Latency
- **FastFillPodStartupLatency**: Startup time for initial fill pods
- **ChurnPodStartupLatency**: Startup time for steady-state pods
- Thresholds: p50 < 40s, p90 < 60s, p99 < 80s

### DRA Operation Latencies  
- **p99_dra_prepare_resources**: 99th percentile PrepareResources latency
- **p99_dra_unprepare_operations**: 99th percentile UnprepareResources latency
- **p99_dra_grpc_node_prepare_resources**: gRPC call latencies
- **p99_dra_grpc_node_unprepare_resources**: gRPC cleanup latencies

### Extended Resource Metrics
- **extended_resource_claims_created**: Number of auto-created ResourceClaims
- **extended_resource_allocation_attempts**: Allocation attempt rate

## Comparison with Other Tests

| Test | Resource Type | Syntax | Purpose |
|------|---------------|--------|---------|
| `dra/` | ResourceClaims | `resourceClaims` section | Test explicit DRA usage |
| `dra-baseline/` | CPU/Memory | `resources.requests` | Baseline without DRA |
| `dra-extended-resources/` | Extended Resources | `resources.limits` | Test DRA extended resources |

## Expected Behavior

1. **Transparent Operation**: Applications work without modification
2. **Automatic Claim Creation**: Scheduler creates ResourceClaims automatically
3. **DRA Driver Integration**: Same DRA driver calls as explicit ResourceClaims
4. **Performance**: Similar performance to explicit DRA with additional scheduler overhead for claim creation

## Troubleshooting

### Common Issues

1. **Feature Gate Not Enabled**
   - Error: Extended resource requests not creating ResourceClaims
   - Solution: Enable `DRAExtendedResource=true` on all components

2. **DeviceClass Missing**
   - Error: Pods stuck in Pending state
   - Solution: Verify DeviceClass exists with correct `extendedResourceName`

3. **Resource Conflicts**
   - Error: Both device plugin and DRA providing same extended resource
   - Solution: Use different extended resource names or migrate fully

4. **Driver Issues**
   - Error: PrepareResources failures
   - Solution: Check DRA driver logs and CDI device creation

### Debug Information

- **Pod Status**: Check for `ExtendedResourceClaimStatus` showing claim mappings
- **ResourceClaim Status**: Verify allocation results in auto-created claims  
- **Scheduler Logs**: Enable verbosity level 5 for extended resource processing
- **Kubelet Logs**: Enable verbosity level 3 for DRA manager operations

## Performance Expectations

### Compared to Regular DRA
- **Similar**: DRA driver operation latencies
- **Additional**: Scheduler overhead for automatic claim creation
- **Benefit**: Application compatibility without code changes

### Compared to Baseline
- **Additional**: DRA allocation and preparation overhead  
- **Additional**: ResourceClaim lifecycle management
- **Benefit**: Dynamic device allocation and advanced scheduling

## Use Cases

1. **Migration Testing**: Validate migration from device plugins to DRA
2. **Performance Validation**: Ensure extended resources don't add excessive overhead
3. **Scale Testing**: Test scheduler performance with mixed resource types
4. **Compatibility Testing**: Verify existing applications work with DRA backend

---

*This test validates the DRA Extended Resources feature introduced in Kubernetes 1.34 (KEP-5004) and measures its performance characteristics at scale.*
