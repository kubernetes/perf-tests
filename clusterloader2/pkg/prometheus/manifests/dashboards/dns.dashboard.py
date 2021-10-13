#!/usr/bin/env python3

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

PROBER_PANEL = [
    d.Graph(
        title="In-cluster DNS latency SLI",
        targets=d.show_quantiles(
            (
                "quantile_over_time("
                + "0.99, "
                + 'probes:dns_lookup_latency:histogram_quantile{{quantile="{quantile}"}}[24h])'
            ),
            legend="{{quantile}}",
        ),
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
        nullPointMode="null",
    ),
    d.Graph(
        title="DNS latency",
        targets=d.show_quantiles(
            'probes:dns_lookup_latency:histogram_quantile{{quantile="{quantile}"}}',
            legend="{{quantile}}",
        ),
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
        nullPointMode="null",
    ),
    d.Graph(
        title="probe: lookup rate",
        targets=[
            d.Target(
                expr='sum(rate(probes_in_cluster_dns_lookup_count{namespace="probes", job="dns"}[1m]))',
                legendFormat="lookup rate",
            ),
            d.Target(
                expr='sum(rate(probes_in_cluster_network_latency_error{namespace="probes", job="dns"}[1m]))',
                legendFormat="error rate",
            ),
        ],
    ),
    d.Graph(
        title="probe: # running",
        targets=[
            d.TargetWithInterval(
                expr='count(container_memory_usage_bytes{namespace="probes", container="dns"}) by (container, namespace)'
            )
        ],
        nullPointMode="null",
    ),
    d.Graph(
        title="probe: memory usage",
        targets=d.min_max_avg(
            base='process_resident_memory_bytes{namespace="probes", job="dns"}',
            by=["job", "namespace"],
            legend="{{job}}",
        ),
        nullPointMode="null",
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
]

SERVICE_PANELS = [
    d.Graph(
        title="Service: # running",
        targets=[
            d.TargetWithInterval(
                expr='count(process_resident_memory_bytes{namespace="kube-system", job="kube-dns"}) by (job, namespace)'
            )
        ],
        nullPointMode="null",
    ),
    d.Graph(
        title="Service: memory usage",
        targets=d.min_max_avg(
            base='process_resident_memory_bytes{namespace="kube-system", job="kube-dns"}',
            by=["job", "namespace"],
            legend="{{job}}",
        ),
        nullPointMode="null",
        yAxes=g.single_y_axis(format=g.BYTES_FORMAT),
    ),
]

dashboard = d.Dashboard(
    title="DNS",
    rows=[
        d.Row(title="In-cluster DNS prober", panels=PROBER_PANEL),
        d.Row(title="In-cluster DNS service", panels=SERVICE_PANELS),
    ],
).auto_panel_ids()
