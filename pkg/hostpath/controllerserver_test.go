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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func Test_validateCreateVolumeRequest(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	t.Run("missing request", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})
	t.Run("missing name", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(&csi.CreateVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "name missing in request")))
	})
	t.Run("missing volume capabilities", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(&csi.CreateVolumeRequest{
			Name: "testname",
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume capabilities missing in request")))
	})
	t.Run("volume capabilities list empty", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(&csi.CreateVolumeRequest{
			Name:               "testname",
			VolumeCapabilities: []*csi.VolumeCapability{},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "must have mount access type")))
	})
	t.Run("missing mount access type", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(&csi.CreateVolumeRequest{
			Name: "testname",
			VolumeCapabilities: []*csi.VolumeCapability{
				{},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "must have mount access type")))
	})
	t.Run("valid request", func(t *testing.T) {
		err := controller.validateCreateVolumeRequest(&csi.CreateVolumeRequest{
			Name: "testname",
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})
}

func Test_validateCreateVolumeRequestTopology(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	t.Run("No AccessibilityRequirements", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{})
		Expect(err).ToNot(HaveOccurred())
	})
	t.Run("No topology", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{
			AccessibilityRequirements: &csi.TopologyRequirement{},
		})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "not correct node")))
	})	
	t.Run("Correct requisite topology", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{
			AccessibilityRequirements: &csi.TopologyRequirement{
				Requisite: []*csi.Topology{
					{
						Segments: map[string]string {
							TopologyKeyNode: "test_node",
						},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})	
	t.Run("Wrong requisite topology", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{
			AccessibilityRequirements: &csi.TopologyRequirement{
				Requisite: []*csi.Topology{
					{
						Segments: map[string]string {
							TopologyKeyNode: "invalid",
						},
					},
				},
			},
		})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "not correct node")))
	})	
	t.Run("Correct preferred topology", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{
			AccessibilityRequirements: &csi.TopologyRequirement{
				Preferred: []*csi.Topology{
					{
						Segments: map[string]string {
							TopologyKeyNode: "test_node",
						},
					},
				},
			},
		})
		Expect(err).ToNot(HaveOccurred())
	})	
	t.Run("Wrong preferred topology", func(t *testing.T){
		err := controller.validateCreateVolumeRequestTopology(&csi.CreateVolumeRequest{
			AccessibilityRequirements: &csi.TopologyRequirement{
				Preferred: []*csi.Topology{
					{
						Segments: map[string]string {
							TopologyKeyNode: "invalid",
						},
					},
				},
			},
		})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "not correct node")))
	})	
}

func Test_CreateVolumeInvalidRequest(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	_, err := controller.CreateVolume(context.TODO(), &csi.CreateVolumeRequest{})
	Expect(err).To(HaveOccurred())
	Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "name missing in request")))
}

func Test_CreateVolumeValidDoesNotExist(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)

	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		Expect(volumePath).To(Equal(tempDir))
		return 0, 1000, 0, 0, 0, 0, nil
	}

	resp, err := controller.CreateVolume(context.TODO(), createTestRequest())
	Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(tempDir, "testname"))
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.Volume.VolumeId).To(Equal("testname"))
	Expect(resp.Volume.CapacityBytes).To(Equal(int64(1000)))
	Expect(resp.Volume.VolumeContext).To(BeNil())
	Expect(resp.Volume.ContentSource).To(BeNil())
	Expect(len(resp.Volume.AccessibleTopology)).To(Equal(1))
	Expect(resp.Volume.AccessibleTopology[0]).ToNot(BeNil())
	Expect(resp.Volume.AccessibleTopology[0].Segments[TopologyKeyNode]).To(Equal("test_node"))
	// Check idempotency, re-running should not re-create
	resp, err = controller.CreateVolume(context.TODO(), createTestRequest())
	Expect(err).ToNot(HaveOccurred())
	_, err = os.Stat(filepath.Join(tempDir, "testname"))
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.Volume.VolumeId).To(Equal("testname"))
	Expect(resp.Volume.CapacityBytes).To(Equal(int64(1000)))
	Expect(resp.Volume.VolumeContext).To(BeNil())
	Expect(resp.Volume.ContentSource).To(BeNil())
	Expect(len(resp.Volume.AccessibleTopology)).To(Equal(1))
	Expect(resp.Volume.AccessibleTopology[0]).ToNot(BeNil())
	Expect(resp.Volume.AccessibleTopology[0].Segments[TopologyKeyNode]).To(Equal("test_node"))
}

func Test_CreateVolumeInvalidPath(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("/invalid")

	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		Expect(volumePath).To(Equal("/invalid"))
		return 0, 1000, 0, 0, 0, 0, nil
	}

	_, err := controller.CreateVolume(context.TODO(), createTestRequest())
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("failed to create volume testname"))
}

func Test_CreateVolumePVStatsErr(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("/invalid")

	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		Expect(volumePath).To(Equal("/invalid"))
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("getPVStatsEror")
	}

	_, err := controller.CreateVolume(context.TODO(), createTestRequest())
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("getPVStatsEror"))
}

func Test_validateDeleteVolumeRequest(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")

	t.Run("missing request", func(t *testing.T) {
		err := controller.validateDeleteVolumeRequest(nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})
	t.Run("missing volume ID", func(t *testing.T) {
		err := controller.validateDeleteVolumeRequest(&csi.DeleteVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "Volume ID missing in request")))
	})
	t.Run("valid request", func(t *testing.T) {
		err := controller.validateDeleteVolumeRequest(&csi.DeleteVolumeRequest{
			VolumeId: validVolId,
		})
		Expect(err).ToNot(HaveOccurred())
	})
}

func Test_DeleteVolumeRequest(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	testDirName := filepath.Join(tempDir, validVolId)

	t.Run("missing volume ID", func(t *testing.T) {
		_, err = controller.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "Volume ID missing in request")))
	})

	t.Run("valid request", func(t *testing.T) {
		err = os.Mkdir(testDirName, 0666)
		Expect(err).ToNot(HaveOccurred())
		_, err = os.Stat(testDirName)
		Expect(err).ToNot(HaveOccurred())

		resp, err := controller.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
			VolumeId: validVolId,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		_, err = os.Stat(testDirName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no such file or directory"))
	})
}

func Test_DeleteVolumeRequestInvalidPath(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("/dev")

	_, err := controller.DeleteVolume(context.TODO(), &csi.DeleteVolumeRequest{
		VolumeId: "net",
	})
	Expect(err).To(HaveOccurred())
}

func Test_ControllerGetCapabilities(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	resp, err := controller.ControllerGetCapabilities(context.TODO(), &csi.ControllerGetCapabilitiesRequest{})
	Expect(err).ToNot(HaveOccurred())
	caps := resp.Capabilities
	Expect(len(caps)).To(Equal(5))
	Expect(caps).To(ContainElement(&csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_CREATE_DELETE_VOLUME,
			},
		},
	}))
	Expect(caps).To(ContainElement(&csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_GET_VOLUME,
			},
		},
	}))
	Expect(caps).To(ContainElement(&csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_GET_CAPACITY,
			},
		},
	}))
	Expect(caps).To(ContainElement(&csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_LIST_VOLUMES,
			},
		},
	}))
	Expect(caps).To(ContainElement(&csi.ControllerServiceCapability{
		Type: &csi.ControllerServiceCapability_Rpc{
			Rpc: &csi.ControllerServiceCapability_RPC{
				Type: csi.ControllerServiceCapability_RPC_VOLUME_CONDITION,
			},
		},
	}))
}

func Test_ValidateVolumeCapabilities(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	err = os.Mkdir(filepath.Join(tempDir, validVolId), 0666)
	Expect(err).ToNot(HaveOccurred())

	t.Run("missing volume ID", func(t *testing.T) {
		_, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume ID not provided")))
	})
	t.Run("missing volume capabilities in request", func(t *testing.T) {
		_, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: validVolId,
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volumeCapabilities not provided for valid")))
	})
	t.Run("empty volume capabilities in request", func(t *testing.T) {
		_, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId:           validVolId,
			VolumeCapabilities: []*csi.VolumeCapability{},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volumeCapabilities not provided for valid")))
	})
	t.Run("block volume in request capabilities", func(t *testing.T) {
		_, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: validVolId,
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Block{
						Block: &csi.VolumeCapability_BlockVolume{},
					},
				},
			},
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "mount type is undefined")))
	})
	t.Run("valid request", func(t *testing.T) {
		resp, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: validVolId,
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
				},
			},
			VolumeContext: map[string]string{
				"context": "value",
			},
			Parameters: map[string]string{
				"parameters": "value",
			},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		Expect(resp.Confirmed).ToNot(BeNil())
		Expect(resp.Confirmed.VolumeContext["context"]).To(Equal("value"))
		Expect(resp.Confirmed.Parameters["parameters"]).To(Equal("value"))
		Expect(resp.Confirmed.VolumeCapabilities).To(Equal([]*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		}))
	})
	t.Run("valid request, not found", func(t *testing.T) {
		_, err := controller.ValidateVolumeCapabilities(context.TODO(), &csi.ValidateVolumeCapabilitiesRequest{
			VolumeId: invalidVolId,
			VolumeCapabilities: []*csi.VolumeCapability{
				{
					AccessType: &csi.VolumeCapability_Mount{
						Mount: &csi.VolumeCapability_MountVolume{},
					},
				},
			},
		})
		Expect(err).To(BeEquivalentTo(status.Errorf(codes.NotFound, "volume %s not found", invalidVolId)))
	})
}

func Test_ControllerPublishVolume(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	t.Run("missing request", func(t *testing.T) {
		_, err := controller.ControllerPublishVolume(context.TODO(), nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "ControllerPublishVolume is not supported")))
	})
	t.Run("not supported", func(t *testing.T) {
		_, err := controller.ControllerPublishVolume(context.TODO(), &csi.ControllerPublishVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "ControllerPublishVolume is not supported")))
	})
}

func Test_ControllerUnpublishVolume(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	t.Run("missing request", func(t *testing.T) {
		_, err := controller.ControllerUnpublishVolume(context.TODO(), nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "ControllerUnpublishVolume is not supported")))
	})
	t.Run("not supported", func(t *testing.T) {
		_, err := controller.ControllerUnpublishVolume(context.TODO(), &csi.ControllerUnpublishVolumeRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "ControllerUnpublishVolume is not supported")))
	})
}

func Test_GetCapacityRequest(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	t.Run("missing request", func(t *testing.T) {
		_, err = controller.GetCapacity(context.TODO(), nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})

	t.Run("valid request", func(t *testing.T) {
		resp, err := controller.GetCapacity(context.TODO(), &csi.GetCapacityRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.AvailableCapacity).To(BeNumerically(">", int64(0)))
		Expect(resp.MaximumVolumeSize.Value).To(BeNumerically(">=", resp.AvailableCapacity))
		Expect(resp.MinimumVolumeSize.Value).To(Equal(int64(0)))
	})
}

func Test_GetCapacityRequestPVStatError(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("pvStatError")
	}

	_, err = controller.GetCapacity(context.TODO(), &csi.GetCapacityRequest{})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("pvStatError"))
}

func Test_getVolumeDirectoriesFail(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("/dev/null")
	_, err := controller.getVolumeDirectories()
	Expect(err).To(HaveOccurred())
}

func Test_getVolumeDirectories(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	t.Run("no volumes", func(t *testing.T) {
		res, err := controller.getVolumeDirectories()
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res)).To(Equal(0))
	})

	t.Run("2 existing volumes", func(t *testing.T) {
		err = os.Mkdir(filepath.Join(tempDir, validVolId), 0666)
		Expect(err).ToNot(HaveOccurred())
		err = os.Mkdir(filepath.Join(tempDir, invalidVolId), 0666)
		Expect(err).ToNot(HaveOccurred())
		res, err := controller.getVolumeDirectories()
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res)).To(Equal(2))
		Expect(res[0]).To(Equal(filepath.Join(tempDir, invalidVolId)))
		Expect(res[1]).To(Equal(filepath.Join(tempDir, validVolId)))
	})

	t.Run("adding single file", func(t *testing.T) {
		_, err = os.Create(filepath.Join(tempDir, "filename"))
		Expect(err).ToNot(HaveOccurred())
		res, err := controller.getVolumeDirectories()
		Expect(err).ToNot(HaveOccurred())
		Expect(len(res)).To(Equal(2))
		Expect(res[0]).To(Equal(filepath.Join(tempDir, invalidVolId)))
		Expect(res[1]).To(Equal(filepath.Join(tempDir, validVolId)))
	})
}

func Test_validateListVolumesRequest(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	t.Run("missing request", func(t *testing.T) {
		err := controller.validateListVolumesRequest(nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})

	t.Run("valid request", func(t *testing.T) {
		err := controller.validateListVolumesRequest(&csi.ListVolumesRequest{})
		Expect(err).ToNot(HaveOccurred())
	})

	t.Run("negative max entries", func(t *testing.T) {
		err := controller.validateListVolumesRequest(&csi.ListVolumesRequest{
			MaxEntries: -1,
		})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "maxEntries < 0")))
	})
}
func Test_ListVolumes(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	for i := 1; i < 10; i++ {
		err = os.Mkdir(filepath.Join(tempDir, fmt.Sprintf("dir%d", i)), 0666)
		Expect(err).ToNot(HaveOccurred())
	}
	controller := createControllerServer(tempDir)

	t.Run("missing request", func(t *testing.T) {
		_, err = controller.ListVolumes(context.TODO(), nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})

	t.Run("no explicit start or end", func(t *testing.T) {
		resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(resp.Entries)).To(Equal(9))
		for _, entry := range resp.Entries {
			Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
			Expect(entry.Status).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
		}
		Expect(resp.GetNextToken()).To(BeEmpty())
	})

	t.Run("max entries 4", func(t *testing.T) {
		// No start max 4 entries
		resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
			MaxEntries: 4,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(resp.Entries)).To(Equal(4))
		for _, entry := range resp.Entries {
			Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
			Expect(entry.Status).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
		}
		Expect(resp.GetNextToken()).To(Equal("dir5"))
	})

	t.Run("start at 3rd entry request max result 1", func(t *testing.T) {
		resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
			MaxEntries:    1,
			StartingToken: "dir3",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(resp.Entries)).To(Equal(1))
		for _, entry := range resp.Entries {
			Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
			Expect(entry.Status).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
		}
		Expect(resp.GetNextToken()).To(Equal("dir4"))
		t.Run("request next page", func(t *testing.T) {
			// Next page
			resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
				MaxEntries:    3,
				StartingToken: resp.GetNextToken(),
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(len(resp.Entries)).To(Equal(3))
			for _, entry := range resp.Entries {
				Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
				Expect(entry.Status).ToNot(BeNil())
				Expect(entry.Status.VolumeCondition).ToNot(BeNil())
				Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
			}
			Expect(resp.GetNextToken()).To(Equal("dir7"))
			t.Run("request next page > max", func(t *testing.T) {
				resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
					MaxEntries:    4,
					StartingToken: resp.GetNextToken(),
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(len(resp.Entries)).To(Equal(3))
				Expect(resp.GetNextToken()).To(BeEmpty())
				for _, entry := range resp.Entries {
					Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
					Expect(entry.Status).ToNot(BeNil())
					Expect(entry.Status.VolumeCondition).ToNot(BeNil())
					Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
				}
			})
		})
	})
	t.Run("invalid volume name", func(t *testing.T) {
		_, err = controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
			MaxEntries:    3,
			StartingToken: invalidVolId,
		})
		Expect(err).To(BeEquivalentTo(status.Errorf(codes.InvalidArgument, "volume %s not found", invalidVolId)))
	})

	t.Run("blank starting token, no max", func(t *testing.T) {
		resp, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
			StartingToken: "",
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(len(resp.Entries)).To(Equal(9))
		for _, entry := range resp.Entries {
			Expect(entry.Volume.CapacityBytes).To(BeNumerically(">", 0))
			Expect(entry.Status).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition).ToNot(BeNil())
			Expect(entry.Status.VolumeCondition.Abnormal).To(BeFalse())
		}
		Expect(resp.GetNextToken()).To(BeEmpty())
	})
}

func Test_ListVolumesNonEmptyStartTokenNoVolumes(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 1000, 10000, 0, 0, 0, 0, nil
	}
	_, err = controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{
		StartingToken: "invalid_token",
	})
	Expect(err).To(BeEquivalentTo(status.Errorf(codes.Aborted, "volume %s not found", "invalid_token")))
}
func Test_ListVolumesErrorStat(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("pvStatError")
	}
	_, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("pvStatError"))
}

func Test_ListVolumesErrorGetVolumeDirectories(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("/invalid")
	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 0, 0, 0, 0, 0, 0, nil
	}

	_, err := controller.ListVolumes(context.TODO(), &csi.ListVolumesRequest{})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("open /invalid: no such file or directory"))
}

func Test_ControllerGetVolume(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	controller := createControllerServer(tempDir)
	err = os.Mkdir(filepath.Join(tempDir, validVolId), 0666)
	Expect(err).ToNot(HaveOccurred())
	t.Run("missing request", func(t *testing.T) {
		_, err = controller.ControllerGetVolume(context.TODO(), nil)
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "missing request")))
	})
	t.Run("missing volume ID", func(t *testing.T) {
		_, err = controller.ControllerGetVolume(context.TODO(), &csi.ControllerGetVolumeRequest{})
		Expect(err).To(BeEquivalentTo(status.Error(codes.InvalidArgument, "volume ID not provided")))
	})
	t.Run("valid request", func(t *testing.T) {
		resp, err := controller.ControllerGetVolume(context.TODO(), &csi.ControllerGetVolumeRequest{
			VolumeId: validVolId,
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp).ToNot(BeNil())
		Expect(resp.GetVolume()).ToNot(BeNil())
		Expect(resp.GetVolume().VolumeId).To(Equal(validVolId))
		Expect(resp.GetStatus()).ToNot(BeNil())
		Expect(resp.GetStatus().GetVolumeCondition()).ToNot(BeNil())
		Expect(resp.GetStatus().GetVolumeCondition().Abnormal).To(BeFalse())
		Expect(resp.GetVolume().GetCapacityBytes()).To(BeNumerically(">", 0))
	})
}

func Test_ControllerGetVolumeError(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	oldGetPVStatsFunc := getPVStatsFunc
	defer func() {
		getPVStatsFunc = oldGetPVStatsFunc
	}()
	getPVStatsFunc = func(volumePath string) (int64, int64, int64, int64, int64, int64, error) {
		return 0, 0, 0, 0, 0, 0, fmt.Errorf("pvStatsError")
	}

	_, err := controller.ControllerGetVolume(context.TODO(), &csi.ControllerGetVolumeRequest{
		VolumeId: validVolId,
	})
	Expect(err).To(HaveOccurred())
	Expect(err.Error()).To(ContainSubstring("pvStatsError"))
}

func Test_CreateSnapshot(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	_, err := controller.CreateSnapshot(context.TODO(), nil)
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "createSnapshot is not supported")))
}

func Test_DeleteSnapshot(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	_, err := controller.DeleteSnapshot(context.TODO(), nil)
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "deleteSnapshot is not supported")))
}

func Test_ListSnapshots(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	_, err := controller.ListSnapshots(context.TODO(), nil)
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "listSnapshots is not supported")))
}

func Test_ControllerExpandVolume(t *testing.T) {
	RegisterTestingT(t)
	controller := createControllerServer("")
	_, err := controller.ControllerExpandVolume(context.TODO(), nil)
	Expect(err).To(BeEquivalentTo(status.Error(codes.Unimplemented, "controllerExpandVolume is not supported")))
}

func createTestRequest() *csi.CreateVolumeRequest {
	return &csi.CreateVolumeRequest{
		Name: "testname",
		VolumeCapabilities: []*csi.VolumeCapability{
			{
				AccessType: &csi.VolumeCapability_Mount{
					Mount: &csi.VolumeCapability_MountVolume{},
				},
			},
		},
	}
}

func createControllerServer(dataDir string) *hostPathController {
	config := Config{
		DriverName: "test_driver",
		Version:    "test_version",
		NodeID:     "test_node",
		StoragePoolDataDir: map[string]string{legacyStoragePoolName: dataDir},
	}
	return NewHostPathController(&config)

}
