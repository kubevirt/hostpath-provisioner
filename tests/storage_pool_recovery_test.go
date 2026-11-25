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
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const recoveryDriverContainer = "hostpath-provisioner"

// Save the original filesystem identity and options before deliberately unmounting it.
// Mount IDs can change during recovery; the filesystem and exposed directory must not.
type poolRecoveryMountInfo struct {
	ID      int    `json:"id"`      // Kernel mount ID within this namespace.
	Device  string `json:"maj:min"` // Backing filesystem's device number.
	Root    string `json:"fsroot"`  // Directory exposed from inside that filesystem.
	Source  string `json:"source"`
	FSType  string `json:"fstype"`
	Options string `json:"options"`
}

// Describe one expected view of the pool: the removed mount, the node, or CSI.
// Mount IDs may be reused; successful unmount and a new recovery log prove recreation.
type poolRecoveryMountCheck struct {
	pod, container, path string
	onHost               bool
	before               poolRecoveryMountInfo
}

// Verify one view without changing any mounts; the polling loop can safely retry it.
func (check poolRecoveryMountCheck) verify() (poolRecoveryMountInfo, error) {
	current, err := readPoolRecoveryMount(check.pod, check.container, check.path, check.onHost)
	if err != nil {
		return current, err
	}
	return current, poolRecoveryMountMatches(current, check.before)
}

// Host commands must enter the node's mount namespace; chroot alone still sees container mounts.
func execPoolRecoveryCommand(pod, container string, host bool, command ...string) (string, error) {
	args := []string{"exec", "-n", hppNamespace, pod, "-c", container, "--"}
	if host {
		args = append(args, "/usr/bin/nsenter", "--mount=/host/proc/1/ns/mnt",
			"--root=/host/proc/1/root", "--wd=/host/proc/1/root", "--")
	}
	return RunKubeCtlCommand(append(args, command...)...)
}

// An exact mountpoint and device identity prevent a bare directory on the node root from passing.
func readPoolRecoveryMount(pod, container, path string, host bool) (poolRecoveryMountInfo, error) {
	out, err := execPoolRecoveryCommand(pod, container, host,
		"/usr/bin/findmnt", "--json", "--mountpoint", path,
		"--output", "ID,MAJ:MIN,FSROOT,SOURCE,FSTYPE,OPTIONS")
	if err != nil {
		return poolRecoveryMountInfo{}, fmt.Errorf("failed to inspect mount %s in %s: %w", path, pod, err)
	}
	var result struct {
		Filesystems []poolRecoveryMountInfo `json:"filesystems"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return poolRecoveryMountInfo{}, fmt.Errorf("failed to parse findmnt output %q: %w", out, err)
	}
	if len(result.Filesystems) != 1 || result.Filesystems[0].ID == 0 {
		return poolRecoveryMountInfo{}, fmt.Errorf("expected one mount at %s, got %q", path, out)
	}
	return result.Filesystems[0], nil
}

// Accept a remount only if it exposes the same filesystem directory and still enforces quotas.
func poolRecoveryMountMatches(actual, expected poolRecoveryMountInfo) error {
	if actual.Device != expected.Device || actual.Root != expected.Root || actual.FSType != expected.FSType {
		return fmt.Errorf("mount identity changed: got device=%s root=%s fs=%s, expected device=%s root=%s fs=%s",
			actual.Device, actual.Root, actual.FSType, expected.Device, expected.Root, expected.FSType)
	}
	return requirePoolRecoveryQuotaFlags(actual)
}

// Require enforcement, both before the test and after recovery.
func requirePoolRecoveryQuotaFlags(info poolRecoveryMountInfo) error {
	for _, option := range strings.Split(info.Options, ",") {
		if option == "prjquota" || option == "pquota" {
			return nil
		}
	}
	return fmt.Errorf("pool mount is missing project quota flags: %s", info.Options)
}

// Pick a ready pod, optionally on a particular node, before inspecting its mount namespace.
func readyPoolRecoveryPod(ctx context.Context, t *testing.T, client *kubernetes.Clientset, selector, node string) *corev1.Pod {
	t.Helper()
	t.Logf("waiting for ready pod: selector=%s node=%s", selector, node)
	var selected *corev1.Pod
	gomega.Eventually(func() (bool, error) {
		options := metav1.ListOptions{LabelSelector: selector}
		if node != "" {
			options.FieldSelector = "spec.nodeName=" + node
		}
		pods, err := client.CoreV1().Pods(hppNamespace).List(ctx, options)
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if pod.DeletionTimestamp != nil || pod.Spec.NodeName == "" {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					selected = pod.DeepCopy()
					return true, nil
				}
			}
		}
		return false, nil
	}, 5*time.Minute, 2*time.Second).Should(gomega.BeTrue(), "need a ready pod for selector %s on node %s", selector, node)
	t.Logf("selected ready pod %s/%s on node %s", selected.Namespace, selected.Name, selected.Spec.NodeName)
	return selected
}

// Derive the CSI path from the deployed pod instead of assuming a pool-name convention.
func poolRecoveryCSIPath(t *testing.T, pod *corev1.Pod, poolPath string) string {
	t.Helper()
	for _, volume := range pod.Spec.Volumes {
		if volume.HostPath == nil || filepath.Clean(volume.HostPath.Path) != filepath.Clean(poolPath) {
			continue
		}
		for _, container := range pod.Spec.Containers {
			if container.Name != recoveryDriverContainer {
				continue
			}
			for _, mount := range container.VolumeMounts {
				if mount.Name == volume.Name {
					return filepath.Join(mount.MountPath, "csi")
				}
			}
		}
	}
	t.Fatalf("CSI pod %s does not expose host pool path %s", pod.Name, poolPath)
	return ""
}

// The source path belongs to the backing PVC's data mount, not the host-root mount.
func poolRecoverySourcePath(t *testing.T, pod *corev1.Pod) string {
	t.Helper()
	for _, container := range pod.Spec.Containers {
		if container.Name == "mounter" {
			for _, mount := range container.VolumeMounts {
				if mount.Name == "data" {
					return mount.MountPath
				}
			}
		}
	}
	t.Fatalf("mounter %s has no filesystem source volume", pod.Name)
	return ""
}

// Count completed recoveries for this path so kubelet repair or a pod restart cannot masquerade as mounter recovery.
func poolRecoveryLogCount(ctx context.Context, client *kubernetes.Clientset, pod, namespace, target string) (int, error) {
	logs, err := client.CoreV1().Pods(hppNamespace).GetLogs(pod, &corev1.PodLogOptions{Container: "mounter"}).DoRaw(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, line := range strings.Split(string(logs), "\n") {
		var entry struct {
			Message   string `json:"msg"`
			Namespace string `json:"namespace"`
			Target    string `json:"target"`
		}
		if json.Unmarshal([]byte(line), &entry) == nil && entry.Message == "Recovered pool mount" && entry.Namespace == namespace && entry.Target == target {
			count++
		}
	}
	return count, nil
}

// Recovery must happen in the running process, rather than through a replacement pod or container.
func expectPoolRecoveryPodUnchanged(ctx context.Context, t *testing.T, client *kubernetes.Clientset, original *corev1.Pod, container string) {
	t.Helper()
	current, err := client.CoreV1().Pods(original.Namespace).Get(ctx, original.Name, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(current.UID).To(gomega.Equal(original.UID), "mount recovery must not replace pod %s", original.Name)
	gomega.Expect(current.DeletionTimestamp).To(gomega.BeNil())
	for _, before := range original.Status.ContainerStatuses {
		if before.Name != container {
			continue
		}
		for _, after := range current.Status.ContainerStatuses {
			if after.Name == container {
				gomega.Expect(after.RestartCount).To(gomega.Equal(before.RestartCount), "container %s restarted instead of recovering its mounts", container)
				gomega.Expect(after.ContainerID).To(gomega.Equal(before.ContainerID))
				gomega.Expect(after.State.Running).ToNot(gomega.BeNil())
				t.Logf("pod %s container %s stayed Running without restarts (restartCount=%d)", original.Name, container, after.RestartCount)
				return
			}
		}
	}
	t.Fatalf("container %s status is missing in pod %s", container, original.Name)
}

// This fallback runs only during cleanup after a failed test. Never cover an existing
// mount or restore a device whose UUID no longer matches the original pool.
func restorePoolRecoveryTestMount(mounter, target, hostTarget, device, uuid string, before poolRecoveryMountInfo, unmountMounterSource bool) error {
	// Restore the host filesystem first; a container bind must never use the
	// ordinary directory left underneath an unavailable pool mount.
	_, err := execPoolRecoveryCommand(mounter, "mounter", true, "/bin/sh", "-ec", `
if /usr/bin/findmnt --mountpoint "$1" >/dev/null; then exit 0; else result=$?; fi
[ "$result" -eq 1 ]
[ "$(/usr/sbin/blkid -s UUID -o value "$2")" = "$3" ]
/usr/bin/mount -t "$4" -o "$5" "$2" "$1"
`, "sh", hostTarget, device, uuid, before.FSType, before.Options)
	if err != nil {
		return err
	}
	current, err := readPoolRecoveryMount(mounter, "mounter", hostTarget, true)
	if err != nil {
		return err
	}
	if err := poolRecoveryMountMatches(current, before); err != nil {
		return err
	}
	if !unmountMounterSource {
		return nil
	}
	// The host source is now verified. Rebuild /source only if it is still missing.
	_, err = execPoolRecoveryCommand(mounter, "mounter", false, "/bin/sh", "-ec", `
if /usr/bin/findmnt --mountpoint "$1" >/dev/null; then exit 0; else result=$?; fi
[ "$result" -eq 1 ]
/usr/bin/mount --bind "/host$2" "$1"
`, "sh", target, hostTarget)
	if err != nil {
		return err
	}
	current, err = readPoolRecoveryMount(mounter, "mounter", target, false)
	if err != nil {
		return err
	}
	return poolRecoveryMountMatches(current, before)
}

// Run sequentially on a test cluster: these cases briefly remove real pool mounts.
// Verify the node and CSI namespace see the same data afterward without restarting either pod.
func runStoragePoolMountRecovery(t *testing.T, mode corev1.PersistentVolumeMode, unmountMounterSource bool) {
	t.Helper()
	gomega.RegisterTestingT(t)
	t.Logf("testing pool mount recovery: mode=%s unmountMounterSource=%t", mode, unmountMounterSource)
	ctx := context.Background()
	client, err := getKubeClient()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	// 1. Use a ready mounter and CSI driver on the same node.
	pool := getStoragePoolsForModes(t, ctx, mode)[0]
	waitForCSIDS(t, client, ctx)
	mounter := readyPoolRecoveryPod(ctx, t, client, fmt.Sprintf("kubevirt.io.hostpath-provisioner/storagePool=%s-hpp", pool.Name), "")
	ds, err := client.AppsV1().DaemonSets(hppNamespace).Get(ctx, hppCSIName, metav1.GetOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	csiPod := readyPoolRecoveryPod(ctx, t, client, selector.String(), mounter.Spec.NodeName)
	hostTarget := filepath.Join(pool.Path, "csi")
	csiTarget := poolRecoveryCSIPath(t, csiPod, pool.Path)
	t.Logf("pool %s on node %s: host path=%s CSI path=%s", pool.Name, mounter.Spec.NodeName, hostTarget, csiTarget)
	// 2. Save the healthy filesystem identity and confirm that quotas are enabled.
	var nodeBefore, csiBefore poolRecoveryMountInfo
	gomega.Eventually(func() error {
		var err error
		nodeBefore, err = readPoolRecoveryMount(mounter.Name, "mounter", hostTarget, true)
		if err != nil {
			return err
		}
		csiBefore, err = readPoolRecoveryMount(csiPod.Name, recoveryDriverContainer, csiTarget, false)
		if err != nil {
			return err
		}
		if err := requirePoolRecoveryQuotaFlags(nodeBefore); err != nil {
			return err
		}
		return poolRecoveryMountMatches(csiBefore, nodeBefore)
	}, 5*time.Minute, 2*time.Second).Should(gomega.Succeed(), "pool must be mounted with quotas on both node and CSI container before the test")
	gomega.Expect(nodeBefore.FSType).To(gomega.BeElementOf("ext4", "xfs"))
	gomega.Expect(nodeBefore.Root).To(gomega.Equal("/"), "recovery fixture must expose a whole RBD filesystem")
	device := strings.SplitN(nodeBefore.Source, "[", 2)[0]
	uuid, err := execPoolRecoveryCommand(mounter.Name, "mounter", true, "/usr/sbin/blkid", "-s", "UUID", "-o", "value", device)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	uuid = strings.TrimSpace(uuid)
	gomega.Expect(uuid).ToNot(gomega.BeEmpty())
	t.Logf("saved healthy pool: filesystem=%s device=%s UUID=%s hostMountID=%d CSIMountID=%d",
		nodeBefore.FSType, nodeBefore.Device, uuid, nodeBefore.ID, csiBefore.ID)

	// The global snapshot must exist before deliberately removing a mount.
	gomega.Eventually(func() (bool, error) {
		logs, err := client.CoreV1().Pods(hppNamespace).GetLogs(mounter.Name, &corev1.PodLogOptions{Container: "mounter"}).DoRaw(ctx)
		return strings.Contains(string(logs), "Saved pool mount layout for recovery"), err
	}, time.Minute, time.Second).Should(gomega.BeTrue(), "deploy the operator image containing mount recovery before running this test")
	t.Logf("mounter %s has recorded its recovery layout", mounter.Name)

	// 3. Put a unique file in the pool and prove both namespaces see the same inode.
	marker, err := execPoolRecoveryCommand(mounter.Name, "mounter", true, "/bin/sh", "-ec",
		`file=$(mktemp "$1/.hpp-mount-recovery-XXXXXX"); printf '%s' before-recovery > "$file"; printf '%s' "$file"`, "sh", hostTarget)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	marker = strings.TrimSpace(marker)
	gomega.Expect(filepath.Dir(marker)).To(gomega.Equal(hostTarget))
	t.Cleanup(func() {
		// This runs after emergency mount restoration because cleanup callbacks run last-in, first-out.
		current, err := readPoolRecoveryMount(mounter.Name, "mounter", hostTarget, true)
		if err == nil {
			err = poolRecoveryMountMatches(current, nodeBefore)
		}
		if err != nil {
			t.Errorf("cannot safely remove recovery marker %s: %v", marker, err)
			return
		}
		_, err = execPoolRecoveryCommand(mounter.Name, "mounter", true, "/usr/bin/rm", "-f", "--", marker)
		if err != nil {
			t.Errorf("remove recovery marker: %v", err)
		} else {
			t.Logf("removed recovery marker %s", marker)
		}
	})
	csiMarker := filepath.Join(csiTarget, filepath.Base(marker))
	identity, err := execPoolRecoveryCommand(mounter.Name, "mounter", true, "/usr/bin/stat", "-c", "%d:%i", marker)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	out, err := execPoolRecoveryCommand(csiPod.Name, recoveryDriverContainer, false, "/usr/bin/stat", "-c", "%d:%i", csiMarker)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(out).To(gomega.Equal(identity), "node and CSI container must see the same marker inode")
	t.Logf("marker %s is visible inside CSI at %s with device:inode=%s", marker, csiMarker, identity)

	// 4. Arm failure cleanup before unmounting. Manual cleanup cannot make a failed test pass.
	target, namespace, before, onHost := hostTarget, "host", nodeBefore, true
	if unmountMounterSource {
		target, namespace, onHost = poolRecoverySourcePath(t, mounter), "container", false
		before, err = readPoolRecoveryMount(mounter.Name, "mounter", target, false)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}
	previousRecoveries, err := poolRecoveryLogCount(ctx, client, mounter.Name, namespace, target)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	t.Logf("saved %s mount %s: mountID=%d previous completed recoveries=%d", namespace, target, before.ID, previousRecoveries)
	recovered := false
	t.Cleanup(func() {
		if recovered {
			return
		}
		t.Logf("test failed before recovery completed; attempting emergency restoration of %s", target)
		if err := restorePoolRecoveryTestMount(mounter.Name, target, hostTarget, device, uuid, nodeBefore, unmountMounterSource); err != nil {
			t.Errorf("emergency pool mount restoration failed: %v", err)
		} else {
			t.Logf("emergency restoration completed for %s; the original test failure is retained", target)
		}
	})
	t.Logf("unmounting %s in %s namespace on node %s; expecting recovery without pod restarts", target, namespace, mounter.Spec.NodeName)
	if unmountMounterSource {
		// Isolate the container parent non-recursively so removing /source cannot
		// propagate the unmount into kubelet's mounts or the node root namespace.
		_, err = execPoolRecoveryCommand(mounter.Name, "mounter", false, "/bin/sh", "-ec",
			`/usr/bin/mount --make-private /; /usr/bin/umount -- "$1"`, "sh", target)
	} else {
		_, err = execPoolRecoveryCommand(mounter.Name, "mounter", true, "/usr/bin/umount", "--", target)
	}
	gomega.Expect(err).ToNot(gomega.HaveOccurred(), "test must actually unmount %s", target)
	t.Logf("unmount command succeeded for %s; recovery checks start now", target)

	// 5. Require the mounter to restore the mount and preserve both filesystem views.
	// Check the removed mount, node view, and CSI view separately. The marker
	// check below also proves that CSI sees the original data, not an empty directory.
	checks := []poolRecoveryMountCheck{
		{pod: mounter.Name, container: "mounter", path: target, onHost: onHost, before: before},
		{pod: mounter.Name, container: "mounter", path: hostTarget, onHost: true, before: nodeBefore},
		{pod: csiPod.Name, container: recoveryDriverContainer, path: csiTarget, before: csiBefore},
	}
	t.Logf("waiting for mount recovery: target=%s originalMountID=%d; checking node and CSI visibility", target, before.ID)
	lastSeenMountID := 0
	gomega.Eventually(func() error {
		for index, check := range checks {
			current, err := check.verify()
			if err != nil {
				return err
			}
			// Log the observed mount once, even when its ID was recycled by the kernel.
			if index == 0 && current.ID != lastSeenMountID {
				t.Logf("target %s reappeared: mountID=%d device=%s filesystem=%s options=%s", target, current.ID, current.Device, current.FSType, current.Options)
				lastSeenMountID = current.ID
			}
		}
		actualIdentity, err := execPoolRecoveryCommand(csiPod.Name, recoveryDriverContainer, false, "/usr/bin/stat", "-c", "%d:%i", csiMarker)
		if err != nil {
			return err
		}
		if actualIdentity != identity {
			return fmt.Errorf("CSI container sees the wrong marker inode: %q", actualIdentity)
		}
		count, err := poolRecoveryLogCount(ctx, client, mounter.Name, namespace, target)
		if err != nil {
			return err
		}
		if count <= previousRecoveries {
			return fmt.Errorf("mounter has not reported completing recovery for %s", target)
		}
		return nil
	}, 2*time.Minute, time.Second).Should(gomega.Succeed(), "mounter should restore %s on the node and preserve CSI pool visibility", target)
	recovered = true
	t.Logf("mounter confirmed recovery of %s (previous mountID=%d, recovered mountID=%d); node and CSI filesystem identities match",
		target, before.ID, lastSeenMountID)

	// 6. Check real writes after recovery, then confirm neither container was restarted.
	_, err = execPoolRecoveryCommand(csiPod.Name, recoveryDriverContainer, false, "/bin/sh", "-ec",
		`[ "$(cat "$1")" = before-recovery ]; printf '%s' after-recovery > "$1"`, "sh", csiMarker)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	out, err = execPoolRecoveryCommand(mounter.Name, "mounter", true, "/usr/bin/cat", marker)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(out).To(gomega.Equal("after-recovery"))
	expectPoolRecoveryPodUnchanged(ctx, t, client, mounter, "mounter")
	expectPoolRecoveryPodUnchanged(ctx, t, client, csiPod, recoveryDriverContainer)
	t.Logf("recovered %s: node=%s, CSI pod=%s, filesystem=%s; marker data preserved", target, mounter.Spec.NodeName, csiPod.Name, nodeBefore.FSType)
}

// Recover the block pool's node filesystem mount and its propagated CSI view.
func TestBlockStoragePoolMountRecovery(t *testing.T) {
	runStoragePoolMountRecovery(t, corev1.PersistentVolumeBlock, false)
}

// Recover the filesystem pool's node bind mount and verify it reappears inside CSI.
func TestFilesystemStoragePoolBindMountRecovery(t *testing.T) {
	runStoragePoolMountRecovery(t, corev1.PersistentVolumeFilesystem, false)
}

// Removing only the mounter's source bind must be repaired without losing node or CSI access.
func TestFilesystemStoragePoolSourceMountRecovery(t *testing.T) {
	runStoragePoolMountRecovery(t, corev1.PersistentVolumeFilesystem, true)
}
