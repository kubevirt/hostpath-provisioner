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
package hostpath

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

const (
	testSnapshot    = "snapshot-123"
	testSnapshot2   = "snapshot-234"
	testVolume      = "volume-123"
	testSnapshotDir = "snapshots"
	testVolumeDir   = "volumes"
)

func TestReflink_Initialize(t *testing.T) {
	RegisterTestingT(t)
	reflink := &Reflink{
		path:       "/invalid/path",
		sourcePath: "/invalid/path",
		nodeName:   "testnode",
	}
	err := reflink.Initialize()
	Expect(err).To(HaveOccurred())
}

func TestReflink_GetSnapshotById(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	t.Run("snapshot does not exist", func(t *testing.T) {
		snapshotId := testSnapshot
		snapshot, err := reflink.GetSnapshotById(snapshotId)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot).To(BeNil())
	})
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot).ToNot(BeNil())
	t.Run("snapshot exists", func(t *testing.T) {
		snapshotId := testSnapshot
		snapshot, err := reflink.GetSnapshotById(snapshotId)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot).ToNot(BeNil())
		Expect(snapshot.SnapshotId).To(Equal(snapshotId))
	})
}

func TestReflink_GetSnapshotsByVolumeSourceId(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	t.Run("no snapshots", func(t *testing.T) {
		snapshots, err := reflink.GetSnapshotsByVolumeSourceId(testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots).To(BeEmpty())
	})
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot).ToNot(BeNil())
	t.Run("snapshot exists", func(t *testing.T) {
		snapshots, err := reflink.GetSnapshotsByVolumeSourceId(testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].SnapshotId).To(Equal(testSnapshot))
		Expect(snapshots[0].SourceVolumeId).To(Equal(testVolume))
	})
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume+"invalid"))
	Expect(err).ToNot(HaveOccurred())
	snapshot2, err := reflink.CreateSnapshot(testSnapshot2, testVolume+"invalid")
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot2).ToNot(BeNil())
	t.Run("multiple snapshots exist, but unrelated", func(t *testing.T) {
		snapshots, err := reflink.GetSnapshotsByVolumeSourceId(testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots).To(HaveLen(1))
		Expect(snapshots[0].SnapshotId).To(Equal(testSnapshot))
		Expect(snapshots[0].SourceVolumeId).To(Equal(testVolume))
	})
	snapshot3, err := reflink.CreateSnapshot(testSnapshot2+"valid", testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot2).ToNot(BeNil())
	t.Run("multiple snapshots exist, but unrelated", func(t *testing.T) {
		snapshots, err := reflink.GetSnapshotsByVolumeSourceId(testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots).To(HaveLen(2))
		Expect(snapshots[0].SnapshotId).To(Equal(testSnapshot))
		Expect(snapshots[0].SourceVolumeId).To(Equal(testVolume))
		Expect(snapshots[1].SnapshotId).To(Equal(snapshot3.SnapshotId))
		Expect(snapshots[1].SourceVolumeId).To(Equal(testVolume))
	})
}

func TestReflink_GetAllSnapshots(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	snapshots, err := reflink.GetAllSnapshots()
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshots).To(BeEmpty())
	err = ensurePathExists(filepath.Join(reflink.path))
	Expect(err).ToNot(HaveOccurred())
	err = os.WriteFile(filepath.Join(reflink.path, "invalid"), []byte{}, 0644)
	Expect(err).ToNot(HaveOccurred())
	t.Run("non directory snapshot file", func(t *testing.T) {
		snapshots, err := reflink.GetAllSnapshots()
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshots).To(BeEmpty())
	})
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot).ToNot(BeNil())
	snapshot2, err := reflink.CreateSnapshot(testSnapshot2, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot2).ToNot(BeNil())
	snapshots, err = reflink.GetAllSnapshots()
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshots).To(HaveLen(2))
}

func TestReflink_CreateSnapshot(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	t.Run("source volume does not exist", func(t *testing.T) {
		snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
		Expect(err).To(HaveOccurred())
		Expect(snapshot).To(BeNil())
	})
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	t.Run("source volume exists", func(t *testing.T) {
		snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot).ToNot(BeNil())
		Expect(snapshot.SnapshotId).To(Equal(testSnapshot))
		Expect(snapshot.SourceVolumeId).To(Equal(testVolume))
	})
	t.Run("source volume exists, and snapshot already exists", func(t *testing.T) {
		snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot).ToNot(BeNil())
		Expect(snapshot.SnapshotId).To(Equal(testSnapshot))
		Expect(snapshot.SourceVolumeId).To(Equal(testVolume))
	})
}

func TestReflink_DeleteSnapshot(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot).ToNot(BeNil())

	err = reflink.DeleteSnapshot(testSnapshot)
	Expect(err).ToNot(HaveOccurred())
}

func TestReflink_RestoreSnapshot(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := os.MkdirTemp(os.TempDir(), "")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(tempDir)
	reflink := &Reflink{
		path:       filepath.Join(tempDir, testSnapshotDir),
		sourcePath: filepath.Join(tempDir, testVolumeDir),
		nodeName:   "testnode",
	}
	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())
	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
	Expect(err).ToNot(HaveOccurred())
	Expect(snapshot).ToNot(BeNil())
	os.MkdirAll(filepath.Join(reflink.sourcePath, "restored"), 0777)
	err = reflink.RestoreSnapshot(testSnapshot, filepath.Join(reflink.sourcePath, "restored"))
	Expect(err).ToNot(HaveOccurred())
}

// NOTE: this test depends on CreateSnapshot to have reflink copy set to always
// currently it is set to auto
func TestReflink_ReflinkCopy(t *testing.T) {
	RegisterTestingT(t)

	// Check if we have necessary tools and permissions
	if os.Geteuid() != 0 {
		t.Skip("XFS reflink tests require root privileges (use 'go test' with sudo or in privileged container)")
	}

	if _, err := exec.LookPath("mkfs.xfs"); err != nil {
		t.Skip("mkfs.xfs not found - install xfsprogs package")
	}

	if _, err := exec.LookPath("losetup"); err != nil {
		t.Skip("losetup not found")
	}

	tempDir, err := os.MkdirTemp(os.TempDir(), "xfs-reflink-test-")
	Expect(err).ToNot(HaveOccurred())

	mountPoint := filepath.Join(tempDir, "mount")
	if err := os.MkdirAll(mountPoint, 0755); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("could not create mount point %v", err)
	}

	imageFile := filepath.Join(tempDir, "disk.img")
	cmd := exec.Command("truncate", "-s", "1G", imageFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create disk image: %v\n%s", err, output)
	}

	// Create loop device
	cmd = exec.Command("losetup", "--find", "--show", imageFile)
	output, err := cmd.Output()
	if err != nil {
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to create loop device: %v\n%s", err, output)
	}
	loopDevice := strings.TrimSpace(string(output))

	// Format as XFS with reflink support
	// -m reflink=1 enables reflink support
	// -m crc=1 is required for reflink
	cmd = exec.Command("mkfs.xfs", "-m", "reflink=1,crc=1", "-f", loopDevice)
	if output, err := cmd.CombinedOutput(); err != nil {
		exec.Command("losetup", "-d", loopDevice).Run()
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to format XFS: %v\n%s", err, output)
	}

	// Mount the filesystem
	cmd = exec.Command("mount", loopDevice, mountPoint)
	if output, err := cmd.CombinedOutput(); err != nil {
		exec.Command("losetup", "-d", loopDevice).Run()
		os.RemoveAll(tempDir)
		t.Fatalf("Failed to mount XFS: %v\n%s", err, output)
	}

	// Change ownership so tests can write
	if err := os.Chown(mountPoint, os.Getuid(), os.Getgid()); err != nil {
		t.Logf("Warning: failed to chown mount point: %v", err)
	}

	reflink := &Reflink{
		path:       filepath.Join(mountPoint, "snapshots"),
		sourcePath: filepath.Join(mountPoint, "volumes"),
		nodeName:   "testnode",
	}

	err = reflink.Initialize()
	Expect(err).ToNot(HaveOccurred())

	err = ensurePathExists(filepath.Join(reflink.sourcePath, testVolume))
	Expect(err).ToNot(HaveOccurred())
	t.Run("source volume exists", func(t *testing.T) {
		snapshot, err := reflink.CreateSnapshot(testSnapshot, testVolume)
		Expect(err).ToNot(HaveOccurred())
		Expect(snapshot).ToNot(BeNil())
		Expect(snapshot.SnapshotId).To(Equal(testSnapshot))
		Expect(snapshot.SourceVolumeId).To(Equal(testVolume))
	})

	// cleanup
	err = exec.Command("umount", mountPoint).Run()
	Expect(err).ToNot(HaveOccurred())
	err = exec.Command("losetup", "-d", loopDevice).Run()
	Expect(err).ToNot(HaveOccurred())
	err = os.RemoveAll(tempDir)
	Expect(err).ToNot(HaveOccurred())
}
