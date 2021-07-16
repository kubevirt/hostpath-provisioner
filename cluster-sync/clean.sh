#!/bin/bash -e
# Copyright 2019 The hostpath provisioner Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER:-"k8s-1.21"}

source ./cluster-up/hack/common.sh
source ./cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

if _kubectl get crd hostpathprovisioners.hostpathprovisioner.kubevirt.io; then
  _kubectl delete HostPathProvisioner hostpath-provisioner --ignore-not-found
  _kubectl wait hostpathprovisioners.hostpathprovisioner.kubevirt.io/hostpath-provisioner --for=delete | echo "No CR found, continuing"
fi

_kubectl delete -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/operator.yaml -n hostpath-provisioner --ignore-not-found
_kubectl delete -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/namespace.yaml --ignore-not-found
_kubectl delete -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/storageclass-wffc.yaml --ignore-not-found
_kubectl delete -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/storageclass-immediate.yaml --ignore-not-found
