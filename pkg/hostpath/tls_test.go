/*
Copyright 2026 The hostpath provisioner Authors.

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
	"crypto/tls"
	"crypto/x509"
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_generateSelfSignedCert(t *testing.T) {
	RegisterTestingT(t)

	t.Run("generates valid certificate with localhost SANs", func(t *testing.T) {
		// Clear environment variables
		os.Unsetenv("POD_IP")
		os.Unsetenv("POD_NAMESPACE")
		os.Unsetenv("SERVICE_NAME")

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())
		Expect(cert.Certificate).ToNot(BeEmpty())

		// Parse the certificate to verify SANs
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Should have localhost DNS name
		Expect(x509Cert.DNSNames).To(ContainElement("localhost"))

		// Should have 127.0.0.1 IP (compare string representation)
		hasLocalhost := false
		for _, ip := range x509Cert.IPAddresses {
			if ip.String() == "127.0.0.1" {
				hasLocalhost = true
				break
			}
		}
		Expect(hasLocalhost).To(BeTrue(), "certificate should include 127.0.0.1")
	})

	t.Run("includes pod IP when POD_IP is set", func(t *testing.T) {
		os.Setenv("POD_IP", "10.244.0.5")
		defer os.Unsetenv("POD_IP")

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Collect IP strings for easier comparison
		ipStrings := make([]string, len(x509Cert.IPAddresses))
		for i, ip := range x509Cert.IPAddresses {
			ipStrings[i] = ip.String()
		}

		// Should include the pod IP
		Expect(ipStrings).To(ContainElement("10.244.0.5"))
		// Should still include localhost IP
		Expect(ipStrings).To(ContainElement("127.0.0.1"))
	})

	t.Run("includes IP-based pod DNS when POD_IP and POD_NAMESPACE are set", func(t *testing.T) {
		os.Setenv("POD_IP", "10.244.0.5")
		os.Setenv("POD_NAMESPACE", "default")
		defer func() {
			os.Unsetenv("POD_IP")
			os.Unsetenv("POD_NAMESPACE")
		}()

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Should include IP-based pod DNS (Kubernetes standard format)
		Expect(x509Cert.DNSNames).To(ContainElement("10-244-0-5.default.pod.cluster.local"))
		// Should still include localhost
		Expect(x509Cert.DNSNames).To(ContainElement("localhost"))
	})

	t.Run("includes service DNS names when SERVICE_NAME and POD_NAMESPACE are set", func(t *testing.T) {
		os.Setenv("SERVICE_NAME", "hostpath-metrics")
		os.Setenv("POD_NAMESPACE", "default")
		defer func() {
			os.Unsetenv("SERVICE_NAME")
			os.Unsetenv("POD_NAMESPACE")
		}()

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Should include service-based DNS names
		Expect(x509Cert.DNSNames).To(ContainElement("hostpath-metrics"))
		Expect(x509Cert.DNSNames).To(ContainElement("hostpath-metrics.default"))
		Expect(x509Cert.DNSNames).To(ContainElement("hostpath-metrics.default.svc"))
		Expect(x509Cert.DNSNames).To(ContainElement("hostpath-metrics.default.svc.cluster.local"))
	})

	t.Run("includes all SANs when all environment variables are set", func(t *testing.T) {
		os.Setenv("POD_IP", "10.244.0.5")
		os.Setenv("POD_NAMESPACE", "kube-system")
		os.Setenv("SERVICE_NAME", "hostpath-metrics")
		defer func() {
			os.Unsetenv("POD_IP")
			os.Unsetenv("POD_NAMESPACE")
			os.Unsetenv("SERVICE_NAME")
		}()

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Collect IP strings for easier comparison
		ipStrings := make([]string, len(x509Cert.IPAddresses))
		for i, ip := range x509Cert.IPAddresses {
			ipStrings[i] = ip.String()
		}

		// Verify IP addresses
		Expect(ipStrings).To(ContainElement("127.0.0.1"))
		Expect(ipStrings).To(ContainElement("10.244.0.5"))

		// Verify DNS names include all variations
		expectedDNSNames := []string{
			"localhost",
			// IP-based pod DNS (Kubernetes standard)
			"10-244-0-5.kube-system.pod.cluster.local",
			// Service DNS variations
			"hostpath-metrics",
			"hostpath-metrics.kube-system",
			"hostpath-metrics.kube-system.svc",
			"hostpath-metrics.kube-system.svc.cluster.local",
		}
		for _, dns := range expectedDNSNames {
			Expect(x509Cert.DNSNames).To(ContainElement(dns))
		}
	})

	t.Run("includes IPv6-based pod DNS when POD_IP is IPv6", func(t *testing.T) {
		os.Setenv("POD_IP", "2001:db8::1")
		os.Setenv("POD_NAMESPACE", "default")
		defer func() {
			os.Unsetenv("POD_IP")
			os.Unsetenv("POD_NAMESPACE")
		}()

		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Collect IP strings for easier comparison
		ipStrings := make([]string, len(x509Cert.IPAddresses))
		for i, ip := range x509Cert.IPAddresses {
			ipStrings[i] = ip.String()
		}

		// Should include the IPv6 address
		Expect(ipStrings).To(ContainElement("2001:db8::1"))

		// Should include IPv6-based pod DNS (colons replaced with dashes)
		Expect(x509Cert.DNSNames).To(ContainElement("2001-db8--1.default.pod.cluster.local"))
	})

	t.Run("handles invalid POD_IP gracefully", func(t *testing.T) {
		os.Setenv("POD_IP", "invalid-ip")
		defer os.Unsetenv("POD_IP")

		// Should still succeed, just skip the invalid IP
		cert, err := generateSelfSignedCert()
		Expect(err).ToNot(HaveOccurred())

		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		Expect(err).ToNot(HaveOccurred())

		// Should only have localhost IP, not the invalid one
		Expect(x509Cert.IPAddresses).To(HaveLen(1))
		Expect(x509Cert.IPAddresses[0].String()).To(Equal("127.0.0.1"))
	})
}

func Test_getTLSVersion(t *testing.T) {
	RegisterTestingT(t)

	tests := []struct {
		name         string
		versionName  string
		wantVersion  *uint16
		wantNil      bool
	}{
		{
			name:        "VersionTLS10",
			versionName: "VersionTLS10",
			wantVersion: func() *uint16 { v := uint16(tls.VersionTLS10); return &v }(),
		},
		{
			name:        "VersionTLS11",
			versionName: "VersionTLS11",
			wantVersion: func() *uint16 { v := uint16(tls.VersionTLS11); return &v }(),
		},
		{
			name:        "VersionTLS12",
			versionName: "VersionTLS12",
			wantVersion: func() *uint16 { v := uint16(tls.VersionTLS12); return &v }(),
		},
		{
			name:        "VersionTLS13",
			versionName: "VersionTLS13",
			wantVersion: func() *uint16 { v := uint16(tls.VersionTLS13); return &v }(),
		},
		{
			name:        "invalid version string",
			versionName: "InvalidVersion",
			wantNil:     true,
		},
		{
			name:        "empty string",
			versionName: "",
			wantNil:     true,
		},
		{
			name:        "numeric version string",
			versionName: "1.3",
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTLSVersion(tt.versionName)

			if tt.wantNil {
				Expect(result).To(BeNil(), "expected nil for version: %s", tt.versionName)
			} else {
				Expect(result).ToNot(BeNil(), "expected non-nil for version: %s", tt.versionName)
				Expect(*result).To(Equal(*tt.wantVersion), "version mismatch for: %s", tt.versionName)
			}
		})
	}
}
