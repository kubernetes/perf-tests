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

from grafanalib import core as g

DECREASING_ORDER_TOOLTIP = g.Tooltip(sort=g.SORT_DESC)
DEFAULT_PANEL_HEIGHT = g.Pixels(300)


def simple_graph(title, exprs, yAxes=None):
    if not isinstance(exprs, (list, tuple)):
        exprs = [exprs]
    return g.Graph(
        title=title,
        dataSource="$source",
        # One graph per row.
        span=g.TOTAL_SPAN,
        targets=[g.Target(expr=expr) for expr in exprs],
        yAxes=yAxes or g.YAxes(),
        tooltip=DECREASING_ORDER_TOOLTIP,
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
    ),
    simple_graph(
        "API call latency aggregated (1s thresholds)",
        'histogram_quantile(0.99, sum(rate(apiserver_request_duration_seconds_bucket{verb!="LIST", verb!="WATCH", verb!="CONNECT"}[5d])) by (le, resource, verb, scope, subresource))',
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
]

HEALTH_PANELS = [
    simple_graph("Unhealthy nodes", "node_collector_unhealthy_nodes_in_zone"),
    # It's not clear which "Component restarts" shows more accurate results.
    simple_graph(
        "Component restarts",
        "sum(rate(process_start_time_seconds[1m]) > bool 0) by (job, endpoint)",
    ),
    simple_graph(
        "Component restarts 2",
        'sum(min_over_time(container_start_time_seconds{container_name!="",container_name!="POD"}[2m])) by (container_name)',
    ),
]

ETCD_PANELS = [
    simple_graph("etcd leader", "etcd_server_is_leader"),
    simple_graph(
        "etcd disk backend commit duration",
        "histogram_quantile(0.95, sum(rate(etcd_disk_backend_commit_duration_seconds_bucket[5m])) by (le))",
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph(
        "etcd bytes sent",
        "rate(etcd_network_client_grpc_sent_bytes_total[1m])",
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
    simple_graph("etcd objects", "sum(etcd_object_counts) by (resource, instance)"),
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
        "(experimental) Watch events rate",
        "sum(rate(apiserver_watch_events_total[1m])) by (version, kind, instance)",
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
    ),
    simple_graph(
        "Request latency (99th percentile)",
        'apiserver:apiserver_request_latency:histogram_quantile{quantile="0.99", verb!="WATCH"}',
        g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    simple_graph(
        '"Big" LIST requests',
        [
            'sum(rate(apiserver_request_count{verb="LIST", resource="%s"}[1m])) by (resource, client)'
            % resource
            for resource in [
                "nodes",
                "pods",
                "services",
                "endpoints",
                "replicationcontrollers",
            ]
        ],
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
    ),
    simple_graph(
        "fs reads by container",
        "sum(rate(container_fs_reads_total[1m])) by (container_name, instance)",
    ),
    simple_graph(
        "fs bytes writes by container",
        "sum(rate(container_fs_writes_bytes_total[1m])) by (container_name, instance)",
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    simple_graph(
        "fs writes by container",
        "sum(rate(container_fs_writes_total[1m])) by (container_name, instance)",
    ),
    simple_graph(
        "CPU usage by container",
        [
            'sum(rate(container_cpu_usage_seconds_total{container_name!=""}[1m])) by (container_name, instance)',
            "machine_cpu_cores",
        ],
    ),
    simple_graph(
        "memory usage by container",
        [
            'sum(container_memory_usage_bytes{container_name!=""}) by (container_name, instance)',
            "machine_memory_bytes",
        ],
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    simple_graph(
        "memory working set by container",
        [
            'sum(container_memory_working_set_bytes{container_name!=""}) by (container_name, instance)',
            "machine_memory_bytes",
        ],
        g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    simple_graph(
        "Network usage (bytes)",
        [
            'rate(container_network_transmit_bytes_total{id="/"}[1m])',
            'rate(container_network_receive_bytes_total{id="/"}[1m])',
        ],
        g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
    simple_graph(
        "Network usage (packets)",
        [
            'rate(container_network_transmit_packets_total{id="/"}[1m])',
            'rate(container_network_receive_packets_total{id="/"}[1m])',
        ],
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
        g.Row(
            title="Clusterloader",
            height=DEFAULT_PANEL_HEIGHT,
            panels=CLUSTERLOADER_PANELS,
        ),
        g.Row(
            title="Overall cluster health",
            height=DEFAULT_PANEL_HEIGHT,
            panels=HEALTH_PANELS,
        ),
        g.Row(title="etcd", height=DEFAULT_PANEL_HEIGHT, panels=ETCD_PANELS),
        g.Row(
            title="kube-apiserver", height=DEFAULT_PANEL_HEIGHT, panels=APISERVER_PANELS
        ),
        g.Row(
            title="kube-controller-manager",
            height=DEFAULT_PANEL_HEIGHT,
            panels=[simple_graph("Workqueue depths", "workqueue_depth")],
        ),
        g.Row(title="Master VM", height=DEFAULT_PANEL_HEIGHT, panels=VM_PANELS),
        g.Row(
            title="Addons",
            height=DEFAULT_PANEL_HEIGHT,
            panels=[
                g.Graph(
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
