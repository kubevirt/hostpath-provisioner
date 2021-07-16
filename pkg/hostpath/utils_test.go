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
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

const (
	validVolId = "valid"
	invalidVolId = "invalid"
)

func Test_roundDownCapacityPretty(t *testing.T) {
	RegisterTestingT(t)
	type args struct {
		size int64
	}

	tests := []struct {
		name    string
		args    args
		want    int64
	}{
		{
			name: "Rounds Gigs properly",
			args: args{
				size: int64(2 * gib),
			},
			want:    int64(2 * gib),
		},
		{
			name: "Rounds Gigs properly with minor add",
			args: args{
				size: int64((2 * gib) + 2),
			},
			want:    int64(2 * gib),
		},
		{
			name: "Not large enough for GiB, rounded down to smaller MiB",
			args: args{
				size: int64((2 * gib) - 2),
			},
			want:    int64(2047 * mib),
		},
		{
			name: "Large GiB, rounded down to one smaller GiB",
			args: args{
				size: int64((20 * gib) - 2),
			},
			want:    int64(19 * gib),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := roundDownCapacityPretty(tt.args.size)
			Expect(got).To(Equal(tt.want), "calculatePvCapacity() = %d, want %d", got, tt.want)
		})
	}
}

func Test_UtilCreateVolume(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Errorf("Failed to create tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	tests := []struct {
		name string
		base string
		volId string
		want bool
	} {
		{
			name: "validVolId",
			base: tempDir,
			volId: validVolId,
			want: false,
		},
		{
			name: "invalidVolId",
			base: "/notvalid",
			volId: invalidVolId,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CreateVolume(tt.base, tt.volId)
			res := err != nil
			Expect(res).To(Equal(tt.want), "CreateVolume(%s, %s), returned %v, want %v", tt.base, tt.volId, res, tt.want)
		})
	}
}

func Test_DeleteVolume(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Errorf("Failed to create tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("validVolId", func(t *testing.T) {
		err := CreateVolume(tempDir, validVolId)
		Expect(err).ToNot(HaveOccurred())
		err = DeleteVolume(tempDir, validVolId)
		Expect(err).ToNot(HaveOccurred())
	})
	t.Run("invalidVolId", func(t *testing.T) {
		err = DeleteVolume("/dev", "null")
		Expect(err).To(HaveOccurred())
	})
}