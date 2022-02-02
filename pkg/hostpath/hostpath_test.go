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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"

	. "github.com/onsi/gomega"
	"k8s.io/utils/mount"
)

const (
	TestDatadirValue = "[{\"name\":\"local\",\"path\":\"%s\"}]"
)

func Test_NewHostPathDriver(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	oldCsiSocketDir := csiSocketDir
	csiSocketDir = "/dev"
	defer func() {
		os.RemoveAll(tempDir)
		csiSocketDir = oldCsiSocketDir
	}()

	t.Run("blank config", func(t *testing.T) {
		cfg := &Config{}
		_, err = NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, tempDir))
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(errors.New("no driver name provided")))
	})

	t.Run("just driver name", func(t *testing.T) {
		cfg := &Config{
			DriverName: "test_driver",
		}
		_, err = NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, tempDir))
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(errors.New("no node id provided")))
	})

	t.Run("no driver endpoint", func(t *testing.T) {
		cfg := &Config{
			DriverName: "test_driver",
			NodeID:     "test_nodeid",
		}
		_, err = NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, tempDir))
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(errors.New("no driver endpoint provided")))
	})

	t.Run("no version", func(t *testing.T) {
		cfg := &Config{
			DriverName: "test_driver",
			NodeID:     "test_nodeid",
			Endpoint:   "unix://test.sock",
		}
		_, err = NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, tempDir))
		Expect(err).To(HaveOccurred())
		Expect(err).To(BeEquivalentTo(errors.New("no version provided")))
	})

	t.Run("valid config", func(t *testing.T) {
		cfg := &Config{
			DriverName: "test_driver",
			NodeID:     "test_nodeid",
			Endpoint:   "unix://test.sock",
			Version:    "test_version",
			Mounter:    mount.NewFakeMounter([]mount.MountPoint{}), // If not set it will try to create a real mounter
		}
		drv, err := NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, filepath.Join(tempDir, "testdatadir")))
		Expect(err).ToNot(HaveOccurred())
		Expect(drv.node).ToNot(BeNil())
		Expect(drv.controller).ToNot(BeNil())
		Expect(drv.identity).ToNot(BeNil())
		_, err = os.Stat(filepath.Join(tempDir, "testdatadir"))
		Expect(err).ToNot(HaveOccurred())
	})

	t.Run("valid config pool path shared with OS metric", func(t *testing.T) {
		cfg := &Config{
			DriverName: "test_driver",
			NodeID:     "test_nodeid",
			Endpoint:   "unix://test.sock",
			Version:    "test_version",
			Mounter:    mount.NewFakeMounter([]mount.MountPoint{}), // If not set it will try to create a real mounter
		}
		drv, err := NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, filepath.Join(tempDir, "testdatadir")))
		Expect(err).ToNot(HaveOccurred())
		Expect(drv.node).ToNot(BeNil())
		Expect(drv.controller).ToNot(BeNil())
		Expect(drv.identity).ToNot(BeNil())
		_, err = os.Stat(filepath.Join(tempDir, "testdatadir"))
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(1 * time.Second)
		dto := &io_prometheus_client.Metric{}
		poolPathSharedWithOsGauge.Write(dto)
		Expect(dto.Gauge.GetValue()).To(BeEquivalentTo(0))

		// Now switch the socket dir, so we are indeed sharing path with OS (we call NewHostPathDriver with tmpfs backed folder)
		csiSocketDir = "/tmp"
		drv, err = NewHostPathDriver(context.TODO(), cfg, fmt.Sprintf(TestDatadirValue, filepath.Join(tempDir, "testdatadir")))
		Expect(err).ToNot(HaveOccurred())
		Expect(drv.node).ToNot(BeNil())
		Expect(drv.controller).ToNot(BeNil())
		Expect(drv.identity).ToNot(BeNil())
		_, err = os.Stat(filepath.Join(tempDir, "testdatadir"))
		Expect(err).ToNot(HaveOccurred())
		time.Sleep(1 * time.Second)
		dto = &io_prometheus_client.Metric{}
		poolPathSharedWithOsGauge.Write(dto)
		Expect(dto.Gauge.GetValue()).To(BeEquivalentTo(1))
	})
}
