/*
Copyright 2026 The hostpath provisioner Authors.

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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/klog/v2"
)

type SnapshotProvider interface {
	// Initialize initialize the provider.
	Initialize() error
	// GetSnapshotById gets the snapshot meta data of the specified snapshot id.
	GetSnapshotById(snapshotId string) (*csi.Snapshot, error)
	// GetSnapshotsByVolumeSourceId gets the snapshot meta data of the snapshots associated with the volume source id. All snapshots of a volume
	GetSnapshotsByVolumeSourceId(volumeSourceId string) ([]csi.Snapshot, error)
	// GetAllSnapshots gets all the snapshot meta data
	GetAllSnapshots() ([]csi.Snapshot, error)
	// CreateSnapshot creates a snapshot.
	CreateSnapshot(snapshotId, sourceVolumeId string) (*csi.Snapshot, error)
	// DeleteSnapshot removes a snapshot
	DeleteSnapshot(snapshotId string) error
	// RestoreSnapshot restores the content of the snapshot into the target path
	RestoreSnapshot(snapshotId, targetPath string) error
}

type Reflink struct {
	nodeName   string
	path       string
	sourcePath string
}

var (
	CopyReflinkFunc = reflinkCopy
)

const (
	dataPath = "data"
)

func reflinkCopy(src, dst string) error {
	cmd := exec.Command("cp", "-rp", "--reflink=auto", src, dst)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %w", src, dst, err)
	}
	return nil
}

func (r *Reflink) Initialize() error {
	if err := ensurePathExists(r.path); err != nil {
		return err
	}
	klog.V(1).Info("Successfully created reflink snapshot repo")
	return nil
}

func (r *Reflink) GetSnapshotById(snapshotId string) (*csi.Snapshot, error) {
	info, err := os.Stat(filepath.Join(r.path, snapshotId))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("file %s is not a directory", filepath.Join(r.path, snapshotId))
	}
	sourceVolumeId, err := r.getSourceVolumeId(snapshotId)
	if err != nil {
		return nil, err
	}
	st := info.Sys().(*syscall.Stat_t)
	return &csi.Snapshot{
		SnapshotId:     snapshotId,
		SourceVolumeId: string(sourceVolumeId),
		CreationTime:   timestamppb.New(time.Unix(int64(st.Ctim.Sec), int64(st.Ctim.Nsec))),
		ReadyToUse:     true,
	}, nil
}

func (r *Reflink) getSourceVolumeId(snapshotId string) (string, error) {
	entries, err := os.ReadDir(filepath.Join(r.path, snapshotId))
	if err != nil {
		return "", err
	}

	for _, entry := range entries {
		if !entry.IsDir() || entry.Name() == dataPath {
			continue
		}
		return entry.Name(), nil
	}
	return "", fmt.Errorf("source volume not found for snapshot %s", snapshotId)
}

func (r *Reflink) GetSnapshotsByVolumeSourceId(volumeSourceId string) ([]csi.Snapshot, error) {
	snapshots, err := r.GetAllSnapshots()
	if err != nil {
		return nil, err
	}
	res := make([]csi.Snapshot, 0)
	for _, snapshot := range snapshots {
		if snapshot.SourceVolumeId == volumeSourceId {
			res = append(res, snapshot)
		}
	}
	return res, nil
}

func (r *Reflink) GetAllSnapshots() ([]csi.Snapshot, error) {
	entries, err := os.ReadDir(r.path)
	if err != nil {
		return nil, err
	}

	snapshots := make([]csi.Snapshot, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		snapshot, err := r.GetSnapshotById(entry.Name())
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, *snapshot)
	}
	return snapshots, nil
}

func (r *Reflink) CreateSnapshot(snapshotId, sourceVolumeId string) (*csi.Snapshot, error) {
	// Check if the source volume exists, so we can snapshot it.
	sourcePoolDir := filepath.Join(r.sourcePath, sourceVolumeId)
	if exists, err := checkPathExist(sourcePoolDir); err != nil {
		return nil, err
	} else if exists {
		snapshotDir := filepath.Join(r.path, snapshotId, dataPath)
		sourceDir := filepath.Join(r.path, snapshotId, sourceVolumeId)
		snapshotExists, err := checkPathExist(snapshotDir)
		if err != nil {
			return nil, err
		} else if snapshotExists {
			return r.createSnapshotFromDir(snapshotId, sourceVolumeId, snapshotDir)
		}
		if err := os.MkdirAll(snapshotDir, 0755); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			return nil, err
		}
		// Use "/." to copy contents of source PVC into snapshot data dir, not the PVC directory itself
		sourcePoolDirContents := sourcePoolDir + "/."
		if err := CopyReflinkFunc(sourcePoolDirContents, snapshotDir); err != nil {
			return nil, err
		}
		return r.createSnapshotFromDir(snapshotId, sourceVolumeId, snapshotDir)
	}
	return nil, fmt.Errorf("source volume %s not found, unable to create snapshot", sourcePoolDir)
}

func (r *Reflink) createSnapshotFromDir(snapshotId, sourceVolumeId, path string) (*csi.Snapshot, error) {
	creationTime, err := getFileCreationTime(path)
	if err != nil {
		return nil, err
	}
	return &csi.Snapshot{
		SnapshotId:     snapshotId,
		SourceVolumeId: sourceVolumeId,
		ReadyToUse:     true,
		CreationTime:   timestamppb.New(*creationTime),
	}, nil
}

func (r *Reflink) DeleteSnapshot(snapshotId string) error {
	snapPath := filepath.Join(r.path, snapshotId)
	if exists, err := checkPathExist(snapPath); err != nil {
		return err
	} else if exists {
		if err := os.RemoveAll(snapPath); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reflink) RestoreSnapshot(snapshotId, targetPath string) error {
	snapPath := filepath.Join(r.path, snapshotId, dataPath)
	if exists, err := checkPathExist(snapPath); err != nil {
		return err
	} else if exists {
		// Target directory should already exist (created by CreateVolume)
		if exists, err := checkPathExist(targetPath); err != nil {
			return err
		} else if !exists {
			return fmt.Errorf("target path %s does not exist", targetPath)
		}

		// copy the contents of data/ and not the directory itself
		snapPath = snapPath + "/."
		if err := CopyReflinkFunc(snapPath, targetPath); err != nil {
			return err
		}
		return nil
	}
	return status.Errorf(codes.NotFound, "snapshot %s not found, unable to restore into volume", snapshotId)
}

func ensurePathExists(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	return nil
}
