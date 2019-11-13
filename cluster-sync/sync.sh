#!/usr/bin/env bash

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

KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER:-"k8s-1.15.1"}

source ./cluster-up/hack/common.sh
source ./cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

registry=${IMAGE_REGISTRY:-localhost:$(_port registry)}
DOCKER_REPO=${registry} make push

_kubectl create -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/namespace.yaml
_kubectl create -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/operator.yaml -n hostpath-provisioner
  cat <<EOF | _kubectl apply -f -
apiVersion: hostpathprovisioner.kubevirt.io/v1alpha1
kind: HostPathProvisioner
metadata:
  name: hostpath-provisioner
spec:
  imagePullPolicy: Always
  imageRegistry: registry:5000
  imageTag: latest
  pathConfig:
    path: "/var/hpvolumes"
    useNamingPrefix: "false"
EOF

echo "Waiting for hostpath provisioner to be available"
_kubectl wait hostpathprovisioners.hostpathprovisioner.kubevirt.io/hostpath-provisioner --for=condition=Available --timeout=480s
