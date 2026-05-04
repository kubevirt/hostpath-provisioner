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
package main

import (
	"flag"
	"os"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"kubevirt.io/hostpath-provisioner/pkg/hostpath"
)

func init() {
}

func main() {
	defer klog.Flush()
	cfg := &hostpath.Config{}
	var dataDir string
	var metricsCertFile string
	var metricsKeyFile string
	var metricsTLSVersion string
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.StringVar(&cfg.Endpoint, "endpoint", "unix://tmp/csi.sock", "CSI endpoint")
	flag.StringVar(&cfg.DriverName, "drivername", "hostpath.csi.kubevirt.io", "name of the driver")
	flag.StringVar(&dataDir, "datadir", "[{\"name\":\"legacy\",\"path\":\"/csi-data-dir\",\"snapshotPath\":\"/snap-dir\", \"snapshotProvider\":\"reflink\"}]", "storage pool array with each entry including, storage pool name, directory path, and optional snapshot directory path and snapshot provider, all of this in JSON format. Example: [{\"name\":\"legacy\",\"path\":\"/csi-data-dir\",\"snapshotPath\":\"/snap-dir\",\"snapshotProvider\":\"reflink\"}]")
	flag.StringVar(&cfg.NodeID, "nodeid", "", "node id")
	flag.StringVar(&cfg.Version, "version", "", "version of the plugin")
	flag.StringVar(&metricsCertFile, "metrics-cert-file", "", "path to TLS certificate file for metrics server (optional, will use self-signed cert if not provided). Note: self-signed certs only include localhost+127.0.0.1 by default; set POD_IP (most important), POD_NAMESPACE, and SERVICE_NAME env vars for in-cluster SANs needed for certificate verification")
	flag.StringVar(&metricsKeyFile, "metrics-key-file", "", "path to TLS key file for metrics server (optional, will use self-signed cert if not provided)")
	flag.StringVar(&metricsTLSVersion, "metrics-tls-version", "VersionTLS13", "minimum TLS version for metrics server (VersionTLS10, VersionTLS11, VersionTLS12, or VersionTLS13)")
	flag.Parse()

	klog.V(1).Info("Starting Prometheus metrics endpoint server")
	hostpath.RunPrometheusServer(":8443", metricsCertFile, metricsKeyFile, metricsTLSVersion)

	klog.V(1).Infof("Starting new HostPathDriver, config: %v", *cfg)
	ctx := signals.SetupSignalHandler()
	driver, err := hostpath.NewHostPathDriver(ctx, cfg, dataDir)
	if err != nil {
		klog.V(1).Infof("Failed to initialize driver: %s", err.Error())
		os.Exit(1)
	}

	if err := driver.Run(); err != nil {
		klog.V(1).Infof("Failed to run driver: %s", err.Error())
		os.Exit(1)

	}
}
