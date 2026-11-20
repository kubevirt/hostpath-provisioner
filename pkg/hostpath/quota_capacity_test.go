package hostpath

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func TestQuotaPoolCapacityCountsRequestedSize(t *testing.T) {
	const mi = int64(1024 * 1024)
	tests := []struct {
		name                                      string
		free, used, committed, wantFree, wantUsed int64
		disabled, root                            bool
	}{
		{name: "pool without PVCs has its entire capacity unreserved", free: 18, used: 2, wantFree: 20},
		{name: "empty 10Mi PVC reserves 10Mi", free: 20, committed: 10, wantFree: 10, wantUsed: 10},
		{name: "writes do not consume reservation twice", free: 18, used: 2, committed: 10, wantFree: 10, wantUsed: 10},
		{name: "full PVC", free: 10, used: 10, committed: 10, wantFree: 10, wantUsed: 10},
		{name: "two PVCs exhaust 20Mi", free: 20, committed: 20, wantUsed: 20},
		{name: "available depends on quotas rather than physical usage", free: 8, used: 12, committed: 10, wantFree: 10, wantUsed: 10},
		{name: "overcommitted pool never advertises negative space", free: 5, used: 15, committed: 30, wantUsed: 30},
		{name: "quota disabled retains filesystem stats", free: 18, used: 2, disabled: true, wantFree: 18, wantUsed: 2},
		{name: "root pool retains filesystem stats", free: 18, used: 2, root: true, wantFree: 18, wantUsed: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldStats, oldQuota, oldRoot := getPVStatsFunc, getCommittedQuotaBytes, poolOnRootDisk
			t.Cleanup(func() { getPVStatsFunc, getCommittedQuotaBytes, poolOnRootDisk = oldStats, oldQuota, oldRoot })
			getPVStatsFunc = func(string) (int64, int64, int64, int64, int64, int64, error) {
				return tt.free * mi, 20 * mi, tt.used * mi, 100, 90, 10, nil
			}
			poolOnRootDisk = func(string) bool { return tt.root }
			getCommittedQuotaBytes = func(string) (int64, error) {
				if tt.disabled || tt.root {
					t.Fatal("should not query quotas")
				}
				return tt.committed * mi, nil
			}
			free, capacity, used, inodes, inodesFree, inodesUsed, err := getPoolStats("/pool", !tt.disabled)
			if err != nil {
				t.Fatal(err)
			}
			if free != tt.wantFree*mi || used != tt.wantUsed*mi || capacity != 20*mi {
				t.Fatalf("stats = (%d, %d, %d), want (%d, %d, %d)", free, capacity, used, tt.wantFree*mi, 20*mi, tt.wantUsed*mi)
			}
			if inodes != 100 || inodesFree != 90 || inodesUsed != 10 {
				t.Fatal("inode stats changed")
			}
			controller := createControllerServer(t.TempDir())
			req := &csi.GetCapacityRequest{Parameters: map[string]string{enforceQuotaParam: "true"}}
			if tt.disabled {
				req.Parameters = nil
			}
			resp, err := controller.GetCapacity(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			wantMax := tt.wantFree * mi
			wantCapacity := tt.wantFree * mi
			if tt.disabled {
				wantMax = 20 * mi
			}
			if resp.AvailableCapacity != wantCapacity || resp.MaximumVolumeSize.Value != wantMax {
				t.Fatalf("unexpected advertised capacity: %v", resp)
			}
		})
	}
}

func TestQuotaPoolCapacityQueryError(t *testing.T) {
	oldStats, oldQuota, oldRoot := getPVStatsFunc, getCommittedQuotaBytes, poolOnRootDisk
	t.Cleanup(func() { getPVStatsFunc, getCommittedQuotaBytes, poolOnRootDisk = oldStats, oldQuota, oldRoot })
	getPVStatsFunc = func(string) (int64, int64, int64, int64, int64, int64, error) { return 20, 20, 0, 0, 0, 0, nil }
	poolOnRootDisk = func(string) bool { return false }
	getCommittedQuotaBytes = func(string) (int64, error) { return 0, fmt.Errorf("quota unavailable") }
	controller := createControllerServer(t.TempDir())
	resp, err := controller.GetCapacity(context.Background(), &csi.GetCapacityRequest{Parameters: map[string]string{enforceQuotaParam: "true"}})
	if err == nil || resp != nil {
		t.Fatalf("capacity must fail when reservations cannot be read: %v, %v", resp, err)
	}
}

func TestQuotaReportsConvertKiBToBytes(t *testing.T) {
	for _, fsType := range []string{ext4, xfs} {
		t.Run(fsType, func(t *testing.T) {
			dir := t.TempDir()
			binary := quotaBin
			if fsType == xfs {
				binary = xfsQuotaBin
			}
			script := "#!/bin/sh\nprintf '1001 2048 0 10240 -\\n'\n"
			if err := os.WriteFile(filepath.Join(dir, binary), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
			used, hard, err := queryProjectQuotaBytes(1001, "/pool", fsType)
			if err != nil {
				t.Fatal(err)
			}
			if used != 2*1024*1024 || hard != 10*1024*1024 {
				t.Fatalf("quota bytes = (%d, %d), want 2Mi used and 10Mi hard limit", used, hard)
			}
		})
	}
}
