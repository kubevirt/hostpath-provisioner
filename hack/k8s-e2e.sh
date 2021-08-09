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
export KUBEVIRT_NUM_NODES=2
export KUBEVIRT_PROVIDER=k8s-1.21
make cluster-down
make cluster-up

#Setup sshutle
#Get the key to connect.
docker cp $(docker ps | grep vm | awk '{print $1}'):/vagrant.key ./vagrant.key
#Install python 3 on each node so sshuttle will work
for i in $(seq 1 ${KUBEVIRT_NUM_NODES}); do
  ./cluster-up/ssh.sh "node$(printf "%02d" ${i})" "sudo dnf install python39"
done
#Look up the ssh port
ssh_port = $(./cluster-up/cli.sh ports $KUBEVIRT_PROVIDER ssh)
#Start sshuttle
sshuttle -r vagrant@localhost:$ssh_port 192.168.66.0/24 -e 'ssh -i ./vagrant.key'&
SSHUTTLE_PID=$!
function finish() {
  kill $SSHUTTLE_PID
}
trap finish EXIT

#Download test
curl --location https://dl.k8s.io/v1.21.0/kubernetes-test-linux-amd64.tar.gz |   tar --strip-components=3 -zxf - kubernetes/test/bin/e2e.test kubernetes/test/bin/ginkgo
#Run test
./e2e.test -ginkgo.v -ginkgo.focus='External.Storage' -storage.testdriver=./hack/test-driver.yaml -provider=local

