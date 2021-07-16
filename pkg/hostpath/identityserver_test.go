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
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func Test_GetPluginInfo(t *testing.T) {
	RegisterTestingT(t)
	identityServer := createIdentityServer()
	t.Run("valid config", func(t *testing.T) {
		resp, err := identityServer.GetPluginInfo(context.TODO(), &csi.GetPluginInfoRequest{})
		Expect(err).ToNot(HaveOccurred())
		Expect(resp.GetVendorVersion()).To(Equal("test_version"))
		Expect(resp.GetName()).To(Equal("test_driver"))
	})
	t.Run("blank config", func(t *testing.T) {
		identityServer.cfg = &Config{}
		_, err := identityServer.GetPluginInfo(context.TODO(), &csi.GetPluginInfoRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unavailable, "Driver name not configured")))
	})
	t.Run("config without version", func(t *testing.T) {
		identityServer.cfg = &Config{
			DriverName: "test_driver",
		}
		_, err := identityServer.GetPluginInfo(context.TODO(), &csi.GetPluginInfoRequest{})
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(status.Error(codes.Unavailable, "Version not configured")))
	})
}

func Test_GetPluginCapabilities(t *testing.T) {
	RegisterTestingT(t)
	identityServer := createIdentityServer()
	resp, _ := identityServer.GetPluginCapabilities(context.TODO(), &csi.GetPluginCapabilitiesRequest{})
	Expect(resp.Capabilities).ToNot(BeEmpty())
	Expect(resp.Capabilities).To(ContainElement(&csi.PluginCapability{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		},
	))
	Expect(resp.Capabilities).To(ContainElement(&csi.PluginCapability{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
				},
			},
		},
	))
}

func Test_Probe(t *testing.T) {
	RegisterTestingT(t)
	identityServer := createIdentityServer()
	resp, _ := identityServer.Probe(context.TODO(), &csi.ProbeRequest{})
	Expect(resp).ToNot(BeNil())
}
func createIdentityServer() *hostPathIdentity {
	config := Config{
		DriverName: "test_driver",
		Version: "test_version",
	}
	return NewHostPathIdentity(&config)
}
