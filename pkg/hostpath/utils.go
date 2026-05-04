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
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
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
	csiSocketDir          = "/csi"
	CreateVolumeDirectory = createVolumeDirectoryFunc
	checkPathExist        = checkPathExistFunc
	checkPathIsEmpty      = checkPathIsEmptyFunc
	getFileCreationTime   = getFileCreationTimeFunc
)

type SnapshotProviderType string

const (
	ReflinkProvider SnapshotProviderType = "reflink"
)

// StoragePoolInfo contains the name and path of a storage pool.
type StoragePoolInfo struct {
	Name             string                `json:"name"`
	Path             string                `json:"path"`
	SnapshotPath     *string               `json:"snapshotPath,omitempty"`
	SnapshotProvider *SnapshotProviderType `json:"snapshotProvider,omitempty"`
	Shared           bool                  `json:"shared"`
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

// IndexOfStartingToken returns the index of a matching string, or -1 if not found
func IndexOfStartingToken(value string, list []string) int {
	for i, match := range list {
		if filepath.Base(match) == value {
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

func getStoragePoolNameFromMap(params map[string]string) string {
	if _, ok := params[storagePoolName]; ok {
		return params[storagePoolName]
	}
	return legacyStoragePoolName
}

func getStoragePoolDataDirectories(storagePoolInfo map[string]StoragePoolInfo) ([]string, error) {
	directories := make([]string, 0)
	for _, info := range storagePoolInfo {
		files, err := os.ReadDir(info.Path)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() {
				directories = append(directories, filepath.Join(info.Path, file.Name()))
			}
		}
	}
	sort.Strings(directories)
	return directories, nil
}

// getTLSVersion converts a version string to a tls.Version constant.
// Supported versions: VersionTLS10, VersionTLS11, VersionTLS12, VersionTLS13.
// Returns nil if the version string is not recognized.
func getTLSVersion(versionName string) *uint16 {
	var versions = map[string]uint16{
		"VersionTLS10": tls.VersionTLS10,
		"VersionTLS11": tls.VersionTLS11,
		"VersionTLS12": tls.VersionTLS12,
		"VersionTLS13": tls.VersionTLS13,
	}
	if version, ok := versions[versionName]; ok {
		return &version
	}

	return nil
}

// RunPrometheusServer runs a prometheus server for metrics with TLS support.
// If certFile and keyFile are provided, they will be used for TLS.
// Otherwise, a self-signed certificate will be generated automatically.
// tlsVersion should be "VersionTLS10", "VersionTLS11", "VersionTLS12", or "VersionTLS13" (default: "VersionTLS13").
func RunPrometheusServer(metricsAddr, certFile, keyFile, tlsVersion string) {
	err := metrics.SetupMetrics()
	if err != nil {
		klog.Error(err, "Failed to Setup Prometheus metrics")
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    metricsAddr,
		Handler: mux,
	}

	// Validate certificate configuration
	certProvided := certFile != ""
	keyProvided := keyFile != ""
	if certProvided != keyProvided {
		klog.Warningf("Partial TLS certificate configuration: cert=%q, key=%q. Both --metrics-cert-file and --metrics-key-file must be provided together. Falling back to self-signed certificate.", certFile, keyFile)
	}

	// Parse TLS version (maps string name to crypto/tls MinVersion constant)
	minTLSVersion := getTLSVersion(tlsVersion)
	if minTLSVersion == nil {
		klog.Warningf("Invalid TLS version '%s', defaulting to TLS 1.3", tlsVersion)
		defaultVersion := uint16(tls.VersionTLS13)
		minTLSVersion = &defaultVersion
	}

	// Configure TLS
	tlsConfig := &tls.Config{
		MinVersion: *minTLSVersion,
	}

	// If cert and key files are provided, use them
	if certProvided && keyProvided {
		klog.V(1).Infof("Using provided certificates for metrics server: cert=%s, key=%s", certFile, keyFile)
		server.TLSConfig = tlsConfig
	} else {
		// Generate self-signed certificate
		klog.V(1).Info("Generating self-signed certificate for metrics server")
		cert, err := generateSelfSignedCert()
		if err != nil {
			klog.Error(err, "Failed to generate self-signed certificate for metrics server")
			return
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
		server.TLSConfig = tlsConfig
	}

	go func() {
		var err error
		if certProvided && keyProvided {
			err = server.ListenAndServeTLS(certFile, keyFile)
		} else {
			// Use the self-signed cert from TLSConfig
			err = server.ListenAndServeTLS("", "")
		}
		if err != nil && err != http.ErrServerClosed {
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

func evaluateSharedPathMetric(storagePoolDataDir map[string]StoragePoolInfo) {
	pathShared := false
	for k, v := range storagePoolDataDir {
		if checkVolumePathSharedWithOS(v.Path) {
			pathShared = true
			klog.V(1).Infof("pool (%s, %s), shares path with OS which can lead to node disk pressure", k, v.Path)
		}
		if v.SnapshotPath != nil {
			if checkVolumePathSharedWithOS(*v.SnapshotPath) {
				pathShared = true
				klog.V(1).Infof("pool (%s, %s), shares path with OS which can lead to node disk pressure", k, *v.SnapshotPath)
			}
		}
	}
	if pathShared {
		metrics.SetPoolPathSharedWithOs(1)
	} else {
		metrics.SetPoolPathSharedWithOs(0)
	}
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
