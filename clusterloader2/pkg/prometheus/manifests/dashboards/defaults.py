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

import re
import attr
from grafanalib import core as g

DECREASING_ORDER_TOOLTIP = g.Tooltip(sort=g.SORT_DESC)
PANEL_HEIGHT = g.Pixels(300)
QUANTILES = [0.99, 0.9, 0.5]

SOURCE_TEMPLATE = g.Template(name="source", type="datasource", query="prometheus")


@attr.s
class Dashboard(g.Dashboard):
    time = attr.ib(default=g.Time("now-30d", "now"))
    # Make it possible to use $source as a source.
    templating = attr.ib(default=g.Templating(list=[SOURCE_TEMPLATE]))


# Graph is a g.Graph with reasonable defaults applied.
@attr.s
class Graph(g.Graph):
    dataSource = attr.ib(default="$source")
    span = attr.ib(default=g.TOTAL_SPAN)
    tooltip = attr.ib(default=DECREASING_ORDER_TOOLTIP)
    nullPointMode = attr.ib(default=None)


@attr.s
class Row(g.Row):
    height = attr.ib(default=PANEL_HEIGHT)


@attr.s
class Target(g.Target):
    datasource = attr.ib(default="$source")


@attr.s
class TargetWithInterval(Target):
    interval = attr.ib(default="5s")
    intervalFactor = attr.ib(default=1)


def simple_graph(title, exprs, legend="", interval="5s", **kwargs):
    if not isinstance(exprs, (list, tuple)):
        exprs = [exprs]
    if legend != "" and len(exprs) != 1:
        raise ValueError("legend can be specified only for a 1-element exprs")
    return Graph(
        title=title,
        # One graph per row.
        targets=[
            Target(
                expr=expr, legendFormat=legend, interval=interval, intervalFactor=1
            )
            for expr in exprs
        ],
        **kwargs
    )


def show_quantiles(queryTemplate, quantiles=None, legend=""):
    quantiles = quantiles or QUANTILES
    targets = []
    for quantile in quantiles:
        q = "{:.2f}".format(quantile)
        l = legend or q
        targets.append(Target(expr=queryTemplate.format(quantile=q), legendFormat=l))
    return targets


def min_max_avg(base, by, legend=""):
    return [
        Target(
            expr="{func}({query}) by ({by})".format(
                func=f, query=base, by=", ".join(by)
            ),
            legendFormat="{func} {legend}".format(func=f, legend=legend),
        )
        for f in ("min", "avg", "max")
    ]


def any_of(*choices):
    return "|".join(choices)


def one_line(text):
    """Turns multiline PromQL string into a one line.

    Useful to keep sane diffs for generated (*.json) dashboards.
    """
    tokens = text.split('"')
    for i, item in enumerate(tokens):
        if not i % 2:
            item = re.sub(r"\s+", "", item)
            item = re.sub(",", ", ", item)
            item = re.sub(r"\)by\(", ") by (", item)
            tokens[i] = item
    return '"'.join(tokens)
