package hostpath

/*
* Important distinctions between volPath and mount
* poolPath - path where the logical PVC subdirectories will be stored
* volPath - PVC+UID; the directory that recieves and contains the project ID
* mount - identifies the node filesystem root path where the quotas will be tracked for each project id (id -> x GiB)
 */

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	maxProjectID = math.MaxInt32 // 2147483647; stays positive for quota tools
	minProjectID = 1             // 0 means "no project"
)

// common Linux FS types used on k8s nodes
const (
	MagicExt4 = 0xEF53
	MagicXfs  = 0x58465342

	ext4 = "ext4"
	xfs  = "xfs"
)

// Linux binaries
const (
	xfsQuotaBin = "xfs_quota"
	limitBin    = "limit"
	chattrBin   = "chattr"
	setquotaBin = "setquota"
	quotaBin    = "quota"
)

// fsxattr matches struct fsxattr in <linux/fs.h>. Field order and sizes must
// match the kernel or the project ID read back is garbage.
//
//	__u32 fsx_xflags;
//	__u32 fsx_extsize;
//	__u32 fsx_nextents;
//	__u32 fsx_projid;
//	__u32 fsx_cowextsize;
//	unsigned char fsx_pad[8];
type fsxattr struct {
	Xflags     uint32
	Extsize    uint32
	Nextents   uint32
	Projid     uint32
	Cowextsize uint32
	Pad        [8]byte
}

const (
	// _IOR('X', 31, struct fsxattr) on 64-bit Linux. Not in this vendored x/sys.
	fsIOCGetXattr = 0x801C581F
	// _IOW('X', 32, struct fsxattr). Pair of fsIOCGetXattr.
	fsIOCSetXattr = 0x401C5820

	// New files created under this directory keep the same project ID.
	fsXFlagProjInherit = 0x00000200
)

// committedQuotaBytes is the sum of hard limits on immediate children of baseDir.
func committedQuotaBytes(baseDir string) (int64, error) {
	mount, err := fileSystemMount(baseDir)
	if err != nil {
		return 0, err
	}
	fsType, err := getFilesystemType(baseDir)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return 0, err
	}
	var committed int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		limit, err := projectHardLimitBytes(filepath.Join(baseDir, entry.Name()), mount, fsType)
		if err != nil {
			return 0, err
		}
		committed += limit
	}
	return committed, nil
}

// projectHardLimitBytes returns the kernel hard limit for dir's project, in bytes.
// The inode only stores the project ID (0 means no quota). The byte cap lives
// in the filesystem quota table on mount, which is why this shells out.
// xfs_quota -b reports bytes; quota -P reports 1KiB blocks like setquota -P.
// Not using quotactl(2): it wants the block device, which the CSI pod may not have.
func projectHardLimitBytes(dir, mount, fsType string) (int64, error) {
	id, err := getProjectID(dir)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, nil
	}

	idStr := strconv.FormatUint(uint64(id), 10)
	switch fsType {
	case xfs:
		inner := fmt.Sprintf("quota -p -N -b %s", idStr)
		out, err := exec.Command(xfsQuotaBin, "-x", "-c", inner, mount).CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("%s quota: %w: %s", xfsQuotaBin, err, out)
		}
		hard, err := parseHardLimitField(out)
		if err != nil {
			return 0, err
		}
		return hard, nil
	case ext4:
		// -w: one line so fields[3] is hard limit. --filesystem=: mount is not a second project name.
		out, err := exec.Command(quotaBin, "-P", "-v", "-w", "--filesystem="+mount, idStr).CombinedOutput()
		if err != nil {
			return 0, fmt.Errorf("%s: %w: %s", quotaBin, err, out)
		}
		kib, err := parseHardLimitField(out)
		if err != nil {
			return 0, err
		}
		return kib * 1024, nil
	default:
		return 0, fmt.Errorf("unsupported filesystem %q", fsType)
	}
}

// last data line: id/device, used, soft, hard, ...
func parseHardLimitField(out []byte) (int64, error) {
	var fields []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields = strings.Fields(line)
	}
	if len(fields) < 4 {
		return 0, fmt.Errorf("Unexpected quota output: %q", out)
	}
	return strconv.ParseInt(fields[3], 10, 64)
}

// helper to set project id at a given path
func setProjectID(path string, projectID uint32) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	var attr fsxattr
	// read current struct first so other inode flags are preserved
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCGetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return errno
	}
	attr.Projid = projectID
	attr.Xflags |= fsXFlagProjInherit
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCSetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return errno
	}
	return nil
}

// getProjectID returns the project ID stored on the directory inode.
func getProjectID(path string) (uint32, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, err
	}
	defer unix.Close(fd)

	var attr fsxattr
	// IoctlSetPointerInt only passes an int. This ioctl fills a struct, so call ioctl directly.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCGetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return 0, errno
	}
	return attr.Projid, nil
}

// getNextProjectID returns the next project ID under baseDir by counting down.
// No directories yet means maxProjectID. Otherwise it is one less than the
// lowest ID already on a directory. 0 means "no project" and is never returned.
func getNextProjectID(baseDir string) (uint32, error) {
	var minID uint32
	found := false

	err := filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		id, err := getProjectID(path)
		if err != nil {
			return fmt.Errorf("error reading project id for %s: %w", path, err)
		}
		if id == 0 {
			return nil
		}
		if !found || id < minID {
			minID = id
			found = true
		}
		return nil
	})
	if err != nil {
		return 0, err
	}

	if !found {
		return uint32(maxProjectID), nil
	}
	if minID <= minProjectID {
		return 0, fmt.Errorf("no free project ids in range [%d, %d]", minProjectID, maxProjectID)
	}
	return minID - 1, nil
}

// getFilesystemType looks at the disk under path and returns "ext4" or "xfs".
// That tells us which quota tool to use later.
func getFilesystemType(path string) (string, error) {
	var stat unix.Statfs_t

	err := unix.Statfs(path, &stat)
	if err != nil {
		return "", err
	}

	switch stat.Type {
	case MagicExt4:
		return ext4, nil
	case MagicXfs:
		return xfs, nil
	default:
		return "", err
	}
}

// assignProjectID records a new project ID for this volume, or returns the
// one it already has if CreateVolume is retried.
// It also refuses the create when the pool has already promised more bytes
// than the disk can hold.
func assignProjectID(volPath, volID string, capacityBytes, poolCapacity int64) (uint32, error) {
	_ = volID

	// in case of CreateVolume retries
	existingID, err := getProjectID(volPath)
	if err != nil {
		return 0, err
	}
	if existingID != 0 {
		return existingID, nil
	}

	// poolPath is the logical HPP directory containing sibling volumes. It is
	// used to scan those directories and calculate committed capacity. Unlike
	// fileSystemMount(volPath), it is not necessarily a mount point: the pool
	// may simply be a directory on a larger mounted filesystem (for example,
	// poolPath=/var/hpvolumes while fileSystemMount(volPath) returns "/").
	poolPath := filepath.Dir(volPath)

	committed, err := committedQuotaBytes(poolPath)
	if err != nil {
		return 0, err
	}
	if committed+capacityBytes > poolCapacity {
		// This is a CreateVolume capacity rejection, not a filesystem write.
		// Report the CSI ResourceExhausted status; actual writes that exceed a
		// successfully configured project quota will receive filesystem ENOSPC.
		return 0, status.Errorf(codes.ResourceExhausted,
			"insufficient pool capacity: committed %d + requested %d > pool %d", committed, capacityBytes, poolCapacity,
		)
	}

	projectID, err := getNextProjectID(poolPath)
	if err != nil {
		return 0, err
	}

	fsType, err := getFilesystemType(volPath)
	if err != nil {
		return 0, err
	}

	switch fsType {
	case ext4:
		err = setExt4ProjectQuota(volPath, projectID, capacityBytes)
	case xfs:
		err = setXfsProjectQuota(volPath, projectID, capacityBytes)
	default:
		err = fmt.Errorf("unsupported filesystem %q", fsType)
	}
	if err != nil {
		return 0, err
	}
	return projectID, nil
}

// removeProjectId sets the projid at vol path to 0.
// It does not clear the quota.
// Called while the volume directory still exists, so we know the pool path.
func removeProjectID(volPath string) error {
	if err := setProjectID(volPath, 0); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("clearing project ID failed for %s: %w", volPath, err)
	}
	return nil
}

// fileSystemMount returns the disk mount that contains path.
// For /var/hpvolumes/pvc-abc that is often /var/hpvolumes, or / if the
// pool is just a folder on the root disk. Quota limits are stored there.
func fileSystemMount(path string) (string, error) {
	infos, err := getMountInfos("-T", path)
	if err != nil {
		return "", err
	}
	if len(infos) != 1 {
		return "", fmt.Errorf("expected 1 mount for %s, got %d", path, len(infos))
	}
	return infos[0].Target, nil
}

// setExt4ProjectQuota tags the PVC directory with projectID and sets a hard
// block limit so writes past capacityBytes fail with ENOSPC inside the pod.
func setExt4ProjectQuota(volPath string, projectID uint32, capacityBytes int64) error {
	mount, err := fileSystemMount(volPath)
	if err != nil {
		return err
	}

	// Tag this PVC directory with the project ID and inherit flag so new files
	// stay in the same project. tune2fs is not used here: it enables project
	// quotas on the whole filesystem (one-time mount/setup), not a per-volume limit.
	out, err := exec.Command(chattrBin, "+P", volPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s +P: %w: %s", chattrBin, err, out)
	}

	projectIDStr := strconv.FormatUint(uint64(projectID), 10)
	out, err = exec.Command(chattrBin, "-p", projectIDStr, volPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s -p: %w: %s", chattrBin, err, out)
	}

	// setquota -P takes block limits in 1KiB units, not bytes. Round up so a
	// request that is not a multiple of 1024 is not truncated below capacityBytes.
	// repquota is not used: it only reports usage, it cannot set a hard limit.
	capacityKiB := (capacityBytes + 1023) / 1024
	out, err = exec.Command(setquotaBin, "-P", projectIDStr,
		"0", strconv.FormatInt(capacityKiB, 10),
		"0", "0",
		mount,
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", setquotaBin, err, out)
	}

	return nil
}

// setXfsProjectQuota does the same job as setExt4ProjectQuota, using xfs_quota.
// "project -s" tags the PVC directory. "limit" sets the hard byte cap on the disk mount.
func setXfsProjectQuota(volPath string, projectID uint32, capacityBytes int64) error {
	mount, err := fileSystemMount(volPath)
	if err != nil {
		return err
	}

	projectCmd := fmt.Sprintf("project -s -p %s %d", volPath, projectID)
	out, err := exec.Command(xfsQuotaBin, "-x", "-c", projectCmd, mount).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s project: %w: %s", xfsQuotaBin, err, out)
	}

	limitCmd := fmt.Sprintf("%s -p bhard=%d %d", limitBin, capacityBytes, projectID)
	out, err = exec.Command(xfsQuotaBin, "-x", "-c", limitCmd, mount).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s limit: %w: %s", xfsQuotaBin, err, out)
	}

	return nil
}
