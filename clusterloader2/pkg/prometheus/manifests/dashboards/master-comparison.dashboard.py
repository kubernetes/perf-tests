#!/usr/bin/env python3

# Copyright 2022 The Kubernetes Authors.
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

import importlib
from copy import deepcopy

from grafanalib import core as g
import defaults as d

from master_panels import API_CALL_LATENCY_PANELS, QUANTILE_API_CALL_LATENCY_PANELS, PAF_PANELS, HEALTH_PANELS, ETCD_PANELS, APISERVER_PANELS, CONTROLLER_MANAGER_PANELS, VM_PANELS


def extended_copy(panels):
    extended_panels = []

    for panel in panels:
        # copy of an original panel
        extended_panels.append(deepcopy(panel))

        # secondary panel
        # same criteria, different data source and starting point
        panel.title = "[SECONDARY] " + panel.title
        panel.dataSource = "$secondary_source"
        panel.timeShift = "$timeshift"
        extended_panels.append(panel)

    return extended_panels


dashboard = d.Dashboard(
    title="Comparison Master dashboard",
    refresh="",
    rows=[
        d.Row(title="API call latency", panels=extended_copy(API_CALL_LATENCY_PANELS)),
        d.Row(title="API call latency aggregated with quantile", panels=extended_copy(QUANTILE_API_CALL_LATENCY_PANELS), collapse=True),
        d.Row(title="P&F metrics", panels=extended_copy(PAF_PANELS), collapse=True),
        d.Row(title="Overall cluster health", panels=extended_copy(HEALTH_PANELS), collapse=True),
        d.Row(title="etcd", panels=extended_copy(ETCD_PANELS), collapse=True),
        d.Row(title="kube-apiserver", panels=extended_copy(APISERVER_PANELS), collapse=True),
        d.Row(title="kube-controller-manager", panels=extended_copy(CONTROLLER_MANAGER_PANELS), collapse=True),
        d.Row(title="Master VM", panels=extended_copy(VM_PANELS), collapse=True),
    ],
    templating=g.Templating(
        list=[
            d.SOURCE_TEMPLATE,
            g.Template(
                name="secondary_source",
                type="datasource",
                query="prometheus",
            ),
            g.Template(
                name="timeshift",
                type="interval",
                query="",
            ),
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
