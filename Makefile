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

KUBEVIRT_PROVIDER?=k8s-1.25
HPP_IMAGE?=hostpath-provisioner
HPP_CSI_IMAGE?=hostpath-csi-driver
TAG?=latest
DOCKER_REPO?=quay.io/kubevirt
ARTIFACTS_PATH?=_out
GOLANG_VER?=1.19.4
GOOS?=linux
GOARCH?=amd64
BUILDAH_PLATFORM_FLAG?=--platform $(GOOS)/$(GOARCH)
OCI_BIN ?= $(shell if podman ps >/dev/null 2>&1; then echo podman; elif docker ps >/dev/null 2>&1; then echo docker; fi)

export GOLANG_VER
export KUBEVIRT_PROVIDER
export DOCKER_REPO
export GOOS
export GOARCH
export OCI_BIN

all: controller hostpath-provisioner

hostpath-provisioner:
	./hack/build-provisioner.sh

hostpath-csi-driver:
	./hack/build-csi.sh

image: image-controller image-csi

push: clean manifest manifest-push

manifest: manifest-controller manifest-csi

manifest-push: push-csi push-controller

image-controller: hostpath-provisioner
	buildah build $(BUILDAH_PLATFORM_FLAG) -t $(DOCKER_REPO)/$(HPP_IMAGE):$(GOARCH) -f Dockerfile.controller .

image-csi: hostpath-csi-driver
	buildah build $(BUILDAH_PLATFORM_FLAG) -t $(DOCKER_REPO)/$(HPP_CSI_IMAGE):$(GOARCH) -f Dockerfile.csi .

manifest-controller: image-controller
	-buildah manifest create $(DOCKER_REPO)/$(HPP_IMAGE):local
	buildah manifest add --arch $(GOARCH) $(DOCKER_REPO)/$(HPP_IMAGE):local containers-storage:$(DOCKER_REPO)/$(HPP_IMAGE):$(GOARCH)

manifest-csi: image-csi
	-buildah manifest create $(DOCKER_REPO)/$(HPP_CSI_IMAGE):local
	buildah manifest add --arch $(GOARCH) $(DOCKER_REPO)/$(HPP_CSI_IMAGE):local containers-storage:$(DOCKER_REPO)/$(HPP_CSI_IMAGE):$(GOARCH)

push-csi:
	buildah manifest push $(BUILDAH_PUSH_FLAGS) --all $(DOCKER_REPO)/$(HPP_CSI_IMAGE):local docker://$(DOCKER_REPO)/$(HPP_CSI_IMAGE):$(TAG)

push-controller:
	buildah manifest push $(BUILDAH_PUSH_FLAGS) --all $(DOCKER_REPO)/$(HPP_IMAGE):local docker://$(DOCKER_REPO)/$(HPP_IMAGE):$(TAG)

clean: manifest-clean
	rm -rf _out

manifest-clean:
	-buildah manifest rm $(DOCKER_REPO)/$(HPP_IMAGE):local
	-buildah manifest rm $(DOCKER_REPO)/$(HPP_CSI_IMAGE):local

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
	go version && gotestsum --format short-verbose --junitfile ${ARTIFACTS_PATH}/junit.functest.xml -- ./tests/... -kubeconfig="../_ci-configs/$(KUBEVIRT_PROVIDER)/.kubeconfig"

test-sanity:
	hack/sanity.sh
