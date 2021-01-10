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

"""Checks out a gcp account from E2E"""

import urllib
import httplib
import os
import sys
import json

BOSKOS_HOST=os.environ.get("BOSKOS_HOST", "boskos")

RESOURCE_TYPE = "gce-project"
USER = "kubeadm-windows"

def post_request(host, input_state = "clean"):
    conn = httplib.HTTPConnection(host)
    conn.request("POST", "/acquire?%s" % urllib.urlencode({
        'type': RESOURCE_TYPE,
        'owner': USER,
        'state': input_state,
        'dest': 'busy',
    })

    )
    resp = conn.getresponse()
    status = resp.status
    reason = resp.reason
    body = resp.read()
    conn.close()
    return status, reason, body

if __name__ == "__main__":
    status, reason, result = post_request(BOSKOS_HOST)
    #  we're working around an issue with the data in boskos.
    #  We'll remove the code that tries both free and clean once all the data is good.
    #  Afterwards we should just check for free
    if status == 404:
        status, reason, result = post_request(BOSKOS_HOST, "free")

    if status != 200:
        sys.exit("Got invalid response %d: %s" % (status, reason))

    body = json.loads(result)
    print 'export BOSKOS_RESOURCE_NAME="%s";' % body['name']
    print 'export GCP_PROJECT="%s";' % body['name']
