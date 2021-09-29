#!/usr/bin/env bash

#Copyright 2021 The hostpath provisioner Authors.
#
#Licensed under the Apache License, Version 2.0 (the "License");
#you may not use this file except in compliance with the License.
#You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
#Unless required by applicable law or agreed to in writing, software
#distributed under the License is distributed on an "AS IS" BASIS,
#WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#See the License for the specific language governing permissions and
#limitations under the License.
set -e

script_dir="$(cd "$(dirname "$0")" && pwd -P)"
source "${script_dir}"/common.sh
setGoInProw $GOLANG_VER

if [[ -v PROW_JOB_ID ]] ; then
  useradd prow -s /bin/bash
  chown prow:prow -R /home/prow
  echo "Run go test -v in $PWD"
  sudo -i -u prow /bin/bash -c 'cd /home/prow/go/src/github.com/kubevirt/hostpath-provisioner && /usr/local/go/bin/go test -v ./cmd/... ./controller/... ./pkg/...'
  go get -u golang.org/x/lint/golint
else
  echo "Run go test -v in $PWD"
  # Run test
  go test -v ./cmd/... ./controller/... ./pkg/...
fi

hack/run-lint-checks.sh
