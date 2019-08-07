#!/usr/bin/env python

# Copyright 2019 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import attr
from grafanalib import core as g
import defaults

def simple_graph(title, exprs, yAxes=None, legend="", interval="5s"):
    if not isinstance(exprs, (list, tuple)):
        exprs = [exprs]
    if legend != "" and len(exprs) != 1:
        raise ValueError("legend can be specified only for a 1-element exprs")
    return defaults.Graph(
        title=title,
        # One graph per row.
        targets=[
            g.Target(
                expr=expr, legendFormat=legend, interval=interval, intervalFactor=1
            )
            for expr in exprs
        ],
        yAxes=yAxes or g.YAxes(),
    )


CLUSTERLOADER_PANELS = [
    simple_graph(
        "Requests",
        'sum(rate(apiserver_request_count{client="clusterloader/v0.0.0 (linux/amd64) kubernetes/$Format"}[1m])) by (verb, resource)',
    ),
    simple_graph(
        "API call latency (1s thresholds)",
        'apiserver:apiserver_request_latency:histogram_quantile{quantile="0.99", verb!="LIST", verb!="WATCH", verb!="CONNECT"}',
        g.single_y_axis(format=g.SECONDS_FORMAT),
        "{{verb}} {{scope}}/{{resource}}",
    ),
    simple_graph(
        "API call latency aggregated (1s thresholds)",
        'histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{verb!~"LIST|WATCH|CONNECT|DELETECOLLECTION"}[1d])) by (le, resource, verb, scope, subresource))',
        g.single_y_axis(format=g.SECONDS_FORMAT),
        "{{verb}} {{scope}}/{{resource}}",
    ),
    simple_graph(
        "API call latency aggregated (prometheus, 1s threshold)",
        'quantile_over_time(0.99, apiserver:apiserver_request_latency:histogram_quantile{verb!~"LIST|WATCH|CONNECT|DELETECOLLECTION"}[1d])',
        g.single_y_axis(format=g.SECONDS_FORMAT),
        "{{verb}} {{scope}}/{{resource}}",
    ),
]

HEALTH_PANELS = [
    simple_graph(
        "Unhealthy nodes",
        "sum(node_collector_unhealthy_nodes_in_zone) by (zone)",
        legend="{{zone}}",
    ),
    # It's not clear which "Component restarts" shows more accurate results.
    simple_graph(
        "Component restarts",
        "sum(rate(process_start_time_seconds[1m]) > bool 0) by (job, endpoint)",
    ),
    simple_graph(
        "Component restarts 2",
        'sum(min_over_time(container_start_time_seconds{container_name!="",container_name!="POD"}[2m])) by (container_name)',
    ),
    simple_graph(
        "Active component", "sum(leader_election_master_status) by (name, instance)"
    ),
]

ETCD_PANELS = [
    simple_graph("etcd leader", "etcd_server_is_leader"),
    simple_graph(
        "etcd bytes sent",
        "irate(etcd_network_client_grpc_sent_bytes_total[1m])",
        g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
    simple_graph(
        "etcd lists rate",
        'sum(rate(etcd_request_duration_seconds_count{operation="list"}[1m])) by (type)',
        g.single_y_axis(format=g.OPS_FORMAT),
    ),
    simple_graph(
        "etcd operations rate",
        "sum(rate(etcd_request_duration_seconds_count[1m])) by (operation, type)",
        g.single_y_axis(format=g.OPS_FORMAT),
    ),
    simple_graph(
        "etcd get lease latency by instance (99th percentile)",
        'histogram_quantile(0.99, sum(rate(etcd_request_duration_seconds_bucket{operation="get", type="*coordination.Lease"}[1m])) by (le, type, instance))',
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph(
        "etcd get latency by type (99th percentile)",
        'histogram_quantile(0.99, sum(rate(etcd_request_duration_seconds_bucket{operation="get"}[1m])) by (le, type))',
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph(
        "etcd get latency by type (50th percentile)",
        'histogram_quantile(0.50, sum(rate(etcd_request_duration_seconds_bucket{operation="get"}[1m])) by (le, type))',
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph("etcd instance id", "sum(etcd_server_id) by (instance, server_id)"),
    simple_graph(
        "etcd network latency (99th percentile)",
        "histogram_quantile(0.99, sum(rate(etcd_network_peer_round_trip_time_seconds_bucket[1m])) by (le, instance, To))",
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph(
        "etcd compaction keys",
        "delta(etcd_debugging_mvcc_db_compaction_keys_total[1m])",
    ),
    simple_graph(
        "etcd compaction pause sum duration",
        "delta(etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds_sum[1m])",
        g.single_y_axis(format=g.MILLISECONDS_FORMAT),
    ),
    simple_graph(
        "etcd compaction pause num chunks",
        "delta(etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds_count[1m])",
    ),
    simple_graph(
        "etcd_disk_backend_commit_duration_seconds",
        "histogram_quantile(0.99, sum(rate(etcd_disk_backend_commit_duration_seconds[1m])) by (le, instance))",
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    defaults.Graph(
        title="etcd compaction max pause",
        points=True,
        lines=False,
        targets=[
            g.Target(
                expr="histogram_quantile(1.0, sum(rate(etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds_bucket[1m])) by (le, instance))"
            )
        ],
        yAxes=g.single_y_axis(format=g.MILLISECONDS_FORMAT),
    ),
    simple_graph("etcd objects", "sum(etcd_object_counts) by (resource, instance)"),
    simple_graph(
        "etcd db size",
        [
            "etcd_mvcc_db_total_size_in_bytes",
            "etcd_mvcc_db_total_size_in_use_in_bytes",
            "etcd_server_quota_backend_bytes",
        ],
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
]

APISERVER_PANELS = [
    simple_graph("goroutines", 'go_goroutines{job="apiserver"}'),
    simple_graph("gc rate", 'rate(go_gc_duration_seconds_count{job="apiserver"}[1m])'),
    simple_graph(
        "alloc rate",
        'rate(go_memstats_alloc_bytes_total{job="apiserver"}[1m])',
        g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
    simple_graph(
        "Number of active watches",
        "sum(apiserver_registered_watchers) by (group, kind, instance)",
    ),
    simple_graph(
        "Watch events rate",
        "sum(irate(apiserver_watch_events_total[1m])) by (version, kind, instance)",
    ),
    simple_graph(
        "(Experimental) Watch events traffic",
        "sum(irate(apiserver_watch_events_sizes_sum[1m])) by (version, kind, instance)",
        g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
    simple_graph(
        "Watch event avg size",
        "sum(rate(apiserver_watch_events_sizes_sum[1m]) / rate(apiserver_watch_events_sizes_count[1m])) by (version, kind, instance)",
    ),
    simple_graph(
        "Inflight requests",
        "sum(apiserver_current_inflight_requests) by (requestKind, instance)",
    ),
    simple_graph(
        "Request rate",
        "sum(rate(apiserver_request_count[1m])) by (verb, resource, instance)",
    ),
    simple_graph(
        "Request rate by code",
        "sum(rate(apiserver_request_count[1m])) by (code, instance)",
    ),
    simple_graph(
        "Request latency (50th percentile)",
        'apiserver:apiserver_request_latency:histogram_quantile{quantile="0.50", verb!="WATCH"}',
        g.single_y_axis(format=g.SECONDS_FORMAT),
        "{{verb}} {{scope}}/{{resource}}",
    ),
    simple_graph(
        "Request latency (99th percentile)",
        'apiserver:apiserver_request_latency:histogram_quantile{quantile="0.99", verb!="WATCH"}',
        g.single_y_axis(format=g.SECONDS_FORMAT),
        "{{verb}} {{scope}}/{{resource}}",
    ),
    simple_graph(
        '"Big" LIST requests',
        'sum(rate(apiserver_request_count{verb="LIST", resource=~"nodes|pods|services|endpoints|replicationcontrollers"}[1m])) by (resource, client)',
    ),
    simple_graph(
        "Traffic",
        'sum(rate(apiserver_response_sizes_sum{verb!="WATCH"}[1m])) by (verb, version, resource, scope, instance)',
        g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
]

VM_PANELS = [
    simple_graph(
        "fs bytes reads by container",
        "sum(rate(container_fs_reads_bytes_total[1m])) by (container_name, instance)",
        g.single_y_axis(format=g.BYTES_FORMAT),
        legend="{{container_name}} {{instance}}",
    ),
    simple_graph(
        "fs reads by container",
        "sum(rate(container_fs_reads_total[1m])) by (container_name, instance)",
        legend="{{container_name}} {{instance}}",
    ),
    simple_graph(
        "fs bytes writes by container",
        "sum(rate(container_fs_writes_bytes_total[1m])) by (container_name, instance)",
        g.single_y_axis(format=g.BYTES_FORMAT),
        legend="{{container_name}} {{instance}}",
    ),
    simple_graph(
        "fs writes by container",
        "sum(rate(container_fs_writes_total[1m])) by (container_name, instance)",
        legend="{{container_name}} {{instance}}",
    ),
    simple_graph(
        "CPU usage by container",
        [
            g.Target(
                expr='sum(rate(container_cpu_usage_seconds_total{container_name!=""}[1m])) by (container_name, instance)',
                legendFormat="{{container_name}} {{instance}}",
            ),
            g.Target(expr="machine_cpu_cores", legendFormat="limit"),
        ],
    ),
    simple_graph(
        "memory usage by container",
        [
            g.Target(
                expr='sum(container_memory_usage_bytes{container_name!=""}) by (container_name, instance)',
                legendFormat="{{container_name}} {{instance}}",
            ),
            g.Target(expr="machine_memory_bytes", legendFormat="limit"),
        ],
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    simple_graph(
        "memory working set by container",
        [
            g.Target(
                expr='sum(container_memory_working_set_bytes{container_name!=""}) by (container_name, instance)',
                legendFormat="{{container_name}} {{instance}}",
            ),
            g.Target(expr="machine_memory_bytes", legendFormat="limit"),
        ],
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    defaults.Graph(
        title="Network usage (bytes)",
        targets=[
            g.Target(
                expr='rate(container_network_transmit_bytes_total{id="/"}[1m])',
                legendFormat="{{instance}} transmit",
            ),
            g.Target(
                expr='rate(container_network_receive_bytes_total{id="/"}[1m])',
                legendFormat="{{instance}} receive",
            ),
        ],
        yAxes=g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
    defaults.Graph(
        title="Network usage (packets)",
        targets=[
            g.Target(
                expr='rate(container_network_transmit_packets_total{id="/"}[1m])',
                legendFormat="{{instance}} transmit",
            ),
            g.Target(
                expr='rate(container_network_receive_packets_total{id="/"}[1m])',
                legendFormat="{{instance}} receive",
            ),
        ],
    ),
    defaults.Graph(
        title="Network usage (avg packet size)",
        targets=[
            g.Target(
                expr='rate(container_network_transmit_bytes_total{id="/"}[1m]) / rate(container_network_transmit_packets_total{id="/"}[1m])',
                legendFormat="{{instance}} transmit",
            ),
            g.Target(
                expr='rate(container_network_receive_bytes_total{id="/"}[1m]) / rate(container_network_receive_packets_total{id="/"}[1m])',
                legendFormat="{{instance}} receive",
            ),
        ],
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    defaults.Graph(
        title="Network tcp segments",
        targets=[
            g.Target(
                expr="sum(rate(node_netstat_Tcp_InSegs[1m])) by (instance)",
                legendFormat="InSegs {{instance}}",
            ),
            g.Target(
                expr="sum(rate(node_netstat_Tcp_OutSegs[1m])) by (instance)",
                legendFormat="OutSegs {{instance}}",
            ),
            g.Target(
                expr="sum(rate(node_netstat_Tcp_RetransSegs[1m])) by (instance)",
                legendFormat="RetransSegs {{instance}}",
            ),
        ],
        yAxes=g.single_y_axis(format=g.SHORT_FORMAT, logBase=10),
    ),
]

# The final dashboard must be named 'dashboard' so that grafanalib will find it.
dashboard = g.Dashboard(
    title="Master dashboard",
    time=g.Time("now-30d", "now"),
    templating=g.Templating(
        list=[
            # Make it possible to use $source as a source.
            g.Template(name="source", type="datasource", query="prometheus")
        ]
    ),
    rows=[
        defaults.Row(title="Clusterloader", panels=CLUSTERLOADER_PANELS),
        defaults.Row(title="Overall cluster health", panels=HEALTH_PANELS),
        defaults.Row(title="etcd", panels=ETCD_PANELS),
        defaults.Row(title="kube-apiserver", panels=APISERVER_PANELS),
        defaults.Row(
            title="kube-controller-manager",
            panels=[
                simple_graph(
                    "Workqueue depths",
                    'workqueue_depth{endpoint="kube-controller-manager"}',
                    legend="{{name}}",
                )
            ],
        ),
        defaults.Row(title="Master VM", panels=VM_PANELS),
        defaults.Row(
            title="Addons",
            panels=[
                defaults.Graph(
                    title="Coredns memory",
                    dataSource="$source",
                    targets=[
                        g.Target(
                            expr='quantile(1, sum(process_resident_memory_bytes{job="kube-dns"}) by (pod))',
                            legendFormat="coredns-mem-100pctl",
                        ),
                        g.Target(
                            expr='quantile(0.99, sum(process_resident_memory_bytes{job="kube-dns"}) by (pod))',
                            legendFormat="coredns-mem-99ctl",
                        ),
                        g.Target(
                            expr='quantile(0.90, sum(process_resident_memory_bytes{job="kube-dns"}) by (pod))',
                            legendFormat="coredns-mem-90ctl",
                        ),
                        g.Target(
                            expr='quantile(0.50, sum(process_resident_memory_bytes{job="kube-dns"}) by (pod))',
                            legendFormat="coredns-mem-50ctl",
                        ),
                    ],
                    yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
                )
            ],
        ),
    ],
).auto_panel_ids()
