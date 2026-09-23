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
	"k8s.io/klog/v2"
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

// committedQuotaBytes is the sum of hard limits on immediate children of baseDir (aka each PVC).
func committedQuotaBytes(baseDir string) (int64, error) {
	mount, err := fileSystemMount(baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to find filesystem mount for pool %s: %w", baseDir, err)
	}
	fsType, err := getFilesystemType(baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to determine filesystem type for pool %s: %w", baseDir, err)
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return 0, fmt.Errorf("failed to read pool directory %s: %w", baseDir, err)
	}
	var committed int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		limit, err := projectHardLimitBytes(filepath.Join(baseDir, entry.Name()), mount, fsType)
		if err != nil {
			return 0, fmt.Errorf("failed to read quota limit for volume %s: %w", entry.Name(), err)
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
	_, hard, err := projectUsageAndHardLimitBytes(dir, mount, fsType)
	return hard, err
}

// projectUsageAndHardLimitBytes returns the current usage and hard limit for
// dir's project in bytes. A directory without a project ID returns zeroes.
func projectUsageAndHardLimitBytes(dir, mount, fsType string) (int64, int64, error) {
	id, err := getProjectID(dir)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to read project ID for %s: %w", dir, err)
	}
	if id == 0 {
		return 0, 0, nil
	}

	idStr := strconv.FormatUint(uint64(id), 10)
	switch fsType {
	case xfs:
		inner := fmt.Sprintf("report -p -b -N -L %s -U %s", idStr, idStr)
		klog.Info("Inner report command", "innerReportCmd", inner)
		out, err := exec.Command(xfsQuotaBin, "-x", "-c", inner, mount).CombinedOutput()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to run %s for project %s on mount %s: %w (output: %q)", xfsQuotaBin, idStr, mount, err, out)
		}
		used, hard, err := parseQuotaUsageAndHardLimit(out)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse %s output for project %s on mount %s: %w", xfsQuotaBin, idStr, mount, err)
		}
		return used, hard, nil
	case ext4:
		// -w: one line so fields[3] is hard limit. --filesystem=: mount is not a second project name.
		out, err := exec.Command(quotaBin, "-P", "-v", "-w", "--filesystem="+mount, idStr).CombinedOutput()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to run %s for project %s on mount %s: %w (output: %q)", quotaBin, idStr, mount, err, out)
		}
		usedKiB, hardKiB, err := parseQuotaUsageAndHardLimit(out)
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse %s output for project %s on mount %s: %w", quotaBin, idStr, mount, err)
		}
		return usedKiB * 1024, hardKiB * 1024, nil
	default:
		return 0, 0, fmt.Errorf("failed to query project quota: unsupported filesystem %q", fsType)
	}
}

// projectQuotaStatsBytes looks up the filesystem containing dir and returns
// that directory project's current used and hard-limit bytes.
func projectQuotaStatsBytes(dir string) (int64, int64, error) {
	mount, err := fileSystemMount(dir)
	if err != nil {
		klog.Errorf("failed to get project quota stats: unable to resolve filesystem mount for %s: %v", dir, err)
		return 0, 0, fmt.Errorf("failed to resolve filesystem mount for %s: %w", dir, err)
	}
	fsType, err := getFilesystemType(dir)
	if err != nil {
		klog.Errorf("failed to get project quota stats: unable to determine filesystem type for %s: %v", dir, err)
		return 0, 0, fmt.Errorf("failed to determine filesystem type for %s: %w", dir, err)
	}
	used, hardLimit, err := projectUsageAndHardLimitBytes(dir, mount, fsType)
	if err != nil {
		klog.Errorf("failed to get project quota stats: unable to query quota for volume %s on mount %s (filesystem %s): %v", dir, mount, fsType, err)
		return 0, 0, fmt.Errorf("failed to query project quota for volume %s on mount %s: %w", dir, mount, err)
	}
	return used, hardLimit, nil
}

// last data line: id/device, used, soft, hard, ...
func parseHardLimitField(out []byte) (int64, error) {
	_, hard, err := parseQuotaUsageAndHardLimit(out)
	return hard, err
}

// last data line: id/device, used, soft, hard, ...
func parseQuotaUsageAndHardLimit(out []byte) (int64, int64, error) {
	var fields []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields = strings.Fields(line)
	}
	if len(fields) < 4 {
		return 0, 0, fmt.Errorf("failed to parse quota output: expected a data row with at least 4 fields, got %q", out)
	}
	used, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse quota usage %q: %w", fields[1], err)
	}
	hard, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse quota hard limit %q: %w", fields[3], err)
	}
	return used, hard, nil
}

// helper to set project id at a given path
func setProjectID(path string, projectID uint32) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("failed to open %s to set project ID: %w", path, err)
	}
	defer unix.Close(fd)

	var attr fsxattr
	// read current struct first so other inode flags are preserved
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCGetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return fmt.Errorf("failed to read filesystem attributes for %s: %w", path, errno)
	}
	attr.Projid = projectID
	attr.Xflags |= fsXFlagProjInherit
	_, _, errno = unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCSetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return fmt.Errorf("failed to set project ID %d for %s: %w", projectID, path, errno)
	}
	return nil
}

// getProjectID returns the project ID stored on the directory inode.
func getProjectID(path string) (uint32, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return 0, fmt.Errorf("failed to open %s to read project ID: %w", path, err)
	}
	defer unix.Close(fd)

	var attr fsxattr
	// IoctlSetPointerInt only passes an int. This ioctl fills a struct, so call ioctl directly.
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(fsIOCGetXattr), uintptr(unsafe.Pointer(&attr)))
	if errno != 0 {
		return 0, fmt.Errorf("failed to read filesystem attributes for %s: %w", path, errno)
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
			return fmt.Errorf("failed to read project ID for %s: %w", path, err)
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
		return 0, fmt.Errorf("failed to scan pool %s for project IDs: %w", baseDir, err)
	}

	if !found {
		return uint32(maxProjectID), nil
	}
	if minID <= minProjectID {
		return 0, fmt.Errorf("failed to allocate project ID: no free IDs in range [%d, %d]", minProjectID, maxProjectID)
	}
	return minID - 1, nil
}

// getFilesystemType looks at the disk under path and returns "ext4" or "xfs".
// That tells us which quota tool to use later.
func getFilesystemType(path string) (string, error) {
	var stat unix.Statfs_t

	err := unix.Statfs(path, &stat)
	if err != nil {
		return "", fmt.Errorf("failed to stat filesystem for %s: %w", path, err)
	}

	switch stat.Type {
	case MagicExt4:
		return ext4, nil
	case MagicXfs:
		return xfs, nil
	default:
		return "", fmt.Errorf("failed to determine filesystem type for %s: unsupported filesystem magic %#x", path, stat.Type)
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
		return 0, fmt.Errorf("failed to read existing project ID for volume %s: %w", volID, err)
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
		return 0, fmt.Errorf("failed to calculate committed quota for pool %s: %w", poolPath, err)
	}
	if committed+capacityBytes > poolCapacity {
		// This is a CreateVolume capacity rejection, not a filesystem write.
		// Report the CSI ResourceExhausted status; actual writes that exceed a
		// successfully configured project quota will receive filesystem ENOSPC.
		return 0, status.Errorf(codes.ResourceExhausted,
			"failed to reserve quota: committed %d + requested %d exceeds pool capacity %d", committed, capacityBytes, poolCapacity,
		)
	}

	projectID, err := getNextProjectID(poolPath)
	if err != nil {
		return 0, fmt.Errorf("failed to allocate project ID for volume %s: %w", volID, err)
	}

	fsType, err := getFilesystemType(volPath)
	if err != nil {
		return 0, fmt.Errorf("failed to determine filesystem type for volume %s: %w", volID, err)
	}

	switch fsType {
	case ext4:
		err = setExt4ProjectQuota(volPath, projectID, capacityBytes)
	case xfs:
		err = setXfsProjectQuota(volPath, projectID, capacityBytes)
	default:
		err = fmt.Errorf("failed to apply project quota: unsupported filesystem %q", fsType)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to apply project quota to volume %s: %w", volID, err)
	}
	klog.Infof("applied project quota: volume=%s projectID=%d limitBytes=%d", volPath, projectID, capacityBytes)
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
		return fmt.Errorf("failed to clear project ID for %s: %w", volPath, err)
	}
	return nil
}

// fileSystemMount returns the disk mount that contains path.
// For /var/hpvolumes/pvc-abc that is often /var/hpvolumes, or / if the
// pool is just a folder on the root disk. Quota limits are stored there.
func fileSystemMount(path string) (string, error) {
	infos, err := getMountInfos("-T", path)
	if err != nil {
		return "", fmt.Errorf("failed to find mount containing %s: %w", path, err)
	}
	if len(infos) != 1 {
		return "", fmt.Errorf("failed to find a unique mount for %s: expected 1, got %d", path, len(infos))
	}
	return infos[0].Target, nil
}

// setExt4ProjectQuota tags the PVC directory with projectID and sets a hard
// block limit so writes past capacityBytes fail with ENOSPC inside the pod.
func setExt4ProjectQuota(volPath string, projectID uint32, capacityBytes int64) error {
	mount, err := fileSystemMount(volPath)
	if err != nil {
		return fmt.Errorf("failed to find filesystem mount for volume %s: %w", volPath, err)
	}

	// Tag this PVC directory with the project ID and inherit flag so new files
	// stay in the same project. tune2fs is not used here: it enables project
	// quotas on the whole filesystem (one-time mount/setup), not a per-volume limit.
	out, err := exec.Command(chattrBin, "+P", volPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run %s +P for volume %s: %w (output: %q)", chattrBin, volPath, err, out)
	}

	projectIDStr := strconv.FormatUint(uint64(projectID), 10)
	out, err = exec.Command(chattrBin, "-p", projectIDStr, volPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run %s -p %s for volume %s: %w (output: %q)", chattrBin, projectIDStr, volPath, err, out)
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
		return fmt.Errorf("failed to run %s for project %s on mount %s: %w (output: %q)", setquotaBin, projectIDStr, mount, err, out)
	}

	return nil
}

// setXfsProjectQuota does the same job as setExt4ProjectQuota, using xfs_quota.
// "project -s" tags the PVC directory. "limit" sets the hard byte cap on the disk mount.
func setXfsProjectQuota(volPath string, projectID uint32, capacityBytes int64) error {
	mount, err := fileSystemMount(volPath)
	if err != nil {
		return fmt.Errorf("failed to find filesystem mount for volume %s: %w", volPath, err)
	}

	projectCmd := fmt.Sprintf("project -s -p %s %d", volPath, projectID)
	out, err := exec.Command(xfsQuotaBin, "-x", "-c", projectCmd, mount).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to assign XFS project %d to volume %s on mount %s: %w (output: %q)", projectID, volPath, mount, err, out)
	}

	limitCmd := fmt.Sprintf("%s -p bhard=%d %d", limitBin, capacityBytes, projectID)
	out, err = exec.Command(xfsQuotaBin, "-x", "-c", limitCmd, mount).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to set XFS quota limit for project %d on mount %s: %w (output: %q)", projectID, mount, err, out)
	}

	return nil
}
