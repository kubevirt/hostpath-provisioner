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
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_checkAvailableSpace(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	} ()
	t.Run("0 space left", func(t *testing.T) {
		getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
			return 0, 0, 0, 0, 0, 0, nil
		}
		res, msg := checkIfSpaceAvailable("/test")
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("No space left on device"))
	})

	t.Run("0 inodes left", func(t *testing.T) {
		getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
			return 1000, 0, 0, 0, 0, 0, nil
		}
		res, msg := checkIfSpaceAvailable("/test")
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("No inodes remaining on device"))
	})
	t.Run("pv stats error", func(t *testing.T) {
		getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
			return 1000, 0, 0, 0, 100, 0, fmt.Errorf("pv stats error")
		}
		res, msg := checkIfSpaceAvailable("/test")
		Expect(res).To(BeFalse())
		Expect(msg).To(ContainSubstring("pv stats error"))
	})
	t.Run("valid result", func(t *testing.T) {
		getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
			return 1000, 0, 0, 0, 100, 0, nil
		}
		res, msg := checkIfSpaceAvailable("/test")
		Expect(res).To(BeTrue())
		Expect(msg).To(BeEmpty())
	})
}

func Test_doHealthCheckInControllerSide(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	} ()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 1000, 0, 0, 0, 100, 0, nil
	}
	t.Run("valid health check", func(t *testing.T) {
		res, _ := doHealthCheckInControllerSide(tempDir)
		Expect(res).To(BeTrue())
	})
	t.Run("missing source path", func(t *testing.T) {
		res, msg := doHealthCheckInControllerSide("/invalid")
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("The source path of the volume doesn't exist"))
	})
}