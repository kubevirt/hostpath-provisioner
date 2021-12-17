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
	"encoding/json"
	"errors"
	"fmt"
	"os"

	klog "k8s.io/klog/v2"
	"k8s.io/utils/mount"
)

const (
	kib    int64 = 1024
	mib    int64 = kib * 1024
	gib    int64 = mib * 1024
	gib100 int64 = gib * 100
	tib    int64 = gib * 1024
	tib100 int64 = tib * 100
)

type Config struct {
	DriverName            string
	Endpoint              string
	NodeID                string
	StoragePoolDataDir			map[string]string
	DefaultStoragePoolName string
	Version	       		  string
	Mounter mount.Interface
}

type hostPath struct {
	cfg *Config
	node *hostPathNode
	controller *hostPathController
	identity *hostPathIdentity
}

func NewHostPathDriver(cfg *Config, dataDir string) (*hostPath, error) {
	if cfg.DriverName == "" {
		return nil, errors.New("no driver name provided")
	}

	if cfg.NodeID == "" {
		return nil, errors.New("no node id provided")
	}

	if cfg.Endpoint == "" {
		return nil, errors.New("no driver endpoint provided")
	}
	if cfg.Version == "" {
		return nil, errors.New("no version provided")
	}
	if cfg.Mounter == nil {
		cfg.Mounter = mount.New("")
	}
	cfg.StoragePoolDataDir = make(map[string]string)

	storagePools := make([]StoragePoolInfo, 0)
	if err := json.Unmarshal([]byte(dataDir), &storagePools); err != nil {
		return nil, errors.New("unable to parse storage pool info")
	}
	for _, storagePool := range storagePools {
		if len(cfg.DefaultStoragePoolName) == 0 {
			cfg.DefaultStoragePoolName = storagePool.Name
		}
		cfg.StoragePoolDataDir[storagePool.Name] = storagePool.Path
	}

	for k, v := range cfg.StoragePoolDataDir {
		klog.V(1).Infof("name: %s, dataDir: %s", k, v)
		if err := os.MkdirAll(v, 0750); err != nil {
			return nil, fmt.Errorf("failed to create dataRoot for storage pool %s: %v", k, err)
		}
	}

	klog.V(1).Infof("Driver: %s, version: %s ", cfg.DriverName, cfg.Version)

	hp := &hostPath{
		cfg: cfg,
	}
	hp.node = NewHostPathNode(cfg)
	hp.controller = NewHostPathController(cfg)
	hp.identity = NewHostPathIdentity(cfg)
	return hp, nil
}

func (hp *hostPath) Run() error {
	s := NewNonBlockingGRPCServer()
	s.Start(hp.cfg.Endpoint, hp.identity, hp.controller, hp.node)
	s.Wait()

	return nil
}
