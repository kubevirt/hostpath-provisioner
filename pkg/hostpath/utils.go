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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog/v2"

	"kubevirt.io/hostpath-provisioner/pkg/monitoring/metrics"
)

const (
	// The storagePool field name in the storage class arguments.
	storagePoolName       = "storagePool"
	legacyStoragePoolName = "legacy"
)

var (
	csiSocketDir = "/csi"
)

// StoragePoolInfo contains the name and path of a storage pool.
type StoragePoolInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

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

// CreateVolume allocates creates the directory for the hostpath volume
//
// It returns the err if one occurs. That error is suitable as result of a gRPC call.
func CreateVolume(base, volID string) error {
	path := filepath.Join(base, volID)

	err := os.MkdirAll(path, 0777)
	if err != nil {
		return err
	}
	klog.V(4).Infof("adding hostpath volume: %s", volID)
	return nil
}

// DeleteVolume deletes the directory for the hostpath volume.
func DeleteVolume(base, volID string) error {
	klog.V(4).Infof("starting to delete hostpath volume: %s", volID)

	path := filepath.Join(base, volID)
	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	klog.V(4).Infof("deleted hostpath volume: %s", volID)
	return nil
}

// IndexOfStartingToken returns the index of a matching string, or -1 if not found
func IndexOfStartingToken(value string, list []string) int {
	for i, match := range list {
		if filepath.Base(match) == value {
			return i
		}
	}
	return -1
}

func checkPathExist(path string) (bool, error) {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}

		return false, err
	}

	return true, nil
}

func getStoragePoolNameFromMap(params map[string]string) string {
	if _, ok := params[storagePoolName]; ok {
		return params[storagePoolName]
	}
	return legacyStoragePoolName
}

func getVolumeDirectories(storagePoolDataDirs map[string]string) ([]string, error) {
	directories := make([]string, 0)
	for _, path := range storagePoolDataDirs {
		files, err := ioutil.ReadDir(path)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() {
				directories = append(directories, filepath.Join(path, file.Name()))
			}
		}
	}
	sort.Strings(directories)
	return directories, nil
}

// RunPrometheusServer runs a prometheus server for metrics
func RunPrometheusServer(metricsAddr string) {
	err := metrics.SetupMetrics()
	if err != nil {
		klog.Error(err, "Failed to Setup Prometheus metrics")
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	server := http.Server{
		Addr:    metricsAddr,
		Handler: mux,
	}

	go func() {
		err := server.ListenAndServe()
		if err != nil {
			klog.Error(err, "Failed to start Prometheus metrics endpoint server")
		}
	}()
}

func getMountInfos(args ...string) ([]MountPointInfo, error) {
	cmdPath, err := exec.LookPath("findmnt")
	if err != nil {
		return nil, fmt.Errorf("findmnt not found: %w", err)
	}

	args = append(args, "--json")
	out, err := exec.Command(cmdPath, args...).CombinedOutput()
	if err != nil {
		return nil, err
	}

	if len(out) < 1 {
		return nil, fmt.Errorf("mount point info is nil")
	}

	mountInfos, err := parseMountInfo([]byte(out))
	if err != nil {
		return nil, fmt.Errorf("failed to parse the mount infos: %+v", err)
	}

	return mountInfos, nil
}

func checkVolumePathSharedWithOS(volumePath string) bool {
	mountInfosForPath, err := getMountInfos("-T", volumePath)
	if err != nil {
		return false
	}

	mountInfosForCsiSocketDir, err := getMountInfos("-T", csiSocketDir)
	if err != nil {
		return false
	}

	if len(mountInfosForPath) != 1 || len(mountInfosForCsiSocketDir) != 1 {
		return false
	}

	pathSource := extractDeviceFromMountInfoSource(mountInfosForPath[0].Source)
	csiSocketSource := extractDeviceFromMountInfoSource(mountInfosForCsiSocketDir[0].Source)

	return pathSource == csiSocketSource
}

func extractDeviceFromMountInfoSource(source string) string {
	if strings.Contains(source, "[") {
		return strings.Split(source, "[")[0]
	}
	return source
}

func evaluateSharedPathMetric(storagePoolDataDir map[string]string) {
	pathShared := false
	for k, v := range storagePoolDataDir {
		if checkVolumePathSharedWithOS(v) {
			pathShared = true
			klog.V(1).Infof("pool (%s, %s), shares path with OS which can lead to node disk pressure", k, v)
		}
	}
	if pathShared {
		metrics.SetPoolPathSharedWithOs(1)
	} else {
		metrics.SetPoolPathSharedWithOs(0)
	}
}
