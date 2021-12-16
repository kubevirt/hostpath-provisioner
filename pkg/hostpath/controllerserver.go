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
	"path/filepath"

	"github.com/golang/protobuf/ptypes/wrappers"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/klog/v2"
)

const (
	deviceID = "deviceID"
)

type hostPathController struct {
	cfg *Config
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

	storagePoolName := getStoragePoolNameFromMap(req.GetParameters())
	if _, ok := hpc.cfg.StoragePoolDataDir[storagePoolName]; !ok {
		return nil, fmt.Errorf("unable to locate path for storage pool %s", storagePoolName)
	}
	capacity, err := hpc.getVolumeDirCapacity(hpc.cfg.StoragePoolDataDir[storagePoolName])
	if err != nil {
		return nil, err
	}
	topologies := []*csi.Topology{}
	topologies = append(topologies, &csi.Topology{Segments: map[string]string{TopologyKeyNode: hpc.cfg.NodeID}})

	if exists, err := checkPathExist(filepath.Join(hpc.cfg.StoragePoolDataDir[storagePoolName], req.GetName())); err != nil {
		return nil, err
	} else if !exists {
		if err := CreateVolume(hpc.cfg.StoragePoolDataDir[storagePoolName], req.GetName()); err != nil {
			return nil, fmt.Errorf("failed to create volume %v: %w", req.GetName(), err)
		}
		klog.V(4).Infof("created volume %s at path %s", req.GetName(), filepath.Join(hpc.cfg.StoragePoolDataDir[storagePoolName], req.GetName()))
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
func (hpc *hostPathController) getVolumeDirCapacity(path string) (int64, error) {
	_, capacity, _, _, _, _, err := getPVStatsFunc(path)
	if err != nil {
		return int64(0), status.Error(codes.Internal, fmt.Sprintf("Unable to determine capacity for volume %s: %v", filepath.Base(path), err))
	}
	capacity, _ = resource.NewQuantity(int64(roundDownCapacityPretty(capacity)), resource.BinarySI).AsInt64()
	return capacity, nil
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
	volumeDirs, err := hpc.getVolumeDirectories()
	if err != nil {
		return nil, err
	}
	volumePath := ""
	for _, volumeDir := range volumeDirs {
		if filepath.Base(volumeDir) == req.GetVolumeId() {
			volumePath = volumeDir
		}
	}
	if volumePath != "" {
		if err := DeleteVolume(filepath.Dir(volumePath), req.GetVolumeId()); err != nil {
			return nil, fmt.Errorf("failed to delete volume %s: %v", req.GetVolumeId(), err)
		}
		klog.V(4).Infof("volume %v successfully deleted", req.GetVolumeId())
	}

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
	storagePoolName := getStoragePoolNameFromMap(req.GetParameters())
	if exists, err := checkPathExist(filepath.Join(hpc.cfg.StoragePoolDataDir[storagePoolName], req.GetVolumeId())); err != nil {
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
	storagePoolName := getStoragePoolNameFromMap(req.GetParameters())
	klog.V(3).Infof("Checking capacity for storage pool %s", storagePoolName)
	if _, ok := hpc.cfg.StoragePoolDataDir[storagePoolName]; !ok {
		return nil, fmt.Errorf("unable to locate path for storage pool %s", storagePoolName)
	}
	available, capacity, _, _, _, _, err := getPVStatsFunc(hpc.cfg.StoragePoolDataDir[storagePoolName])
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
	return getVolumeDirectories(hpc.cfg.StoragePoolDataDir)
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

	capacityMap := make(map[string]int64)
	for _, path := range hpc.cfg.StoragePoolDataDir {
		capacity, err := hpc.getVolumeDirCapacity(path)
		if err != nil {
			return nil, err
		}
		capacityMap[path] = capacity
	}

	volumeDirs, err := hpc.getVolumeDirectories()
	if err != nil {
		return nil, err
	}

	if len(volumeDirs) > 0 {
		if req.StartingToken == "" {
			req.StartingToken = filepath.Base(volumeDirs[0])
		}

		volumesLength := int64(len(volumeDirs))
		maxLength := int64(req.MaxEntries)
		if maxLength == 0 {
			maxLength = volumesLength
		}
		start := IndexOfStartingToken(req.StartingToken, volumeDirs)
		if start == -1 {
			return nil, status.Errorf(codes.InvalidArgument, "volume %s not found", req.StartingToken)
		}

		end := int64(start) + maxLength

		if end > volumesLength {
			end = volumesLength
		}

		for _, volumeId := range volumeDirs[start:end] {
			healthy, msg := doHealthCheckInControllerSide(volumeId)
			klog.V(3).Infof("Healthy state: %s Volume: %t", volumeId, healthy)
			volumeRes.Entries = append(volumeRes.Entries, &csi.ListVolumesResponse_Entry{
				Volume: &csi.Volume{
					VolumeId:      filepath.Base(volumeId),
					CapacityBytes: capacityMap[filepath.Dir(volumeId)],
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
			volumeRes.NextToken = filepath.Base(volumeDirs[end])
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

	capacityMap := make(map[string]int64)
	for _, path := range hpc.cfg.StoragePoolDataDir {
		capacity, err := hpc.getVolumeDirCapacity(path)
		if err != nil {
			return nil, err
		}
		capacityMap[path] = capacity
	}
	volumeDirs, err := hpc.getVolumeDirectories()
	if err != nil {
		return nil, err
	}
	volumePath := ""
	for _, volumeDir := range volumeDirs {
		if filepath.Base(volumeDir) == req.GetVolumeId() {
			volumePath = volumeDir
		}
	}

	if volumePath == "" {
		return nil, status.Errorf(codes.NotFound, "volume %s not found", req.GetVolumeId())
	}
	healthy, msg := doHealthCheckInControllerSide(volumePath)
	klog.V(3).Infof("Healthy state: %s Volume: %t", req.GetVolumeId(), healthy)
	return &csi.ControllerGetVolumeResponse{
		Volume: &csi.Volume{
			VolumeId:      req.GetVolumeId(),
			CapacityBytes: capacityMap[filepath.Dir(volumePath)],
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

func (hpc *hostPathController) CreateSnapshot(ctx context.Context, req *csi.CreateSnapshotRequest) (*csi.CreateSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "createSnapshot is not supported")
}

func (hpc *hostPathController) DeleteSnapshot(ctx context.Context, req *csi.DeleteSnapshotRequest) (*csi.DeleteSnapshotResponse, error) {
	return nil, status.Error(codes.Unimplemented, "deleteSnapshot is not supported")
}

func (hpc *hostPathController) ListSnapshots(ctx context.Context, req *csi.ListSnapshotsRequest) (*csi.ListSnapshotsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "listSnapshots is not supported")
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
