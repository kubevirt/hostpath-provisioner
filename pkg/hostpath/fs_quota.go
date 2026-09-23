package hostpath

import (
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	projQuotasFile = ".hpp-quotas"
	hppPrefix      = "hpp-"
)

// Overridable so tests do not write to /etc.
var (
	projidFile   = "/etc/projid"
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

// getFilesystemType returns the various types of file systems Linux uses
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

// generateProjectId will generate a guaranteed unique project id
func generateProjectId(used map[uint32]struct{}) (uint32, error) {
	for id := uint32(maxProjectID); id >= minProjectID; id-- {
		if _, exists := used[id]; !exists {
			return id, nil
		}
		if id == minProjectID {
			break
		}
	}
	return 0, fmt.Errorf("no free project ids in range [%d, %d]", minProjectID, maxProjectID)
}

func parseProjectMaps() (used map[uint32]struct{}, byPath map[string]uint32, err error) {
	used = make(map[uint32]struct{})
	byPath = make(map[string]uint32)

	projidLines, err := readLines(projidFile)
	if err != nil {
		return nil, nil, err
	}
	for _, line := range projidLines {
		// name:id
		name, idStr, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32)
		if err != nil {
			continue
		}
		used[uint32(id)] = struct{}{}
		_ = name
	}

	projectLines, err := readLines(projectsFile)
	if err != nil {
		return nil, nil, err
	}
	for _, line := range projectLines {
		// id:pathname
		idStr, path, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 32)
		if err != nil {
			continue
		}
		used[uint32(id)] = struct{}{}
		byPath[path] = uint32(id)
	}

	return used, byPath, nil
}

func appendProjid(name string, id uint32) error {
	return appendLine(projidFile, fmt.Sprintf("%s:%d\n", name, id))
}

func appendProjects(id uint32, volPath string) error {
	return appendLine(projectsFile, fmt.Sprintf("%d:%s\n", id, volPath))
}

func appendQuota(quotaFile, volID string, id uint32, capacityBytes int64) error {
	return appendLine(quotaFile, fmt.Sprintf("%s:%d:%d\n", volID, id, capacityBytes))
}

func parseCommitted(quotaFile string) (int64, error) {
	lines, err := readLines(quotaFile)
	if err != nil {
		return 0, err
	}
	var committed int64
	for _, line := range lines {
		parts := strings.Split(line, ":")
		if len(parts) != 3 {
			continue
		}
		size, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil {
			continue
		}
		committed += size
	}
	return committed, nil
}

func readLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines, nil
}

func appendLine(path, line string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line)
	return err
}

// rewriteFiltered rewrites path, dropping lines for which drop(trimmed) is true.
// Comments and blank lines are kept. Missing files are a no-op. If nothing is
// dropped the file is left untouched.
func rewriteFiltered(path string, drop func(trimmed string) bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	raw := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	dropped := false
	kept := make([]string, 0, len(raw))
	for _, line := range raw {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") && drop(trimmed) {
			dropped = true
			continue
		}
		kept = append(kept, line)
	}
	if !dropped {
		return nil
	}

	out := strings.Join(kept, "\n")
	if len(kept) > 0 {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0644)
}

// removeProjectId drops this volume's lines from /etc/projid, /etc/projects,
// and {pool}/.hpp-quotas. Linear in the number of entries; CSI delete is idempotent
// if the lines are already gone.
func removeProjectId(volPath, volID string) error {
	quotaFile := filepath.Join(filepath.Dir(volPath), projQuotasFile)

	unlock, err := lockProjectMaps()
	if err != nil {
		return err
	}
	defer unlock()

	projName := hppPrefix + volID

	if err := rewriteFiltered(projidFile, func(line string) bool {
		name, rest, ok := strings.Cut(line, ":")
		if !ok {
			return false
		}
		return name == projName || strings.TrimSpace(rest) == projName
	}); err != nil {
		return err
	}

	if err := rewriteFiltered(projectsFile, func(line string) bool {
		_, path, ok := strings.Cut(line, ":")
		return ok && path == volPath
	}); err != nil {
		return err
	}

	return rewriteFiltered(quotaFile, func(line string) bool {
		parts := strings.Split(line, ":")
		return len(parts) == 3 && parts[0] == volID
	})
}

func lockProjectMaps() (func(), error) {
	f, err := os.OpenFile(projectsFile, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		_ = f.Close()
	}, nil
}

// assignProjectId will assign a unique project id
// at /etc/projects and /etc/projid
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

// fileSystemMount finds the root point of the mount
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
