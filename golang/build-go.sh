#!/bin/bash

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

set -euxo pipefail

function install_go_compiler {
  wget -q $GO_COMPILER_URL
  tar -C /usr/local -xzf $GO_COMPILER_PKG
  rm $GO_COMPILER_PKG
  export PATH=$PATH:/usr/local/go/bin
}

function build_go {
  git clone https://go.googlesource.com/go
  cd go/src
  ./make.bash
}

function print_recent_commits {
  local -r hours=5
  local -r dashline=$(printf %100s | tr " " "-")
  printf "Changes committed to the Golang master branch in the last ${hours} hours:\n${dashline}\n"
  git --no-pager log --format="commit %h%nDate: %ci%n%n%B${dashline}" --since="${hours}".hours
}

install_go_compiler
build_go
print_recent_commits
