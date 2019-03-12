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

"""
The test parameter space explored by the performance test is
defined in this module. Each Param below represents a tunable used to
configure the test runs. Add additional parameters to the test by
subclassing *Param class and adding them to the `PARAMETERS` global
variable.
"""

from __future__ import print_function

import copy
import logging
import re
import time
import yaml


_log = logging.getLogger(__name__)


class Inputs(object):
  """
  Inputs to the dns performance test.
  """
  def __init__(self, deployment_yaml, configmap_yaml, dnsperf_cmdline):
    self.deployment_yaml = copy.deepcopy(deployment_yaml)
    self.configmap_yaml = copy.deepcopy(configmap_yaml)
    self.dnsperf_cmdline = dnsperf_cmdline


class Param(object):
  """
  A test parameter.
  """
  def __init__(self, name, data_type):
    """
    |name| of the parameter (e.g. 'kubedns_cpu')
    |data_type| of the parameter value (e.g. `int`)
    |attributes| optional, associated with a parameter. Is a `set` of str.
    """
    self.name = name
    self.data_type = data_type

  def is_relevant(self, attributes): #pylint: disable=no-self-use,unused-argument
    """
    Determine whether or not this parameter is relevant to the current
    test case run.
    |attributes| set of string attributes.
    |return| True if this parameter is relevant given the test run
      attributes.
    """
    return True

  def set(self, inputs, value):
    """
    Modify the inputs according the value given.
    |inputs| is of type Inputs.
    |value| to assign
    """
    raise NotImplementedError()

  def __repr__(self):
    return '<%s>' % self.name


class DeploymentContainerSpecParam(Param):
  """
  Parameterizes a field in a deployment container specification.
  """
  def __init__(self, name, data_type, container_name, yaml_path):
    super(DeploymentContainerSpecParam, self).__init__(name, data_type)
    self.yaml_path = yaml_path
    self.container_name = container_name

  def is_relevant(self, attributes):
    return 'cluster-dns' not in attributes and 'node-local-dns' not in attributes

  def set(self, inputs, value):
    spec = _item_by_predicate(
        inputs.deployment_yaml['spec']['template']['spec']['containers'],
        lambda x: x['name'] == self.container_name)
    _set_or_remove(spec, self.yaml_path, value)


class KubednsCPU(DeploymentContainerSpecParam):
  def __init__(self):
    super(KubednsCPU, self).__init__(
        'kubedns_cpu', int, 'kubedns', ['resources', 'limits', 'cpu'])

  def set(self, inputs, value):
    super(KubednsCPU, self).set(
        inputs, None if value is None else '%dm' % value)

class CorednsCPU(DeploymentContainerSpecParam):
  def __init__(self):
    super(CorednsCPU, self).__init__(
        'coredns_cpu', int, 'coredns', ['resources', 'limits', 'cpu'])

  def set(self, inputs, value):
    super(CorednsCPU, self).set(
        inputs, None if value is None else '%dm' % value)

class DnsmasqCPU(DeploymentContainerSpecParam):
  def __init__(self):
    super(DnsmasqCPU, self).__init__(
        'dnsmasq_cpu', int, 'dnsmasq', ['resources', 'limits', 'cpu'])

  def set(self, inputs, value):
    super(DnsmasqCPU, self).set(
        inputs, None if value is None else '%dm' % value)


class DnsmasqCache(Param):
  """
  Changes the command line for dnsmasq.
  """
  def __init__(self):
    super(DnsmasqCache, self).__init__('dnsmasq_cache', int)

  def is_relevant(self, attributes):
    return 'cluster-dns' not in attributes

  def set(self, inputs, value):
    spec = _item_by_predicate(
        inputs.deployment_yaml['spec']['template']['spec']['containers'],
        lambda x: x['name'] == 'dnsmasq')

    args = spec['args']
    args = [x for x in args if not x.startswith('--cache-size')]
    args.append('--cache-size=%d' % value)

    spec['args'] = args

class CorednsCache(Param):
  """
  Changes the cache setting in the CoreDNS configmap.
  """
  def __init__(self):
    super(CorednsCache, self).__init__('coredns_cache', int)

  def is_relevant(self, attributes):
    return 'cluster-dns' not in attributes

  def set(self, inputs, value):
    if value > 0:
      cf = inputs.configmap_yaml['data']['Corefile']
      cfList = cf.split("\n")
      cfList.insert(1,
                    "  cache {\n"
                    "    success " + repr(value) + "\n"
                    "    denial " + repr(value) + "\n"
                    "  }")
      inputs.configmap_yaml['data']['Corefile'] = "\n".join(cfList)

class DnsperfCmdlineParam(Param):
  """
  Parameterizes a field from the dnsperf command line

  |use_equals| Add the command line param as ['key=value'], rather
      than ['key', 'value'].
  """
  def __init__(self, name, data_type, option_name, use_equals):
    super(DnsperfCmdlineParam, self).__init__(name, data_type)
    self._option_name = option_name
    self._use_equals = use_equals

  def set(self, inputs, value):
    if value is None:
      return
    if self._use_equals:
      inputs.dnsperf_cmdline.append('%s=%s' % self._option_name, value)
    else:
      inputs.dnsperf_cmdline.append(self._option_name)
      inputs.dnsperf_cmdline.append(str(value))


class QueryFile(DnsperfCmdlineParam):
  def __init__(self):
    super(QueryFile, self).__init__('query_file', str, '-d', False)

  def set(self, inputs, value):
    super(QueryFile, self).set(
        inputs, None if value is None else '/queries/' + value)


class RunLengthSeconds(DnsperfCmdlineParam):
  def __init__(self):
    super(RunLengthSeconds, self).__init__(
        'run_length_seconds', str, '-l', False)


class MaxQPS(DnsperfCmdlineParam):
  def __init__(self):
    super(MaxQPS, self).__init__('max_qps', str, '-Q', False)


class TestCase(object):
  def __init__(self, run_id, run_subid, pv):
    self.run_id = run_id
    self.run_subid = run_subid
    self.pv = pv

  def __repr__(self):
    return str(vars(self))

  def to_yaml(self):
    fields = {
        'run_id': self.run_id,
        'run_subid': self.run_subid,
    }
    for param, value in self.pv:
      fields[param.name] = value
    return fields

  def configure(self, inputs):
    """
    Generate the right set of inputs to the test run.
    """
    for param, value in self.pv:
      param.set(inputs, value)


class TestCases(object):
  """
  Parameters to range over for the peformance test.
  """
  @staticmethod
  def load_from_file(filename):
    """
    |filename| yaml file to read from.
    """
    raw = yaml.load(open(filename, 'r'))
    return TestCases(raw)

  def __init__(self, values):
    self.values = values

  def generate(self, attributes):
    """
    |return| a list of TestCases to run.
    """
    cases = []
    run_id = int(time.time())

    def iterate(remaining, pv):
      if len(remaining) == 0:
        run_subid = len(cases)
        return cases.append(TestCase(run_id, run_subid, pv))

      param = remaining[0]
      if param.name not in self.values or \
          not param.is_relevant(attributes):
        iterate(remaining[1:], pv)
        return

      for value in self.values[param.name]:
        iterate(remaining[1:], pv + [(param, value)])

    iterate(PARAMETERS, [])

    return cases

  def set_param(self, param_name, values):
    if param_name not in self.values:
      return
    self.values[param_name].append(values)

  def get_param(self, param_name):
    if param_name not in self.values:
      return None
    return self.values[param_name]

def _item_by_predicate(list_obj, predicate):
  """
  Iterate through list_obj and return the object that matches the predicate.
  |list_obj| list
  |predicate| f(x) -> bool
  |return| object otherwise None if not found
  """
  for x in list_obj:
    if predicate(x):
      return x
  return None


def _set_or_remove(root, path, value):
  """
  |root| JSON-style object
  |path| path to modify
  |value| if not None, value to set, otherwise remove.
  """
  if value is not None:
    for label in path[:-1]:
      if label not in root:
        root[label] = {}
      root = root[label]
    root[path[-1]] = value
  else:
    for label in path[:-1]:
      if label not in root:
        return
      root = root[label]
    if path[-1] in root:
      del root[path[-1]]

# Test parameters available for the performance test.
#
# Note: this should be sorted in order with most disruptive to least
# disruptive (disruptive = requires daemon restarts) as this is the
# iteration order used to run the perf tests.
PARAMETERS = [
    RunLengthSeconds(),
    DnsmasqCPU(),
    DnsmasqCache(),
    CorednsCache(),
    KubednsCPU(),
    CorednsCPU(),
    MaxQPS(),
    QueryFile(),
]

# Given as an attribute to TestCases.generate, specifies that the test
# case is run with cluster-dns.
ATTRIBUTE_CLUSTER_DNS = 'cluster-dns'
# specifies that the test uses node-cache
ATTRIBUTE_NODELOCAL_DNS = 'node-local-dns'
