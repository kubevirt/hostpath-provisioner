package tests

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestCreatePVCOnNode1(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)
	nodes, err := getAllNodes(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	annotations := make(map[string]string)
	annotations["kubevirt.io/provisionOnNode"] = nodes.Items[0].Name

	pvc := createPVCDef(ns.Name, "hostpath-provisioner", annotations)
	defer func() {
		// Cleanup
		if pvc != nil {
			t.Logf("Removing PVC: %s", pvc.Name)
			err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Delete(pvc.Name, &metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	t.Logf("Creating PVC: %s", pvc.Name)
	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(pvc)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimBound))

	pvs, err := k8sClient.CoreV1().PersistentVolumes().List(metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	hostpathPVs := getHostpathPVs(pvs.Items)
	Expect(pvc.Spec.VolumeName).To(Equal(hostpathPVs[0].Name))

	Expect(hostpathPVs[0].Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Key).To(Equal("kubernetes.io/hostname"))
	Expect(hostpathPVs[0].Spec.NodeAffinity.Required.NodeSelectorTerms[0].MatchExpressions[0].Values[0]).To(Equal(nodes.Items[0].Name))
}

func TestCreatePVCWaitForConsumer(t *testing.T) {
	RegisterTestingT(t)
	tearDown, ns, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)
	annotations := make(map[string]string)

	pvc := createPVCDef(ns.Name, "hostpath-provisioner", annotations)
	defer func() {
		// Cleanup
		if pvc != nil {
			t.Logf("Removing PVC: %s", pvc.Name)
			err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Delete(pvc.Name, &metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()

	t.Logf("Creating PVC: %s", pvc.Name)
	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(pvc)
	Expect(err).ToNot(HaveOccurred())

	Eventually(func() corev1.PersistentVolumeClaimPhase {
		pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(pvc.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pvc.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.ClaimPending))

	Expect(pvc.Spec.VolumeName).To(BeEmpty())

	pod := createPodUsingPVC(ns.Name, pvc, annotations)
	pod, err = k8sClient.CoreV1().Pods(ns.Name).Create(pod)
	Expect(err).ToNot(HaveOccurred())
	Eventually(func() corev1.PodPhase {
		pod, err = k8sClient.CoreV1().Pods(ns.Name).Get(pod.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return pod.Status.Phase
	}, 90*time.Second, 1*time.Second).Should(BeEquivalentTo(corev1.PodRunning))

	// Verify that the PVC is now Bound
	t.Logf("Creating POD %s that uses PVC %s", pod.Name, pvc.Name)
	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(pvc.Name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		// Cleanup
		if pod != nil {
			t.Logf("Removing Pod: %s", pod.Name)
			err := k8sClient.CoreV1().Pods(ns.Name).Delete(pod.Name, &metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
		}
	}()
	Expect(pvc.Status.Phase).To(Equal(corev1.ClaimBound))
}

func createPVCDef(namespace, storageClassName string, annotations map[string]string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-pvc",
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
		if ok && val == "kubevirt.io/hostpath-provisioner" {
			result = append(result, pv)
		}
	}
	return result
}
