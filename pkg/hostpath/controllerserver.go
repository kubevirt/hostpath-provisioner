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
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/golang/protobuf/ptypes/wrappers"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
	"k8s.io/utils/exec"
)

const (
	deviceID = "deviceID"
	snapExt = ".tar.std"
)

type hostPathController struct {
	cfg *Config
	snapMutex sync.Mutex
}

func NewHostPathController(config *Config) *hostPathController {
	return &hostPathController {
		cfg: config,
	}
}

func (hpc *hostPathController) validateCreateVolumeRequest(req *csi.CreateVolumeRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "missing request")
	}
	// Check arguments
	if len(req.GetName()) == 0 {
		return status.Error(codes.InvalidArgument, "name missing in request")
	}
	caps := req.GetVolumeCapabilities()
	if caps == nil {
		return status.Error(codes.InvalidArgument, "volume capabilities missing in request")
	}
	// Keep a record of the requested access types.
	var accessTypeMount bool

	for _, cap := range caps {
		if cap.GetMount() != nil {
			accessTypeMount = true
		}
	}

	if !accessTypeMount {
		return status.Error(codes.InvalidArgument, "must have mount access type")
	}
	return nil
}

func (hpc *hostPathController) validateCreateVolumeRequestTopology(req *csi.CreateVolumeRequest) error {
	if req.AccessibilityRequirements != nil {
		for _, requisite := range req.AccessibilityRequirements.Requisite {
			if requisite.Segments[TopologyKeyNode] == hpc.cfg.NodeID {
				return nil
			}
		}
		for _, preferred := range req.AccessibilityRequirements.Preferred {
			if preferred.Segments[TopologyKeyNode] == hpc.cfg.NodeID {
				return nil
			}
		}
		return status.Error(codes.InvalidArgument, "not correct node")
	}
	return nil
}

func (hpc *hostPathController) CreateVolume(ctx context.Context, req *csi.CreateVolumeRequest) (resp *csi.CreateVolumeResponse, finalErr error) {
	if req != nil {
		klog.V(3).Infof("Create Volume Request: %+v", *req)
	}

	if err := hpc.validateCreateVolumeRequest(req); err != nil {
		return nil, err
	}

	if err := hpc.validateCreateVolumeRequestTopology(req); err != nil {
		return nil, err
	}

	capacity, err := hpc.getVolumeDirCapacity()
	if err != nil {
		return nil, err
	}
	topologies := []*csi.Topology{}
	topologies = append(topologies, &csi.Topology{Segments: map[string]string{TopologyKeyNode: hpc.cfg.NodeID}})

	if exists, err := checkPathExist(filepath.Join(hpc.cfg.DataDir, req.GetName())); err != nil {
		return nil, err
	} else if !exists {
		if err := CreateVolumeDirectory(hpc.cfg.DataDir, req.GetName()); err != nil {
			return nil, fmt.Errorf("failed to create volume %v: %w", req.GetName(), err)
		}
		klog.V(4).Infof("created volume %s at path %s", req.GetName(), filepath.Join(hpc.cfg.DataDir, req.GetName()))
	}	

	if req.GetVolumeContentSource() != nil {
		source := req.GetVolumeContentSource()
		switch source.Type.(type) {
		case *csi.VolumeContentSource_Snapshot:
			if snapshot := source.GetSnapshot(); snapshot != nil {
				if err := hpc.restoreFromSnapshot(snapshot.GetSnapshotId(), req.GetName()); err != nil {
					if err := DeleteVolume(hpc.cfg.DataDir, req.GetName()); err != nil {
						return nil, fmt.Errorf("failed to delete volume %v: %w", req.GetName(), err)
					}
					return nil, err
				}
			}
		}
	}
	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:           req.Name,
			CapacityBytes:      capacity,
			VolumeContext:      req.GetParameters(),
			ContentSource:      req.GetVolumeContentSource(),
			AccessibleTopology: topologies,
		},
	}, nil
}

// getVolumeDirCapacity returns the total available space in the directory where the volumes are being created because
// there is nothing stopping anyone from using more space than the requested because it is all shared storage.
func (hpc *hostPathController) getVolumeDirCapacity() (int64, error) {
	_, capacity, _, _, _, _, err := getPVStatsFunc(hpc.cfg.DataDir)
	if err != nil {
		return int64(0), status.Error(codes.Internal, fmt.Sprintf("Unable to determine capacity: %v", err))
	}
	capacity, _ = resource.NewQuantity(int64(roundDownCapacityPretty(capacity)), resource.BinarySI).AsInt64()
	return capacity, nil
}

func (hpc *hostPathController) getSnapshotContentSize(snapName, sourceVolumeId string) (int64, error) {

	snapshotFile := filepath.Join(hpc.cfg.SnapshotDir, snapName, fmt.Sprintf("%s.tar.std", sourceVolumeId))
	cmd := []string{"bash", "-c", fmt.Sprintf("tar -tv --zstd -f %s | sed 's/ \\+/ /g' | cut -f3 -d' ' | sed '2,$s/^/+ /' | paste -sd' ' | bc", snapshotFile)}
	executor := exec.New()
	klog.V(1).Infof("Executing %v", cmd)
	out, err := executor.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return int64(0), fmt.Errorf("failed to determine restore size needed: %v, %s", err, out)
	}
	sizeString := strings.TrimSpace(string(out))
	if sizeString == "" {
		sizeString = "0"
	}
	return strconv.ParseInt(sizeString, 10, 64)
}

func (hpc *hostPathController) validateDeleteVolumeRequest(req *csi.DeleteVolumeRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "missing request")
	}
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}
	return nil
}

func (hpc *hostPathController) DeleteVolume(ctx context.Context, req *csi.DeleteVolumeRequest) (*csi.DeleteVolumeResponse, error) {
	if err := hpc.validateDeleteVolumeRequest(req); err != nil {
		return nil, err
	}

	if err := DeleteVolume(hpc.cfg.DataDir, req.GetVolumeId()); err != nil {
		return nil, fmt.Errorf("failed to delete volume %v: %w", req.GetVolumeId(), err)
	}
	klog.V(4).Infof("volume %v successfully deleted", req.GetVolumeId())

	return &csi.DeleteVolumeResponse{}, nil
}

func (hpc *hostPathController) ControllerGetCapabilities(ctx context.Context, req *csi.ControllerGetCapabilitiesRequest) (*csi.ControllerGetCapabilitiesResponse, error) {
	return &csi.ControllerGetCapabilitiesResponse{
		Capabilities: hpc.getControllerServiceCapabilities(),
	}, nil
}

func (hpc *hostPathController) ValidateVolumeCapabilities(ctx context.Context, req *csi.ValidateVolumeCapabilitiesRequest) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "volume ID not provided")
	}
	if len(req.VolumeCapabilities) == 0 {
		return nil, status.Errorf(codes.InvalidArgument, "volumeCapabilities not provided for %s", req.VolumeId)
	}

	for _, cap := range req.GetVolumeCapabilities() {
		if cap.GetMount() == nil {
			return nil, status.Error(codes.InvalidArgument, "mount type is undefined")
		}
	}

	if exists, err := checkPathExist(filepath.Join(hpc.cfg.DataDir, req.GetVolumeId())); err != nil {
		return nil, err
	} else if !exists {
		return nil, status.Errorf(codes.NotFound, "volume %s not found", req.GetVolumeId())
	}

	return &csi.ValidateVolumeCapabilitiesResponse{
		Confirmed: &csi.ValidateVolumeCapabilitiesResponse_Confirmed{
			VolumeContext:      req.GetVolumeContext(),
			VolumeCapabilities: req.GetVolumeCapabilities(),
			Parameters:         req.GetParameters(),
		},
	}, nil
}

func (hpc *hostPathController) ControllerPublishVolume(ctx context.Context, req *csi.ControllerPublishVolumeRequest) (*csi.ControllerPublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerPublishVolume is not supported")
}

func (hpc *hostPathController) ControllerUnpublishVolume(ctx context.Context, req *csi.ControllerUnpublishVolumeRequest) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "ControllerUnpublishVolume is not supported")
}

func (hpc *hostPathController) GetCapacity(ctx context.Context, req *csi.GetCapacityRequest) (*csi.GetCapacityResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "missing request")
	}
	available, capacity, _, _, _, _, err := getPVStatsFunc(hpc.cfg.DataDir)
	if err != nil {
		return nil, err
	}
	return &csi.GetCapacityResponse{
		AvailableCapacity: available,
		MaximumVolumeSize: &wrappers.Int64Value{Value: capacity},
		MinimumVolumeSize: &wrappers.Int64Value{Value: 0},
	}, nil
}

func (hpc *hostPathController) getVolumeDirectories() ([]string, error) {
	files, err := ioutil.ReadDir(hpc.cfg.DataDir)
	if err != nil {
		return nil, err
	}
	directories := make([]string, 0)
	for _, file := range files {
		if file.IsDir() {
			directories = append(directories, file.Name())
		}
	}
	sort.Strings(directories)
	return directories, nil
}

func (hpc *hostPathController) validateListVolumesRequest(req *csi.ListVolumesRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "missing request")
	}
	if req.MaxEntries < 0 {
		return status.Error(codes.InvalidArgument, "maxEntries < 0")
	}
	return nil
}

func (hpc *hostPathController) ListVolumes(ctx context.Context, req *csi.ListVolumesRequest) (*csi.ListVolumesResponse, error) {
	if err := hpc.validateListVolumesRequest(req); err != nil {
		return nil, err
	}

	volumeRes := &csi.ListVolumesResponse{
		Entries: []*csi.ListVolumesResponse_Entry{},
	}

	capacity, err := hpc.getVolumeDirCapacity()
	if err != nil {
		return nil, err
	}

	volumeDirs, err := hpc.getVolumeDirectories()
	if err != nil {
		return nil, err
	}

	if len(volumeDirs) > 0 {
		if req.StartingToken == "" || req.StartingToken == "0" {
			req.StartingToken = "1"
		}

		volumesLength := int64(len(volumeDirs))
		maxLength := int64(req.MaxEntries)
		if maxLength == 0 {
			maxLength = volumesLength
		}
		start, err := strconv.ParseUint(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Error(codes.Aborted, "The type of startingToken should be integer")
		}
		start = start - 1

		end := int64(start) + maxLength

		if end > volumesLength {
			end = volumesLength
		}

		for _, volumeId := range volumeDirs[start:end] {
			healthy, msg := doHealthCheckInControllerSide(filepath.Join(hpc.cfg.DataDir, volumeId))
			klog.V(3).Infof("Healthy state: %s Volume: %t", volumeId, healthy)
			volumeRes.Entries = append(volumeRes.Entries, &csi.ListVolumesResponse_Entry{
				Volume: &csi.Volume{
					VolumeId:      volumeId,
					CapacityBytes: capacity,
				},
				Status: &csi.ListVolumesResponse_VolumeStatus{
					PublishedNodeIds: []string{hpc.cfg.NodeID},
					VolumeCondition: &csi.VolumeCondition{
						Abnormal: !healthy,
						Message:  msg,
					},
				},
			})
		}
		if end < volumesLength - 1 {
			volumeRes.NextToken = strconv.FormatInt(end + 1, 10)
		}
	} else {
		if req.StartingToken != "" {
			return nil, status.Errorf(codes.Aborted, "volume %s not found", req.StartingToken)
		}
	}
	return volumeRes, nil
}

func (hpc *hostPathController) ControllerGetVolume(ctx context.Context, req *csi.ControllerGetVolumeRequest) (*csi.ControllerGetVolumeResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "missing request")
	}
	if req.GetVolumeId() == "" {
		return nil, status.Error(codes.InvalidArgument, "volume ID not provided")
	}
	capacity, err := hpc.getVolumeDirCapacity()
	if err != nil {
		return nil, err
	}

	healthy, msg := doHealthCheckInControllerSide(filepath.Join(hpc.cfg.DataDir, req.GetVolumeId()))
	klog.V(3).Infof("Healthy state: %s Volume: %t", req.GetVolumeId(), healthy)
	return &csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.GetVolumeId(),
			CapacityBytes: capacity,
		},
		Status: &csi.ControllerGetVolumeResponse_VolumeStatus{
			PublishedNodeIds: []string{hpc.cfg.NodeID},
			VolumeCondition: &csi.VolumeCondition{
				Abnormal: !healthy,
				Message:  msg,
			},
		},
	}, nil
}

func (hpc *hostPathController) validateCreateSnapshotRequest(req *csi.CreateSnapshotRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "missing request")
	}
	if len(req.GetName()) == 0 {
		return status.Error(codes.InvalidArgument, "name missing in request")
	}
	if len(req.GetSourceVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "source volume id missing in request")
	}
	return nil
}

func (hpc *hostPathController) createSnapshotResponseFromFile(req *csi.CreateSnapshotRequest, file string) (*csi.CreateSnapshotResponse, error) {
	// Found the file, return information about it.
	if _, err := os.Stat(file); err != nil {
		return nil, err
	} else {
		snap := hpc.createSnapshotObject(req.GetName(), req.GetSourceVolumeId(), file)
		return &csi.CreateSnapshotResponse{
			Snapshot: snap,
		}, nil
	}
}

func (hpc *hostPathController) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	if err := hpc.validateCreateSnapshotRequest(req); err != nil {
		return nil, err
	}
	hpc.snapMutex.Lock()
	defer hpc.snapMutex.Unlock()
	snapPath := filepath.Join(hpc.cfg.SnapshotDir, req.GetName())
	// Make sure the directory exists.
	if exists, err := checkPathExist(snapPath); err != nil {
		return nil, err
	} else if !exists {
		if err := CreateSnapshotDirectory(hpc.cfg.SnapshotDir, req.GetName()); err != nil {
			return nil, fmt.Errorf("failed to create snapshot directory %v: %w", req.GetName(), err)
		}
		klog.V(4).Infof("created snapshot directory %s", snapPath)
	}
	// Check if there is a snapshot file in the directory.
	volumeSnapshotFile := filepath.Join(snapPath, fmt.Sprintf("%s%s", req.GetSourceVolumeId(), snapExt))
	if isEmpty, err := checkPathIsEmpty(snapPath); err != nil {
		return nil, err
	} else if !isEmpty {
		// Not empty, check if volume source matches
		if exists, err := checkPathExist(volumeSnapshotFile); err != nil {
			return nil, err
		} else if !exists {
			// Found a different file, already exists.
			return nil, status.Errorf(codes.AlreadyExists, "snapshot with the same name: %s but with different SourceVolumeId already exist", req.GetName())
		} else {
			return hpc.createSnapshotResponseFromFile(req, volumeSnapshotFile)
		}
	}
	// File not there, create it.
	cmd := []string{"tar", "-c", "--zstd", "-f", volumeSnapshotFile, "-C", filepath.Join(hpc.cfg.DataDir, req.GetSourceVolumeId()), "."}
	executor := exec.New()
	out, err := executor.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed create snapshot: %w: %s", err, out)
	}
	// Successfully create snapshot.
	return hpc.createSnapshotResponseFromFile(req, volumeSnapshotFile)
}

func (hpc *hostPathController) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "missing request")
	}
	if len(req.GetSnapshotId()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "snapshot id missing in request")
	}
	hpc.snapMutex.Lock()
	defer hpc.snapMutex.Unlock()
	snapPath := filepath.Join(hpc.cfg.SnapshotDir, req.GetSnapshotId())
	if err := os.RemoveAll(snapPath); err != nil {
		return nil, err
	}
	return &csi.DeleteSnapshotResponse{}, nil
}

func (hpc *hostPathController) createSnapshotObject(snapshotId, sourceVolumeId, fileName string) *csi.Snapshot {
	creationTime, err := getFileCreationTime(fileName)
	if err != nil {
		klog.V(1).Infof("Error getting snapshot creation time %v", err)
		return nil
	}
	size, err := hpc.getSnapshotContentSize(snapshotId, sourceVolumeId)
	if err != nil {
		klog.V(1).Infof("Error getting volume %s used size %v", sourceVolumeId, err)
		return nil
	}
	return &csi.Snapshot{
		SnapshotId: snapshotId,
		SourceVolumeId: sourceVolumeId,
		CreationTime: timestamppb.New(*creationTime),
		SizeBytes: size,
		ReadyToUse: true,
	}
}

// createSnapshotObjectFromDir creates a snapshot object if the path exists, and a snapshot
// file exists in the path.
func (hpc *hostPathController) createSnapshotObjectFromDir(path string) *csi.Snapshot {
	if exists, err := checkPathExist(path); err != nil || !exists {
		return nil
	}
	snapFile := ""
	err := filepath.Walk(path, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, snapExt) {
			snapFile = path
		}
		return nil
	})
	if err != nil || len(snapFile) == 0 {
		return nil
	}
	return hpc.createSnapshotObject(filepath.Base(path), strings.ReplaceAll(filepath.Base(snapFile), snapExt, ""), snapFile)
}

func (hpc *hostPathController) getSnapshotFromId(snapshotId string) []csi.Snapshot {
	res := make([]csi.Snapshot, 0)
	snapshot := hpc.createSnapshotObjectFromDir(filepath.Join(hpc.cfg.SnapshotDir, snapshotId))
	if snapshot != nil {
		res = append(res, *snapshot)
	}
	return res
}

func (hpc *hostPathController) getSnapshotsFromSourceVolumeId(sourceVolumeId string) []csi.Snapshot {
	return hpc.getSnapshotsWithFilter(func(fileName string) bool {
		return strings.ReplaceAll(filepath.Base(fileName), snapExt, "") == sourceVolumeId
	})
}

func (hpc *hostPathController) getAllSnapshots() []csi.Snapshot {
	return hpc.getSnapshotsWithFilter(func(fileName string) bool {
		return true
	})
}

func (hpc *hostPathController) getSnapshotsWithFilter(filterFunc func(string) bool) []csi.Snapshot {
	res := make([]csi.Snapshot, 0)
	err := filepath.Walk(hpc.cfg.SnapshotDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, snapExt) && filterFunc(path) {
			// ignore error, we can't do anything about it here.
			snapshotObject := hpc.createSnapshotObject(filepath.Base(filepath.Dir(path)), strings.ReplaceAll(filepath.Base(path), snapExt, ""), path)
			if snapshotObject != nil {
				res = append(res, *snapshotObject)
			}
		}
		return nil
	})
	if err != nil {
		return nil
	}
	sort.Slice(res, func(i, j int) bool {
		return strings.Compare(res[i].SnapshotId, res[j].SnapshotId) == -1
	})

	return res
}

func (hpc *hostPathController) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "missing request")
	}
	hpc.snapMutex.Lock()
	defer hpc.snapMutex.Unlock()

	var snapshots []csi.Snapshot
	if len(req.GetSnapshotId()) != 0 {
		snapshots = hpc.getSnapshotFromId(req.GetSnapshotId())
	} else if len(req.GetSourceVolumeId()) != 0 {
		snapshots = hpc.getSnapshotsFromSourceVolumeId(req.GetSourceVolumeId())
	} else {
		snapshots = hpc.getAllSnapshots()
	}

	snapshotRes := &csi.ListSnapshotsResponse{}
	if len(snapshots) > 0 {
		snapshotRes.Entries = []*csi.ListSnapshotsResponse_Entry{}
		if req.StartingToken == "" || req.StartingToken == "0" {
			req.StartingToken = "1"
		}

		snapshotLength := int64(len(snapshots))
		maxLength := int64(req.MaxEntries)
		if maxLength == 0 {
			maxLength = snapshotLength
		}
		start, err := strconv.ParseUint(req.StartingToken, 10, 32)
		if err != nil {
			return nil, status.Error(codes.Aborted, "The type of startingToken should be integer")
		}
		start = start - 1
		end := int64(start) + maxLength

		if end > snapshotLength {
			end = snapshotLength
		}

		for _, val := range snapshots[start:end] {
			snapshotRes.Entries = append(snapshotRes.Entries, &csi.ListSnapshotsResponse_Entry{
				Snapshot: &val,
			})
		}
		if end < snapshotLength - 1 {
			snapshotRes.NextToken = strconv.FormatInt(end + 1, 10)
		}
	} else {
		if req.StartingToken != "" {
			return nil, status.Errorf(codes.Aborted, "snapshot %s not found", req.StartingToken)
		}
	}

	return snapshotRes, nil
}

func (hpc *hostPathController) restoreFromSnapshot(snapshotId, targetVolume string) error {
	hpc.snapMutex.Lock()
	defer hpc.snapMutex.Unlock()

	snapshotPath := filepath.Join(hpc.cfg.SnapshotDir, snapshotId)
	snapshot := hpc.createSnapshotObjectFromDir(snapshotPath)
	if snapshot == nil {
		return status.Errorf(codes.NotFound, "failed to restore snapshot %s to volume %s", snapshotId, targetVolume)
	}
	snapshotFile := filepath.Join(snapshotPath, fmt.Sprintf("%s.tar.std", snapshot.SourceVolumeId))
	destPath := filepath.Join(hpc.cfg.DataDir, targetVolume)
	cmd := []string{"tar", "-x", "--zstd", "-f", snapshotFile, "-C", destPath}
	executor := exec.New()
	out, err := executor.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to restore snapshot %s to volume %s: %w: %s", snapshotId, targetVolume, err, out)
	}
	return nil
}

func (hpc *hostPathController) ControllerExpandVolume(ctx context.Context, req *csi.ControllerExpandVolumeRequest) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "controllerExpandVolume is not supported")
}

func (hpc *hostPathController) getControllerServiceCapabilities() []*csi.ControllerServiceCapability {
	cl := []csi.ControllerServiceCapability_RPC_Type{
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
		csi.ControllerServiceCapability_RPC_GET_VOLUME,
		csi.ControllerServiceCapability_RPC_GET_CAPACITY,
		csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
		csi.ControllerServiceCapability_RPC_CREATE_DELETE_SNAPSHOT,
		csi.ControllerServiceCapability_RPC_LIST_SNAPSHOTS,
		csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
	}

	var csc []*csi.ControllerServiceCapability

	for _, cap := range cl {
		csc = append(csc, &csi.ControllerServiceCapability{
			Type: &csi.ControllerServiceCapability_Rpc{
				Rpc: &csi.ControllerServiceCapability_RPC{
					Type: cap,
				},
			},
		})
	}
	return csc
}
