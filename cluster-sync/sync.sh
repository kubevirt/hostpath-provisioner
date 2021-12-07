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

KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER:-"k8s-1.22"}
KUBEVIRT_NUM_NODES=${KUBEVIRT_NUM_NODES:-1}
HPP_NAMESPACE=${HPP_NAMESPACE:-"hostpath-provisioner"}

source ./cluster-up/hack/common.sh
source ./cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

for i in $(seq 1 ${KUBEVIRT_NUM_NODES}); do
    ./cluster-up/ssh.sh "node$(printf "%02d" ${i})" "sudo mkdir -p /var/hpvolumes"
    ./cluster-up/ssh.sh "node$(printf "%02d" ${i})" "sudo chcon -t container_file_t -R /var/hpvolumes"
done

registry=${IMAGE_REGISTRY:-localhost:$(_port registry)}
DOCKER_REPO=${registry} make push

if [ ! -z $UPGRADE_FROM ]; then
  _kubectl apply -f https://github.com/kubevirt/hostpath-provisioner-operator/releases/download/$UPGRADE_FROM/namespace.yaml
  _kubectl apply -f https://github.com/kubevirt/hostpath-provisioner-operator/releases/download/$UPGRADE_FROM/operator.yaml -n ${HPP_NAMESPACE}
  cat <<EOF | _kubectl apply -f -
apiVersion: hostpathprovisioner.kubevirt.io/v1beta1
kind: HostPathProvisioner
metadata:
  name: hostpath-provisioner
spec:
  imagePullPolicy: Always
  imageRegistry: quay.io/kubevirt
  imageTag: $UPGRADE_FROM
  pathConfig:
    path: "/var/hpvolumes"
    useNamingPrefix: false
EOF
  _kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/$UPGRADE_FROM/deploy/storageclass-wffc.yaml
  #Wait for it to be available.
  _kubectl wait hostpathprovisioners.hostpathprovisioner.kubevirt.io/hostpath-provisioner --for=condition=Available --timeout=480s

  retry_counter=0
  while [[ $retry_counter -lt 10 ]] && [ "$observed_version" != "$UPGRADE_FROM" ]; do
    observed_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.observedVersion}{"\n"}'`
    target_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.targetVersion}{"\n"}'`
    operator_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.operatorVersion}{"\n"}'`
    echo "observedVersion: $observed_version, operatorVersion: $operator_version, targetVersion: $target_version"
    retry_counter=$((retry_counter + 1))
  sleep 5
  done
  if [ $retry_counter -eq 10 ]; then
	echo "Unable to deploy to version $UPGRADE_FROM"
	hpp_obj=$(_kubectl get Hostpathprovisioner -o yaml)
	echo $hpp_obj
	exit 1
  fi

fi

if [ ${HPP_NAMESPACE} == "hostpath-provisioner" ]; then
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/namespace.yaml
fi
_kubectl apply -f https://github.com/jetstack/cert-manager/releases/download/v1.6.1/cert-manager.yaml
_kubectl wait --for=condition=available -n cert-manager --timeout=120s --all deployments
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/webhook.yaml -n hostpath-provisioner
echo "Deploying"
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/operator.yaml -n ${HPP_NAMESPACE}

echo "Waiting for it to be ready"
_kubectl rollout status -n hostpath-provisioner deployment/hostpath-provisioner-operator --timeout=120s

echo "Updating deployment"
_kubectl get pods -n hostpath-provisioner
# patch the correct development image name.
_kubectl patch deployment hostpath-provisioner-operator -n hostpath-provisioner --patch-file cluster-sync/patch.yaml

_kubectl rollout status -n hostpath-provisioner deployment/hostpath-provisioner-operator --timeout=120s
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/hostpathprovisioner_legacy_cr.yaml
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/storageclass-wffc.yaml
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/storageclass-wffc-legacy-csi.yaml

cat <<EOF | _kubectl apply -f -
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: hostpath-provisioner-immediate
provisioner: kubevirt.io/hostpath-provisioner
reclaimPolicy: Delete
volumeBindingMode: Immediate
EOF
echo "Waiting for hostpath provisioner to be available"
_kubectl wait hostpathprovisioners.hostpathprovisioner.kubevirt.io/hostpath-provisioner --for=condition=Available --timeout=480s

retry_counter=0
while [[ $retry_counter -lt 10 ]] && [ "$observed_version" == "$UPGRADE_FROM" ]; do
  observed_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.observedVersion}{"\n"}'`
  target_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.targetVersion}{"\n"}'`
  operator_version=`_kubectl get Hostpathprovisioner -o=jsonpath='{.items[*].status.operatorVersion}{"\n"}'`
  echo "observedVersion: $observed_version, operatorVersion: $operator_version, targetVersion: $target_version"
  retry_counter=$((retry_counter + 1))
sleep 5
done
if [ $retry_counter -eq 20 ]; then
echo "Unable to deploy to latest version"
hpp_obj=$(_kubectl get hostpathprovisioner -o yaml)
echo $hpp_obj
exit 1
fi

function configure_prometheus {
  if _kubectl get crd prometheuses.monitoring.coreos.com; then
    _kubectl patch prometheus k8s -n monitoring --type=json -p '[{"op": "replace", "path": "/spec/ruleSelector", "value":{}}, {"op": "replace", "path": "/spec/ruleNamespaceSelector", "value":{"matchLabels": {"kubernetes.io/metadata.name": "hostpath-provisioner"}}}]'
  fi
}

configure_prometheus

function check_structural_schema {
  for crd in "$@"; do
    status=$(_kubectl get crd $crd -o jsonpath={.status.conditions[?\(@.type==\"NonStructuralSchema\"\)].status})
    if [ "$status" == "True" ]; then
      echo "ERROR CRD $crd is not a structural schema!, please fix"
      _kubectl get crd $crd -o yaml
      exit 1
    fi
    echo "CRD $crd is a StructuralSchema"
  done
}
check_structural_schema "hostpathprovisioners.hostpathprovisioner.kubevirt.io"
