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
source ./cluster-up/hack/common.sh
source ./cluster-up/cluster/${KUBEVIRT_PROVIDER}/provider.sh

export KUBEVIRT_NUM_NODES=2
export KUBEVIRT_PROVIDER=k8s-1.21
make cluster-down
make cluster-up

if ! command -v go &> /dev/null
then
  wget https://dl.google.com/go/go1.16.7.linux-amd64.tar.gz
  tar -xzf go1.16.7.linux-amd64.tar.gz
  export GOROOT=$PWD/go
  export PATH=$GOROOT/bin:$PATH
  echo $PATH
fi

if ! command -v sshuttle &> /dev/null
then
  #Setup sshutle
  dnf install -y sshuttle

  docker_id=($(docker ps | grep vm | awk '{print $1}'))
  echo "docker node: [${docker_id[0]}]"

  #Get the key to connect.
  docker cp ${docker_id[0]}:/vagrant.key ./vagrant.key
  md5sum ./vagrant.key

  #Install python 3 on each node so sshuttle will work
  for i in $(seq 1 ${KUBEVIRT_NUM_NODES}); do
    ./cluster-up/ssh.sh "node$(printf "%02d" ${i})" "sudo dnf install -y python39"
  done
  #Look up the ssh port
  ssh_port=$(./cluster-up/cli.sh ports ssh)
  echo "ssh port: ${ssh_port}"
  #Start sshuttle
  sshuttle -r vagrant@localhost:${ssh_port} 192.168.66.0/24 -e 'ssh -o StrictHostKeyChecking=no -i ./vagrant.key'&
  SSHUTTLE_PID=$!
  function finish() {
    echo "TERMINATING SSHUTTLE!!!!"
    kill $SSHUTTLE_PID
  }
  trap finish EXIT
fi

echo "install hpp"
registry=${IMAGE_REGISTRY:-localhost:$(_port registry)}
echo "registry: ${registry}"
DOCKER_REPO=${registry} make push

#install hpp
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/namespace.yaml
_kubectl apply -f deploy/tests/operator.yaml -n hostpath-provisioner
_kubectl apply -f deploy/tests/hostpathprovisioner_cr.yaml
_kubectl apply -f https://raw.githubusercontent.com/kubevirt/hostpath-provisioner-operator/main/deploy/storageclass-wffc-csi.yaml
#Wait for hpp to be available.
_kubectl wait hostpathprovisioners.hostpathprovisioner.kubevirt.io/hostpath-provisioner --for=condition=Available --timeout=480s

export KUBE_SSH_KEY_PATH=./vagrant.key
export KUBE_SSH_USER=vagrant

echo "KUBE_SSH_USER=${KUBE_SSH_USER}, KEY_FILE=${KUBE_SSH_KEY_PATH}"
#Download test
curl --location https://dl.k8s.io/v1.21.0/kubernetes-test-linux-amd64.tar.gz |   tar --strip-components=3 -zxf - kubernetes/test/bin/e2e.test kubernetes/test/bin/ginkgo
#Run test
# Some of these tests assume immediate binding, which is a random node, however if multiple volumes are involved sometimes they end up on different nodes and the test fails. Excluding that test.
./e2e.test -ginkgo.v -ginkgo.focus='External.Storage.*kubevirt.io.hostpath-provisioner' -ginkgo.skip='immediate binding|External.Storage.*should access to two volumes with the same volume mode and retain data across pod recreation on the same node \[LinuxOnly\]' -storage.testdriver=./hack/test-driver.yaml -provider=local

