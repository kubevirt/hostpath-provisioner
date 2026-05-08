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
	"fmt"
	"net"
	"os"
	"strings"

	certutil "k8s.io/client-go/util/cert"
	"k8s.io/klog/v2"
)

// generateSelfSignedCert generates a self-signed certificate for the metrics server.
//
// Default SANs (always included):
//   - DNS: localhost
//   - IP: 127.0.0.1
//
// Additional SANs (if environment variables are set, typically via downward API):
//   - POD_IP: adds pod IP address (most reliable for Endpoints-based scraping)
//   - POD_IP + POD_NAMESPACE: adds IP-based pod DNS (e.g., 10-244-0-5.default.pod.cluster.local)
//   - SERVICE_NAME + POD_NAMESPACE: adds service DNS names (*.svc, *.svc.cluster.local)
//
// IMPORTANT: Without the additional environment variables, certificate verification will fail
// for clients that verify the server certificate. These env vars should be set via the downward
// API if TLS verification is required.
//
// NOTE: For most in-cluster clients, the pod IP SAN is sufficient since they typically connect
// via pod IPs directly (e.g., Endpoints). Service DNS is included for service-based connections.
func generateSelfSignedCert() (tls.Certificate, error) {
	// Collect IP addresses
	ips := []net.IP{{127, 0, 0, 1}} // localhost

	// Try to get pod IP from environment (set by downward API)
	// This is the most reliable SAN for Endpoints-based Prometheus scraping
	podIP := os.Getenv("POD_IP")
	var ipWithDashes string
	if podIP != "" {
		if ip := net.ParseIP(podIP); ip != nil {
			ips = append(ips, ip)
			klog.V(3).Infof("Added pod IP %s to certificate SANs", podIP)

			// Generate IP-with-dashes format for Kubernetes pod DNS
			// IPv4: 10.244.0.5 → 10-244-0-5
			// IPv6: 2001:db8::1 → 2001-db8--1
			if ip.To4() != nil {
				// IPv4: replace dots with dashes
				ipWithDashes = strings.ReplaceAll(podIP, ".", "-")
			} else {
				// IPv6: replace colons with dashes
				ipWithDashes = strings.ReplaceAll(podIP, ":", "-")
			}
		}
	}

	// Collect DNS names for common in-cluster scraping patterns
	dnsNames := []string{"localhost"}

	// Add IP-based pod DNS pattern (the standard Kubernetes pattern)
	// Format: <ip-with-dashes>.<namespace>.pod.cluster.local
	podNamespace := os.Getenv("POD_NAMESPACE")
	if ipWithDashes != "" && podNamespace != "" {
		dnsNames = append(dnsNames, fmt.Sprintf("%s.%s.pod.cluster.local", ipWithDashes, podNamespace))
		klog.V(3).Infof("Added IP-based pod DNS: %s.%s.pod.cluster.local", ipWithDashes, podNamespace)
	}

	// Add service-based DNS patterns if SERVICE_NAME is available
	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName != "" && podNamespace != "" {
		// Service FQDN: <service-name>.<namespace>.svc.cluster.local
		dnsNames = append(dnsNames, serviceName)
		dnsNames = append(dnsNames, fmt.Sprintf("%s.%s", serviceName, podNamespace))
		dnsNames = append(dnsNames, fmt.Sprintf("%s.%s.svc", serviceName, podNamespace))
		dnsNames = append(dnsNames, fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, podNamespace))
	}

	klog.V(3).Infof("Generating self-signed certificate with DNS SANs: %s", strings.Join(dnsNames, ", "))

	// Generate self-signed certificate
	// Note: Using self-signed certificates here should be good enough. It's just important that we
	// encrypt the communication. For example kube-controller-manager also uses a self-signed certificate
	// for metrics.
	cert, key, err := certutil.GenerateSelfSignedCertKey("localhost", ips, dnsNames)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to generate self-signed certificate: %w", err)
	}

	keyPair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("failed to create self-signed key pair: %w", err)
	}

	return keyPair, nil
}
