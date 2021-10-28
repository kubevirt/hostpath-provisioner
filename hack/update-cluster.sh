#!/usr/bin/env bash

#Copyright 2019 The hostpath provisioner Authors.
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

# the kubevirtci commit hash to vendor from
kubevirtci_git_hash=2110131039-6703e85

# remove previous cluster-up dir entirely before vendoring
rm -rf cluster-up

# download and extract the cluster-up dir from a specific hash in kubevirtci
curl -L https://github.com/kubevirt/kubevirtci/archive/${kubevirtci_git_hash}/kubevirtci.tar.gz | tar xz kubevirtci-${kubevirtci_git_hash}/cluster-up --strip-component 1

# the environment variable KUBEVIRTCI_TAG must be exported and set to the tag which was used to vendor kubevirtci
# it allows the content to find the right gocli version
echo "KUBEVIRTCI_TAG=${kubevirtci_git_hash}" >>cluster-up/hack/common.sh
