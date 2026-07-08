/*
Copyright 2024 The hostpath provisioner Authors.

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
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	hostpathprovisionerv1 "kubevirt.io/hostpath-provisioner-operator/pkg/apis/hostpathprovisioner/v1beta1"
)

const (
	hppNamespace  = "hostpath-provisioner"
	hppCRName     = "hostpath-provisioner"
	poolMountName = "pool-volume"
)

// sharedPoolPVCName returns the pool PVC name for a shared (RWX) storage pool.
func sharedPoolPVCName(poolName string) string {
	return fmt.Sprintf("hpp-pool-%s-shared", poolName)
}

// findNFSOverlayPool returns the first storage pool backed by an NFS PVC with an
// overlay class configured, or nil if none exists.
func findNFSOverlayPool(k8sClient *kubernetes.Clientset) *hostpathprovisionerv1.StoragePool {
	hppClient, err := getHPPClient()
	Expect(err).ToNot(HaveOccurred())
	cr, err := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.TODO(), "hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	for i, pool := range cr.Spec.StoragePools {
		if pool.PVCTemplate == nil || pool.OverlayClassName == "" || pool.PVCTemplate.StorageClassName == nil {
			continue
		}
		sc, err := k8sClient.StorageV1().StorageClasses().Get(context.TODO(), *pool.PVCTemplate.StorageClassName, metav1.GetOptions{})
		if err != nil {
			continue
		}
		if strings.Contains(sc.Provisioner, "nfs") {
			return &cr.Spec.StoragePools[i]
		}
	}
	return nil
}

// TestNFSStoragePoolMounterRecovery verifies that the hpp-pool mounter pod recovers correctly
// when multiple pods are mounting the same NFS-backed pool PVC simultaneously.
// This is a regression test for https://github.com/kubevirt/hostpath-provisioner-operator/issues/721.
func TestNFSStoragePoolMounterRecovery(t *testing.T) {
	RegisterTestingT(t)

	k8sClient, err := getKubeClient()
	Expect(err).ToNot(HaveOccurred())

	pool := findNFSOverlayPool(k8sClient)
	if pool == nil {
		t.Skip("No NFS-backed overlay storage pool found — skipping")
	}
	t.Logf("Found NFS overlay pool: %s", pool.Name)

	pvcName := sharedPoolPVCName(pool.Name)
	poolPVC, err := k8sClient.CoreV1().PersistentVolumeClaims(hppNamespace).Get(context.TODO(), pvcName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Logf("Pool PVC: %s", poolPVC.Name)

	// Find a node running an hpp-pool pod for this pool.
	poolPods, err := k8sClient.CoreV1().Pods(hppNamespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: fmt.Sprintf("k8s-app=hostpath-provisioner,kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", pool.Name),
	})
	Expect(err).ToNot(HaveOccurred())
	if len(poolPods.Items) == 0 {
		t.Skip("No hpp-pool pods found for pool — skipping")
	}
	targetNode := poolPods.Items[0].Spec.NodeName
	t.Logf("Targeting node: %s", targetNode)

	// Create a second pod that directly mounts the pool PVC on the same node.
	// This simulates the scenario from issue #721 where multiple pod mounts of
	// the same NFS source cause the mounter to crash on restart.
	consumerPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pool-consumer-test-",
			Namespace:    hppNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName:      targetNode,
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    "consumer",
					Image:   "quay.io/kubevirt/cdi-importer:latest-amd64",
					Command: []string{"/bin/sh", "-c", "sleep infinity"},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      poolMountName,
							MountPath: "/mnt/pool",
						},
					},
					SecurityContext: &corev1.SecurityContext{
						AllowPrivilegeEscalation: ptr.To(false),
						Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
						RunAsNonRoot:             ptr.To(true),
						SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: poolMountName,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: poolPVC.Name,
						},
					},
				},
			},
		},
	}
	consumerPod, err = k8sClient.CoreV1().Pods(hppNamespace).Create(context.TODO(), consumerPod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		t.Logf("Cleaning up consumer pod: %s", consumerPod.Name)
		_ = k8sClient.CoreV1().Pods(hppNamespace).Delete(context.TODO(), consumerPod.Name, metav1.DeleteOptions{})
	}()

	t.Logf("Waiting for consumer pod %s to be Running", consumerPod.Name)
	Eventually(func() corev1.PodPhase {
		consumerPod, err = k8sClient.CoreV1().Pods(hppNamespace).Get(context.TODO(), consumerPod.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return consumerPod.Status.Phase
	}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

	// Delete the hpp-pool mounter pod on the target node to force a restart.
	// With multiple pod mounts on the same NFS source, the mounter must correctly
	// identify the bind source and not crash.
	originalPodName := poolPods.Items[0].Name
	t.Logf("Deleting hpp-pool pod %s to trigger restart", originalPodName)
	err = k8sClient.CoreV1().Pods(hppNamespace).Delete(context.TODO(), originalPodName, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())

	// Verify the new mounter pod comes up Running and does not enter CrashLoopBackOff.
	t.Logf("Waiting for hpp-pool pod on %s to recover", targetNode)
	Eventually(func() bool {
		pods, err := k8sClient.CoreV1().Pods(hppNamespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("k8s-app=hostpath-provisioner,kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", pool.Name),
			FieldSelector: fmt.Sprintf("spec.nodeName=%s", targetNode),
		})
		if err != nil {
			return false
		}
		for _, pod := range pods.Items {
			if pod.Name == originalPodName {
				continue // still the old pod being terminated
			}
			if pod.Status.Phase == corev1.PodRunning {
				t.Logf("hpp-pool pod %s is Running", pod.Name)
				return true
			}
			// Fail fast if it enters CrashLoopBackOff
			for _, cs := range pod.Status.ContainerStatuses {
				if cs.State.Waiting != nil && cs.State.Waiting.Reason == "CrashLoopBackOff" {
					t.Errorf("hpp-pool mounter entered CrashLoopBackOff: %s", cs.State.Waiting.Message)
					return false
				}
			}
		}
		return false
	}, 120*time.Second, 2*time.Second).Should(BeTrue(), "hpp-pool mounter should recover to Running with multiple NFS pod mounts present")

	// Verify the overlay storage class can still provision PVCs after mounter recovery.
	// This confirms the bind mount at the pool path was correctly re-established.
	t.Logf("Verifying PVC provisioning from overlay class %s after recovery", pool.OverlayClassName)
	overlayPVC := createPVCDef(hppNamespace, pool.OverlayClassName, map[string]string{})
	overlayPVC, err = k8sClient.CoreV1().PersistentVolumeClaims(hppNamespace).Create(context.TODO(), overlayPVC, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		t.Logf("Cleaning up overlay PVC: %s", overlayPVC.Name)
		_ = k8sClient.CoreV1().PersistentVolumeClaims(hppNamespace).Delete(context.TODO(), overlayPVC.Name, metav1.DeleteOptions{})
	}()

	overlayPod := createPodUsingPVCWithCommand(hppNamespace, "overlay-verify-pod", overlayPVC, "sleep 1", map[string]string{})
	overlayPod, err = k8sClient.CoreV1().Pods(hppNamespace).Create(context.TODO(), overlayPod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	defer func() {
		t.Logf("Cleaning up overlay pod: %s", overlayPod.Name)
		_ = k8sClient.CoreV1().Pods(hppNamespace).Delete(context.TODO(), overlayPod.Name, metav1.DeleteOptions{})
	}()

	Eventually(func() bool {
		overlayPod, err = k8sClient.CoreV1().Pods(hppNamespace).Get(context.TODO(), overlayPod.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return overlayPod.Status.Phase == corev1.PodRunning || overlayPod.Status.Phase == corev1.PodSucceeded
	}, 90*time.Second, 2*time.Second).Should(BeTrue(), "pod using overlay PVC should run successfully after mounter recovery")
	t.Logf("Overlay PVC provisioning and pod scheduling verified successfully")
}
