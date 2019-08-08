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

DECREASING_ORDER_TOOLTIP = g.Tooltip(sort=g.SORT_DESC)

PANEL_HEIGHT = g.Pixels(300)


@attr.s
class Dashboard(g.Dashboard):
    time = attr.ib(default=g.Time("now-30d", "now"))
    templating = attr.ib(
        default=g.Templating(
            list=[
                # Make it possible to use $source as a source.
                g.Template(name="source", type="datasource", query="prometheus")
            ]
        )
    )


# Graph is a g.Graph with reasonable defaults applied.
@attr.s
class Graph(g.Graph):
    dataSource = attr.ib(default="$source")
    span = attr.ib(default=g.TOTAL_SPAN)
    tooltip = attr.ib(default=DECREASING_ORDER_TOOLTIP)


@attr.s
class Row(g.Row):
    height = attr.ib(default=PANEL_HEIGHT)
