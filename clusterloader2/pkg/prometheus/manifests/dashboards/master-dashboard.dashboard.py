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
import defaults as d


def api_call_latency(title, verb, scope, threshold):
    return d.Graph(
        title=title,
        targets=[
            g.Target(expr=str(threshold), legendFormat="threshold"),
            g.Target(
                expr=d.one_line(
                    """
apiserver:apiserver_request_latency_1m:histogram_quantile{
  quantile="0.99",
  verb=~"%(verb)s",
  scope=~"%(scope)s",
  resource=~"${resource:regex}s*",
}"""
                    % {"verb": verb, "scope": scope}
                ),
                # TODO(github.com/grafana/grafana/issues/19410): uncomment once fixed
                # legendFormat="{{verb}} {{scope}}/{{resource}}",
            ),
        ],
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    )


CLUSTERLOADER_PANELS = [
    api_call_latency(
        title="Read-only API call latency (percentaile=99, scope=resource, threshold=1s)",
        verb="GET",
        scope="namespace",
        threshold=1,
    ),
    api_call_latency(
        title="Read-only API call latency (percentaile=99, scope=namespace, threshold=5s)",
        verb="LIST",
        scope="namespace",
        threshold=5,
    ),
    api_call_latency(
        title="Read-only API call latency (percentaile=99, scope=cluster, threshold=30s)",
        verb="LIST",
        scope="cluster",
        threshold=30,
    ),
    api_call_latency(
        title="Mutating API call latency (threshold=1s)",
        verb=d.any_of("CREATE", "DELETE", "PATCH", "POST", "PUT"),
        scope=d.any_of("namespace", "cluster"),
        threshold=1,
    ),
]

HEALTH_PANELS = [
    d.simple_graph(
        "Unhealthy nodes",
        "sum(node_collector_unhealthy_nodes_in_zone) by (zone)",
        legend="{{zone}}",
    ),
    d.simple_graph(
        "Pod creations",
        'sum(irate(apiserver_request_total{verb="POST", resource="pods", subresource=""}[1m]))',
        yAxes=g.single_y_axis(format=g.OPS_FORMAT),
    ),
    d.simple_graph(
        "Pod bindings",
        'sum(irate(apiserver_request_total{verb="POST", resource="pods", subresource="binding"}[1m]))',
        yAxes=g.single_y_axis(format=g.OPS_FORMAT),
    ),
    # It's not clear which "Component restarts" shows more accurate results.
    d.simple_graph(
        "Component restarts",
        "sum(rate(process_start_time_seconds[1m]) > bool 0) by (job, endpoint)",
    ),
    d.simple_graph(
        "Component restarts 2",
        'sum(min_over_time(container_start_time_seconds{container!="",container!="POD"}[2m])) by (container)',
    ),
    d.simple_graph(
        "Active component", "sum(leader_election_master_status) by (name, instance)"
    ),
]

ETCD_PANELS = [
    d.simple_graph("etcd leader", "etcd_server_is_leader", legend="{{instance}}"),
    d.simple_graph(
        "etcd bytes sent",
        "irate(etcd_network_client_grpc_sent_bytes_total[1m])",
        yAxes=g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
        legend="{{instance}}",
    ),
    d.simple_graph(
        "etcd operations rate",
        d.one_line(
            """
sum(
  rate(
    etcd_request_duration_seconds_count{
      operation=~"${etcd_operation:regex}",
      type=~".*(${etcd_type:pipe})"
    }[1m]
  )
) by (operation, type)
"""
        ),
        yAxes=g.single_y_axis(format=g.OPS_FORMAT),
        legend="{{operation}} {{type}}",
    ),
    d.simple_graph(
        "etcd get latency by type (99th percentile)",
        d.one_line(
            """
histogram_quantile(
  0.99,
  sum(
    rate(
      etcd_request_duration_seconds_bucket{
        operation=~"${etcd_operation:regex}",
        type=~".*(${etcd_type:pipe})"
      }[1m]
    )
  ) by (le, operation, type, instance)
)
"""
        ),
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
        legend="{{operation}} {{type}} on {{instance}}",
    ),
    d.simple_graph(
        "etcd get latency by type (50th percentile)",
        d.one_line(
            """
histogram_quantile(
  0.50,
  sum(
    rate(
      etcd_request_duration_seconds_bucket{
        operation=~"${etcd_operation:regex}",
        type=~".*(${etcd_type:pipe})"
      }[1m]
    )
  ) by (le, operation, type, instance)
)
"""
        ),
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.simple_graph("etcd instance id", "sum(etcd_server_id) by (instance, server_id)"),
    d.simple_graph(
        "etcd network latency (99th percentile)",
        "histogram_quantile(0.99, sum(rate(etcd_network_peer_round_trip_time_seconds_bucket[1m])) by (le, instance, To))",
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.simple_graph(
        "etcd compaction keys",
        "delta(etcd_debugging_mvcc_db_compaction_keys_total[1m])",
    ),
    d.simple_graph(
        "etcd compaction pause sum duration",
        "delta(etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds_sum[1m])",
        yAxes=g.single_y_axis(format=g.MILLISECONDS_FORMAT),
    ),
    d.simple_graph(
        "etcd compaction pause num chunks",
        "delta(etcd_debugging_mvcc_db_compaction_pause_duration_milliseconds_count[1m])",
    ),
    d.simple_graph(
        "etcd_disk_backend_commit_duration_seconds",
        "histogram_quantile(0.99, sum(rate(etcd_disk_backend_commit_duration_seconds[1m])) by (le, instance))",
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.simple_graph(
        "etcd wal fsync duration",
        "histogram_quantile(1.0, sum(rate(etcd_disk_wal_fsync_duration_seconds_bucket[1m])) by (le, endpoint))",
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.Graph(
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
    d.simple_graph(
        "etcd objects",
        "sum(etcd_object_counts) by (resource, instance)",
        legend="{{instance}}: {{resource}}",
    ),
    d.simple_graph(
        "etcd db size",
        [
            "etcd_mvcc_db_total_size_in_bytes",
            "etcd_mvcc_db_total_size_in_use_in_bytes",
            "etcd_server_quota_backend_bytes",
        ],
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
]

APISERVER_PANELS = [
    d.simple_graph(
        "goroutines",
        'go_goroutines{job="master", endpoint="apiserver"}',
        legend="{{instance}}",
    ),
    d.simple_graph(
        "gc rate",
        'rate(go_gc_duration_seconds_count{job="master", endpoint="apiserver"}[1m])',
        legend="{{instance}}",
    ),
    d.simple_graph(
        "alloc rate",
        'rate(go_memstats_alloc_bytes_total{job="master", endpoint="apiserver"}[1m])',
        yAxes=g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
        legend="{{instance}}",
    ),
    d.simple_graph(
        "Number of active watches",
        'sum(apiserver_registered_watchers{kind=~"(?i:(${resource:regex}))s*"}) by (instance, group, version, kind)',
        legend="{{instance}}: {{version}}.{{group}}.{{kind}}",
    ),
    d.simple_graph(
        "Watch events rate",
        d.one_line(
            """
sum(
  irate(
    apiserver_watch_events_total{
      kind=~"(?i:(${resource:regex}))s*"
    }[1m]
  )
) by (instance, group, version, kind)"""
        ),
        legend="{{instance}}: {{version}}.{{group}}.{{kind}}",
    ),
    d.simple_graph(
        "Watch events traffic",
        d.one_line(
            """
sum(
  irate(
    apiserver_watch_events_sizes_sum{
      kind=~"(?i:(${resource:regex}))s*"
   }[1m]
  )
) by (instance, group, version, kind)"""
        ),
        yAxes=g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
        legend="{{instance}}: {{version}}.{{group}}.{{kind}}",
    ),
    d.simple_graph(
        "Watch event avg size",
        d.one_line(
            """
sum(
  rate(
    apiserver_watch_events_sizes_sum{
      kind=~"(?i:(${resource:regex}))s*"
    }[1m]
  )
  /
  rate(
    apiserver_watch_events_sizes_count{
      kind=~"(?i:(${resource:regex}))s*"
    }[1m]
  )
) by (instance, group, version, kind)"""
        ),
        legend="{{instance}}: {{version}}.{{group}}.{{kind}}",
    ),
    d.simple_graph(
        "Inflight requests",
        "sum(apiserver_current_inflight_requests) by (requestKind, instance)",
        legend="{{instance}}: {{requestKind}}",
    ),
    d.simple_graph(
        "Request rate",
        d.one_line(
            """
sum(
  rate(
    apiserver_request_total{
      verb=~"${verb:regex}",
      resource=~"${resource:regex}s*"
    }[1m]
  )
) by (verb, resource, subresource, instance)"""
        ),
        # TODO(github.com/grafana/grafana/issues/19410): uncomment once fixed
        # legend="{{instance}}: {{verb}} {{resource}}",
    ),
    d.simple_graph(
        "Request rate by code",
        "sum(rate(apiserver_request_total[1m])) by (code, instance)",
        legend="{{instance}}: {{code}}",
    ),
    d.simple_graph(
        "Request latency (50th percentile) (excl. WATCH)",
        d.one_line(
            """
apiserver:apiserver_request_latency:histogram_quantile{
  quantile="0.50",
  verb!="WATCH",
  verb=~"${verb:regex}",
  resource=~"${resource:regex}s*"
}"""
        ),
        # TODO(github.com/grafana/grafana/issues/19410): uncomment once fixed
        # legend="{{verb}} {{scope}}/{{resource}}",
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.simple_graph(
        "Request latency (99th percentile) (excl. WATCH)",
        d.one_line(
            """
apiserver:apiserver_request_latency:histogram_quantile{
  quantile="0.99",
  verb!="WATCH",
  verb=~"${verb:regex}",
   resource=~"${resource:regex}s*"
}"""
        ),
        # TODO(github.com/grafana/grafana/issues/19410): uncomment once fixed
        # legend="{{verb}} {{scope}}/{{resource}}",
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    ),
    d.simple_graph(
        "Traffic (excl. WATCH)",
        d.one_line(
            """
sum(
  rate(
    apiserver_response_sizes_sum{
      verb!="WATCH",
      verb=~"${verb:regex}",
      resource=~"${resource:regex}s*"
    }[1m]
  )
) by (verb, version, resource, subresource, scope, instance)"""
        ),
        yAxes=g.single_y_axis(format=g.BYTES_PER_SEC_FORMAT),
    ),
]

VM_PANELS = [
    d.simple_graph(
        "fs bytes reads by container",
        "sum(rate(container_fs_reads_bytes_total[1m])) by (container, instance)",
        legend="{{instance}}: {{container}}",
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    d.simple_graph(
        "fs reads by container",
        "sum(rate(container_fs_reads_total[1m])) by (container, instance)",
        legend="{{instance}}: {{container}}",
    ),
    d.simple_graph(
        "fs bytes writes by container",
        "sum(rate(container_fs_writes_bytes_total[1m])) by (container, instance)",
        legend="{{instance}}: {{container}}",
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    d.simple_graph(
        "fs writes by container",
        "sum(rate(container_fs_writes_total[1m])) by (container, instance)",
        legend="{{instance}}: {{container}}",
    ),
    d.Graph(
        title="CPU usage by container",
        targets=[
            d.Target(
                expr='sum(rate(container_cpu_usage_seconds_total{container!=""}[1m])) by (container, instance)',
                legendFormat="{{instance}}: {{container}}",
            ),
            d.Target(expr="machine_cpu_cores", legendFormat="limit"),
        ],
    ),
    d.Graph(
        title="memory usage by container",
        targets=[
            d.Target(
                expr='sum(container_memory_usage_bytes{container!=""}) by (container, instance)',
                legendFormat="{{instance}}: {{container}}",
            ),
            d.Target(expr="machine_memory_bytes", legendFormat="limit"),
        ],
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    d.Graph(
        title="memory working set by container",
        targets=[
            d.Target(
                expr='sum(container_memory_working_set_bytes{container!=""}) by (container, instance)',
                legendFormat="{{instance}}: {{container}}",
            ),
            d.Target(expr="machine_memory_bytes", legendFormat="limit"),
        ],
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
    d.Graph(
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
    d.Graph(
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
    d.Graph(
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
    d.Graph(
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
dashboard = d.Dashboard(
    title="Master dashboard",
    refresh="",
    rows=[
        d.Row(title="Clusterloader", panels=CLUSTERLOADER_PANELS),
        d.Row(title="Overall cluster health", panels=HEALTH_PANELS, collapse=True),
        d.Row(title="etcd", panels=ETCD_PANELS, collapse=True),
        d.Row(title="kube-apiserver", panels=APISERVER_PANELS, collapse=True),
        d.Row(
            title="kube-controller-manager",
            panels=[
                d.simple_graph(
                    "Workqueue depths",
                    'workqueue_depth{endpoint="kube-controller-manager"}',
                    legend="{{name}}",
                )
            ],
            collapse=True,
        ),
        d.Row(title="Master VM", panels=VM_PANELS, collapse=True),
    ],
    templating=g.Templating(
        list=[
            d.SOURCE_TEMPLATE,
            g.Template(
                name="etcd_type",
                type="query",
                dataSource="$source",
                regex=r"\*\[+\]+(.*)",
                query="label_values(etcd_request_duration_seconds_count, type)",
                multi=True,
                includeAll=True,
                refresh=g.REFRESH_ON_TIME_RANGE_CHANGE,
            ),
            g.Template(
                name="etcd_operation",
                type="query",
                dataSource="$source",
                query="label_values(etcd_request_duration_seconds_count, operation)",
                multi=True,
                includeAll=True,
                refresh=g.REFRESH_ON_TIME_RANGE_CHANGE,
            ),
            g.Template(
                name="verb",
                type="query",
                dataSource="$source",
                query="label_values(apiserver_request_duration_seconds_count, verb)",
                multi=True,
                includeAll=True,
                refresh=g.REFRESH_ON_TIME_RANGE_CHANGE,
            ),
            g.Template(
                name="resource",
                type="query",
                dataSource="$source",
                regex="(.*)s",
                query="label_values(apiserver_request_duration_seconds_count, resource)",
                multi=True,
                includeAll=True,
                refresh=g.REFRESH_ON_TIME_RANGE_CHANGE,
            ),
        ]
    ),
).auto_panel_ids()
