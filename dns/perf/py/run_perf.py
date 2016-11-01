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

import argparse
import logging
import sys

from runner import Runner

_log = logging.getLogger('main')


def parse_args():
  parser = argparse.ArgumentParser(
      description="""
      Run a DNS performance test on a kubernetes cluster. Assumes a
      working `kubectl` executable.
      """)
  parser.add_argument(
      '--kubectl-exec', type=str, default='kubectl',
      help='location of the kubectl executable')
  parser.add_argument(
      '--deployment-yaml', type=str, default='cluster/kube-dns-deployment.yaml',
      help='yaml for the kube-dns server')
  parser.add_argument(
      '--service-yaml', type=str, default='cluster/kube-dns-service.yaml',
      help='yaml for the dns service')
  parser.add_argument(
      '--dnsperf-yaml', type=str, default='cluster/dnsperf.yaml',
      help='yaml for the dnsperf client')
  parser.add_argument(
      '--query-dir', type=str, default='queries/',
      help='location of query files')
  parser.add_argument(
      '--params', type=str, required=True,
      help='perf test parameters')
  parser.add_argument(
      '--out-dir', type=str, default='out',
      help='output directory')
  parser.add_argument(
      '--db', type=str, required=False,
      help='if set, put results in db for analysis')
  parser.add_argument(
      '--client-node', type=str,
      help='if set, force the client pod to be created on the given node')
  parser.add_argument(
      '--server-node', type=str,
      help='if set, force the server pod to be created on the given node')
  parser.add_argument(
      '--use-cluster-dns', action='store_true',
      help='if set, use cluster DNS instead of creating one')
  parser.add_argument(
      '--dns-ip', type=str, default='10.0.0.20',
      help='IP to use for the DNS service. Note: --use-cluster-dns '
        'implicitly sets this to 10.0.0.10')
  parser.add_argument(
      '-v', '--verbose', action='store_true',
      help='show verbose logging')

  return parser.parse_args()


if __name__ == '__main__':
  args = parse_args()

  logging.basicConfig(
      level=logging.DEBUG if args.verbose else logging.INFO,
      format='%(levelname)s %(asctime)s %(filename)s:%(lineno)d] %(message)s',
      datefmt="%m-%d %H:%M:%S")

  runner = Runner(args)
  sys.exit(runner.go())
