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
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/klog/v2"
)

var (
	// CreateVolumeDirectory allocates creates the directory for the hostpath volume
	CreateVolumeDirectory = createVolumeDirectoryFunc
	checkPathExist = checkPathExistFunc
	checkPathIsEmpty = checkPathIsEmptyFunc
	getFileCreationTime = getFileCreationTimeFunc
)
// roundDownCapacityPretty Round down the capacity to an easy to read value. Blatantly stolen from here: https://github.com/kubernetes-incubator/external-storage/blob/master/local-volume/provisioner/pkg/discovery/discovery.go#L339
func roundDownCapacityPretty(capacityBytes int64) int64 {

	easyToReadUnitsBytes := []int64{gib, mib}

	// Round down to the nearest easy to read unit
	// such that there are at least 10 units at that size.
	for _, easyToReadUnitBytes := range easyToReadUnitsBytes {
		// Round down the capacity to the nearest unit.
		size := capacityBytes / easyToReadUnitBytes
		if size >= 10 {
			return size * easyToReadUnitBytes
		}
	}
	return capacityBytes
}

// CreateSnapshotDirectory allocates creates the directory for the hostpath snapshot
//
// It returns the err if one occurs. That error is suitable as result of a gRPC call.
func CreateSnapshotDirectory(base, snapID string) error {
	return CreateVolumeDirectory(base, snapID)
}

// It returns the err if one occurs. That error is suitable as result of a gRPC call.
func createVolumeDirectoryFunc(base, volID string) error {
	path := filepath.Join(base, volID)

	err := os.MkdirAll(path, 0777)
	if err != nil {
		return err
	}
	klog.V(4).Infof("adding hostpath volume: %s", volID)
	return nil
}

// DeleteVolume deletes the directory for the hostpath volume.
//
// It returns the err if one occurs. That error is suitable as result of a gRPC call.
func DeleteVolume(base, volID string) error {
	klog.V(4).Infof("starting to delete hostpath volume: %s", volID)

	path := filepath.Join(base, volID)
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	klog.V(4).Infof("deleted hostpath volume: %s", volID)
	return nil
}

// IndexOfString returns the index of a matching string, or -1 if not found
func IndexOfString(value string, list []string) int {
	for i, match := range list {
		if match == value {
			return i
		}
	}
	return -1
}

// IndexOfSnapshotId returns the index of a matching snapshotId, or -1 if not found
func IndexOfSnapshotId(value string, list []csi.Snapshot) int {
	for i, match := range list {
		if match.SnapshotId == value {
			return i
		}
	}
	return -1
}

func checkPathExistFunc(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func checkPathIsEmptyFunc(path string) (bool, error) {
	f, err := os.Open(path)
    if err != nil {
        return false, err
    }
    defer f.Close()

    _, err = f.Readdirnames(1)
    if err != io.EOF {
        return false, err
    }
    return true, nil
}

func getFileCreationTimeFunc(file string) (*time.Time, error) {
	var stat syscall.Stat_t
	err := syscall.Stat(file, &stat)
	if err != nil {
		return nil, err
	}
	creationTime := time.Unix(stat.Ctim.Sec, 0)
	return &creationTime, nil
}