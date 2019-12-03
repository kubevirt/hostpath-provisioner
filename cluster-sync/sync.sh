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

if [ ! -z $UPGRADE_FROM ]; then
  _kubectl apply -f https://github.com/kubevirt/hostpath-provisioner-operator/releases/download/$UPGRADE_FROM/namespace.yaml
  _kubectl apply -f https://github.com/kubevirt/hostpath-provisioner-operator/releases/download/$UPGRADE_FROM/operator.yaml -n hostpath-provisioner
  cat <<EOF | _kubectl apply -f -
apiVersion: hostpathprovisioner.kubevirt.io/v1alpha1
kind: HostPathProvisioner
metadata:
  name: hostpath-provisioner
spec:
  imagePullPolicy: Always
  imageRegistry: quay.io/kubevirt
  imageTag: $UPGRADE_FROM
  pathConfig:
    path: "/var/hpvolumes"
    useNamingPrefix: "false"
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

_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/namespace.yaml
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/operator.yaml -n hostpath-provisioner

# Remove deployment
#_kubectl delete deployment hostpath-provisioner-operator -n hostpath-provisioner --ignore-not-found
# Redeploy with the correct image name.
  cat <<EOF | _kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: hostpath-provisioner-operator
  namespace: hostpath-provisioner
spec:
  replicas: 1
  selector:
    matchLabels:
      name: hostpath-provisioner-operator
  template:
    metadata:
      labels:
        name: hostpath-provisioner-operator
    spec:
      serviceAccountName: hostpath-provisioner-operator
      containers:
        - name: hostpath-provisioner-operator
          # Replace this with the built image name
          image: quay.io/kubevirt/hostpath-provisioner-operator:latest
          command:
          - hostpath-provisioner-operator
          imagePullPolicy: Always
          env:
            - name: WATCH_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: OPERATOR_NAME
              value: "hostpath-provisioner-operator"
            - name: PROVISIONER_IMAGE
              value: "registry:5000/hostpath-provisioner"
EOF

  cat <<EOF | _kubectl apply -f -
apiVersion: hostpathprovisioner.kubevirt.io/v1alpha1
kind: HostPathProvisioner
metadata:
  name: hostpath-provisioner
spec:
  imagePullPolicy: Always
  pathConfig:
    path: "/var/hpvolumes"
    useNamingPrefix: "false"
EOF

_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/master/deploy/storageclass-wffc.yaml


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
if [ $retry_counter -eq 10 ]; then
echo "Unable to deploy to latest version"
hpp_obj=$(_kubectl get Hostpathprovisioner -o yaml)
echo $hpp_obj
exit 1
fi
