#!/usr/bin/env python

# Copyright 2020 The Kubernetes Authors.
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

.PHONY: verify-all
verify-all: test verify-boilerplate verify-flags

# verify-lint-dev is convenience target for developers to run all checks
# at once locally
.PHONY: verify-all-dev
verify-all-dev: verify-all verify-dashboard verify-lint

# TODO(oxddr): go-build.sh doesn't work at the moment decide whether we need this at all
# .PHONY: build
# build:
# 	verify/go-build.sh

.PHONY: test
test:
	# TODO(oxddr): allow tests to fail, until we get confidence in the new presubmit
	verify/test.sh

.PHONY: verify-boilerplate
verify-boilerplate:
	verify/verify-boilerplate.sh

.PHONY: verify-dashboard
verify-dashboard:
	verify/verify-dashboard-format.sh

.PHONY: verify-flags
verify-flags:
	verify/verify-flags-underscore.py

.PHONY: verify-lint
verify-lint:
	verify/verify-lint.sh
