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
	storagev1 "k8s.io/api/storage/v1"
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
	poolPath      = "/var/hpp-quota-test"
	hppSCName     = "test-quota"
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

func waitForMounters(hppAPI *hppclientv1.HostPathProvisionerInterface, ctx *context.Context, poolName string) {
	Eventually(func() error {
		hpp, err := (*hppAPI).Get(*ctx, hppName, metav1.GetOptions{})
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

func waitForCSIDS(t *testing.T, k8sClient **kubernetes.Clientset, ctx *context.Context) {
	t.Helper()

	t.Log("waiting for CSI DaemonSet")

	Eventually(func() error {
		ds, err := (*k8sClient).AppsV1().DaemonSets(hppNamespace).Get(*ctx, hppCSIName, metav1.GetOptions{})
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

// verifyBackingDeviceAlias checks that the CSI driver sees the pool's backing
// block device through its alias on the same node as the mounter.
func verifyBackingDeviceAlias(t *testing.T, ctx context.Context, k8sClient *kubernetes.Clientset, mounter corev1.Pod, csiSelector, aliasPath string) {
	t.Helper()

	t.Log("verifying backing device")

	backingDevice, err := RunKubeCtlCommand(
		"exec", "-n", hppNamespace, mounter.Name,
		"-c", "mounter", "--",
		"stat", "-L", "-c", "%t:%T", "/dev/data",
	)
	Expect(err).ToNot(HaveOccurred())
	backingDevice = strings.TrimSpace(backingDevice)
	Expect(backingDevice).ToNot(BeEmpty())

	csiPods, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: csiSelector,
		FieldSelector: "spec.nodeName=" + mounter.Spec.NodeName,
	})
	Expect(err).ToNot(HaveOccurred())

	var csiPodName string
	for _, candidate := range csiPods.Items {
		if candidate.DeletionTimestamp == nil && candidate.Status.Phase == corev1.PodRunning {
			csiPodName = candidate.Name
			break
		}
	}
	Expect(csiPodName).ToNot(BeEmpty(), "expected CSI pod on node %s", mounter.Spec.NodeName)

	aliasDevice, err := RunKubeCtlCommand(
		"exec", "-n", hppNamespace, csiPodName,
		"-c", "hostpath-provisioner", "--",
		"sh", "-c", `test -b "$1" && stat -L -c '%t:%T' "$1"`,
		"sh", aliasPath,
	)
	Expect(err).ToNot(HaveOccurred(), "%s must be a block device in CSI pod %s", aliasPath, csiPodName)
	Expect(strings.TrimSpace(aliasDevice)).To(Equal(backingDevice),
		"alias must match the backing device on node %s", mounter.Spec.NodeName)
}

// verifyPVCQuota checks that writes below the PVC limit succeed and writes
// above it fail. Remove the test file so the same check can run after recovery.
func verifyPVCQuota(t *testing.T, pod corev1.Pod, limitBytes int64) {
	t.Helper()

	const mebibyte = int64(1024 * 1024)
	Expect(limitBytes).To(BeNumerically(">", mebibyte), "test requires a PVC larger than 1Mi")
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
    echo "ERROR: write above the PVC limit succeeded"
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
	), "expected a quota or space error for a write above the PVC limit")
}

// NOTE: THIS NEEDS THE MODIFIED OPERATOR RUNNING ON THE TARGET CLUSTER
func TestBlockStoragePoolQuota(t *testing.T) {
	RegisterTestingT(t)

	ctx := context.Background()
	t.Log("getting hpp client")
	hppClient, err := getHPPClient()
	Expect(err).ToNot(HaveOccurred())

	// 1. establish pool
	t.Log("establishing hpp pool")
	hppAPI := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners()
	hpp, err := hppAPI.Get(ctx, hppName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	const poolName = "quota-test"

	hpp.Spec.StoragePools = make([]hostpathprovisionerv1.StoragePool, 0)

	hpp.Spec.StoragePools = append(hpp.Spec.StoragePools,
		hostpathprovisionerv1.StoragePool{
			Name: poolName,
			Path: poolPath,
			PVCTemplate: &corev1.PersistentVolumeClaimSpec{
				StorageClassName: ptr.To("rook-ceph-block"),
				VolumeMode:       ptr.To(corev1.PersistentVolumeBlock),
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Gi"),
					},
				},
			},
		},
	)

	_, err = hppAPI.Update(ctx, hpp, metav1.UpdateOptions{})
	Expect(err).ToNot(HaveOccurred(), "err updating HPP CR Spec")

	// 2. wait for its mtr and csi pods to become ready
	t.Log("waiting for mounter pods to be ready")
	waitForMounters(&hppAPI, &ctx, poolName)

	k8sClient, err := getKubeClient()
	Expect(err).ToNot(HaveOccurred())

	// 3. wait for csi DS rollout to finish
	waitForCSIDS(t, &k8sClient, &ctx)

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

		t.Logf("checking pool mount on node %s", pod.Spec.NodeName)

		out, err := RunKubeCtlCommand(
			"exec", "-n", hppNamespace, pod.Name, "-c", "mounter", "--", "chroot", "/host", "/usr/bin/findmnt", "--noheadings", "--raw", "--mountpoint",
			poolPath, "--output", "FTYPE,OPTIONS",
		)
		Expect(err).ToNot(HaveOccurred(), "unable to inspect pool mount on node %s", pod.Spec.NodeName)

		fields := strings.Fields(out)
		Expect(fields).To(HaveLen(2), "unexpected findmnt output: %q")

		fsType := fields[0]
		options := strings.Split(fields[1], ",")

		Expect(fsType).To(BeElementOf("xfs", "ext4"))
		Expect(options).To(ContainElement(BeElementOf("prjquota", "pquota")), "project quotas missing on node %S: %s", pod.Spec.NodeName, out)
	}

	// 4. ensure backing device is block inside CSI provisioner container
	aliasPath := fmt.Sprintf("/dev/%s-device", poolName)

	ds, err := k8sClient.AppsV1().DaemonSets(hppNamespace).Get(ctx, hppCSIName, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	Expect(err).ToNot(HaveOccurred())

	checked := 0

	// reusing pods from above
	for _, mounter := range pods.Items {
		if mounter.DeletionTimestamp != nil {
			continue
		}

		verifyBackingDeviceAlias(t, ctx, k8sClient, mounter, selector.String(), aliasPath)

		checked++
	}

	Expect(checked).To(BeNumerically(">", 0), "expected to verify at least one device alias")

	// 5. create SC targeting above pool with enforceQuota: "true"
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: hppSCName,
		},
		Provisioner: "kubevirt.io.hostpath-provisioner",
		Parameters: map[string]string{
			"storagePool":  poolName,
			"enforceQuota": "true",
		},
		ReclaimPolicy:     ptr.To(corev1.PersistentVolumeReclaimDelete),
		VolumeBindingMode: ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
	}
	sc, err = k8sClient.StorageV1().StorageClasses().Create(ctx, sc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	t.Cleanup(func() {
		err := k8sClient.StorageV1().StorageClasses().Delete(
			context.Background(), sc.Name, metav1.DeleteOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
	})

	// 5.a create small PVC
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-quota-pvc",
			Namespace: corev1.NamespaceDefault,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse("500Mi"),
				},
			},
			StorageClassName: &sc.Name,
		},
	}

	// 5.b create small pod consuming the above pvc
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-quota-pod",
			Namespace: corev1.NamespaceDefault,
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
					Name: "tes-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvc.Name,
						},
					},
				},
			},
		},
	}

	pvc, err = k8sClient.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault).Create(ctx, pvc, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())
	t.Cleanup(func() {
		err := k8sClient.CoreV1().PersistentVolumeClaims(corev1.NamespaceDefault).Delete(
			context.Background(), pvc.Name, metav1.DeleteOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
	})
	pod, err = k8sClient.CoreV1().Pods(corev1.NamespaceDefault).Create(ctx, pod, metav1.CreateOptions{})
	t.Cleanup(func() {
		err := k8sClient.CoreV1().Pods(corev1.NamespaceDefault).Delete(
			context.Background(), pod.Name, metav1.DeleteOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
	})
	Expect(err).ToNot(HaveOccurred())

	// wait for pvc status to be bound
	Eventually(func() (corev1.PersistentVolumeClaimPhase, error) {
		current, err := k8sClient.CoreV1().
			PersistentVolumeClaims(pvc.Namespace).
			Get(ctx, pvc.Name, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		return current.Status.Phase, nil
	}, 5*time.Minute, 2*time.Second).Should(Equal(corev1.ClaimBound))

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

	// 6. verify writes below and above the PVC's requested capacity.
	pvcCapacity := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
	verifyPVCQuota(t, *pod, pvcCapacity.Value())

	// 7. restart mounter, wait for recovery, and verify alias and quota enforcement still work
	consumer, err := k8sClient.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())

	poolPodsOnNOde := metav1.ListOptions{
		LabelSelector: fmt.Sprintf(
			"k8s-app=hostpath-provisioner,kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", poolName,
		),
		FieldSelector: "spec.nodeName=" + consumer.Spec.NodeName,
	}

	mounters, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, poolPodsOnNOde)
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
	var replacement corev1.Pod
	Eventually(func() (bool, error) {
		current, err := k8sClient.CoreV1().Pods(hppNamespace).List(ctx, poolPodsOnNOde)
		if err != nil {
			return false, err
		}

		for _, candidate := range current.Items {
			if candidate.UID == oldMounter.UID || candidate.DeletionTimestamp != nil {
				continue
			}

			for _, condition := range candidate.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					replacement = candidate
					return true, nil
				}
			}
		}

		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(BeTrue())

	verifyBackingDeviceAlias(t, ctx, k8sClient, replacement, selector.String(), aliasPath)
	verifyPVCQuota(t, *pod, pvcCapacity.Value())

}
