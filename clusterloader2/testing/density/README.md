# Density test

## Assumptions
- Underlying cluster should have 100+ nodes.
- Number of nodes should be divisible by ```NODES_PER_NAMESPACE``` (default 100).

## Constants
- ```MIN_LATENCY_PODS``` \
Value: 500 \
Minimal number of latency pods that will be created during the test.
- ```MIN_SATURATION_PODS_TIMEOUT``` \
Value: 180 \
Minimal timeout for saturation pods creation.

## Input parameters
The input parameters values can be changed using overrides.
- ```DENSITY_RESOURCE_CONSTRAINTS_FILE``` \
Default value: "" \
Path to the resource constraints file that is used by ```ResourceUsageSummary```.
- ```DENSITY_TEST_THROUGHPUT``` \
Default value: 20 \
Assumed throughput of pod creation actions. Pods creation/deletion phase timeouts
will be calculated based on this parameter.
- ```NODE_MODE``` \
Default value: "allnodes"
Parameter for `ResourceUsageSummary` measurement. Specifies set of nodes,
that metrics will be gather from.
- ```NODES_PER_NAMESPACE``` \
Default value: 100 \
For every ```NODES_PER_NAMESPACE``` nodes a namespace will be created.
- ```PODS_PER_NODE``` \
Default value: 30 \
Specifies how many saturation pods should be created for each node.
- ```LATENCY_POD_CPU``` \
Default value: 100 \
Requested cpu (milicores) by latency pod. It is calculated as 10% of 1 core node.
Increasing allocation of both memory and cpu by 10%
decreases the value of priority function in scheduler by one point.
This results in decreased probability of choosing the same node again.
- ```LATENCY_POD_MEMORY``` \
Default value: 400 \
Requested memory (MB) by latency pod. It is calculated as 10% of 4GB node.
- ```ENABLE_CHAOSMONKEY``` \
Default value: false \
Enables chaos monkey feature.

## Phases

### Creating saturation pods
During this phase ```PODS_PER_NODE``` pods will be created for every node.
Saturation pods are bounded by replication controller, one in each namespace.
Each re placation controller will create ```PODS_PER_NODE*NODES_PER_NAMESPACE``` pods. \
The timeout is calculated with assumption that all saturation pods will be created
with ```DENSITY_TEST_THROUGHPUT``` throughput. Additional ```MIN_SATURATION_PODS_TIMEOUT```
is added to prevent rising timeout too soon.

### Creating latency pods
During this phase ```MAX(NODES, MIN_LATENCY_PODS)``` will be created. Latency pods will
be equally divided across namespaces. Pods will be created one by one with 5qps.

### Removing latency pods
Deleting pods created in "creating latency pods" phase. As in previous phase,
pods will be deleted one by one with 5qps

### Removing saturation pods
Deleting pods created in "creating saturation pods" phase with ```DENSITY_TEST_THROUGHPUT``` qps.
Timeout for this phase is twice as long as for creation phase - kubernetes pod deletion is twice
slower than pod creation.

## SLOs
- Api calls latency \
This SLO is changed by the APIResponsiveness measurement. This measurement gathers
call data through whole test execution.
- Pod startup latency \
The data are gather by PodStartupLatency measurement. This is verified one for
"creating latency pods" phase.
