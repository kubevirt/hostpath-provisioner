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
	"os"
	"path/filepath"
	"strings"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
	"k8s.io/utils/mount"
)

const TopologyKeyNode = "topology.hostpath.csi/node"

type hostPathNode struct {
	cfg *Config
}

func NewHostPathNode(config *Config) *hostPathNode {
	return &hostPathNode {
		cfg: config,
	}
}

func (hpn *hostPathNode) validateRequestCapabilties(req *csi.NodePublishVolumeRequest) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "missing request")
	}
	// Check ID and targetPath
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "volume ID not provided")
	}
	if req.GetTargetPath() == "" {
		return status.Error(codes.InvalidArgument, "target path not provided")
	}
	if req.GetVolumeCapability() == nil {
		return status.Error(codes.InvalidArgument, "volume capability not provided")
	}
	return nil
}

// validateNodePublishRequest validates that the request contains all the required elements.
func (hpn *hostPathNode) validateNodePublishRequest(req *csi.NodePublishVolumeRequest) error {
	if err := hpn.validateRequestCapabilties(req); err != nil {
		return err
	}

	if req.GetVolumeCapability().GetBlock() != nil {
		return status.Error(codes.InvalidArgument, "cannot publish a non-block volume as block volume")
	}
	if req.GetVolumeCapability().GetMount() == nil {
		return status.Error(codes.InvalidArgument, "can only publish a non-block volume")
	}

	return nil
}

func (hpn *hostPathNode) NodePublishVolume(ctx context.Context, req *csi.NodePublishVolumeRequest) (*csi.NodePublishVolumeResponse, error) {
	if req != nil {
		klog.V(3).Infof("Node Publish Request: %+v", *req)
	}
	if err := hpn.validateNodePublishRequest(req); err != nil {
		return nil, err
	}

	targetPath := req.GetTargetPath()
	
	if canMnt, err := hpn.canMountVolume(targetPath); err != nil {
		return nil, err
	} else if !canMnt {
		klog.V(3).Infof("Cannot mount to target path: %s", targetPath)
		return &csi.NodePublishVolumeResponse{}, nil
	}
	
	if err := hpn.mountVolume(targetPath, req); err != nil {
		return nil, err
	}

	return &csi.NodePublishVolumeResponse{}, nil
}

func (hpn *hostPathNode) canMountVolume(targetPath string) (bool, error) {
	notMnt, err := mount.IsNotMountPoint(hpn.cfg.Mounter, targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err = os.Mkdir(targetPath, 0750); err != nil {
				return false, fmt.Errorf("create target path: %w", err)
			}
			notMnt = true
		} else {
			return false, fmt.Errorf("check target path: %w", err)
		}
	}

	return notMnt, nil
}

func (hpn *hostPathNode) mountVolume(targetPath string, req *csi.NodePublishVolumeRequest) error {
	fsType := req.GetVolumeCapability().GetMount().GetFsType()

	deviceId := ""
	if req.GetPublishContext() != nil {
		deviceId = req.GetPublishContext()[deviceID]
	}

	readOnly := req.GetReadonly()
	volumeId := req.GetVolumeId()

	klog.V(4).Infof("target %v\nfstype %v\ndevice %v\nreadonly %v\nvolumeId %v\nattributes %v\nmountflags %v\n",
		targetPath, fsType, deviceId, readOnly, volumeId, req.GetVolumeContext(), req.GetVolumeCapability().GetMount().GetMountFlags())

	options := []string{"bind"}
	if readOnly {
		options = append(options, "ro")
	}
	mounter := hpn.cfg.Mounter
	path := filepath.Join(hpn.cfg.DataDir, volumeId)

	if err := mounter.Mount(path, targetPath, fsType, options); err != nil {
		var errList strings.Builder
		errList.WriteString(err.Error())
		fileInfo, err := os.Stat(targetPath)
		if err != nil {
			errList.WriteString(err.Error())
		}
		errList.WriteString(fmt.Sprintf("%v", fileInfo.Mode()))
		return fmt.Errorf("failed to mount device: %s at %s: %s", path, targetPath, errList.String())
	}
	return nil
}

func (hpn *hostPathNode) validateNodeUnpublishRequest(req *csi.NodeUnpublishVolumeRequest) error {
	// Check arguments
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "volume ID not provided")
	}
	if len(req.GetTargetPath()) == 0 {
		return status.Error(codes.InvalidArgument, "target path not provided")
	}
	return nil
}

func (hpn *hostPathNode) NodeUnpublishVolume(ctx context.Context, req *csi.NodeUnpublishVolumeRequest) (*csi.NodeUnpublishVolumeResponse, error) {
	if err := hpn.validateNodeUnpublishRequest(req); err != nil {
		return nil, err
	}
	targetPath := req.GetTargetPath()

	klog.V(3).Infof("Unmounting path: %s", targetPath)
	// Unmount only if the target path is really a mount point.
	if notMnt, err := mount.IsNotMountPoint(hpn.cfg.Mounter, targetPath); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("check target path: %w", err)
		}
	} else if !notMnt {
		klog.V(4).Info("Mount point")
		// Unmounting the image or filesystem.
		err = hpn.cfg.Mounter.Unmount(targetPath)
		if err != nil {
			return nil, fmt.Errorf("unmount target path: %w", err)
		}
	}
	klog.V(4).Infof("Deleting mount point: %s", targetPath)
	// Delete the mount point.
	// Does not return error for non-existent path, repeated calls OK for idempotency.
	if err := os.RemoveAll(targetPath); err != nil {
		return nil, fmt.Errorf("remove target path: %w", err)
	}
	klog.V(4).Infof("hostpath: volume %s has been unpublished.", targetPath)

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (hpn *hostPathNode) NodeStageVolume(ctx context.Context, req *csi.NodeStageVolumeRequest) (*csi.NodeStageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "NodeStageVolume is not supported")
}

func (hpn *hostPathNode) NodeUnstageVolume(ctx context.Context, req *csi.NodeUnstageVolumeRequest) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "NodeUnstageVolume is not supported")
}

func (hpn *hostPathNode) NodeGetInfo(ctx context.Context, req *csi.NodeGetInfoRequest) (*csi.NodeGetInfoResponse, error) {
	resp := &csi.NodeGetInfoResponse{
		NodeId:            hpn.cfg.NodeID,
	}

	resp.AccessibleTopology = &csi.Topology{
		Segments: map[string]string{TopologyKeyNode: hpn.cfg.NodeID},
	}

	return resp, nil
}

func (hpn *hostPathNode) NodeGetCapabilities(ctx context.Context, req *csi.NodeGetCapabilitiesRequest) (*csi.NodeGetCapabilitiesResponse, error) {
	caps := []*csi.NodeServiceCapability{
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		},
		{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		},
	}

	return &csi.NodeGetCapabilitiesResponse{Capabilities: caps}, nil
}


func (hpn *hostPathNode) validateNodeGetVolumeStatsRequest(req *csi.NodeGetVolumeStatsRequest) error {
	if len(req.GetVolumeId()) == 0 {
		return status.Error(codes.InvalidArgument, "volume ID not provided")
	}
	if len(req.GetVolumePath()) == 0 {
		return status.Error(codes.InvalidArgument, "volume path not provided")
	}
	return nil
}

func (hpn *hostPathNode) NodeGetVolumeStats(ctx context.Context, req *csi.NodeGetVolumeStatsRequest) (*csi.NodeGetVolumeStatsResponse, error) {
	if err := hpn.validateNodeGetVolumeStatsRequest(req); err != nil {
		return nil, err
	}
	klog.V(3).Infof("Node Get Volume Stats Request: %+v", *req)

	if _, err := os.Stat(req.GetVolumePath()); err != nil {
		return nil, status.Errorf(codes.NotFound, "Could not get file information from %s: %+v", req.GetVolumePath(), err)
	}

	healthy, msg := doHealthCheckInNodeSide(req.GetVolumePath())
	klog.V(3).Infof("Healthy state: %+v Volume: %+v", req.GetVolumeId(), healthy)
	if !healthy {
		klog.V(1).Infof("Volume %s not healthy: %s", req.GetVolumeId(), msg)
	}
	available, capacity, used, inodes, inodesFree, inodesUsed, err := getPVStatsFunc(req.GetVolumePath())
	if err != nil {
		return nil, fmt.Errorf("get volume stats failed: %w", err)
	}

	klog.V(3).Infof("Capacity: %+v Used: %+v Available: %+v Inodes: %+v Free inodes: %+v Used inodes: %+v", capacity, used, available, inodes, inodesFree, inodesUsed)
	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			{
				Available: available,
				Used:      used,
				Total:     capacity,
				Unit:      csi.VolumeUsage_BYTES,
			}, {
				Available: inodesFree,
				Used:      inodesUsed,
				Total:     inodes,
				Unit:      csi.VolumeUsage_INODES,
			},
		},
		VolumeCondition: &csi.VolumeCondition{
			Abnormal: !healthy,
			Message:  msg,
		},
	}, nil
}

// NodeExpandVolume is only implemented so the driver can be used for e2e testing.
func (hpn *hostPathNode) NodeExpandVolume(ctx context.Context, req *csi.NodeExpandVolumeRequest) (*csi.NodeExpandVolumeResponse, error) {
	return nil, status.Error(codes.Unimplemented, "NodeExpandVolume is not supported")
}
