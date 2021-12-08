#!/bin/bash
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
readonly ARTIFACTS_PATH="${ARTIFACTS}"
export KUBEVIRT_NUM_NODES=2
export KUBEVIRT_PROVIDER=k8s-1.22
export KUBEVIRT_DEPLOY_PROMETHEUS=true
make cluster-down
make cluster-up
if [[ -v PROW_JOB_ID ]] ; then
  GOLANG_VER=${GOLANG_VER:-1.16.8}
  eval $(gimme ${GOLANG_VER})
  cp -R ~/.gimme/versions/go${GOLANG_VER}.linux.amd64 /usr/local/go
fi

go get gotest.tools/gotestsum
#export UPGRADE_FROM=$(curl -s https://github.com/kubevirt/hostpath-provisioner-operator/releases/latest | grep -o "v[0-9]\.[0-9]*\.[0-9]*")
#The upgrade from code path doesn't setup the webhook and cert-manager, only the code after the upgrade does. This is a critical test so we want
#to fix the version to 0.10.0.
export UPGRADE_FROM=v0.10.0
echo "Upgrading from verions: $UPGRADE_FROM"

make cluster-sync
make test-functional
