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
from master_panels import API_CALL_LATENCY_PANELS, QUANTILE_API_CALL_LATENCY_PANELS, APF_PANELS, HEALTH_PANELS, ETCD_PANELS, APISERVER_PANELS, CONTROLLER_MANAGER_PANELS, VM_PANELS


# The final dashboard must be named 'dashboard' so that grafanalib will find it.
dashboard = d.Dashboard(
    title="Master dashboard",
    refresh="",
    rows=[
        d.Row(title="API call latency", panels=API_CALL_LATENCY_PANELS),
        d.Row(title="API call latency aggregated with quantile", panels=QUANTILE_API_CALL_LATENCY_PANELS, collapse=True),
        d.Row(title="P&F metrics", panels=APF_PANELS, collapse=True),
        d.Row(title="Overall cluster health", panels=HEALTH_PANELS, collapse=True),
        d.Row(title="etcd", panels=ETCD_PANELS, collapse=True),
        d.Row(title="kube-apiserver", panels=APISERVER_PANELS, collapse=True),
        d.Row(title="kube-controller-manager", panels=CONTROLLER_MANAGER_PANELS, collapse=True),
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
            g.Template(
                name="instance",
                type="query",
                dataSource="$source",
                query="label_values(apiserver_request_duration_seconds_count, instance)",
                multi=True,
                includeAll=True,
                refresh=g.REFRESH_ON_TIME_RANGE_CHANGE,
            ),
        ]
    ),
).auto_panel_ids()
