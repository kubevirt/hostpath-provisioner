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
export KUBEVIRT_DEPLOY_PROMETHEUS=true
make cluster-down
make cluster-up

# Apply updated Prometheus Operator CRDs matching the version in operator's go.mod
OPERATOR_BRANCH="${OPERATOR_BRANCH:-main}"
echo "Fetching Prometheus Operator version from hostpath-provisioner-operator/${OPERATOR_BRANCH}..."
OPERATOR_GO_MOD_URL="https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/${OPERATOR_BRANCH}/go.mod"
PROMETHEUS_OPERATOR_VERSION=$(curl -sL "${OPERATOR_GO_MOD_URL}" | grep "prometheus-operator/prometheus-operator/pkg/apis/monitoring" | awk '{print $2}')

if [ -n "${PROMETHEUS_OPERATOR_VERSION}" ]; then
    echo "Applying Prometheus Operator CRDs from ${PROMETHEUS_OPERATOR_VERSION}..."
    ./cluster-up/kubectl.sh apply -f "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/${PROMETHEUS_OPERATOR_VERSION}/example/prometheus-operator-crd/monitoring.coreos.com_servicemonitors.yaml"
    ./cluster-up/kubectl.sh apply -f "https://raw.githubusercontent.com/prometheus-operator/prometheus-operator/${PROMETHEUS_OPERATOR_VERSION}/example/prometheus-operator-crd/monitoring.coreos.com_prometheusrules.yaml"
else
    echo "Warning: Could not determine Prometheus Operator version from ${OPERATOR_GO_MOD_URL}"
fi

go install gotest.tools/gotestsum@latest
#export UPGRADE_FROM=$(curl -s https://github.com/kubevirt/hostpath-provisioner-operator/releases/latest | grep -o "v[0-9]\.[0-9]*\.[0-9]*")
#The upgrade from code path doesn't setup the webhook and cert-manager, only the code after the upgrade does. This is a critical test so we want
#to fix the version to 0.10.0.
export UPGRADE_FROM=v0.10.0
echo "Upgrading from verions: $UPGRADE_FROM"

make cluster-sync
make test-functional
