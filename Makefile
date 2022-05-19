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

KUBEVIRT_PROVIDER?=k8s-1.23
HPP_IMAGE?=hostpath-provisioner
HPP_CSI_IMAGE?=hostpath-csi-driver
TAG?=latest
DOCKER_REPO?=kubevirt
ARTIFACTS_PATH?=_out
GOLANG_VER?=1.18.2

export GOLANG_VER
export KUBEVIRT_PROVIDER
export DOCKER_REPO

all: controller hostpath-provisioner

hostpath-provisioner:
	./hack/build-provisioner.sh

hostpath-csi-driver:
	./hack/build-csi.sh

image: image-controller image-csi

push: push-controller push-csi

push-controller: hostpath-provisioner image
	docker push $(DOCKER_REPO)/$(HPP_IMAGE):$(TAG)

image-controller: hostpath-provisioner
	docker build -t $(DOCKER_REPO)/$(HPP_IMAGE):$(TAG) -f Dockerfile.controller .

image-csi: hostpath-csi-driver
	docker build -t $(DOCKER_REPO)/$(HPP_CSI_IMAGE):$(TAG) -f Dockerfile.csi .

push-csi: hostpath-csi-driver image-csi
	docker push $(DOCKER_REPO)/$(HPP_CSI_IMAGE):$(TAG)

clean:
	rm -rf _out

build: clean hostpath-provisioner hostpath-csi-driver

cluster-up:
	./cluster-up/up.sh

cluster-down: 
	./cluster-up/down.sh

cluster-sync: cluster-clean
	./cluster-sync/sync.sh

cluster-clean:
	./cluster-sync/clean.sh

test:
	./hack/run-unit-test.sh
	hack/language.sh

test-functional:
	gotestsum --format short-verbose --junitfile ${ARTIFACTS_PATH}/junit.functest.xml -- ./tests/... -kubeconfig="../_ci-configs/$(KUBEVIRT_PROVIDER)/.kubeconfig"

test-sanity:
	hack/sanity.sh
