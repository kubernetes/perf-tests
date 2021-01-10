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

import sys
import httplib
import urllib
import os

BOSKOS_HOST=os.environ.get("BOSKOS_HOST", "boskos")
BOSKOS_RESOURCE_NAME=os.environ['BOSKOS_RESOURCE_NAME']

USER = "kubeadm-windows"

if __name__ == "__main__":
    conn = httplib.HTTPConnection(BOSKOS_HOST)
    conn.request("POST", "/release?%s" % urllib.urlencode({
        'name': BOSKOS_RESOURCE_NAME,
        'dest': 'dirty',
        'owner': USER,
    }))

    resp = conn.getresponse()
    if resp.status != 200:
        sys.exit("Got invalid response %d" % resp.status)
    conn.close()
