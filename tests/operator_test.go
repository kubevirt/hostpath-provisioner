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
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	hostpathprovisioner "kubevirt.io/hostpath-provisioner-operator/pkg/apis/hostpathprovisioner/v1beta1"
)

const (
	csiSa = "hostpath-provisioner-admin-csi"
	legacySa = "hostpath-provisioner-admin"
	csiClusterRole = "hostpath-provisioner-admin-csi"
	legacyClusterRole = "hostpath-provisioner"
	csiClusterRoleBinding = "hostpath-provisioner-admin-csi"
	legacyClusterRoleBinding = "hostpath-provisioner"
	csiRoleBinding = "hostpath-provisioner-admin-csi"
	csiRole = "hostpath-provisioner-admin-csi"

	dsName = "hostpath-provisioner"
	dsCsiName = "hostpath-provisioner-csi"

	namespace = "hostpath-provisioner"
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

	t.Run("Modify daemonset should be reverted", func(t *testing.T) {
		ds, err := k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		originalEnvVarLen := len(ds.Spec.Template.Spec.Containers[0].Env)

		ds.Spec.Template.Spec.Containers[0].Env = append(ds.Spec.Template.Spec.Containers[0].Env, v1.EnvVar{
			Name: "something",
			Value: "true",
		})
		_, err = k8sClient.AppsV1().DaemonSets(namespace).Update(context.TODO(), ds, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() int {
			// Assure original value is restored
			ds, err = k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			return len(ds.Spec.Template.Spec.Containers[0].Env)
		}, 2*time.Minute, 1*time.Second).Should(Equal(originalEnvVarLen))
	})

	t.Run("Modify daemonset should be reverted CSI", func(t *testing.T) {
		ds, err := k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsCsiName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		originalEnvVarLen := len(ds.Spec.Template.Spec.Containers[0].Env)

		ds.Spec.Template.Spec.Containers[0].Env = append(ds.Spec.Template.Spec.Containers[0].Env, v1.EnvVar{
			Name: "something",
			Value: "true",
		})
		_, err = k8sClient.AppsV1().DaemonSets(namespace).Update(context.TODO(), ds, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() int {
			// Assure original value is restored
			ds, err = k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsCsiName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			return len(ds.Spec.Template.Spec.Containers[0].Env)
		}, 2*time.Minute, 1*time.Second).Should(Equal(originalEnvVarLen))
	})

	t.Run("Delete daemonset should be restored", func(t *testing.T) {
		ds, err := k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.AppsV1().DaemonSets(namespace).Delete(context.TODO(), ds.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			// Assure original value is restored
			ds, err = k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())
			return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
	})

	t.Run("Delete daemonset should be restored CSI", func(t *testing.T) {
		ds, err := k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsCsiName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		err = k8sClient.AppsV1().DaemonSets(namespace).Delete(context.TODO(), ds.Name, metav1.DeleteOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() bool {
			// Assure original value is restored
			ds, err = k8sClient.AppsV1().DaemonSets(namespace).Get(context.TODO(), dsCsiName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return false
			}
			Expect(err).ToNot(HaveOccurred())
			return ds.Status.DesiredNumberScheduled == ds.Status.NumberReady
		}, 2*time.Minute, 1*time.Second).Should(BeTrue())
	})
}

func runChangeOnSaTest(saName string, k8sClient *kubernetes.Clientset) {
	sa, err := k8sClient.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), saName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	sa.Secrets = []corev1.ObjectReference{}
	_, err = k8sClient.CoreV1().ServiceAccounts(namespace).Update(context.TODO(), sa, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure secrets get repopulated
	Eventually(func() []corev1.ObjectReference {
		sa, err := k8sClient.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), saName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return sa.Secrets
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())
}

func runDeleteSaTest(saName string, k8sClient *kubernetes.Clientset) {
	sa, err := k8sClient.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), saName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	err = k8sClient.CoreV1().ServiceAccounts(namespace).Delete(context.TODO(), sa.Name, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure secrets get repopulated
	Eventually(func() error {
		_, err := k8sClient.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), saName, metav1.GetOptions{})
		return err
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
}

func TestReconcileChangeOnServiceAccount(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	t.Run("legacy provisioner sa modify", func(t *testing.T) {
		runChangeOnSaTest(legacySa, k8sClient)
	})
	t.Run("legacy provisioner sa delete", func(t *testing.T) {
		runDeleteSaTest(legacySa, k8sClient)
	})
	t.Run("csi driver sa modify", func(t *testing.T) {
		if isCSIStorageClass(k8sClient) {
			runChangeOnSaTest(csiSa, k8sClient)
		}
	})
	t.Run("csi driver sa delete", func(t *testing.T) {
		if isCSIStorageClass(k8sClient) {
			runDeleteSaTest(csiSa, k8sClient)
		}
	})
}

func runClusterRoleTest(roleName string, k8sClient *kubernetes.Clientset) {
	cr, err := k8sClient.RbacV1().ClusterRoles().Get(context.TODO(), roleName, metav1.GetOptions{})
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
		cr, err = k8sClient.RbacV1().ClusterRoles().Get(context.TODO(), roleName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return cr.Rules[0].Verbs
	}, 2*time.Minute, 1*time.Second).Should(ContainElement("list"))
}

func TestReconcileChangeOnClusterRole(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	t.Run("legacy provisioner", func(t *testing.T) {
		runClusterRoleTest(legacyClusterRole, k8sClient)
	})
	t.Run("csi driver", func(t *testing.T) {
		if isCSIStorageClass(k8sClient) {
			runClusterRoleTest(csiClusterRole, k8sClient)
		}
	})
}

func runClusterRoleBindingTest(clusterRoleName string,  k8sClient *kubernetes.Clientset) {
	crb, err := k8sClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), clusterRoleName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	crb.Subjects = []rbacv1.Subject{}
	_, err = k8sClient.RbacV1().ClusterRoleBindings().Update(context.TODO(), crb, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure subjects get repopulated
	Eventually(func() []rbacv1.Subject {
		crb, err = k8sClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), clusterRoleName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return crb.Subjects
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())

	err = k8sClient.RbacV1().ClusterRoleBindings().Delete(context.TODO(), crb.Name, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() error {
		_, err = k8sClient.RbacV1().ClusterRoleBindings().Get(context.TODO(), clusterRoleName, metav1.GetOptions{})
		return err
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())

}

func TestReconcileChangeOnClusterRoleBinding(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	t.Run("legacy provisioner", func(t *testing.T) {
		runClusterRoleBindingTest(legacyClusterRoleBinding, k8sClient)
	})
	t.Run("csi driver", func(t *testing.T) {
		if isCSIStorageClass(k8sClient) {
			runClusterRoleBindingTest(csiClusterRoleBinding, k8sClient)
		}
	})
}

func TestReconcileRoleBinding(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	rb, err := k8sClient.RbacV1().RoleBindings(namespace).Get(context.TODO(), csiRoleBinding, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	rb.Subjects = []rbacv1.Subject{}
	_, err = k8sClient.RbacV1().RoleBindings(namespace).Update(context.TODO(), rb, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure subjects get repopulated
	Eventually(func() []rbacv1.Subject {
		rb, err = k8sClient.RbacV1().RoleBindings(namespace).Get(context.TODO(), csiRoleBinding, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return rb.Subjects
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())

	err = k8sClient.RbacV1().RoleBindings(namespace).Delete(context.TODO(), rb.Name, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() error {
		_, err = k8sClient.RbacV1().RoleBindings(namespace).Get(context.TODO(), csiRoleBinding, metav1.GetOptions{})
		return err
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
}

func TestReconcileRole(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	role, err := k8sClient.RbacV1().Roles(namespace).Get(context.TODO(), csiRole, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	role.Rules = []rbacv1.PolicyRule{}
	_, err = k8sClient.RbacV1().Roles(namespace).Update(context.TODO(), role, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Assure subjects get repopulated
	Eventually(func() []rbacv1.PolicyRule {
		role, err = k8sClient.RbacV1().Roles(namespace).Get(context.TODO(), csiRole, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return role.Rules
	}, 2*time.Minute, 1*time.Second).ShouldNot(BeEmpty())

	err = k8sClient.RbacV1().Roles(namespace).Delete(context.TODO(), role.Name, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() error {
		_, err = k8sClient.RbacV1().Roles(namespace).Get(context.TODO(), csiRole, metav1.GetOptions{})
		return err
	}, 2*time.Minute, 1*time.Second).ShouldNot(HaveOccurred())
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

	origWorkload := cr.Spec.Workload.DeepCopy()
	defer func() {
		cr, err = hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())

		cr.Spec.Workload = *origWorkload.DeepCopy()

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

	cr.Spec.Workload = hostpathprovisioner.NodePlacement{
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

func verifyCsiDriver(k8sClient *kubernetes.Clientset) *storagev1.CSIDriver {
	driver, err := k8sClient.StorageV1().CSIDrivers().Get(context.TODO(), csiProvisionerName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(driver.Name).To(Equal(csiProvisionerName))
	Expect(driver.Spec.AttachRequired).ToNot(BeNil())
	Expect(*driver.Spec.AttachRequired).To(BeFalse())
	Expect(driver.Spec.PodInfoOnMount).ToNot(BeNil())
	Expect(*driver.Spec.PodInfoOnMount).To(BeTrue())
	Expect(driver.Spec.VolumeLifecycleModes).To(ContainElement(storagev1.VolumeLifecyclePersistent))
	return driver
}

func Test_CSIDriver(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	if !isCSIStorageClass(k8sClient) {
		t.Skip("Not CSI driver")
	}
	driver := verifyCsiDriver(k8sClient)
	t.Run("delete CSI driver", func(t *testing.T) {
		attachedRequiredMod := true
		driver.Spec.AttachRequired = &attachedRequiredMod
		err := k8sClient.StorageV1().CSIDrivers().Delete(context.TODO(), driver.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			// Assure original value is restored
			_, err = k8sClient.StorageV1().CSIDrivers().Get(context.TODO(), csiProvisionerName, metav1.GetOptions{})
			return err
		}, 2*time.Minute, 1*time.Second).Should(Not(HaveOccurred()))

		verifyCsiDriver(k8sClient)
	})
}

