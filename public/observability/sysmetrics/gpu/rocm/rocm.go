//go:build linux && gpu_rocm

package rocm

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const (
	// AMDPCIVendor is the 16-bit PCI vendor id used by every AMD GPU.
	AMDPCIVendor uint16 = 0x1002

	// SysClassDRM is the sysfs-relative class root for DRM cards.
	SysClassDRM = "class/drm"
)

// Device is one AMD GPU's sample.
type Device struct {
	// Card is the /sys/class/drm/card* basename.
	Card string

	// PCIID is the lowercase hex device id, e.g. "0x1586".
	PCIID string

	// Name is the human-readable codename derived from PCIID via the
	// built-in lookup table; falls back to "AMD Graphics" when unknown.
	Name string

	// BusyPercent is amdgpu's gpu_busy_percent — overall device
	// utilization, 0..100.
	BusyPercent uint8

	// MemoryUsedBytes / MemoryTotalBytes are the VRAM accounting from
	// mem_info_vram_used and mem_info_vram_total respectively.
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64

	// TempC is the junction temperature in Celsius, from
	// hwmon/temp1_input (sysfs reports millidegrees).
	TempC float32

	// PowerWatts is the instantaneous power draw, from
	// hwmon/power1_average (sysfs reports microwatts). 0 when not
	// exposed.
	PowerWatts float32

	// GraphicsClockMHz is the current graphics clock in MHz. Preferred
	// source is hwmon/freq1_input (Hz); fallback is the asterisk-marked
	// row of pp_dpm_sclk.
	GraphicsClockMHz uint32
}

// Snapshot is a single sample of every detected AMD GPU.
type Snapshot struct {
	SampledAtUnixMs int64
	Devices         []Device
}

// CollectorI is the public surface an AMD GPU sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
	Close() (err error)
}

// Options configures a [Collector].
type Options struct {
	Sys     *sysfs.Reader
	NowFunc func() time.Time
}

// Collector samples AMD GPU state from sysfs. Zero-value not directly
// usable — call [New].
type Collector struct {
	sys   *sysfs.Reader
	nowFn func() time.Time
}

// New returns a Collector configured by opts. The returned error is
// always nil today; the signature reserves the slot for forward-
// compatibility (e.g. capability probing or ROCm-SMI fallback init).
func New(opts Options) (inst *Collector, err error) {
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	inst = &Collector{sys: opts.Sys, nowFn: opts.NowFunc}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads /sys/class/drm/card*/device/* for every AMD GPU. An
// absent /sys/class/drm yields an empty Snapshot without error
// (matches low-end / virtualized hardware).
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}
	snap.SampledAtUnixMs = inst.nowFn().UnixMilli()

	names, lerr := inst.sys.ListDir(SysClassDRM)
	if lerr != nil {
		if errors.Is(lerr, fs.ErrNotExist) {
			return
		}
		err = lerr
		return
	}

	for _, name := range names {
		if !strings.HasPrefix(name, "card") {
			continue
		}
		// skip render nodes (renderD128) and partition entries (cardN-DP-1)
		if strings.ContainsAny(strings.TrimPrefix(name, "card"), "-D") {
			continue
		}
		d, ok := inst.readOne(name)
		if !ok {
			continue
		}
		snap.Devices = append(snap.Devices, d)
	}

	slices.SortFunc(snap.Devices, func(a, b Device) int {
		return strings.Compare(a.Card, b.Card)
	})
	return
}

// Close is a no-op — the sysfs path holds no resources. Implemented so
// the type can satisfy [io.Closer] for symmetry with the other GPU
// adapters that do hold open fds.
func (inst *Collector) Close() (err error) {
	return nil
}

// readOne returns ok=false when the card is not an AMD GPU or the
// vendor file is unreadable.
func (inst *Collector) readOne(card string) (d Device, ok bool) {
	base := filepath.Join(SysClassDRM, card, "device")

	vendor, verr := inst.sys.ReadString(filepath.Join(base, "vendor"))
	if verr != nil {
		return
	}
	v, perr := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(vendor), "0x"), 16, 16)
	if perr != nil || uint16(v) != AMDPCIVendor {
		return
	}

	pciID, derr := inst.sys.ReadString(filepath.Join(base, "device"))
	if derr != nil {
		return
	}

	d.Card = card
	d.PCIID = strings.ToLower(strings.TrimSpace(pciID))
	d.Name = pciIDName(d.PCIID)

	if val, err := inst.sys.ReadString(filepath.Join(base, "gpu_busy_percent")); err == nil {
		if n, perr := strconv.ParseUint(strings.TrimSpace(val), 10, 8); perr == nil && n <= 100 {
			d.BusyPercent = uint8(n)
		}
	}

	if val, err := inst.sys.ReadString(filepath.Join(base, "mem_info_vram_total")); err == nil {
		d.MemoryTotalBytes, _ = strconv.ParseUint(strings.TrimSpace(val), 10, 64)
	}
	if val, err := inst.sys.ReadString(filepath.Join(base, "mem_info_vram_used")); err == nil {
		d.MemoryUsedBytes, _ = strconv.ParseUint(strings.TrimSpace(val), 10, 64)
	}

	if hwmon, hOk := inst.findHwmon(base); hOk {
		if val, err := inst.sys.ReadString(filepath.Join(hwmon, "temp1_input")); err == nil {
			if n, perr := strconv.ParseInt(strings.TrimSpace(val), 10, 64); perr == nil {
				d.TempC = float32(n) / 1000
			}
		}
		if val, err := inst.sys.ReadString(filepath.Join(hwmon, "power1_average")); err == nil {
			if n, perr := strconv.ParseInt(strings.TrimSpace(val), 10, 64); perr == nil {
				d.PowerWatts = float32(n) / 1_000_000
			}
		}
		if val, err := inst.sys.ReadString(filepath.Join(hwmon, "freq1_input")); err == nil {
			if n, perr := strconv.ParseUint(strings.TrimSpace(val), 10, 64); perr == nil {
				d.GraphicsClockMHz = uint32(n / 1_000_000)
			}
		}
	}

	// Fallback when freq1_input wasn't exposed — parse pp_dpm_sclk.
	if d.GraphicsClockMHz == 0 {
		if val, err := inst.sys.ReadString(filepath.Join(base, "pp_dpm_sclk")); err == nil {
			d.GraphicsClockMHz = parseDpmCurrent(val)
		}
	}

	ok = true
	return
}

// findHwmon returns the first hwmon* entry under <base>/hwmon. amdgpu
// publishes exactly one hwmon child per card on modern kernels; older
// kernels with multiple were never seen on consumer hardware.
func (inst *Collector) findHwmon(base string) (path string, ok bool) {
	dir := filepath.Join(base, "hwmon")
	names, err := inst.sys.ListDir(dir)
	if err != nil {
		return
	}
	for _, n := range names {
		if strings.HasPrefix(n, "hwmon") {
			return filepath.Join(dir, n), true
		}
	}
	return
}

// parseDpmCurrent extracts the asterisk-marked row from pp_dpm_sclk
// content. Format per amdgpu kernel docs:
//
//	0: 600Mhz
//	1: 845Mhz *
//	2: 2900Mhz
//
// Returns 0 when no row is marked or no Mhz token parses.
func parseDpmCurrent(content string) (mhz uint32) {
	for _, line := range strings.Split(content, "\n") {
		if !strings.Contains(line, "*") {
			continue
		}
		for _, f := range strings.Fields(line) {
			t := strings.TrimSuffix(f, "Mhz")
			t = strings.TrimSuffix(t, "MHz")
			t = strings.TrimSuffix(t, "mhz")
			if t == f {
				continue // no Mhz suffix on this token
			}
			if n, err := strconv.ParseUint(t, 10, 32); err == nil {
				return uint32(n)
			}
		}
	}
	return
}
