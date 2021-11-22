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
DOCKER_REPO=${DOCKER_REPO:-registry:5000}
script_dir="$(cd "$(dirname "$0")" && pwd -P)"
source "${script_dir}"/common.sh
setGoInProw $GOLANG_VER

echo "docker repo: [$DOCKER_REPO]"
go test -o _out/sanity.test -c -v ./sanity/...
docker build -t ${DOCKER_REPO}/sanity:test -f ./sanity/Dockerfile .
# Need privileged so we can bind mount inside container, and hostpath capacity cannot change, so skipping that test
docker run --privileged ${DOCKER_REPO}/sanity:test -ginkgo.noColor -ginkgo.skip="should fail when requesting to create a volume with already existing name and different capacity"

