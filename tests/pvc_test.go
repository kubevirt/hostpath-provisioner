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
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	csiStorageClassName = "hostpath-csi"
	legacyStorageClassName = "hostpath-provisioner"
	legacyStorageClassNameImmediate = "hostpath-provisioner-immediate"
)
func TestCreatePVCOnNode1(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)

	nodes, err := getAllNodes(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	annotations := make(map[string]string)
	annotations["kubevirt.io/provisionOnNode"] = nodes.Items[0].Name

	pvc := createPVCDef(ns.Name, legacyStorageClassNameImmediate, annotations)
	defer func() {
		// Cleanup
		if pvc != nil {
			t.Logf("Removing PVC: %s", pvc.Name)
			err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	t.Logf("Creating PVC: %s", pvc.Name)
	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(context.TODO(), pvc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimBound))

	pvs, err := k8sClient.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	hostpathPVs := getHostpathPVs(pvs.Items)
	found := false
	for _, pv := range hostpathPVs {
		if pvc.Spec.VolumeName == pv.Name {
			found = true
			Expect(pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Key).To(Equal("kubernetes.io/hostname"))
			Expect(pv.Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values[0]).To(Equal(nodes.Items[0].Name))
		}
	}
	Expect(found).To(BeTrue())
}

func createPVCWaitForFirstConsumerTest(storageClassName string, ns *v1.Namespace, k8sClient *kubernetes.Clientset, t *testing.T) {
	annotations := make(map[string]string)
	pvc := createPVCDef(ns.Name, storageClassName, annotations)
	defer func() {
		// Cleanup
		if pvc != nil {
			t.Logf("Removing PVC: %s", pvc.Name)
			err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	t.Logf("Creating PVC: %s", pvc.Name)
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(context.TODO(), pvc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimPending))

	Expect(pvc.Spec.VolumeName).To(BeEmpty())

	pod := createPodUsingPVC(ns.Name, pvc, annotations)
	pod, err = k8sClient.CoreV1().Pods(ns.Name).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() bool {
		pod, err = k8sClient.CoreV1().Pods(ns.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
	}, 90*time.Second, 1*time.Second).Should(BeTrue())

	// Verify that the PVC is now Bound
	t.Logf("Creating POD %s that uses PVC %s", pod.Name, pvc.Name)
	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		// Cleanup
		if pod != nil {
			t.Logf("Removing Pod: %s", pod.Name)
			err := k8sClient.CoreV1().Pods(ns.Name).Delete(context.TODO(), pod.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()
	Expect(pvc.Status.Phase).To(Equal(corev1.ClaimBound))
}

func TestCreatePVCWaitForConsumerLegacy(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)

	createPVCWaitForFirstConsumerTest(legacyStorageClassName, ns, k8sClient, t)
}

func TestCreatePVCWaitForConsumerCsi(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)

	createPVCWaitForFirstConsumerTest(csiStorageClassName, ns, k8sClient, t)
}

func TestPVCSize(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)
	annotations := make(map[string]string)

	pvc := createPVCDef(ns.Name, legacyStorageClassName, annotations)
	defer func() {
		// Cleanup
		if pvc != nil {
			t.Logf("Removing PVC: %s", pvc.Name)
			err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	dfString, err := RunGoCLICommand("../cluster-up/ssh.sh", "node01", "df -Bk /var/hpvolumes | sed 1d")
	Expect(err).ToNot(HaveOccurred())
	sizeQuantity := resource.MustParse(strings.ToLower(strings.Fields(dfString)[1]))
	int64Size, _ := sizeQuantity.AsInt64()
	hostQuantity := resource.NewQuantity(int64(roundDownCapacityPretty(int64Size)), resource.BinarySI)
	t.Logf("Reported size on host: %s", hostQuantity.String())

	t.Logf("Creating PVC: %s", pvc.Name)
	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(context.TODO(), pvc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimPending))

	Expect(pvc.Spec.VolumeName).To(BeEmpty())

	pod := createPodUsingPVC(ns.Name, pvc, annotations)
	pod, err = k8sClient.CoreV1().Pods(ns.Name).Create(context.TODO(), pod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() bool {
		pod, err = k8sClient.CoreV1().Pods(ns.Name).Get(context.TODO(), pod.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded
	}, 90*time.Second, 1*time.Second).Should(BeTrue())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimBound))

	pvs, err := k8sClient.CoreV1().PersistentVolumes().List(context.TODO(), metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	hostpathPVs := getHostpathPVs(pvs.Items)

	found := false
	for _, pv := range hostpathPVs {
		if pvc.Spec.VolumeName == pv.Name {
			found = true
			pvQuantity := pv.Spec.Capacity[v1.ResourceStorage]
			t.Logf("pv: %v, host: %v", pvQuantity, *hostQuantity)
			Expect(pvQuantity.Cmp(*hostQuantity)).To(Equal(0))
		}
	}
	Expect(found).To(BeTrue())

}

func createPVCDef(namespace, storageClassName string, annotations map[string]string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-pvc",
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("200Mi"),
				},
			},
			StorageClassName: &storageClassName,
		},
	}
}

func createPodUsingPVC(namespace string, pvc *corev1.PersistentVolumeClaim, annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pod",
			Namespace:   namespace,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "runner",
					Image:   "kubevirt/cdi-importer:latest",
					Command: []string{"/bin/sh", "-c", "sleep 1"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: pvc.GetName(),
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.GetName(),
						},
					},
				},
			},
		},
	}
}

func getHostpathPVs(allPvs []corev1.PersistentVolume) []corev1.PersistentVolume {
	result := make([]corev1.PersistentVolume, 0)
	for _, pv := range allPvs {
		val, ok := pv.GetAnnotations()["pv.kubernetes.io/provisioned-by"]
		if ok && (val == legacyProvisionerName || val == csiProvisionerName) {
			result = append(result, pv)
		}
	}
	return result
}
