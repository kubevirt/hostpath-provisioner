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
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/utils/mount"
)

const (
	testNode = "testNode"
)

func Test_NodeExpandVolume(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	resp, err := nodeServer.NodeExpandVolume(context.TODO(), &csi.NodeExpandVolumeRequest{})
	Expect(resp).To(BeNil(), "Expected no response")
	Expect(err).To(HaveOccurred())
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "NodeExpandVolume is not supported")))
}

func Test_NodeGetVolumeStatsValidation(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	t.Run("missing volume ID", func(t *testing.T) {
		_, err := nodeServer.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume ID not provided")))
	})

	t.Run("missing volume path", func(t *testing.T) {
		_, err := nodeServer.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{
			VolumeId: "abcd",
		})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume path not provided")))
	})
	t.Run("invalid volume path", func(t *testing.T) {
		_, err := nodeServer.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{
			VolumeId: "abcd",
			VolumePath: "/invalidpath",
		})
		_, expectedWrappedErr := os.Stat("/invalidpath")
		Expect(err).To(BeEquivalentTo(status.Errorf(codes.NotFound, "Could not get file information from %s: %+v", "/invalidpath", expectedWrappedErr)))
	})
}

func Test_NodeGetVolumeStatsHealthy(t *testing.T) {
	RegisterTestingT(t)
	defer func() {
		checkMountPointExistsFunc = checkMountPointExist
	} ()

	checkMountPointExistsFunc = func(volumePath string) (bool, error) {
		// Mount point exists!
		return true, nil
	}

	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	nodeServer := createNodeServer(testNode)
	resp, err := nodeServer.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{
		VolumeId: "abcd",
		VolumePath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.VolumeCondition.Abnormal).To(BeFalse())
	Expect(resp.Usage[0].Available).To(BeNumerically(">", 0))
}

func Test_NodeGetVolumeStatsUnhealthy(t *testing.T) {
	RegisterTestingT(t)
	defer func() {
		checkMountPointExistsFunc = checkMountPointExist
	} ()

	checkMountPointExistsFunc = func(volumePath string) (bool, error) {
		// Mount point missing exists!
		return false, nil
	}

	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	nodeServer := createNodeServer(testNode)
	resp, err := nodeServer.NodeGetVolumeStats(context.TODO(), &csi.NodeGetVolumeStatsRequest{
		VolumeId: "abcd",
		VolumePath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.VolumeCondition.Abnormal).To(BeTrue())
	Expect(resp.Usage[0].Available).To(BeNumerically(">", 0))
}

func Test_NodeGetInfo(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	resp, err := nodeServer.NodeGetInfo(context.TODO(), &csi.NodeGetInfoRequest{})
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.NodeId).To(Equal(testNode))
	Expect(resp.AccessibleTopology).ToNot(BeNil())
	if v, ok := resp.AccessibleTopology.Segments[TopologyKeyNode]; !ok {
		t.Errorf("Unable to find topology key in Segments")
	} else {
		Expect(v).To(Equal(testNode))
	}
}

func Test_NodeGetStageVolume(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	_, err := nodeServer.NodeStageVolume(context.TODO(), &csi.NodeStageVolumeRequest{})
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "NodeStageVolume is not supported")))
}

func Test_NodeGetUnstageVolume(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	_, err := nodeServer.NodeUnstageVolume(context.TODO(), &csi.NodeUnstageVolumeRequest{})
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "NodeUnstageVolume is not supported")))
}

func Test_NodeUnpublishVolumeValidation(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	_, err := nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{})
	Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume ID not provided")))
	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
	})
	Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "target path not provided")))
}

func Test_NodeUnpublishVolumeNotMount(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	//Verifying the directory exists
	_, err = os.Stat(tempDir)
	Expect(err).ToNot(HaveOccurred())

	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
	//Verifying the directory no longer exists
	_, err = os.Stat(tempDir)
	Expect(err).To(HaveOccurred())
	Expect(os.IsNotExist(err)).To(BeTrue())
	// Repeat call to ensure idempotency
	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
}

func Test_NodeUnpublishVolumeMount(t *testing.T) {
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{
		{
			Device: "/dev/test",
			Path: tempDir,
			Type: "disk",
		},
	})
	fakeMounter.UnmountFunc = func(path string) error {
		Expect(path).To(Equal(tempDir))
		return nil
	}
	nodeServer.cfg.Mounter = fakeMounter

	//Verifying the directory exists
	_, err = os.Stat(tempDir)
	Expect(err).ToNot(HaveOccurred())

	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
	//Verifying the directory no longer exists
	_, err = os.Stat(tempDir)
	Expect(err).To(HaveOccurred())
	Expect(os.IsNotExist(err)).To(BeTrue())
	// Repeat call to ensure idempotency
	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).ToNot(HaveOccurred())
}

func Test_NodeUnpublishVolumeMountLikelyError(t *testing.T) {
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{
		{
			Device: "/dev/test",
			Path: tempDir,
			Type: "disk",
		},
	})
	fakeMounter.MountCheckErrors = make(map[string]error)
	fakeMounter.MountCheckErrors[tempDir] = fmt.Errorf("likelyMountError")
	nodeServer.cfg.Mounter = fakeMounter

	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("likelyMountError"))
}

func Test_NodeUnpublishVolumeMountUnmountError (t *testing.T) {
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{
		{
			Device: "/dev/test",
			Path: tempDir,
			Type: "disk",
		},
	})
	fakeMounter.UnmountFunc = func(path string) error {
		Expect(path).To(Equal(tempDir))
		return fmt.Errorf("unmountError")
	}
	nodeServer.cfg.Mounter = fakeMounter

	_, err = nodeServer.NodeUnpublishVolume(context.TODO(), &csi.NodeUnpublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: tempDir,
	})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("unmountError"))	
}

func Test_validateRequestCapabilties(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	t.Run("missing request", func(t *testing.T) {
		err := nodeServer.validateRequestCapabilties(nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})
	t.Run("missing volume ID", func(t *testing.T) {
		err := nodeServer.validateRequestCapabilties(&csi.NodePublishVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume ID not provided")))
	})
	t.Run("missing target path", func(t *testing.T) {
		err := nodeServer.validateRequestCapabilties(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "target path not provided")))
	})
	t.Run("missing volume capability", func(t *testing.T) {
		err := nodeServer.validateRequestCapabilties(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
			TargetPath: "/test",
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume capability not provided")))
	})
	t.Run("valid request", func(t *testing.T) {
		err := nodeServer.validateRequestCapabilties(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
			TargetPath: "/test",
			VolumeCapability: &csi.VolumeCapability{},
		})
		Expect(err).ToNot(HaveOccurred())
	})
}

func Test_validateNodePublishRequest(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)

	t.Run("missing capability", func(t *testing.T) {
		err := nodeServer.validateNodePublishRequest(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
			TargetPath: "/test",
			VolumeCapability: &csi.VolumeCapability{},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "can only publish a non-block volume")))
	})	
	t.Run("block volume capability", func(t *testing.T) {
		err := nodeServer.validateNodePublishRequest(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
			TargetPath: "/test",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Block{
					Block: &csi.VolumeCapability_BlockVolume{},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "cannot publish a non-block volume as block volume")))
	})
	t.Run("valid request", func(t *testing.T) {
		err := nodeServer.validateNodePublishRequest(&csi.NodePublishVolumeRequest{
			VolumeId: "abcd",
			TargetPath: "/test",
			VolumeCapability: &csi.VolumeCapability{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})
}

func Test_NodePublishVolumeValidMountPoint(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{})
	nodeServer.cfg.Mounter = fakeMounter
	_, err = nodeServer.NodePublishVolume(context.TODO(), &csi.NodePublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: filepath.Join(tempDir, validVolId),
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(len(fakeMounter.GetLog())).To(Equal(1))
	mountAction := fakeMounter.GetLog()[0]
	Expect(mountAction.Action).To(Equal(mount.FakeActionMount))
	Expect(mountAction.Target).To(Equal(filepath.Join(tempDir, validVolId)))
	Expect(mountAction.Source).To(Equal("abcd"))
	Expect(mountAction.FSType).To(Equal("ext4"))
	Expect(len(fakeMounter.MountPoints)).To(Equal(1))
	mountPoint := fakeMounter.MountPoints[0]
	Expect(mountPoint.Opts).To(ContainElement("bind"))
}

func Test_NodePublishVolumeValidMountPointReadOnly(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{})
	nodeServer.cfg.Mounter = fakeMounter
	_, err = nodeServer.NodePublishVolume(context.TODO(), &csi.NodePublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: filepath.Join(tempDir, validVolId),
		Readonly: true,
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
		},
	})
	Expect(err).ToNot(HaveOccurred())
	Expect(len(fakeMounter.GetLog())).To(Equal(1))
	mountAction := fakeMounter.GetLog()[0]
	Expect(mountAction.Action).To(Equal(mount.FakeActionMount))
	Expect(mountAction.Target).To(Equal(filepath.Join(tempDir, validVolId)))
	Expect(mountAction.Source).To(Equal("abcd"))
	Expect(mountAction.FSType).To(Equal("ext4"))
	Expect(len(fakeMounter.MountPoints)).To(Equal(1))
	mountPoint := fakeMounter.MountPoints[0]
	Expect(mountPoint.Opts).To(ContainElement("bind"))
	Expect(mountPoint.Opts).To(ContainElement("ro"))
}

func Test_NodePublishVolumeInValidMountPoint(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	fakeMounter := mount.NewFakeMounter([]mount.MountPoint{})
	fakeMounter.MountCheckErrors = make(map[string]error)
	fakeMounter.MountCheckErrors["/test"] = fmt.Errorf("likelyMountError")
	nodeServer.cfg.Mounter = fakeMounter
	_, err := nodeServer.NodePublishVolume(context.TODO(), &csi.NodePublishVolumeRequest{
		VolumeId: "abcd",
		TargetPath: "/test",
		VolumeCapability: &csi.VolumeCapability{
			AccessType: &csi.VolumeCapability_Mount{
				Mount: &csi.VolumeCapability_MountVolume{
					FsType: "ext4",
				},
			},
		},
	})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("check target path"))
}

func Test_NodeGetCapabilities(t *testing.T) {
	RegisterTestingT(t)
	nodeServer := createNodeServer(testNode)
	resp, err := nodeServer.NodeGetCapabilities(context.TODO(), &csi.NodeGetCapabilitiesRequest{})
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.Capabilities).ToNot(BeEmpty())
	Expect(resp.Capabilities).To(ContainElement(&csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_VOLUME_CONDITION,
				},
			},
		}))
	Expect(resp.Capabilities).To(ContainElement(&csi.NodeServiceCapability{
			Type: &csi.NodeServiceCapability_Rpc{
				Rpc: &csi.NodeServiceCapability_RPC{
					Type: csi.NodeServiceCapability_RPC_GET_VOLUME_STATS,
				},
			},
		}))
}

func createNodeServer(nodeId string) *hostPathNode {
	config := Config{
		NodeID: nodeId,
		Mounter: mount.NewFakeMounter([]mount.MountPoint{}),
	}
	return NewHostPathNode(&config)
}
