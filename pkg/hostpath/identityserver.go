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
	"github.com/container-storage-interface/spec/lib/go/csi"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/klog/v2"
)
type hostPathIdentity struct {
	cfg *Config
}

func NewHostPathIdentity(config *Config) *hostPathIdentity {
	return &hostPathIdentity {
		cfg: config,
	}
}
func (hpi *hostPathIdentity) GetPluginInfo(ctx context.Context, req *csi.GetPluginInfoRequest) (*csi.GetPluginInfoResponse, error) {
	klog.V(5).Infof("Using default GetPluginInfo")

	if hpi.cfg.DriverName == "" {
		return nil, status.Error(codes.Unavailable, "Driver name not configured")
	}

	if hpi.cfg.Version == "" {
		return nil, status.Error(codes.Unavailable, "Version not configured")
	}

	return &csi.GetPluginInfoResponse{
		Name:          hpi.cfg.DriverName,
		VendorVersion: hpi.cfg.Version,
	}, nil
}

func (hpi *hostPathIdentity) Probe(ctx context.Context, req *csi.ProbeRequest) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{}, nil
}

func (hpi *hostPathIdentity) GetPluginCapabilities(ctx context.Context, req *csi.GetPluginCapabilitiesRequest) (*csi.GetPluginCapabilitiesResponse, error) {
	klog.V(5).Infof("Using default capabilities")
	caps := []*csi.PluginCapability{
		{
			Type: &csi.PluginCapability_Service_{
				Service: &csi.PluginCapability_Service{
					Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
				},
			},
		},
	}
	caps = append(caps, &csi.PluginCapability{
		Type: &csi.PluginCapability_Service_{
			Service: &csi.PluginCapability_Service{
				Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
			},
		},
	})

	return &csi.GetPluginCapabilitiesResponse{Capabilities: caps}, nil
}
