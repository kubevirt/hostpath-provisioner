/*
Copyright 2021 The hostpath provisioner Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package hostpath

import (
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func TestRemoveProjectIDMissingPathIsIdempotent(t *testing.T) {
	RegisterTestingT(t)

	Expect(removeProjectID("/no/such/pvc-dir")).To(Succeed())
}

func TestParseHardLimitField(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want int64
	}{
		{
			name: "ignores headers and parses last data line",
			out:  "Blocks quota limit grace\n# comment\n1001 12 0 2048 -\n",
			want: 2048,
		},
		{
			name: "parses xfs byte output",
			out:  "Project ID 1001\n1001 4096 0 10485760 0\n",
			want: 10485760,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := parseHardLimitField([]byte(test.out))
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(test.want))
		})
	}
}

func TestParseHardLimitFieldRejectsMalformedOutput(t *testing.T) {
	RegisterTestingT(t)

	_, err := parseHardLimitField([]byte("quota header only\n"))
	Expect(err).To(HaveOccurred())
}

func TestRemoveProjectIDExistingUnsupportedFilesystem(t *testing.T) {
	RegisterTestingT(t)

	path, err := os.MkdirTemp("", "hpp-quota-dir")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(path)

	// Temporary test filesystems normally do not expose the fsxattr ioctl.
	// A real ioctl error must be returned rather than silently reporting that
	// the project ID was cleared.
	Expect(removeProjectID(path)).To(HaveOccurred())
}
