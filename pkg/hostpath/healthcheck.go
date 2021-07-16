/*
Copyright 2021 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"os/exec"

	"k8s.io/klog/v2"
	"k8s.io/kubernetes/pkg/volume/util/fs"
)

type MountPointInfo struct {
	Target              string           `json:"target"`
	Source              string           `json:"source"`
	FsType              string           `json:"fstype"`
	Options             string           `json:"options"`
}

var (
	checkMountPointExistsFunc = checkMountPointExist
	getPVStatsFunc = getPVStats
)

type FileSystems struct {
	Filsystem []MountPointInfo `json:"filesystems"`
}

func parseMountInfo(originalMountInfo []byte) ([]MountPointInfo, error) {
	fs := FileSystems{
		Filsystem: make([]MountPointInfo, 0),
	}

	if err := json.Unmarshal(originalMountInfo, &fs); err != nil {
		return nil, err
	}

	if len(fs.Filsystem) <= 0 {
		return nil, fmt.Errorf("failed to get mount info")
	}

	return fs.Filsystem, nil
}

func checkMountPointExist(volumePath string) (bool, error) {
	cmdPath, err := exec.LookPath("findmnt")
	if err != nil {
		return false, fmt.Errorf("findmnt not found: %w", err)
	}

	out, err := exec.Command(cmdPath, volumePath, "--json").CombinedOutput()
	if err != nil {
		return false, err
	}

	if len(out) < 1 {
		return false, fmt.Errorf("mount point info is nil")
	}

	mountInfos, err := parseMountInfo([]byte(out))
	if err != nil {
		return false, fmt.Errorf("failed to parse the mount infos: %+v", err)
	}

	for _, mountInfo := range mountInfos {
		if mountInfo.Target == volumePath {
			return true, nil
		}
	}

	return false, nil
}

func getPVStats(volumePath string) (available int64, capacity int64, used int64, inodes int64, inodesFree int64, inodesUsed int64, err error) {
	return fs.Info(volumePath)
}

func checkPVUsage(volumePath string) (int64, int64, error) {
	fsavailable, capacity, _, _, inodesFree, _, err := getPVStatsFunc(volumePath)
	if err != nil {
		return fsavailable, inodesFree, err
	}

	klog.V(3).Infof("fs available: %+v, total capacity: %d, percentage available: %.2f, number of free inodes: %d", fsavailable, capacity, float64(fsavailable) / float64(capacity) * 100, inodesFree)
	return fsavailable, inodesFree, nil
}

func doHealthCheckInControllerSide(volumePath string) (bool, string) {
	spExist, err := checkPathExist(volumePath)
	if err != nil {
		return false, err.Error()
	}

	if !spExist {
		return false, "The source path of the volume doesn't exist"
	}

	return checkIfSpaceAvailable(volumePath)
}

func doHealthCheckInNodeSide(volumePath string) (bool, string) {
	mpExist, err := checkMountPointExistsFunc(volumePath)
	if err != nil {
		return false, err.Error()
	}

	if !mpExist {
		return false, "The volume isn't mounted"
	}

	return checkIfSpaceAvailable(volumePath)
}

func checkIfSpaceAvailable(volumePath string) (bool, string) {
	fsAvailable, inodesFree, err := checkPVUsage(volumePath)
	if err != nil {
		return false, err.Error()
	}

	if fsAvailable == 0 {
		return false, "No space left on device"
	}
	if inodesFree == 0 {
		return false, "No inodes remaining on device"
	}
	return true, ""
}