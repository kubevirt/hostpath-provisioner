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
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	hostpathprovisionerv1 "kubevirt.io/hostpath-provisioner-operator/pkg/apis/hostpathprovisioner/v1beta1"
	hppclientv1 "kubevirt.io/hostpath-provisioner-operator/pkg/client/clientset/versioned/typed/hostpathprovisioner/v1beta1"
)

const (
	hppNamespace  = "hostpath-provisioner"
	poolMountName = "pool-volume"
	hppName       = "hostpath-provisioner"
	hppCSIName    = "hostpath-provisioner-csi"
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
	cr, err := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.TODO(), hppName, metav1.GetOptions{})
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

// exec into the mounter pod to retrive total block size of a mount pount
func getMountSize(mounterPod, mountPath string) string {
	blockSize, err := RunKubeCtlCommand("exec", "-n", hppNamespace, mounterPod, "-c", "mounter", "--",
		"/bin/sh", "-c", fmt.Sprintf("df %s | sed 1d | awk '{print $2}'", mountPath))
	Expect(err).ToNot(HaveOccurred())
	Expect(blockSize).ToNot(BeEmpty())
	return blockSize
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
	newPoolPod := ""
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
				newPoolPod = pod.Name
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

	// Check that the directory bind mounted on the host is the actual nfs share by comparing the total block sizes
	t.Logf("Verifying bind mount size matches the NFS share")
	hostMountSize := getMountSize(newPoolPod, fmt.Sprintf("/host/%s/csi", pool.Path))
	sourceMountSize := getMountSize(newPoolPod, "/source")
	Expect(hostMountSize).To(Equal(sourceMountSize))

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

func waitForMounters(hppAPI hppclientv1.HostPathProvisionerInterface, ctx context.Context, poolName string) {
	Eventually(func() error {
		hpp, err := hppAPI.Get(ctx, hppName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		for _, pool := range hpp.Status.StoragePoolStatuses {
			if pool.Name != poolName {
				continue
			}

			if pool.Phase != hostpathprovisionerv1.StoragePoolReady || pool.DesiredReady == 0 || pool.CurrentReady != pool.DesiredReady {
				return fmt.Errorf(
					"pool %s: phase=%s, ready=%d/%d", poolName, pool.Phase, pool.CurrentReady, pool.DesiredReady,
				)
			}

			return nil
		}

		return fmt.Errorf("pool %s has no status yet", poolName)
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
}

func waitForCSIDS(t *testing.T, k8sClient *kubernetes.Clientset, ctx context.Context) {
	t.Helper()

	t.Log("waiting for CSI DaemonSet")

	Eventually(func() error {
		ds, err := k8sClient.AppsV1().DaemonSets(hppNamespace).Get(ctx, hppCSIName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		status := ds.Status
		desired := status.DesiredNumberScheduled

		if status.ObservedGeneration < ds.Generation || desired == 0 || status.UpdatedNumberScheduled != desired || status.NumberAvailable != desired {
			return fmt.Errorf(
				"CSI rollout pending: generation=%d/%d, updated=%d, available=%d, desired=%d",
				status.ObservedGeneration, ds.Generation,
				status.UpdatedNumberScheduled,
				status.NumberAvailable, desired,
			)
		}

		return nil
	}, 5*time.Minute, 2*time.Second).Should(Succeed())
}

// verifyVolumeQuota checks that writes below the volume limit succeed and writes
// above it fail. Remove the test file so the same check can run after recovery.
func verifyVolumeQuota(t *testing.T, pod corev1.Pod, limitBytes int64) {
	t.Helper()

	const mebibyte = int64(1024 * 1024)
	Expect(limitBytes).To(BeNumerically(">", mebibyte), "test requires a volume larger than 1Mi")
	aboveLimitMiB := limitBytes/mebibyte + 1

	out, err := RunKubeCtlCommand(
		"exec", "-n", pod.Namespace, pod.Name,
		"-c", "test-ctr", "--",
		"sh", "-c", `
set -eu
export LC_ALL=C
file=/mnt/storage/quota-write-test.img
trap 'rm -f "$file" /tmp/quota-write.err' EXIT

# Flush a small write before testing the hard limit.
dd if=/dev/zero of="$file" bs=1M count=1 conv=fsync
rm "$file"

if dd if=/dev/zero of="$file" bs=1M count="$1" conv=fsync 2>/tmp/quota-write.err; then
    echo "ERROR: write above the volume limit succeeded"
    exit 1
fi

# Return dd's error through stdout for RunKubeCtlCommand.
cat /tmp/quota-write.err
`, "sh", fmt.Sprintf("%d", aboveLimitMiB),
	)
	Expect(err).ToNot(HaveOccurred(), "quota verification failed in pod %s: %s", pod.Name, out)
	Expect(out).To(Or(
		ContainSubstring("Disk quota exceeded"),
		ContainSubstring("No space left on device"),
	), "expected a quota or space error for a write above the volume limit")
}

// runBlockStoragePoolQuota shares quota and mounter recovery checks between
// persistent PVCs and inline CSI ephemeral volumes in a configured block pool.
func runBlockStoragePoolQuota(t *testing.T, ephemeral bool) {
	t.Helper()
	RegisterTestingT(t)
	t.Logf("testing Block storage pool quota (ephemeral=%t)", ephemeral)

	ctx := context.Background()
	pool := getStoragePoolsForModes(t, ctx, corev1.PersistentVolumeBlock)[0]
	poolName := pool.Name
	poolMountPath := filepath.Join(pool.Path, "csi")

	k8sClient, err := getKubeClient()
	Expect(err).ToNot(HaveOccurred())

	// 3. wait for csi DS rollout to finish
	waitForCSIDS(t, k8sClient, ctx)

	// verify pool fs has prjquota
	pods, err := k8sClient.CoreV1().Pods(hppNamespace).List(
		ctx, metav1.ListOptions{
			LabelSelector: fmt.Sprintf("k8s-app=hostpath-provisioner,kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", poolName),
		},
	)
	Expect(err).ToNot(HaveOccurred())
	Expect(pods.Items).ToNot(BeEmpty(), "expected pool mounter pods")

	t.Log("verifying pool has prjquota flag")

	for _, pod := range pods.Items {
		if pod.DeletionTimestamp != nil {
			continue
		}

		t.Logf("checking pool mount on node %s from pod %s", pod.Spec.NodeName, pod.Name)

		out, err := RunKubeCtlCommand(
			"exec", "-n", hppNamespace, pod.Name, "-c", "mounter", "--", "/usr/sbin/chroot", "/host", "/usr/bin/findmnt", "--noheadings", "--raw", "--mountpoint",
			poolMountPath, "--output", "FSTYPE,OPTIONS",
		)
		Expect(err).ToNot(HaveOccurred(), "unable to inspect pool mount on node %s", pod.Spec.NodeName)

		fields := strings.Fields(out)
		Expect(fields).To(HaveLen(2), "unexpected findmnt output: %q", out)

		fsType := fields[0]
		options := strings.Split(fields[1], ",")

		Expect(fsType).To(BeElementOf("xfs", "ext4"))
		Expect(options).To(ContainElement(BeElementOf("prjquota", "pquota")), "project quotas missing on node %s: %s", pod.Spec.NodeName, out)
	}

	volumeSize := resource.MustParse("500Mi")
	volumeSource := createQuotaVolumeSource(t, ctx, k8sClient, poolName, volumeSize, ephemeral)

	// Create a pod consuming the persistent or ephemeral volume.
	t.Log("creating quota test pod")
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-quota-pod-",
			Namespace:    corev1.NamespaceDefault,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "test-ctr",
					Image: "alpine:latest",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "test-vol",
							MountPath: "/mnt/storage",
						},
					},
					Command: []string{"sh", "-c", "sleep 3600"},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name:         "test-vol",
					VolumeSource: volumeSource,
				},
			},
		},
	}

	pod, err = k8sClient.CoreV1().Pods(corev1.NamespaceDefault).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		err := k8sClient.CoreV1().Pods(corev1.NamespaceDefault).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
	})

	if claim := volumeSource.PersistentVolumeClaim; claim != nil {
		Eventually(func() (corev1.PersistentVolumeClaimPhase, error) {
			current, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claim.ClaimName, metav1.GetOptions{})
			if err != nil {
				return "", err
			}
			return current.Status.Phase, nil
		}, 5*time.Minute, 2*time.Second).Should(Equal(corev1.ClaimBound))
	}

	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().
			Pods(pod.Namespace).
			Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		for _, condition := range current.Status.Conditions {
			if condition.Type == corev1.PodReady {
				return condition.Status == corev1.ConditionTrue, nil
			}
		}
		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue())

	// Verify writes below and above the volume's requested capacity.
	verifyVolumeQuota(t, *pod, volumeSize.Value())

	// Restart the mounter and repeat the quota checks after recovery.
	consumer, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	poolPodsOnNode := metav1.ListOptions{
		LabelSelector: fmt.Sprintf(
			"k8s-app=hostpath-provisioner,kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", poolName,
		),
		FieldSelector: "spec.nodeName=" + consumer.Spec.NodeName,
	}

	mounters, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, poolPodsOnNode)
	Expect(err).ToNot(HaveOccurred())

	var oldMounter corev1.Pod
	for _, candidate := range mounters.Items {
		if candidate.DeletionTimestamp == nil {
			oldMounter = candidate
			break
		}
	}
	Expect(oldMounter.Name).ToNot(BeEmpty())

	// restart mtr thru its managing deployment
	err = k8sClient.CoreV1().Pods(hppNamespace).Delete(ctx, oldMounter.Name, metav1.DeleteOptions{})
	Expect(err).ToNot(HaveOccurred())

	// require new, ready pod rather than accepting stale pool status
	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, poolPodsOnNode)
		if err != nil {
			return false, err
		}

		for _, candidate := range current.Items {
			if candidate.UID == oldMounter.UID || candidate.DeletionTimestamp != nil {
				continue
			}

			for _, condition := range candidate.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}

		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue())

	verifyVolumeQuota(t, *pod, volumeSize.Value())
}

// createQuotaVolumeSource creates the volume-specific resources; the remaining
// test steps use the same pod mount and quota checks for either lifecycle.
func createQuotaVolumeSource(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, poolName string, size resource.Quantity, ephemeral bool) corev1.VolumeSource {
	t.Helper()
	if ephemeral {
		driver, err := k8sClient.StorageV1().CSIDrivers().Get(ctx, "kubevirt.io.hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(driver.Spec.VolumeLifecycleModes).To(ContainElement(storagev1.VolumeLifecycleEphemeral),
			"the deployed CSIDriver must advertise Ephemeral to test inline CSI volumes")
		return corev1.VolumeSource{
			CSI: &corev1.CSIVolumeSource{
				Driver: "kubevirt.io.hostpath-provisioner",
				VolumeAttributes: map[string]string{
					"storagePool":  poolName,
					"enforceQuota": "true",
					"size":         size.String(),
				},
			},
		}
	}

	sc := getQuotaStorageClassForPool(t, ctx, k8sClient, poolName)

	// Request the same size used by the inline CSI case.
	t.Logf("creating PVC with %s", size.String())
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "test-quota-pvc-",
			Namespace:    corev1.NamespaceDefault,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: size,
				},
			},
			StorageClassName: &sc.Name,
		},
	}

	pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault).Create(ctx, pvc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		t.Logf("deleting quota test PVC %s/%s", pvc.Namespace, pvc.Name)
		cleanupCtx := context.Background()
		current, err := k8sClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(cleanupCtx, pvc.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).ToNot(HaveOccurred())
		err = k8sClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Delete(
			cleanupCtx, pvc.Name, metav1.DeleteOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
		Eventually(func() bool {
			_, err := k8sClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(cleanupCtx, pvc.Name, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "PVC %s should be deleted", pvc.Name)
		// Wait for quota reservations to be released before another capacity test starts.
		if current.Spec.VolumeName != "" {
			Eventually(func() bool {
				_, err := k8sClient.CoreV1().PersistentVolumes().Get(cleanupCtx, current.Spec.VolumeName, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "PV %s should be deleted", current.Spec.VolumeName)
		}
	})
	return corev1.VolumeSource{
		PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name},
	}
}

// NOTE: THIS NEEDS THE MODIFIED OPERATOR RUNNING ON THE TARGET CLUSTER
func TestBlockStoragePoolQuota(t *testing.T) {
	runBlockStoragePoolQuota(t, false)
}

func TestBlockStoragePoolQuotaEphemeral(t *testing.T) {
	runBlockStoragePoolQuota(t, true)
}

// The second 500Mi claim should bind on another node when the first node lacks capacity.
func TestBlockStoragePoolTwo500MiPVCs(t *testing.T) {
	runStoragePoolTwo500MiPVCs(t, corev1.PersistentVolumeBlock, false)
}

// Exercise the same scheduling and capacity checks with a filesystem-backed pool.
func TestFilesystemStoragePoolTwo500MiPVCs(t *testing.T) {
	runStoragePoolTwo500MiPVCs(t, corev1.PersistentVolumeFilesystem, false)
}

// Force the second claim onto the first node and require a quota-capacity rejection.
func TestBlockStoragePoolTwo500MiPVCsSameNodeRejected(t *testing.T) {
	runStoragePoolTwo500MiPVCs(t, corev1.PersistentVolumeBlock, true)
}

// Filesystem-backed pools must reject the same overcommit as block-backed pools.
func TestFilesystemStoragePoolTwo500MiPVCsSameNodeRejected(t *testing.T) {
	runStoragePoolTwo500MiPVCs(t, corev1.PersistentVolumeFilesystem, true)
}

// Share creation and capacity checks between pool modes and node-placement cases.
// These tests need pools that fit one 500Mi reservation per node, but cannot fit two.
func runStoragePoolTwo500MiPVCs(t *testing.T, mode corev1.PersistentVolumeMode, secondOnSameNode bool) {
	t.Helper()
	RegisterTestingT(t)
	t.Logf("testing two 500Mi claims: poolMode=%s forceSecondOntoFirstNode=%t", mode, secondOnSameNode)
	ctx := context.Background()
	k8sClient, err := getKubeClient()
	Expect(err).ToNot(HaveOccurred())
	pool := getStoragePoolsForModes(t, ctx, mode)[0]
	sc := getQuotaStorageClassForPool(t, ctx, k8sClient, pool.Name)
	t.Logf("waiting for CSI DaemonSet before provisioning two 500Mi PVCs from %s pool %s using class %s", mode, pool.Name, sc.Name)
	waitForCSIDS(t, k8sClient, ctx)

	volumeSize := resource.MustParse("500Mi")
	// Create both consumers the same way; only the rejection case pins the second node.
	createConsumer := func(claimNumber int, targetNode string) *corev1.Pod {
		t.Helper()
		t.Logf("creating PVC %d/2 (500Mi, enforceQuota=true)", claimNumber)
		volumeSource := createQuotaVolumeSource(t, ctx, k8sClient, pool.Name, volumeSize, false)
		claimName := volumeSource.PersistentVolumeClaim.ClaimName
		t.Logf("created PVC %d/2: %s", claimNumber, claimName)
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "two-quota-pvcs-",
				Namespace:    corev1.NamespaceDefault,
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:         "consumer",
					Image:        "alpine:latest",
					Command:      []string{"sh", "-c", "sleep 3600"},
					VolumeMounts: []corev1.VolumeMount{{Name: "volume", MountPath: "/mnt/storage"}},
				}},
				Volumes: []corev1.Volume{{Name: "volume", VolumeSource: volumeSource}},
			},
		}
		if targetNode != "" {
			// Keep the consumer on the exact node, even if its hostname label differs.
			pod.Spec.Affinity = &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchFields: []corev1.NodeSelectorRequirement{{
						Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{targetNode},
					}}}},
				},
			}}
			// Explicitly select this node to exercise CreateVolume rejection; otherwise
			// the scheduler can reject the pod using CSIStorageCapacity before provisioning.
			claim, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			if claim.Annotations == nil {
				claim.Annotations = make(map[string]string)
			}
			claim.Annotations["volume.kubernetes.io/selected-node"] = targetNode
			_, err = k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Update(ctx, claim, metav1.UpdateOptions{})
			Expect(err).ToNot(HaveOccurred())
			t.Logf("forcing PVC %s provisioning on node %s to test quota admission", claimName, targetNode)
		}
		pod, err := k8sClient.CoreV1().Pods(pod.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		t.Logf("created consumer pod %s for PVC %s", pod.Name, claimName)
		t.Cleanup(func() {
			t.Logf("deleting consumer pod %s/%s before PVC %s", pod.Namespace, pod.Name, claimName)
			err := k8sClient.CoreV1().Pods(pod.Namespace).Delete(context.Background(), pod.Name, metav1.DeleteOptions{})
			Expect(err).ToNot(HaveOccurred())
			Eventually(func() bool {
				_, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(context.Background(), pod.Name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, 2*time.Minute, 2*time.Second).Should(BeTrue())
		})
		return pod
	}

	first := waitForQuotaPVCConsumer(t, ctx, k8sClient, createConsumer(1, ""))
	// Wait for the provisioner's capacity update before creating the second claim.
	// Otherwise the second consumer could be scheduled using an outdated capacity value.
	t.Logf("waiting for node %s to advertise less than 500Mi after its first reservation", first.Spec.NodeName)
	var lastMaximumSize string
	Eventually(func() (int64, error) {
		capacities, err := k8sClient.StorageV1().CSIStorageCapacities(hppNamespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return 0, err
		}
		for _, capacity := range capacities.Items {
			if capacity.StorageClassName != sc.Name || capacity.NodeTopology == nil ||
				capacity.NodeTopology.MatchLabels["topology.hostpath.csi/node"] != first.Spec.NodeName ||
				capacity.MaximumVolumeSize == nil {
				continue
			}
			maximumSize := capacity.MaximumVolumeSize.String()
			if maximumSize != lastMaximumSize {
				t.Logf("node %s advertises maximum volume size %s after first PVC", first.Spec.NodeName, maximumSize)
				lastMaximumSize = maximumSize
			}
			return capacity.MaximumVolumeSize.Value(), nil
		}
		return 0, fmt.Errorf("no CSIStorageCapacity for class %s on node %s", sc.Name, first.Spec.NodeName)
	}, 5*time.Minute, 2*time.Second).Should(BeNumerically("<", volumeSize.Value()),
		"the selected node must advertise less than 500Mi for this test; use the 1Gi pool fixture")

	if secondOnSameNode {
		t.Logf("expecting the second reservation to fail on exhausted node %s", first.Spec.NodeName)
		second := createConsumer(2, first.Spec.NodeName)
		waitForQuotaPVCRejection(t, ctx, k8sClient, second)
		// A rejected reservation must not disrupt the first bound volume and its consumer.
		first = waitForQuotaPVCConsumer(t, ctx, k8sClient, first)
		t.Logf("second 500Mi PVC rejected on node %s; first consumer %s remains Running", first.Spec.NodeName, first.Name)
		return
	}

	t.Log("allowing the scheduler to choose another node for the second reservation")
	second := waitForQuotaPVCConsumer(t, ctx, k8sClient, createConsumer(2, ""))
	Expect(second.Spec.NodeName).ToNot(Equal(first.Spec.NodeName), "second pod should be scheduled on a node with capacity")
	t.Logf("both 500Mi PVCs bound: first pod on %s, second pod on %s", first.Spec.NodeName, second.Spec.NodeName)
}

// Wait for the PVC to bind and return its running consumer, including its assigned node.
func waitForQuotaPVCConsumer(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, pod *corev1.Pod) *corev1.Pod {
	t.Helper()
	claimName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
	t.Logf("waiting for PVC %s/%s to bind and consumer %s to run", pod.Namespace, claimName, pod.Name)
	Eventually(func() (corev1.PersistentVolumeClaimPhase, error) {
		claim, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return claim.Status.Phase, nil
	}, 5*time.Minute, 2*time.Second).Should(Equal(corev1.ClaimBound), "PVC %s should bind", claimName)
	t.Logf("PVC %s is Bound", claimName)

	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		pod = current
		return current.Status.Phase == corev1.PodRunning, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "pod for PVC %s should run", claimName)
	t.Logf("consumer pod %s is Running on node %s", pod.Name, pod.Spec.NodeName)
	return pod
}

// Require a real quota admission error; Pending alone could hide unrelated provisioning failures.
func waitForQuotaPVCRejection(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, pod *corev1.Pod) {
	t.Helper()
	claimName := pod.Spec.Volumes[0].PersistentVolumeClaim.ClaimName
	claim, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Logf("waiting for a quota ResourceExhausted event for PVC %s/%s", pod.Namespace, claimName)
	seenFailures := make(map[string]bool)
	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		Expect(current.Status.Phase).To(Equal(corev1.ClaimPending), "PVC %s must not bind on an exhausted node", claimName)
		Expect(current.Spec.VolumeName).To(BeEmpty())
		events, err := k8sClient.CoreV1().Events(pod.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: "involvedObject.uid=" + string(claim.UID),
		})
		if err != nil {
			return false, err
		}
		for _, event := range events.Items {
			if event.Reason != "ProvisioningFailed" {
				continue
			}
			if !seenFailures[event.Message] {
				t.Logf("PVC %s provisioning failure: %s", claimName, event.Message)
				seenFailures[event.Message] = true
			}
			if strings.Contains(event.Message, "ResourceExhausted") && strings.Contains(event.Message, "failed to reserve quota:") {
				return true, nil
			}
		}
		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "PVC %s should fail because its quota exceeds the node's remaining pool capacity", claimName)

	// Confirm the rejected claim stays unbound during the observation period.
	t.Logf("quota rejection observed for %s; verifying the claim remains Pending during retries", claimName)
	Consistently(func() (bool, error) {
		current, err := k8sClient.CoreV1().PersistentVolumeClaims(pod.Namespace).Get(ctx, claimName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return current.Status.Phase == corev1.ClaimPending && current.Spec.VolumeName == "", nil
	}, 15*time.Second, 2*time.Second).Should(BeTrue(), "rejected PVC %s must remain unbound", claimName)
}

// getQuotaStorageClassForPool uses a deployed class; tests never create or delete it.
func getQuotaStorageClassForPool(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, poolName string) *storagev1.StorageClass {
	t.Helper()
	classes, err := k8sClient.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	for i := range classes.Items {
		sc := &classes.Items[i]
		if sc.DeletionTimestamp != nil || sc.Provisioner != csiProvisionerName ||
			sc.Parameters["storagePool"] != poolName || sc.Parameters["enforceQuota"] != "true" {
			continue
		}
		Expect(sc.ReclaimPolicy).To(Equal(ptr.To(corev1.PersistentVolumeReclaimDelete)), "StorageClass %s must delete test volumes", sc.Name)
		Expect(sc.VolumeBindingMode).To(Equal(ptr.To(storagev1.VolumeBindingWaitForFirstConsumer)), "StorageClass %s must use WaitForFirstConsumer", sc.Name)
		t.Logf("using deployed StorageClass %s for pool %s (enforceQuota=true)", sc.Name, poolName)
		return sc
	}
	t.Fatalf("no deployed quota-enabled StorageClass for pool %s; apply the matching deploy/tests/storageclass_wffc_ceph_pool_quota*.yaml manifest", poolName)
	return nil
}

// getStoragePoolsForModes selects distinct configured pools by their backing PVC mode.
func getStoragePoolsForModes(t *testing.T, ctx context.Context, modes ...corev1.PersistentVolumeMode) []hostpathprovisionerv1.StoragePool {
	t.Helper()
	hppClient, err := getHPPClient()
	Expect(err).ToNot(HaveOccurred())
	hppAPI := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners()
	hpp, err := hppAPI.Get(ctx, hppName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	var pools []hostpathprovisionerv1.StoragePool
	used := make(map[string]bool)
	for _, mode := range modes {
		found := false
		for _, pool := range hpp.Spec.StoragePools {
			if pool.PVCTemplate == nil || used[pool.Name] {
				continue
			}
			actualMode := corev1.PersistentVolumeFilesystem
			if pool.PVCTemplate.VolumeMode != nil {
				actualMode = *pool.PVCTemplate.VolumeMode
			}
			if actualMode == mode {
				pools = append(pools, pool)
				used[pool.Name] = true
				found = true
				break
			}
		}
		if !found {
			t.Skipf("requires distinct PVC-backed pools with modes %v; no unused %s pool found", modes, mode)
		}
	}
	for i, pool := range pools {
		t.Logf("waiting for %s pool %s at %s", modes[i], pool.Name, pool.Path)
		waitForMounters(hppAPI, ctx, pool.Name)
		t.Logf("pool %s is ready", pool.Name)
	}
	return pools
}

// These tests use configured HPP pools and their deployed quota StorageClasses.
// They must run sequentially with tests that modify the HPP CR. The mixed-pool fixture is
// deploy/tests/hostpathprovisioner_ceph_mixed_pools_cr.yaml.
func TestBlockStoragePoolProvisioning(t *testing.T) {
	runStoragePoolProvisioning(t, corev1.PersistentVolumeBlock)
}

func TestFilesystemStoragePoolProvisioning(t *testing.T) {
	runStoragePoolProvisioning(t, corev1.PersistentVolumeFilesystem)
}

// TestTwoBlockStoragePools uses deploy/tests/hostpathprovisioner_ceph_two_block_pools_cr.yaml.
func TestTwoBlockStoragePools(t *testing.T) {
	runStoragePoolProvisioning(t, corev1.PersistentVolumeBlock, corev1.PersistentVolumeBlock)
}

func TestMixedStoragePools(t *testing.T) {
	runStoragePoolProvisioning(t, corev1.PersistentVolumeBlock, corev1.PersistentVolumeFilesystem)
}

func runStoragePoolProvisioning(t *testing.T, modes ...corev1.PersistentVolumeMode) {
	t.Helper()
	RegisterTestingT(t)
	t.Logf("testing provisioning from storage pools with backing modes %v", modes)
	ctx := context.Background()
	k8sClient, err := getKubeClient()
	Expect(err).ToNot(HaveOccurred())
	pools := getStoragePoolsForModes(t, ctx, modes...)
	waitForCSIDS(t, k8sClient, ctx)

	ns, err := k8sClient.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "storage-pools-"},
	}, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	var volumeNames []string
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		t.Logf("deleting namespace %s with its consumer pod and PVCs", ns.Name)
		err := k8sClient.CoreV1().Namespaces().Delete(cleanupCtx, ns.Name, metav1.DeleteOptions{})
		Expect(err == nil || apierrors.IsNotFound(err)).To(BeTrue(), "delete namespace: %v", err)
		Eventually(func() bool {
			_, err := k8sClient.CoreV1().Namespaces().Get(cleanupCtx, ns.Name, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 5*time.Minute, 2*time.Second).Should(BeTrue())
		t.Logf("namespace %s deleted", ns.Name)
		for _, name := range volumeNames {
			t.Logf("waiting for PV %s to be deleted", name)
			Eventually(func() bool {
				_, err := k8sClient.CoreV1().PersistentVolumes().Get(cleanupCtx, name, metav1.GetOptions{})
				return apierrors.IsNotFound(err)
			}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "PV %s should be deleted", name)
			t.Logf("PV %s deleted", name)
		}
		t.Log("storage pool provisioning test cleanup complete")
	})
	t.Logf("created test namespace %s", ns.Name)

	var claims []*corev1.PersistentVolumeClaim
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: ns.Name},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptr.To(true),
				RunAsUser:    ptr.To(int64(1000)),
				RunAsGroup:   ptr.To(int64(1000)),
				FSGroup:      ptr.To(int64(1000)),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Containers: []corev1.Container{{
				Name: "consumer", Image: "alpine:latest",
				Command: []string{"sh", "-c", "sleep 3600"},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptr.To(false),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
			}},
		},
	}
	for i, pool := range pools {
		sc := getQuotaStorageClassForPool(t, ctx, k8sClient, pool.Name)
		claim, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Create(ctx, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("pool-%d", i), Namespace: ns.Name},
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &sc.Name,
				AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				VolumeMode:       ptr.To(corev1.PersistentVolumeFilesystem),
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("64Mi")},
				},
			},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		claims = append(claims, claim)
		t.Logf("created 64Mi PVC %s/%s for pool %s, to mount at /mnt/pool-%d", ns.Name, claim.Name, pool.Name, i)
		pod.Spec.Volumes = append(pod.Spec.Volumes, corev1.Volume{
			Name:         claim.Name,
			VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: claim.Name}},
		})
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name: claim.Name, MountPath: fmt.Sprintf("/mnt/pool-%d", i),
		})
	}
	// A single consumer makes all claims bind on the same node.
	pod, err = k8sClient.CoreV1().Pods(ns.Name).Create(ctx, pod, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Logf("created consumer pod %s/%s; waiting for claims to bind and mounts to become ready", ns.Name, pod.Name)
	var lastPodState, nodeName string
	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().Pods(ns.Name).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		state := fmt.Sprintf("phase=%s node=%s reason=%s", current.Status.Phase, current.Spec.NodeName, current.Status.Reason)
		if state != lastPodState {
			t.Logf("consumer pod %s/%s: %s", ns.Name, current.Name, state)
			lastPodState = state
		}
		for _, condition := range current.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				nodeName = current.Spec.NodeName
				t.Logf("all %d pools mounted in pod %s on node %s", len(pools), current.Name, nodeName)
				return true, nil
			}
		}
		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "consumer should mount the selected pools")

	filesystems := make(map[string]string)
	for i, claim := range claims {
		bound, err := k8sClient.CoreV1().PersistentVolumeClaims(ns.Name).Get(ctx, claim.Name, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(bound.Status.Phase).To(Equal(corev1.ClaimBound))
		volumeNames = append(volumeNames, bound.Spec.VolumeName)
		pv, err := k8sClient.CoreV1().PersistentVolumes().Get(ctx, bound.Spec.VolumeName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(pv.Spec.CSI).ToNot(BeNil())
		Expect(pv.Spec.CSI.Driver).To(Equal(csiProvisionerName))
		Expect(pv.Spec.CSI.VolumeAttributes["storagePool"]).To(Equal(pools[i].Name))
		t.Logf("PVC %s/%s bound to PV %s, CSI volume %s, pool %s", ns.Name, bound.Name, pv.Name, pv.Spec.CSI.VolumeHandle, pools[i].Name)

		t.Logf("verifying writes to pool %s in consumer %s/%s", pools[i].Name, ns.Name, pod.Name)
		mountPath := fmt.Sprintf("/mnt/pool-%d", i)
		device, err := RunKubeCtlCommand("exec", "-n", ns.Name, pod.Name, "-c", "consumer", "--",
			"sh", "-ec", `
printf '%s\n' "$2" > "$1/pool-marker"
dd if=/dev/zero of="$1/write-test" bs=1M count=1 conv=fsync
test "$(cat "$1/pool-marker")" = "$2"
rm "$1/write-test"
stat -c '%d' "$1"
`, "sh", mountPath, pools[i].Name)
		Expect(err).ToNot(HaveOccurred(), "pool %s should support writes", pools[i].Name)
		device = strings.TrimSpace(device)
		Expect(device).ToNot(BeEmpty())
		previousPool, duplicate := filesystems[device]
		Expect(duplicate).To(BeFalse(), "pools %s and %s must use distinct filesystems", previousPool, pools[i].Name)
		filesystems[device] = pools[i].Name

		// Verify the actual backing claim and marker on the consumer's node.
		var mounter corev1.Pod
		Eventually(func() (bool, error) {
			pods, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, metav1.ListOptions{
				LabelSelector: fmt.Sprintf("kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", pools[i].Name),
				FieldSelector: "spec.nodeName=" + nodeName,
			})
			if err != nil {
				return false, err
			}
			for _, candidate := range pods.Items {
				if candidate.DeletionTimestamp != nil {
					continue
				}
				for _, condition := range candidate.Status.Conditions {
					if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
						mounter = candidate
						return true, nil
					}
				}
			}
			return false, nil
		}, 5*time.Minute, 2*time.Second).Should(BeTrue(), "pool %s needs a ready mounter on node %s", pools[i].Name, nodeName)
		var backingClaim string
		for _, volume := range mounter.Spec.Volumes {
			if volume.Name == "data" && volume.PersistentVolumeClaim != nil {
				backingClaim = volume.PersistentVolumeClaim.ClaimName
			}
		}
		Expect(backingClaim).ToNot(BeEmpty())
		backing, err := k8sClient.CoreV1().PersistentVolumeClaims(hppNamespace).Get(ctx, backingClaim, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(backing.Status.Phase).To(Equal(corev1.ClaimBound))
		Expect(backing.Spec.VolumeMode).ToNot(BeNil())
		Expect(*backing.Spec.VolumeMode).To(Equal(modes[i]), "pool %s backing claim must use %s", pools[i].Name, modes[i])
		markerPath := filepath.Join("/host", pools[i].Path, "csi", pv.Spec.CSI.VolumeHandle, "pool-marker")
		marker, err := RunKubeCtlCommand("exec", "-n", hppNamespace, mounter.Name, "-c", "mounter", "--", "cat", markerPath)
		Expect(err).ToNot(HaveOccurred(), "volume marker should exist in pool %s", pools[i].Name)
		Expect(strings.TrimSpace(marker)).To(Equal(pools[i].Name))
		t.Logf("pool %s verified: backing PVC %s/%s mode=%s, filesystem=%s, marker=%s",
			pools[i].Name, hppNamespace, backing.Name, modes[i], device, markerPath)
	}
	t.Logf("all %d selected pools passed provisioning, backing mode, and write/read checks", len(pools))
}
