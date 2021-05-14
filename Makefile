# Copyright 2016 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

.PHONY: cluster-up cluster-down cluster-sync cluster-clean

KUBEVIRT_PROVIDER?=k8s-1.20
HPP_IMAGE?=hostpath-provisioner
TAG?=latest
DOCKER_REPO?=kubevirt
ARTIFACTS_PATH?=_out

all: controller hostpath-provisioner

controller:
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' controller

hostpath-provisioner: controller
	CGO_ENABLED=0 go build -a -ldflags '-extldflags "-static"' -o _out/hostpath-provisioner cmd/provisioner/hostpath-provisioner.go

image: hostpath-provisioner
	docker build -t $(DOCKER_REPO)/$(HPP_IMAGE):$(TAG) -f Dockerfile .

push: hostpath-provisioner image
	docker push $(DOCKER_REPO)/$(HPP_IMAGE):$(TAG)

clean:
	rm -rf _out

build: clean dep controller hostpath-provisioner

cluster-up:
	KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} ./cluster-up/up.sh

cluster-down: 
	KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} ./cluster-up/down.sh

cluster-sync: cluster-clean
	KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} ./cluster-sync/sync.sh

cluster-clean:
	KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} ./cluster-sync/clean.sh

test:
	go test -v ./cmd/... ./controller/...
	hack/run-lint-checks.sh

test-functional:
	KUBEVIRT_PROVIDER=${KUBEVIRT_PROVIDER} gotestsum --format short-verbose --junitfile ${ARTIFACTS_PATH}/junit.functest.xml -- ./tests/... -master="" -kubeconfig="../_ci-configs/$(KUBEVIRT_PROVIDER)/.kubeconfig"
