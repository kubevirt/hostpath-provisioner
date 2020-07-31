package tests

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOperatorEventsInstall(t *testing.T) {
	RegisterTestingT(t)

	out, err := RunKubeCtlCommand("describe", "hostpathprovisioner", "hostpath-provisioner")
	Expect(err).ToNot(HaveOccurred())
	// Started Deploy
	Expect(out).To(ContainSubstring("DeployStarted"))
	// Finished Deploy
	Expect(out).To(ContainSubstring("ProvisionerHealthy"))
}

func TestOperatorEventsReconcileChange(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	ds, err := k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	ds.Spec.Template.Spec.Containers[0].Env[0].Value = "true"
	_, err = k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Update(context.TODO(), ds, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Started Deploy
	Eventually(func() string {
		out, err := RunKubeCtlCommand("describe", "hostpathprovisioner", "hostpath-provisioner")
		Expect(err).ToNot(HaveOccurred())
		return out
	}, 90*time.Second, 1*time.Second).Should(ContainSubstring("UpdateResourceStart"))
	// Finished Deploy
	Eventually(func() string {
		out, err := RunKubeCtlCommand("describe", "hostpathprovisioner", "hostpath-provisioner")
		Expect(err).ToNot(HaveOccurred())
		return out
	}, 90*time.Second, 1*time.Second).Should(ContainSubstring("UpdateResourceSuccess"))
}

func TestCRDExplainable(t *testing.T) {
	RegisterTestingT(t)

	// This test doesn't test all the fields exhaustively. It checks the top level, and some others to ensure
	// the explain works in general.
	// Test top level fields
	out, err := RunKubeCtlCommand("explain", "hostpathprovisioner")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).To(ContainSubstring("HostPathProvisionerSpec defines the desired state of HostPathProvisioner"))
	Expect(out).To(ContainSubstring("HostPathProvisionerStatus defines the observed state of HostPathProvisioner"))

	// Test status fields
	out, err = RunKubeCtlCommand("explain", "hostpathprovisioner.status")
	Expect(err).ToNot(HaveOccurred())
	Expect(out).To(ContainSubstring("Conditions contains the current conditions observed by the operator"))
	Expect(out).To(ContainSubstring("ObservedVersion The observed version of the HostPathProvisioner deployment"))
	Expect(out).To(ContainSubstring("OperatorVersion The version of the HostPathProvisioner Operator"))
	Expect(out).To(ContainSubstring("TargetVersion The targeted version of the HostPathProvisioner deployment"))
}
