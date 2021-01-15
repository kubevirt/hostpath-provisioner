package tests

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	hostpathprovisioner "kubevirt.io/hostpath-provisioner-operator/pkg/apis/hostpathprovisioner/v1beta1"
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

func TestNodeSelector(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	hppClient, err := getHPPClient()
	Expect(err).ToNot(HaveOccurred())

	cr, err := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	nodes, _ := k8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	affinityTestValue := &v1.Affinity{
		NodeAffinity: &v1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
				NodeSelectorTerms: []v1.NodeSelectorTerm{
					{
						MatchExpressions: []v1.NodeSelectorRequirement{
							{Key: "kubernetes.io/hostname", Operator: v1.NodeSelectorOpIn, Values: []string{nodes.Items[0].Name}},
						},
					},
				},
			},
		},
	}
	nodeSelectorTestValue := map[string]string{"kubernetes.io/arch": "not-a-real-architecture"}
	tolerationsTestValue := []v1.Toleration{{Key: "test", Value: "123"}}

	origWorkloads := cr.Spec.Workloads.DeepCopy()
	defer func() {
		cr, err = hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		cr.Spec.Workloads = *origWorkloads.DeepCopy()

		_, err = hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Update(context.TODO(), cr, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			ds, err := k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
			if err != nil {
				return false
			}

			podSpec := ds.Spec.Template.Spec

			if reflect.DeepEqual(podSpec.NodeSelector, nodeSelectorTestValue) {
				fmt.Printf("Node selector is actually %v\n", podSpec.NodeSelector)
				return false
			}
			return true
		}, 270*time.Second, 1*time.Second).Should(BeTrue())
	}()

	cr.Spec.Workloads = hostpathprovisioner.NodePlacement{
		NodeSelector: nodeSelectorTestValue,
		Affinity:     affinityTestValue,
		Tolerations:  tolerationsTestValue,
	}

	_, err = hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Update(context.TODO(), cr, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() bool {
		ds, err := k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
		if err != nil {
			return false
		}

		podSpec := ds.Spec.Template.Spec

		if !reflect.DeepEqual(podSpec.NodeSelector, nodeSelectorTestValue) {
			fmt.Printf("mismatched nodeSelectors, podSpec:\n%v\nExpected:\n%v\n", podSpec.NodeSelector, nodeSelectorTestValue)
			return false
		}
		if !reflect.DeepEqual(podSpec.Affinity, affinityTestValue) {
			fmt.Printf("mismatched affinity, podSpec:\n%v\nExpected:\n%v\n", podSpec.Affinity, affinityTestValue)
			return false
		}
		foundMatchingTolerations := false
		for _, toleration := range podSpec.Tolerations {
			if toleration == tolerationsTestValue[0] {
				foundMatchingTolerations = true
			}
		}
		if foundMatchingTolerations != true {
			fmt.Printf("no matching tolerations found. podSpec:\n%v\nExpected:\n%v\n", podSpec.Tolerations, tolerationsTestValue)
			return false
		}
		return true
	}, 90*time.Second, 1*time.Second).Should(BeTrue())

}
