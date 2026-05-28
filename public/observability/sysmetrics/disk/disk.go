//go:build llm_generated_opus47

package disk

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"golang.org/x/sys/unix"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/procfs"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const (
	// SysClassBlockDir is the sysfs-relative root for kernel block devices.
	SysClassBlockDir = "class/block"

	// MountsRel is the procfs-relative path for the live mount table.
	MountsRel = "self/mounts"

	// FilesystemsRel is the procfs-relative path for the kernel's
	// supported-filesystems list, used to classify pseudo vs real fs.
	FilesystemsRel = "filesystems"

	// SectorSize is the kernel's reported sector size for /sys/block stat
	// counters. Always 512 regardless of the underlying device's logical
	// or physical block size — see Documentation/admin-guide/iostats.rst.
	SectorSize uint64 = 512
)

// Capacity is one filesystem's space accounting.
type Capacity struct {
	TotalBytes  uint64
	FreeBytes   uint64 // available to non-root (matches statvfs f_bavail)
	UsedBytes   uint64
	UsedPercent float32
}

// Mount is one /proc/self/mounts entry.
type Mount struct {
	Device     string // raw device field, e.g. "/dev/sda1" or "tmpfs" or "none"
	MountPoint string // e.g. "/", "/home"
	FSType     string // e.g. "ext4", "tmpfs"
	Options    string // raw options string from the mount entry
	BlockName  string // basename of canonical /dev path, "" when not a block device
	Real       bool   // true when fstype is in /proc/filesystems without "nodev" prefix
	Capacity   Capacity
}

// BlockDevice is one /sys/class/block/{name}/stat reading with derived
// per-second rates and busy percent.
type BlockDevice struct {
	Name             string // "sda1", "nvme0n1p1", "dm-2"
	ReadBytesPerSec  uint64
	WriteBytesPerSec uint64
	BusyPercent      uint8 // io_ticks delta / elapsed wall-time, clamped 0..100
}

// Snapshot is a single sample.
type Snapshot struct {
	SampledAtUnixMs int64
	Mounts          []Mount
	BlockDevices    []BlockDevice
}

// CollectorI is the public surface a disk sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
}

// StatfsFunc returns the capacity of the filesystem rooted at path. The
// default implementation calls [unix.Statfs]; tests inject a synthetic
// implementation.
type StatfsFunc func(path string) (cap Capacity, err error)

// DeviceResolver maps a mount entry's device field to a /sys/class/block
// entry name. The default implementation uses [filepath.EvalSymlinks]
// when the path exists, falling back to [filepath.Base]; tests can
// override to return synthetic block names.
type DeviceResolver func(device string) (blockName string)

// Options configures a [Collector].
type Options struct {
	Proc *procfs.Reader
	Sys  *sysfs.Reader

	// StatfsFunc, when non-nil, overrides the live statvfs path. Default:
	// [unix.Statfs] against the absolute mount-point string.
	StatfsFunc StatfsFunc

	// DeviceResolver, when non-nil, overrides /dev → /sys/class/block
	// name resolution. Default: EvalSymlinks then basename.
	DeviceResolver DeviceResolver

	// NowFunc, when non-nil, overrides the wall+monotonic clock.
	NowFunc func() time.Time

	// PhysicalOnly drops mounts whose fstype is marked "nodev" in
	// /proc/filesystems. Default false (return everything).
	PhysicalOnly bool

	// SkipSwap drops mount entries whose fstype is "swap".
	SkipSwap bool
}

// Collector samples the live mount table and per-block-device I/O.
type Collector struct {
	proc           *procfs.Reader
	sys            *sysfs.Reader
	statfsFn       StatfsFunc
	resolveDev     DeviceResolver
	nowFn          func() time.Time
	physicalOnly   bool
	skipSwap       bool
	realFSCache    map[string]bool
	realFSCacheTry bool

	prev map[string]ioTick
}

type ioTick struct {
	at             time.Time
	sectorsRead    uint64
	sectorsWritten uint64
	ioTicksMs      uint64
}

// New returns a disk Collector configured by opts. The returned error
// is always nil today; the signature reserves the slot for forward-
// compatibility.
func New(opts Options) (inst *Collector, err error) {
	if opts.Proc == nil {
		opts.Proc = procfs.New("")
	}
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	if opts.StatfsFunc == nil {
		opts.StatfsFunc = realStatfs
	}
	if opts.DeviceResolver == nil {
		opts.DeviceResolver = realDeviceResolver
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Collector{
		proc:         opts.Proc,
		sys:          opts.Sys,
		statfsFn:     opts.StatfsFunc,
		resolveDev:   opts.DeviceResolver,
		nowFn:        opts.NowFunc,
		physicalOnly: opts.PhysicalOnly,
		skipSwap:     opts.SkipSwap,
		prev:         map[string]ioTick{},
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads /proc/self/mounts, calls statvfs per mountpoint, and
// reads /sys/class/block/*/stat for unique block devices.
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	now := inst.nowFn()
	snap.SampledAtUnixMs = now.UnixMilli()

	realFS := inst.realFilesystems()

	rawMounts, rerr := inst.proc.ReadFile(MountsRel)
	if rerr != nil {
		err = eh.Errorf("read /proc/self/mounts: %w", rerr)
		return
	}

	seenBlocks := map[string]struct{}{}
	for line := range procfs.IterLines(rawMounts) {
		var f0, f1, f2, f3 []byte
		idx := 0
		for f := range procfs.IterFields(line) {
			switch idx {
			case 0:
				f0 = f
			case 1:
				f1 = f
			case 2:
				f2 = f
			case 3:
				f3 = f
			}
			idx++
			if idx == 4 {
				break
			}
		}
		if idx < 3 {
			continue
		}
		fstype := string(f2)
		if inst.skipSwap && fstype == "swap" {
			continue
		}
		isReal := realFS[fstype]
		if inst.physicalOnly && !isReal {
			continue
		}

		m := Mount{
			Device:     string(f0),
			MountPoint: string(f1),
			FSType:     fstype,
			Options:    string(f3),
			Real:       isReal,
		}

		if cap, cerr := inst.statfsFn(m.MountPoint); cerr == nil {
			m.Capacity = cap
		}

		blockName := inst.resolveDev(m.Device)
		if blockName != "" && hasBlockStat(inst.sys, blockName) {
			m.BlockName = blockName
			if _, dup := seenBlocks[blockName]; !dup {
				seenBlocks[blockName] = struct{}{}
				if bd, ok := inst.sampleBlock(now, blockName); ok {
					snap.BlockDevices = append(snap.BlockDevices, bd)
				}
			}
		}
		snap.Mounts = append(snap.Mounts, m)
	}

	// Drop disappeared block devices from prior-tick state.
	for k := range inst.prev {
		if _, ok := seenBlocks[k]; !ok {
			delete(inst.prev, k)
		}
	}

	slices.SortFunc(snap.BlockDevices, func(a, b BlockDevice) int {
		return strings.Compare(a.Name, b.Name)
	})
	return
}

func (inst *Collector) realFilesystems() (out map[string]bool) {
	if inst.realFSCacheTry {
		return inst.realFSCache
	}
	inst.realFSCacheTry = true
	raw, err := inst.proc.ReadFile(FilesystemsRel)
	if err != nil {
		return nil
	}
	out = map[string]bool{}
	for line := range procfs.IterLines(raw) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if bytes.HasPrefix(trimmed, []byte("nodev")) {
			continue
		}
		var fstype string
		for f := range procfs.IterFields(trimmed) {
			fstype = string(f)
			break
		}
		if fstype != "" {
			out[fstype] = true
		}
	}
	inst.realFSCache = out
	return out
}

func (inst *Collector) sampleBlock(now time.Time, name string) (bd BlockDevice, ok bool) {
	raw, err := inst.sys.ReadString(filepath.Join(SysClassBlockDir, name, "stat"))
	if err != nil {
		return
	}
	fields := strings.Fields(raw)
	if len(fields) < 10 {
		return
	}
	sectorsRead, err1 := strconv.ParseUint(fields[2], 10, 64)
	sectorsWritten, err2 := strconv.ParseUint(fields[6], 10, 64)
	ioTicksMs, err3 := strconv.ParseUint(fields[9], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return
	}

	bd.Name = name

	if prev, primed := inst.prev[name]; primed {
		elapsed := now.Sub(prev.at)
		if elapsed > 0 {
			secs := elapsed.Seconds()
			elapsedMs := elapsed.Milliseconds()
			if sectorsRead > prev.sectorsRead {
				bd.ReadBytesPerSec = uint64(float64((sectorsRead-prev.sectorsRead)*SectorSize) / secs)
			}
			if sectorsWritten > prev.sectorsWritten {
				bd.WriteBytesPerSec = uint64(float64((sectorsWritten-prev.sectorsWritten)*SectorSize) / secs)
			}
			if ioTicksMs > prev.ioTicksMs && elapsedMs > 0 {
				busy := int64(ioTicksMs-prev.ioTicksMs) * 100 / elapsedMs
				if busy > 100 {
					busy = 100
				}
				if busy < 0 {
					busy = 0
				}
				bd.BusyPercent = uint8(busy)
			}
		}
	}

	inst.prev[name] = ioTick{
		at:             now,
		sectorsRead:    sectorsRead,
		sectorsWritten: sectorsWritten,
		ioTicksMs:      ioTicksMs,
	}
	ok = true
	return
}

func hasBlockStat(sys *sysfs.Reader, name string) (yes bool) {
	_, err := sys.ReadFile(filepath.Join(SysClassBlockDir, name, "stat"))
	if err != nil {
		return errors.Is(err, fs.ErrPermission)
	}
	return true
}

// realStatfs is the production [StatfsFunc].
func realStatfs(path string) (cap Capacity, err error) {
	var s unix.Statfs_t
	err = unix.Statfs(path, &s)
	if err != nil {
		err = eb.Build().Str("path", path).Errorf("statfs: %w", err)
		return
	}
	cap = computeCapacity(uint64(s.Bsize), s.Blocks, s.Bavail)
	return
}

// realDeviceResolver is the production [DeviceResolver].
func realDeviceResolver(device string) (blockName string) {
	if device == "" {
		return ""
	}
	if !strings.HasPrefix(device, "/") {
		return ""
	}
	if real, err := filepath.EvalSymlinks(device); err == nil {
		return filepath.Base(real)
	}
	return filepath.Base(device)
}

func computeCapacity(blockSize, totalBlocks, freeBlocks uint64) (cap Capacity) {
	if blockSize == 0 {
		blockSize = 4096
	}
	cap.TotalBytes = totalBlocks * blockSize
	cap.FreeBytes = freeBlocks * blockSize
	if cap.TotalBytes > cap.FreeBytes {
		cap.UsedBytes = cap.TotalBytes - cap.FreeBytes
	}
	if cap.TotalBytes > 0 {
		cap.UsedPercent = float32(cap.UsedBytes) * 100 / float32(cap.TotalBytes)
	}
	return
}
