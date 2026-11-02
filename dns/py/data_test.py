#!/usr/bin/env python3

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

import copy
import unittest
import yaml

from data import Parser, ResultDb


class DataTest(unittest.TestCase):
  def setUp(self):
    self.db = ResultDb(':memory:')

  def test_parser(self):
    raw = open('fixtures/raw.txt').read()
    parser = Parser(raw)
    parser.parse()
    # Check a subset of fields
    self.assertEqual(1000, parser.results['queries_sent'])

  def test_result_db(self):
    results = yaml.safe_load(open('fixtures/results.yaml'))
    results['params']['pod_name'] = 'dns-perf-client-1'

    self.db.put(results)
    res = self.db.get_results(1234, 0, 'dns-perf-client-1')
    self.assertEqual(727354, res['queries_sent'])
    self.db.put(results) # dup

    self.assertRaises(
        Exception,
        lambda: self.db.put(results, ignore_if_dup=False))

  def test_histograms_keep_pod_name(self):
    results = yaml.safe_load(open('fixtures/results.yaml'))
    histogram_count = len(results['data']['histogram'])

    for pod_name in ['dns-perf-client-1', 'dns-perf-client-2']:
      pod_results = copy.deepcopy(results)
      pod_results['params']['pod_name'] = pod_name
      self.db.put(pod_results)

    rows = self.db.c.execute(
        'SELECT pod_name, COUNT(*) FROM histograms GROUP BY pod_name').fetchall()
    self.assertEqual(
        [('dns-perf-client-1', histogram_count),
         ('dns-perf-client-2', histogram_count)],
        rows)

    join_count = self.db.c.execute(
        'SELECT COUNT(*) FROM histograms h JOIN results r '
        'ON h.run_id = r.run_id AND h.run_subid = r.run_subid '
        'AND h.pod_name = r.pod_name').fetchone()[0]
    self.assertEqual(2 * histogram_count, join_count)
