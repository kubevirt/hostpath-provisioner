package hostpath

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
)

const (
	projQuotasFile = ".hpp-quotas"
	hppPrefix      = "hpp-"
)

// Overridable so tests do not write to /etc.
var (
	// name:id
	projidFile = "/etc/projid"
	// id:dir
	projectsFile = "/etc/projects"
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

// _IOR('X', 31, struct fsxattr) on 64-bit Linux. Not in this vendored x/sys.
const fsIOCGetXattr = 0x801C581F

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

// removeProjectId forgets one volume in the three bookkeeping files.
// It does not clear the kernel quota; it only removes our records so the
// project ID and the promised bytes can be reused.
//
// Called while the volume directory still exists, so we know the pool path.
// TODO: rewrite for new inode struct since that's the SOT
func removeProjectId(volPath, volID string) error {
	// .hpp-quotas lives next to the PVC dirs, in the pool (parent of volPath).
	quotaFile := filepath.Join(filepath.Dir(volPath), projQuotasFile)

	unlock, err := lockProjectMaps()
	if err != nil {
		return err
	}
	defer unlock()

	projName := hppPrefix + volID // "hpp-pvc-abc"

	// /etc/projid lines are "hpp-pvc-abc:2147483647".
	// Drop the line whose name is this volume.
	// Also match a swapped "2147483647:hpp-pvc-abc" line from an older write.
	if err := rewriteFiltered(projidFile, func(line string) bool {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			return false
		}
		return name == projName || strings.TrimSpace(rest) == projName
	}); err != nil {
		return err
	}

	// /etc/projects lines are "2147483647:/var/hpvolumes/pvc-abc".
	// Drop the line whose path is this volume directory.
	if err := rewriteFiltered(projectsFile, func(line string) bool {
		_, path, ok := strings.Cut(line, ":")
		return ok && path == volPath
	}); err != nil {
		return err
	}

	// .hpp-quotas lines are "pvc-abc:2147483647:1073741824".
	// Drop the line whose first field is this volume id.
	return rewriteFiltered(quotaFile, func(line string) bool {
		parts := strings.Split(line, ":")
		return len(parts) == 3 && parts[0] == volID
	})
}

// assignProjectId records a new project ID for this volume, or returns the
// one it already has if CreateVolume is retried.
// It also refuses the create when the pool has already promised more bytes
// than the disk can hold.
// TODO: rewrite for new inode struct
func assignProjectId(volPath string, volID string, capacityBytes, poolCapacity int64) (uint32, error) {
	quotaFile := filepath.Join(filepath.Dir(volPath), projQuotasFile)

	unlock, err := lockProjectMaps()
	if err != nil {
		return 0, err
	}
	defer unlock()

	used, byPath, err := parseProjectMaps()
	if err != nil {
		return 0, err
	}

	projName := hppPrefix + volID

	var id uint32
	if id, ok := byPath[volPath]; ok {
		return id, nil // CSI retry
	}

	committed, err := parseCommitted(quotaFile)
	if err != nil {
		return 0, err
	}
	if committed+capacityBytes > poolCapacity {
		return 0, fmt.Errorf("insufficient pool capacity: committed %d + requested %d > pool %d",
			committed, capacityBytes, poolCapacity)
	}

	id, err = generateProjectId(used)
	if err != nil {
		return 0, err
	}

	if err := appendProjid(projName, id); err != nil {
		return 0, err
	}

	if err := appendProjects(id, volPath); err != nil {
		return 0, err
	}

	if err := appendQuota(quotaFile, volID, id, capacityBytes); err != nil {
		return 0, err
	}

	return id, nil
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
