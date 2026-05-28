//go:build llm_generated_opus47

package disk_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/disk"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const filesystemsFixture = `nodev	sysfs
nodev	tmpfs
nodev	proc
	ext4
	btrfs
	vfat
	squashfs
`

const mountsFixture = `/dev/sda1 / ext4 rw,relatime 0 0
/dev/sda2 /home ext4 rw,noatime 0 0
tmpfs /run tmpfs rw,nosuid 0 0
proc /proc proc rw 0 0
/dev/sdb1 none swap defaults 0 0
`

const sda1Stat = "100 0 1000 0 50 0 500 0 0 200 0\n"
const sda2Stat = "200 0 4000 0 100 0 2000 0 0 300 0\n"

func TestSample_BasicMounts(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
		"sda2": sda2Stat,
	})

	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
	})

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(snap.Mounts), 4, "should include /, /home, /run, /proc")

	byMnt := mountsByMountPoint(snap.Mounts)
	root1 := byMnt["/"]
	assert.Equal(t, "/dev/sda1", root1.Device)
	assert.Equal(t, "ext4", root1.FSType)
	assert.Equal(t, "sda1", root1.BlockName)
	assert.True(t, root1.Real)
	assert.Equal(t, uint64(1<<20), root1.Capacity.TotalBytes)

	tmpfs := byMnt["/run"]
	assert.False(t, tmpfs.Real, "tmpfs is nodev → not real")
	assert.Empty(t, tmpfs.BlockName, "tmpfs has no block device")
}

func TestSample_PhysicalOnly_FiltersPseudo(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
		"sda2": sda2Stat,
	})
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
		PhysicalOnly:   true,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, m := range snap.Mounts {
		assert.True(t, m.Real, "PhysicalOnly should drop pseudo fs %q", m.FSType)
	}
}

func TestSample_SkipSwap(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
	})
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
		SkipSwap:       true,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, m := range snap.Mounts {
		assert.NotEqual(t, "swap", m.FSType)
	}
}

func TestSample_FirstCallZeroRates(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
		"sda2": sda2Stat,
	})
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	for _, bd := range snap.BlockDevices {
		assert.Equal(t, uint64(0), bd.ReadBytesPerSec)
		assert.Equal(t, uint64(0), bd.WriteBytesPerSec)
		assert.Equal(t, uint8(0), bd.BusyPercent)
	}
}

func TestSample_SecondCallRatesComputed(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
	})

	now := time.Unix(0, 0)
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
		NowFunc:        func() time.Time { return now },
	})
	_, err := c.Sample(context.Background())
	require.NoError(t, err)

	// Advance: +1 second, sectors_read +200, sectors_written +100, io_ticks +500ms (busy=50%).
	now = now.Add(time.Second)
	rewriteBlockStat(t, root, "sda1", "100 0 1200 0 50 0 600 0 0 700 0\n")

	snap, err := c.Sample(context.Background())
	require.NoError(t, err)

	require.Len(t, snap.BlockDevices, 1)
	bd := snap.BlockDevices[0]
	assert.Equal(t, "sda1", bd.Name)
	// 200 sectors * 512 = 102400 B/s.
	assert.Equal(t, uint64(102400), bd.ReadBytesPerSec)
	// 100 sectors * 512 = 51200 B/s.
	assert.Equal(t, uint64(51200), bd.WriteBytesPerSec)
	// io_ticks delta 500ms / 1000ms wall = 50%.
	assert.Equal(t, uint8(50), bd.BusyPercent)
}

func TestSample_BlockDevices_AreUnique(t *testing.T) {
	// Same /dev/sda1 mounted twice (e.g. bind mount). BlockDevices must
	// deduplicate by name.
	dupMounts := `/dev/sda1 / ext4 rw 0 0
/dev/sda1 /alt ext4 rw 0 0
`
	root := newRootTree(t, dupMounts, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
	})
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     stubStatfs,
		DeviceResolver: deviceBasename,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	require.Len(t, snap.Mounts, 2)
	require.Len(t, snap.BlockDevices, 1, "block device must dedupe across mounts")
}

func TestSample_StatfsError_LeavesEmptyCapacity(t *testing.T) {
	root := newRootTree(t, mountsFixture, filesystemsFixture, map[string]string{
		"sda1": sda1Stat,
		"sda2": sda2Stat,
	})
	c, _ := disk.New(disk.Options{
		Proc:           procfs.New(filepath.Join(root, "proc")),
		Sys:            sysfs.New(filepath.Join(root, "sys")),
		StatfsFunc:     erroringStatfs,
		DeviceResolver: deviceBasename,
	})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err, "statfs failure must not abort the snapshot")
	for _, m := range snap.Mounts {
		assert.Equal(t, uint64(0), m.Capacity.TotalBytes)
	}
}

func TestSample_LiveProc_Smoke(t *testing.T) {
	if _, err := os.Stat("/proc/self/mounts"); err != nil {
		t.Skipf("no /proc/self/mounts: %v", err)
	}
	c, _ := disk.New(disk.Options{})
	snap, err := c.Sample(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, snap.Mounts, "live system should have at least one mount")

	// Plausibility: every mount's capacity is internally consistent.
	var sawTotal bool
	for _, m := range snap.Mounts {
		if m.Capacity.TotalBytes > 0 {
			assert.LessOrEqual(t, m.Capacity.UsedBytes, m.Capacity.TotalBytes,
				"mount %s used > total", m.MountPoint)
			assert.LessOrEqual(t, m.Capacity.FreeBytes, m.Capacity.TotalBytes,
				"mount %s free > total", m.MountPoint)
			assert.LessOrEqual(t, m.Capacity.UsedPercent, float32(100.001),
				"mount %s used%% > 100", m.MountPoint)
		}
		if m.MountPoint == "/" && m.Capacity.TotalBytes > 0 {
			sawTotal = true
		}
	}
	assert.True(t, sawTotal, "live root mount should report non-zero capacity")

	// BlockDevice values are within physical sanity (10 GB/s upper bound
	// covers NVMe Gen5; busy% is 0..100).
	for _, bd := range snap.BlockDevices {
		assert.LessOrEqual(t, bd.BusyPercent, uint8(100), "block %s busy%% > 100", bd.Name)
		assert.Less(t, bd.ReadBytesPerSec, uint64(10<<30), "block %s read rate implausibly high", bd.Name)
		assert.Less(t, bd.WriteBytesPerSec, uint64(10<<30), "block %s write rate implausibly high", bd.Name)
	}
}

func TestSample_ContextCancelled(t *testing.T) {
	c, _ := disk.New(disk.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Sample(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

// --- helpers -------------------------------------------------------------

func newRootTree(t *testing.T, mounts, filesystems string, blockStats map[string]string) (root string) {
	t.Helper()
	root = t.TempDir()
	procDir := filepath.Join(root, "proc")
	require.NoError(t, os.MkdirAll(filepath.Join(procDir, "self"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "self", "mounts"), []byte(mounts), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(procDir, "filesystems"), []byte(filesystems), 0o644))
	for name, stat := range blockStats {
		dir := filepath.Join(root, "sys", "class", "block", name)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "stat"), []byte(stat), 0o644))
	}
	return
}

func rewriteBlockStat(t *testing.T, root, name, stat string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(root, "sys", "class", "block", name, "stat"), []byte(stat), 0o644))
}

func mountsByMountPoint(ms []disk.Mount) map[string]disk.Mount {
	out := map[string]disk.Mount{}
	for _, m := range ms {
		out[m.MountPoint] = m
	}
	return out
}

func stubStatfs(path string) (cap disk.Capacity, err error) {
	cap = disk.Capacity{
		TotalBytes:  1 << 20,
		FreeBytes:   1 << 19,
		UsedBytes:   1 << 19,
		UsedPercent: 50,
	}
	return
}

func erroringStatfs(path string) (cap disk.Capacity, err error) {
	return disk.Capacity{}, os.ErrPermission
}

// deviceBasename is the test [DeviceResolver] — strips /dev/ prefix and
// returns the rest verbatim, so /dev/sda1 → "sda1". Real resolution uses
// EvalSymlinks; tests don't have a real /dev/.
func deviceBasename(device string) (blockName string) {
	if device == "" {
		return ""
	}
	if !filepath.IsAbs(device) {
		return ""
	}
	return filepath.Base(device)
}
