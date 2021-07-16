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
package sanity

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/gomega"
	"kubevirt.io/hostpath-provisioner/pkg/hostpath"

	"github.com/kubernetes-csi/csi-test/v4/pkg/sanity"
)

const (
	sanityEndpoint = "sanity.sock"
)

func TestMyDriver(t *testing.T) {
	RegisterTestingT(t)
	// Setup the full driver and its environment
	tempDir, err := ioutil.TempDir(os.TempDir(), "csi-sanity")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)

	targetDir := filepath.Join(tempDir, "target-csi")
	volumeDir := filepath.Join(tempDir, "hpvolumes-csi")

	err = os.Mkdir(volumeDir, 0666)
	Expect(err).ToNot(HaveOccurred())
	//... setup driver ...
	cfg := &hostpath.Config{}
	cfg.Endpoint = filepath.Join(tempDir, sanityEndpoint)
	cfg.DriverName = "hostpath.csi.kubevirt.io"
	cfg.DataDir = volumeDir
	cfg.Version = "test-version"
	cfg.NodeID = "testnode"

	driver, err := hostpath.NewHostPathDriver(cfg)
	Expect(err).ToNot(HaveOccurred())

	go func() { 		
		err := driver.Run()
		Expect(err).ToNot(HaveOccurred())
	}() 

	testConfig := sanity.NewTestConfig()
	// Set configuration options as needed
	testConfig.Address = filepath.Join(tempDir, sanityEndpoint)
	testConfig.TargetPath = targetDir

	// Now call the test suite
	sanity.Test(t, testConfig)
}

