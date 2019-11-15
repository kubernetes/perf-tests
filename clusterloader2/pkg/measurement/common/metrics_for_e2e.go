/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package common

import (
	"fmt"
	"strings"

	"k8s.io/klog"
	"k8s.io/kubernetes/test/e2e/framework/metrics"
	"k8s.io/perf-tests/clusterloader2/pkg/measurement"
	"k8s.io/perf-tests/clusterloader2/pkg/util"
)

const (
	metricsForE2EName = "MetricsForE2E"
)

var interestingApiServerMetricsLabels = []string{
	"apiserver_init_events_total",
	"apiserver_request_count",
	"apiserver_request_latencies_summary",
	"etcd_request_latencies_summary",
}

var interestingControllerManagerMetricsLabels = []string{
	//remove all outdated metrics labels and replace with new labels

	//	"apiserver_audit_event_total",
	//	"apiserver_audit_requests_rejected_total",
	//	"apiserver_client_certificate_expiration_seconds_bucket",
	//	"apiserver_client_certificate_expiration_seconds_count",
	//	"apiserver_client_certificate_expiration_seconds_sum",
	//	"apiserver_storage_data_key_generation_duration_seconds_bucket",
	//	"apiserver_storage_data_key_generation_duration_seconds_count",
	//	"apiserver_storage_data_key_generation_duration_seconds_sum",
	//	"apiserver_storage_data_key_generation_failures_total",
	//	"apiserver_storage_data_key_generation_latencies_microseconds_bucket",
	//	"apiserver_storage_data_key_generation_latencies_microseconds_count",
	//	"apiserver_storage_data_key_generation_latencies_microseconds_sum",
	//	"apiserver_storage_envelope_transformation_cache_misses_total",
	//	"attachdetach_controller_forced_detaches",
	//	"authenticated_user_requests",
	//	"authentication_attempts",
	//	"authentication_duration_seconds_bucket",
	//	"authentication_duration_seconds_count",
	//	"authentication_duration_seconds_sum",
	//	"cronjob_controller_rate_limiter_use",
	//	"daemon_controller_rate_limiter_use",
	//	"deployment_controller_rate_limiter_use",
	//	"endpoint_controller_rate_limiter_use",
	//	"gc_controller_rate_limiter_use",
	//	"get_token_count",
	//	"get_token_fail_count",
	"go_gc_duration_seconds",
	"go_gc_duration_seconds_count",
	"go_gc_duration_seconds_sum",
	//	"go_goroutines",
	//	"go_info",
	//	"go_memstats_alloc_bytes",
	//	"go_memstats_alloc_bytes_total",
	//	"go_memstats_buck_hash_sys_bytes",
	//	"go_memstats_frees_total",
	//	"go_memstats_gc_cpu_fraction",
	//	"go_memstats_gc_sys_bytes",
	//	"go_memstats_heap_alloc_bytes",
	//	"go_memstats_heap_idle_bytes",
	//	"go_memstats_heap_inuse_bytes",
	//	"go_memstats_heap_objects",
	//	"go_memstats_heap_released_bytes",
	//	"go_memstats_heap_sys_bytes",
	//	"go_memstats_last_gc_time_seconds",
	//	"go_memstats_lookups_total",
	//	"go_memstats_mallocs_total",
	//	"go_memstats_mcache_inuse_bytes",
	//	"go_memstats_mcache_sys_bytes",
	//	"go_memstats_mspan_inuse_bytes",
	//	"go_memstats_mspan_sys_bytes",
	//	"go_memstats_next_gc_bytes",
	//	"go_memstats_other_sys_bytes",
	//	"go_memstats_stack_inuse_bytes",
	//	"go_memstats_stack_sys_bytes",
	//	"go_memstats_sys_bytes",
	//	"go_threads",
	//	"job_controller_rate_limiter_use",
	//	"kubernetes_build_info",
	//	"leader_election_master_status",
	//	"namespace_controller_rate_limiter_use",
	//	"node_collector_evictions_number",
	//	"node_collector_unhealthy_nodes_in_zone",
	//	"node_collector_zone_health",
	//	"node_collector_zone_size",
	//	"node_ipam_controller_rate_limiter_use",
	//	"node_lifecycle_controller_rate_limiter_use",
	//	"persistentvolume_protection_controller_rate_limiter_use",
	//	"persistentvolumeclaim_protection_controller_rate_limiter_use",
	"process_cpu_seconds_total",
	"process_max_fds",
	"process_open_fds",
	"process_resident_memory_bytes",
	"process_start_time_seconds",
	"process_virtual_memory_bytes",
	"process_virtual_memory_max_bytes",
	//	"replication_controller_rate_limiter_use",
	//	"resource_quota_controller_rate_limiter_use",
	//	"rest_client_request_duration_seconds_bucket",
	//	"rest_client_request_duration_seconds_count",
	"rest_client_request_duration_seconds_sum",
	//	"rest_client_request_latency_seconds_bucket",
	//	"rest_client_request_latency_seconds_count",
	"rest_client_request_latency_seconds_sum",
	"rest_client_requests_total",
	//	"service_controller_rate_limiter_use",
	//	"serviceaccount_controller_rate_limiter_use",
	//	"serviceaccount_tokens_controller_rate_limiter_use",
	"workqueue_adds_total",
	//	"workqueue_depth",
	"workqueue_longest_running_processor_seconds",
	//	"workqueue_queue_duration_seconds_bucket",
	//	"workqueue_queue_duration_seconds_count",
	"workqueue_queue_duration_seconds_sum",
	"workqueue_retries_total",
	"workqueue_unfinished_work_seconds",
	//	"workqueue_work_duration_seconds_bucket",
	//	"workqueue_work_duration_seconds_count",
	"workqueue_work_duration_seconds_sum",
}

var interestingKubeletMetricsLabels = []string{
	"kubelet_container_manager_latency_microseconds",
	"kubelet_docker_errors",
	"kubelet_docker_operations_latency_microseconds",
	"kubelet_generate_pod_status_latency_microseconds",
	"kubelet_pod_start_latency_microseconds",
	"kubelet_pod_worker_latency_microseconds",
	"kubelet_pod_worker_start_latency_microseconds",
	"kubelet_sync_pods_latency_microseconds",
}

func init() {
	if err := measurement.Register(metricsForE2EName, createmetricsForE2EMeasurement); err != nil {
		klog.Fatalf("Cannot register %s: %v", metricsForE2EName, err)
	}
}

func createmetricsForE2EMeasurement() measurement.Measurement {
	return &metricsForE2EMeasurement{}
}

type metricsForE2EMeasurement struct{}

// Execute gathers and prints e2e metrics data.
func (m *metricsForE2EMeasurement) Execute(config *measurement.MeasurementConfig) ([]measurement.Summary, error) {
	provider, err := util.GetStringOrDefault(config.Params, "provider", config.ClusterFramework.GetClusterConfig().Provider)
	if err != nil {
		return nil, err
	}

	grabMetricsFromKubelets, err := util.GetBoolOrDefault(config.Params, "gatherKubeletsMetrics", false)
	if err != nil {
		return nil, err
	}
	grabMetricsFromKubelets = grabMetricsFromKubelets && strings.ToLower(provider) != "kubemark"

	grabber, err := metrics.NewMetricsGrabber(
		config.ClusterFramework.GetClientSets().GetClient(),
		nil, /*external client*/
		grabMetricsFromKubelets,
		true, /*grab metrics from scheduler*/
		true, /*grab metrics from controller manager*/
		true, /*grab metrics from apiserver*/
		false /*grab metrics from cluster autoscaler*/)
	if err != nil {
		return nil, fmt.Errorf("failed to create MetricsGrabber: %v", err)
	}
	// Grab apiserver, scheduler, controller-manager metrics and (optionally) nodes' kubelet metrics.
	received, err := grabber.Grab()
	if err != nil {
		klog.Errorf("%s: metricsGrabber failed to grab some of the metrics: %v", m, err)
	}
	filterMetrics(&received)
	content, jsonErr := util.PrettyPrintJSON(received)
	if jsonErr != nil {
		return nil, jsonErr
	}
	summary := measurement.CreateSummary(metricsForE2EName, "json", content)
	return []measurement.Summary{summary}, err
}

// Dispose cleans up after the measurement.
func (m *metricsForE2EMeasurement) Dispose() {}

// String returns string representation of this measurement.
func (*metricsForE2EMeasurement) String() string {
	return metricsForE2EName
}

func filterMetrics(m *metrics.MetricsCollection) {
	interestingApiServerMetrics := make(metrics.ApiServerMetrics)
	for _, metric := range interestingApiServerMetricsLabels {
		interestingApiServerMetrics[metric] = (*m).ApiServerMetrics[metric]
	}
	interestingControllerManagerMetrics := make(metrics.ControllerManagerMetrics)
	for _, metric := range interestingControllerManagerMetricsLabels {
		interestingControllerManagerMetrics[metric] = (*m).ControllerManagerMetrics[metric]
	}
	interestingKubeletMetrics := make(map[string]metrics.KubeletMetrics)
	for kubelet, grabbed := range (*m).KubeletMetrics {
		interestingKubeletMetrics[kubelet] = make(metrics.KubeletMetrics)
		for _, metric := range interestingKubeletMetricsLabels {
			interestingKubeletMetrics[kubelet][metric] = grabbed[metric]
		}
	}
	(*m).ApiServerMetrics = interestingApiServerMetrics
	(*m).ControllerManagerMetrics = interestingControllerManagerMetrics
	(*m).KubeletMetrics = interestingKubeletMetrics
}
