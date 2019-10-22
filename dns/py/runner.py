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

import json
import logging
import os
import subprocess
import time
import traceback
import yaml
import threading
import re
import Queue
from subprocess import PIPE

from data import Parser, ResultDb
from params import ATTRIBUTE_CLUSTER_DNS, ATTRIBUTE_NODELOCAL_DNS, Inputs, TestCases, QueryFile, RunLengthSeconds

_log = logging.getLogger(__name__)
_app_label = 'app=dns-perf-server'
_client_podname = 'dns-perf-client'
_test_svc_label = 'app=test-svc'
_dnsperf_qfile_name='queryfile-example-current'
_dnsperf_qfile_path='ftp://ftp.nominum.com/pub/nominum/dnsperf/data/queryfile-example-current.gz'
# Remove dns queries to this host since it is associated with behavior pattern
# of some malware
_remove_query_pattern=["setting3[.]yeahost[.]com"]
MAX_TEST_SVC = 20

def add_prefix(prefix, text):
  return '\n'.join([prefix + l for l in text.split('\n')])


class Runner(object):
  """
  Runs the performance experiments.
  """
  def __init__(self, args):
    """
    |args| parsed command line args.
    """
    self.args = args
    self.deployment_yaml = yaml.load(open(self.args.deployment_yaml, 'r'))
    self.configmap_yaml = yaml.load(open(self.args.configmap_yaml, 'r')) if \
      self.args.configmap_yaml else None
    self.service_yaml = yaml.load(open(self.args.service_yaml, 'r')) if \
        self.args.service_yaml else None
    self.dnsperf_yaml = yaml.load(open(self.args.dnsperf_yaml, 'r'))
    self.test_params = TestCases.load_from_file(args.params)
    if self.args.run_large_queries:
      self.test_params.set_param(QueryFile().name, _dnsperf_qfile_name)
    self.args.testsvc_yaml = yaml.load(open(self.args.testsvc_yaml, 'r')) if \
        self.args.testsvc_yaml else None


    self.server_node = None
    self.client_node = None
    self.use_existing = False
    self.db = ResultDb(self.args.db) if self.args.db else None

    self.attributes = set()

    if self.args.use_cluster_dns:
      _log.info('Using cluster DNS for tests')
      self.args.dns_ip = self._get_dns_ip(self.args.dns_server)
      self.attributes.add(ATTRIBUTE_CLUSTER_DNS)
      self.use_existing = True
    elif self.args.nodecache_ip:
      _log.info('Using existing node-local-dns for tests')
      self.args.dns_ip = self.args.nodecache_ip
      self.attributes.add(ATTRIBUTE_NODELOCAL_DNS)
      self.use_existing = True

    _log.info('DNS service IP is %s', args.dns_ip)

  def go(self):
    """
    Run the performance tests.
    """
    self._select_nodes()

    test_cases = self.test_params.generate(self.attributes)
    if len(test_cases) == 0:
      _log.warning('No test cases')
      return 0

    try:
      self._ensure_out_dir(test_cases[0].run_id)
      self._reset_client()
      self._create_test_services()

      last_deploy_yaml = None
      last_config_yaml = None
      for test_case in test_cases:
        try:
          inputs = Inputs(self.deployment_yaml, self.configmap_yaml,
                          ['/dnsperf', '-s', self.args.dns_ip])
          test_case.configure(inputs)
          # pin server to a specific node
          inputs.deployment_yaml['spec']['template']['spec']['nodeName'] = \
              self.server_node

          if not self.use_existing and (
              yaml.dump(inputs.deployment_yaml) !=
              yaml.dump(last_deploy_yaml) or
              yaml.dump(inputs.configmap_yaml) !=
              yaml.dump(last_config_yaml)):
            _log.info('Creating server with new parameters')
            self._teardown()
            self._create(inputs.deployment_yaml)
            self._create(self.service_yaml)
            if self.configmap_yaml is not None:
              self._create(self.configmap_yaml)
            self._wait_for_status(True)

          self._run_perf(test_case, inputs)
          last_deploy_yaml = inputs.deployment_yaml
          last_config_yaml = inputs.configmap_yaml

        except Exception:
          _log.info('Exception caught during run, cleaning up. %s',
                    traceback.format_exc())
          self._teardown()
          self._teardown_client()
          raise

    finally:
      self._teardown()
      self._teardown_client()

      if self.db is not None:
        self.db.commit()

    return 0

  def _kubectl(self, stdin, *args):
    """
    |return| (return_code, stdout, stderr)
    """
    cmdline = [self.args.kubectl_exec] + list(args)
    _log.debug('kubectl %s', cmdline)
    if stdin:
      _log.debug('kubectl stdin\n%s', add_prefix('in  | ', stdin))
    proc = subprocess.Popen(cmdline, stdin=PIPE, stdout=PIPE, stderr=PIPE)
    out, err = proc.communicate(stdin)
    ret = proc.wait()

    _log.debug('kubectl ret=%d', ret)
    _log.debug('kubectl stdout\n%s', add_prefix('out | ', out))
    _log.debug('kubectl stderr\n%s', add_prefix('err | ', err))

    return proc.wait(), out, err

  def _create(self, yaml_obj):
    _log.debug('applying yaml: %s', yaml.dump(yaml_obj))
    ret, out, err = self._kubectl(yaml.dump(yaml_obj), 'create', '-f', '-')
    if ret != 0:
      _log.error('Could not create dns: %d\nstdout:\n%s\nstderr:%s\n',
                 ret, out, err)
      raise Exception('create failed')
    _log.info('Create %s/%s ok', yaml_obj['kind'], yaml_obj['metadata']['name'])

  def _run_top(self, output_q):
    kubedns_top_args = ['-l', 'k8s-app=kube-dns', '-n', 'kube-system']
    if self.args.nodecache_ip:
      perfserver_top_args = ['-l', 'k8s-app=node-local-dns', '-n', 'kube-system']
    else:
      perfserver_top_args = ['-l', _app_label]
    run_time = int(self.test_params.get_param(RunLengthSeconds().name)[0])
    t_end = time.time() + run_time
    while time.time() < t_end:
      code, perfout, err = self._kubectl(*([None, 'top', 'pod'] + perfserver_top_args))
      code, kubeout, err = self._kubectl(*([None, 'top', 'pod'] + kubedns_top_args))
      # Output is of the form:
      # NAME                        CPU(cores)   MEMORY(bytes)
      # kube-dns-686548bc64-4q7wg   2m           31Mi
      pcpu = re.findall(' \d+m ', perfout)
      pmem = re.findall(' \d+Mi ', perfout)
      kcpu = re.findall(' \d+m ', kubeout)
      kmem = re.findall(' \d+Mi ', kubeout)
      max_perfserver_cpu = 0
      max_perfserver_mem = 0
      max_kubedns_cpu = 0
      max_kubedns_mem = 0
      for c in pcpu:
        val = int(re.findall('\d+', c)[0])
        if val > max_perfserver_cpu:
          max_perfserver_cpu = val
      for m in pmem:
        val = int(re.findall('\d+', m)[0])
        if val > max_perfserver_mem:
          max_perfserver_mem = val
      for c in kcpu:
        val = int(re.findall('\d+', c)[0])
        if val > max_kubedns_cpu:
          max_kubedns_cpu = val
      for m in kmem:
        val = int(re.findall('\d+', m)[0])
        if val > max_kubedns_mem:
          max_kubedns_mem = val
      time.sleep(2)
    output_q.put(max_perfserver_cpu)
    output_q.put(max_perfserver_mem)
    output_q.put(max_kubedns_cpu)
    output_q.put(max_kubedns_mem)

  def _run_perf(self, test_case, inputs):
    _log.info('Running test case: %s', test_case)

    output_file = '%s/run-%s/result-%s.out' % \
      (self.args.out_dir, test_case.run_id, test_case.run_subid)
    _log.info('Writing to output file %s', output_file)
    res_usage = Queue.Queue()
    dt = threading.Thread(target=self._run_top,args=[res_usage])
    dt.start()
    header = '''### run_id {run_id}:{run_subid}
### date {now}
### settings {test_case}
'''.format(run_id=test_case.run_id,
           run_subid=test_case.run_subid,
           now=time.ctime(),
           test_case=json.dumps(test_case.to_yaml()))

    with open(output_file + '.raw', 'w') as fh:
      fh.write(header)
      cmdline = inputs.dnsperf_cmdline
      code, out, err = self._kubectl(
          *([None, 'exec', _client_podname, '--'] + [str(x) for x in cmdline]))
      fh.write('%s\n' % add_prefix('out | ', out))
      fh.write('%s\n' % add_prefix('err | ', err))

      if code != 0:
        raise Exception('error running dnsperf')

    dt.join()

    with open(output_file, 'w') as fh:
      results = {}
      results['params'] = test_case.to_yaml()
      results['code'] = code
      results['stdout'] = out.split('\n')
      results['stderr'] = err.split('\n')
      results['data'] = {}

      try:
        parser = Parser(out)
        parser.parse()

        _log.info('Test results parsed')

        results['data']['ok'] = True
        results['data']['msg'] = None

        for key, value in parser.results.items():
          results['data'][key] = value
        results['data']['max_perfserver_cpu'] = res_usage.get()
        results['data']['max_perfserver_memory'] = res_usage.get()
        results['data']['max_kubedns_cpu'] = res_usage.get()
        results['data']['max_kubedns_memory'] = res_usage.get()
        results['data']['histogram'] = parser.histogram
      except Exception as exc:
        _log.error('Error parsing results: %s', exc)
        results['data']['ok'] = False
        results['data']['msg'] = 'parsing error:\n%s' % traceback.format_exc()

      fh.write(yaml.dump(results))

      if self.db is not None and results['data']['ok']:
        self.db.put(results)

  def _create_test_services(self):
    if not self.args.testsvc_yaml:
      _log.info("Not creating test services since no yaml was provided")
      return
    # delete existing services if any
    self._kubectl(None, 'delete', 'services', '-l', _test_svc_label)

    for index in range(1,MAX_TEST_SVC + 1):
      self.args.testsvc_yaml['metadata']['name'] = "test-svc" + str(index)
      self._create(self.args.testsvc_yaml)

  def _select_nodes(self):
    code, out, _ = self._kubectl(None, 'get', 'nodes', '-o', 'yaml')
    if code != 0:
      raise Exception('error gettings nodes: %d', code)

    nodes = [n['metadata']['name'] for n in yaml.load(out)['items']
             if not ('unschedulable' in n['spec'] \
                 and n['spec']['unschedulable'])]
    if len(nodes) < 2 and not self.args.single_node:
      raise Exception('you need 2 or more worker nodes to run the perf test')

    if self.args.client_node:
      if self.args.client_node not in nodes:
        raise Exception('%s is not a valid node' % self.args.client_node)
      _log.info('Manually selected client_node')
      self.client_node = self.args.client_node
    elif self.args.single_node:
      self.client_node = nodes[0]
    else:
      self.client_node = nodes[1]

    _log.info('Client node is %s', self.client_node)

    if self.use_existing:
      return

    if self.args.server_node:
      if self.args.server_node not in nodes:
        raise Exception('%s is not a valid node' % self.args.server_node)
      _log.info('Manually selected server_node')
      self.server_node = self.args.server_node
    else:
      self.server_node = nodes[0]

    _log.info('Server node is %s', self.server_node)

  def _get_dns_ip(self, svcname):
    code, out, _ = self._kubectl(None, 'get', 'svc', '-o', 'yaml',
                                svcname, '-nkube-system')
    if code != 0:
      raise Exception('error gettings dns ip for service %s: %d' %(svcname, code))

    try:
      return yaml.load(out)['spec']['clusterIP']
    except:
      raise Exception('error parsing %s service, could not get dns ip' %(svcname))

  def _teardown(self):
    _log.info('Starting server teardown')

    self._kubectl(None, 'delete', 'deployments', '-l', _app_label)
    self._kubectl(None, 'delete', 'services', '-l', _app_label)
    self._kubectl(None, 'delete', 'configmap', '-l', _app_label)

    self._wait_for_status(False)

    _log.info('Server teardown ok')

    self._kubectl(None, 'delete', 'services', '-l', _test_svc_label)
    if self.args.run_large_queries:
      try:
        subprocess.check_call(['rm', self.args.query_dir +_dnsperf_qfile_name])
      except subprocess.CalledProcessError:
        _log.info("Failed to delete query file")

  def _reset_client(self):
    self._teardown_client()

    self.dnsperf_yaml['spec']['nodeName'] = self.client_node
    self._create(self.dnsperf_yaml)
    while True:
      code, _, _ = self._kubectl(None, 'get', 'pod', _client_podname)
      if code == 0:
        break
      _log.info('Waiting for client pod to start on %s', self.client_node)
      time.sleep(1)
    _log.info('Client pod to started on %s', self.client_node)

    while True:
      code, _, _ = self._kubectl(
          None, 'exec', '-i', _client_podname, '--', 'echo')
      if code == 0:
        break
      time.sleep(1)
    _log.info('Client pod ready for execution')
    self._copy_query_files()

  def _copy_query_files(self):
    if self.args.run_large_queries:
      try:
        _log.info('Downloading large query file')
        subprocess.check_call(['wget', _dnsperf_qfile_path])
        subprocess.check_call(['gunzip', _dnsperf_qfile_path.split('/')[-1]])
        _log.info('Removing hostnames matching specified patterns')
        for pattern in _remove_query_pattern:
          subprocess.check_call(['sed', '-i', '-e', '/%s/d' %(pattern), _dnsperf_qfile_name])
        subprocess.check_call(['mv', _dnsperf_qfile_name, self.args.query_dir])

      except subprocess.CalledProcessError:
          _log.info('Exception caught when downloading query files %s',
                    traceback.format_exc())

    _log.info('Copying query files to client')
    tarfile_contents = subprocess.check_output(
        ['tar', '-czf', '-', self.args.query_dir])
    code, _, _ = self._kubectl(
        tarfile_contents,
        'exec', '-i', _client_podname, '--', 'tar', '-xzf', '-')
    if code != 0:
      raise Exception('error copying query files to client: %d' % code)
    _log.info('Query files copied')

  def _teardown_client(self):
    _log.info('Starting client teardown')
    self._kubectl(None, 'delete', 'pod/dns-perf-client')

    while True:
      code, _, _ = self._kubectl(None, 'get', 'pod', _client_podname)
      if code != 0:
        break
      time.sleep(1)
      _log.info('Waiting for client pod to terminate')

    _log.info('Client teardown complete')

  def _wait_for_status(self, active):
    while True:
      code, out, err = self._kubectl(
          None, 'get', '-o', 'yaml', 'pods', '-l', _app_label)
      if code != 0:
        _log.error('Error: stderr\n%s', add_prefix('err | ', err))
        raise Exception('error getting pod information: %d', code)
      pods = yaml.load(out)

      _log.info('Waiting for server to be %s (%d pods active)',
                'up' if active else 'deleted',
                len(pods['items']))

      if (active and len(pods['items']) > 0) or \
         (not active and len(pods['items']) == 0):
        break

      time.sleep(1)

    if active:
      while True:
        code, out, err = self._kubectl(
            None,
            'exec', _client_podname, '--',
            'dig', '@' + self.args.dns_ip,
            'kubernetes.default.svc.cluster.local.')
        if code == 0:
          break

        _log.info('Waiting for DNS service to start')

        time.sleep(1)

      _log.info('DNS is up')

  def _ensure_out_dir(self, run_id):
    rundir_name = 'run-%s' % run_id
    rundir = os.path.join(self.args.out_dir, rundir_name)
    latest = os.path.join(self.args.out_dir, 'latest')

    if not os.path.exists(rundir):
      os.makedirs(rundir)
      _log.info('Created rundir %s', rundir)

    if os.path.exists(latest):
      os.unlink(latest)
    os.symlink(rundir_name, latest)

    _log.info('Updated symlink %s', latest)
