#!/usr/bin/env bash

#Copyright 2019 The CDI Authors.
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

# NOTE: Not using pipefail because gofmt returns 0 when it finds
# suggestions and 1 when files are clean

SOURCE_DIRS="controller cmd"
LINTABLE=(cmd)
ec=0
out="$(gofmt -l -s ${SOURCE_DIRS} | grep ".*\.go")"
if [[ ${out} ]]; then
    echo "FAIL: Format errors found in the following files:"
    echo "${out}"
    ec=1
fi
for p in "${LINTABLE[@]}"; do
  echo "running go vet on directory: ${p}"
  go vet ${p}/...
done

exit ${ec}
