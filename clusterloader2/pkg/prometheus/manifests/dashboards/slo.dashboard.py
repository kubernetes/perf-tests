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


def api_call_latency(title, metric, verb, scope, threshold):
    return d.Graph(
        title=title,
        targets=[
            g.Target(expr=str(threshold), legendFormat="threshold"),
            g.Target(
                expr='quantile_over_time(0.99, %(metric)s{quantile="0.99", verb=~"%(verb)s", scope=~"%(scope)s"}[12h])'
                % {"metric": metric, "verb": verb, "scope": scope}
            ),
        ],
        yAxes=g.single_y_axis(format=g.SECONDS_FORMAT),
    )


def create_slo_panel(metric="apiserver:apiserver_request_latency:histogram_quantile"):
    return [
        api_call_latency(
            title="Read-only API call latency (scope=resource, threshold=1s)",
            metric=metric,
            verb="GET",
            scope="namespace",
            threshold=1,
        ),
        api_call_latency(
            title="Read-only API call latency (scope=namespace, threshold=5s)",
            metric=metric,
            verb="LIST",
            scope="namespace",
            threshold=5,
        ),
        api_call_latency(
            title="Read-only API call latency (scope=cluster, threshold=30s)",
            metric=metric,
            verb="LIST",
            scope="cluster",
            threshold=30,
        ),
        api_call_latency(
            title="Mutating API call latency (threshold=1s)",
            metric=metric,
            verb=d.any_of("CREATE", "DELETE", "PATCH", "POST", "PUT"),
            scope=d.any_of("namespace", "cluster"),
            threshold=1,
        ),
    ]


# The final dashboard must be named 'dashboard' so that grafanalib will find it.
dashboard = d.Dashboard(
    title="SLO",
    rows=[
        d.Row(title="SLO", panels=create_slo_panel()),
        d.Row(
            title="Experimental: SLO (window 1m)",
            panels=create_slo_panel(
                metric="apiserver:apiserver_request_latency_1m:histogram_quantile"
            ),
        ),
    ],
).auto_panel_ids()
