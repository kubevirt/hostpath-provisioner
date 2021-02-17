package tests

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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

func TestReconcileChangeOnDaemonSet(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	ds, err := k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	originalEnvVarValue := ds.Spec.Template.Spec.Containers[0].Env[0].Value

	ds.Spec.Template.Spec.Containers[0].Env[0].Value = "true"
	_, err = k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Update(context.TODO(), ds, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	checkReconcileEventsOccur()

	// Assure original value is restored
	// No need to use polling here - we know that reconcile events occured prior to this
	ds, err = k8sClient.AppsV1().DaemonSets("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(ds.Spec.Template.Spec.Containers[0].Env[0].Value).To(Equal(originalEnvVarValue))
}

func TestReconcileChangeOnServiceAccount(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	sa, err := k8sClient.CoreV1().ServiceAccounts("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner-admin", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	sa.Secrets = []corev1.ObjectReference{}
	_, err = k8sClient.CoreV1().ServiceAccounts("hostpath-provisioner").Update(context.TODO(), sa, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure secrets get repopulated
	Eventually(func() []corev1.ObjectReference {
		sa, err := k8sClient.CoreV1().ServiceAccounts("hostpath-provisioner").Get(context.TODO(), "hostpath-provisioner-admin", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return sa.Secrets
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())
}

func TestReconcileChangeOnClusterRole(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	cr, err := k8sClient.RbacV1().ClusterRoles().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Remove list verb
	cr.Rules[0] = rbacv1.PolicyRule{
		APIGroups: []string{
			"",
		},
		Resources: []string{
			"persistentvolumes",
		},
		Verbs: []string{
			"get",
			// "list",
			"watch",
			"create",
			"delete",
		},
	}
	_, err = k8sClient.RbacV1().ClusterRoles().Update(context.TODO(), cr, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure "list" verb gets restored
	Eventually(func() []string {
		cr, err = k8sClient.RbacV1().ClusterRoles().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return cr.Rules[0].Verbs
	}, 2*time.Minute, 1*time.Second).Should(ContainElement("list"))
}

func TestReconcileChangeOnClusterRoleBinding(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	crb, err := k8sClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	crb.Subjects = []rbacv1.Subject{}
	_, err = k8sClient.RbacV1().ClusterRoleBindings().Update(context.TODO(), crb, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure subjects get repopulated
	Eventually(func() []rbacv1.Subject {
		crb, err = k8sClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return crb.Subjects
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())
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
