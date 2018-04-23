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
    self.assertEquals(1000, parser.results['queries_sent'])

  def test_result_db(self):
    results = yaml.load(open('fixtures/results.yaml'))

    self.db.put(results)
    res = self.db.get_results(1234, 0)
    self.assertEquals(727354, res['queries_sent'])
    self.db.put(results) # dup

    self.assertRaises(
        Exception,
        lambda: self.db.put(results, ignore_if_dup=False))
