#!/usr/bin/env python

# Copyright 2016 The Kubernetes Authors.
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

from __future__ import print_function

import unittest
import yaml


from params import Inputs, TestCases, ATTRIBUTE_CLUSTER_DNS, PARAMETERS


def make_mock_yaml():
  return yaml.load("""
spec:
  template:
    spec:
      containers:
      - name: kubedns
        args: []
        resources:
          limits:
            cpu: 0m
      - name: dnsmasq
        args: []
        resources:
          limits:
            cpu: 0m
  """)


class ParamsTest(unittest.TestCase):
  def test_params(self):
    values = {
        'dnsmasq_cpu': 100,
        'dnsmasq_cache': 200,
        'kubedns_cpu': 300,
        'max_qps': 400,
        'query_file': 'abc',
        'run_length_seconds': 120,
    }

    inputs = Inputs(make_mock_yaml(), [])
    for param in PARAMETERS:
      param.set(inputs, values[param.name])

    self.assertEqual(
        '100m',
        inputs.deployment_yaml['spec']['template']['spec']['containers']\
            [1]['resources']['limits']['cpu'])
    self.assertTrue(
        '--cache-size=200' in
        inputs.deployment_yaml['spec']['template']['spec']['containers']\
            [1]['args'])
    self.assertEqual(
        '300m',
        inputs.deployment_yaml['spec']['template']['spec']['containers']\
            [0]['resources']['limits']['cpu'])
    self.assertEqual(
        '-l,120,-Q,400,-d,/queries/abc',
        ','.join(inputs.dnsperf_cmdline))

  def test_null_params(self):
    # These should result in no limits.
    values = {
        'dnsmasq_cpu': None,
        'dnsmasq_cache': 100,
        'kubedns_cpu': None,
        'max_qps': None,
        'query_file': 'abc',
        'run_length_seconds': 120,
    }

    inputs = Inputs(make_mock_yaml(), [])
    for param in PARAMETERS:
      param.set(inputs, values[param.name])

    self.assertTrue(
        'cpu' not in inputs.deployment_yaml\
        ['spec']['template']['spec']['containers'][0]['resources']['limits'])
    self.assertTrue(
        'cpu' not in inputs.deployment_yaml\
        ['spec']['template']['spec']['containers'][1]['resources']['limits'])
    self.assertEqual(
        '-l,120,-d,/queries/abc',
        ','.join(inputs.dnsperf_cmdline))

  def test_TestCases(self):
    tp = TestCases({
        'kubedns_cpu': [100],
        'dnsmasq_cpu': [200, 300],
        'query_file': ['a', 'b'],
        })
    tc = tp.generate(set())
    self.assertEqual(4, len(tc))

    self.assertEqual(0, tc[0].run_subid)
    self.assertEqual(
        "[(<dnsmasq_cpu>, 200), (<kubedns_cpu>, 100), (<query_file>, 'a')]",
        str(tc[0].pv))
    self.assertEqual(
        "[(<dnsmasq_cpu>, 200), (<kubedns_cpu>, 100), (<query_file>, 'b')]",
        str(tc[1].pv))
    self.assertEqual(1, tc[1].run_subid)
    self.assertEqual(
        "[(<dnsmasq_cpu>, 300), (<kubedns_cpu>, 100), (<query_file>, 'a')]",
        str(tc[2].pv))
    self.assertEqual(2, tc[2].run_subid)
    self.assertEqual(
        "[(<dnsmasq_cpu>, 300), (<kubedns_cpu>, 100), (<query_file>, 'b')]",
        str(tc[3].pv))
    self.assertEqual(3, tc[3].run_subid)

  def test_TestCases_attributes(self):
    tp = TestCases({
        'kubedns_cpu': [100],
        'dnsmasq_cpu': [200, 300],
        'query_file': ['a', 'b'],
        })
    tc = tp.generate(set([ATTRIBUTE_CLUSTER_DNS]))
    self.assertEqual(2, len(tc))

    self.assertEqual(0, tc[0].run_subid)
    self.assertEqual(
        "[(<query_file>, 'a')]",
        str(tc[0].pv))
    self.assertEqual(
        "[(<query_file>, 'b')]",
        str(tc[1].pv))
    self.assertEqual(1, tc[1].run_subid)
