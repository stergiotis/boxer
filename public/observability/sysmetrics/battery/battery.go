package battery

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

// PowerSupplyDir is the sysfs-relative root for kernel power supplies.
const PowerSupplyDir = "class/power_supply"

// StateE classifies a battery's charge/discharge state.
type StateE uint8

const (
	StateUnknown StateE = iota
	StateCharging
	StateDischarging
	StateFull
	StateNotCharging
)

func (s StateE) String() (out string) {
	switch s {
	case StateCharging:
		return "charging"
	case StateDischarging:
		return "discharging"
	case StateFull:
		return "full"
	case StateNotCharging:
		return "not_charging"
	default:
		return "unknown"
	}
}

// AllStates lists every defined [StateE] value.
var AllStates = []StateE{StateUnknown, StateCharging, StateDischarging, StateFull, StateNotCharging}

// BatteryStatus is the per-battery sample.
type BatteryStatus struct {
	Name string // sysfs entry name, e.g. "BAT0"
	Type string // kernel-reported, "Battery" or "UPS"

	// Percent is the charge level [0..100]. Resolved from `capacity` when
	// present, otherwise derived from energy_now/energy_full or
	// charge_now/charge_full.
	Percent uint8

	// State is the kernel-reported charge state, normalized.
	State StateE

	// PowerWatts is the instantaneous power draw or fill rate. 0 when no
	// power_now / current+voltage path is exposed by the kernel for this
	// battery.
	PowerWatts float32

	// SecondsToFull is positive when charging; -1 when unknown or not
	// charging.
	SecondsToFull int64

	// SecondsToEmpty is positive when discharging; -1 when unknown or
	// charging.
	SecondsToEmpty int64
}

// ACAdapter is one Mains-type power supply.
type ACAdapter struct {
	Name   string // "AC", "ACAD", "ADP1"
	Online bool
}

// Snapshot is a single sample of all power supplies.
type Snapshot struct {
	SampledAtUnixMs int64
	Batteries       []BatteryStatus
	ACAdapters      []ACAdapter
}

// CollectorI is the public surface a battery sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
}

// Options configures a [Collector].
type Options struct {
	Sys     *sysfs.Reader
	NowFunc func() time.Time
}

// Collector samples /sys/class/power_supply.
type Collector struct {
	sys   *sysfs.Reader
	nowFn func() time.Time
}

// New returns a battery Collector configured by opts. The returned
// error is always nil today; the signature reserves the slot for
// forward-compatibility.
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

// Sample enumerates power_supply entries and returns batteries +
// AC adapters. An absent /sys/class/power_supply directory yields an
// empty Snapshot without error (low-end / virtualized hardware).
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}
	snap.SampledAtUnixMs = inst.nowFn().UnixMilli()

	names, err := inst.sys.ListDir(PowerSupplyDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = nil
		}
		return
	}

	for _, name := range names {
		dev := filepath.Join(PowerSupplyDir, name)
		typ, terr := inst.sys.ReadString(filepath.Join(dev, "type"))
		if terr != nil {
			continue
		}
		switch typ {
		case "Mains":
			snap.ACAdapters = append(snap.ACAdapters, inst.readAC(name, dev))
		case "Battery", "UPS":
			b, ok := inst.readBattery(name, typ, dev)
			if ok {
				snap.Batteries = append(snap.Batteries, b)
			}
		}
	}

	// Stable ordering (deterministic snapshots make consumer code easier).
	slices.SortFunc(snap.Batteries, func(a, b BatteryStatus) int {
		return strings.Compare(a.Name, b.Name)
	})
	slices.SortFunc(snap.ACAdapters, func(a, b ACAdapter) int {
		return strings.Compare(a.Name, b.Name)
	})
	return
}

func (inst *Collector) readAC(name, dev string) (ac ACAdapter) {
	ac.Name = name
	if v, err := inst.sys.ReadString(filepath.Join(dev, "online")); err == nil {
		ac.Online = v == "1"
	}
	return
}

// readBattery returns ok=false when the entry is not a usable battery
// (present=0, no readable percent source, etc.).
func (inst *Collector) readBattery(name, typ, dev string) (b BatteryStatus, ok bool) {
	// Some entries have no `present` file (UPS sometimes); when present
	// exists it must read "1".
	if v, err := inst.sys.ReadString(filepath.Join(dev, "present")); err == nil {
		if v != "1" {
			return
		}
	}

	b.Name = name
	b.Type = typ
	b.SecondsToFull = -1
	b.SecondsToEmpty = -1

	pct, gotPct := inst.readPercent(dev)
	if !gotPct {
		return
	}
	b.Percent = pct

	b.State = inst.readState(dev, pct)
	b.PowerWatts = inst.readPower(dev)

	if b.State == StateDischarging {
		if s, gotS := inst.readSecondsRemaining(dev, true); gotS {
			b.SecondsToEmpty = s
		}
	} else if b.State == StateCharging {
		if s, gotS := inst.readSecondsRemaining(dev, false); gotS {
			b.SecondsToFull = s
		}
	}

	ok = true
	return
}

// readPercent prefers the kernel-provided `capacity`, falls back to
// energy_now/energy_full, then charge_now/charge_full.
//
// provenance: btop src/linux/btop_collect.cpp:902-922.
func (inst *Collector) readPercent(dev string) (pct uint8, ok bool) {
	if v, err := inst.sys.ReadString(filepath.Join(dev, "capacity")); err == nil {
		if n, perr := strconv.ParseInt(v, 10, 32); perr == nil && n >= 0 {
			return clamp01(n), true
		}
	}
	for _, pair := range [...][2]string{{"energy_now", "energy_full"}, {"charge_now", "charge_full"}} {
		num, nerr := inst.sys.ReadString(filepath.Join(dev, pair[0]))
		den, derr := inst.sys.ReadString(filepath.Join(dev, pair[1]))
		if nerr != nil || derr != nil {
			continue
		}
		nN, np := strconv.ParseFloat(num, 64)
		dN, dp := strconv.ParseFloat(den, 64)
		if np != nil || dp != nil || dN <= 0 {
			continue
		}
		return clamp01(int64(math.Round(100 * nN / dN))), true
	}
	return
}

func (inst *Collector) readState(dev string, percent uint8) (state StateE) {
	s, err := inst.sys.ReadString(filepath.Join(dev, "status"))
	if err == nil {
		switch strings.ToLower(s) {
		case "charging":
			return StateCharging
		case "discharging":
			return StateDischarging
		case "full":
			return StateFull
		case "not charging":
			return StateNotCharging
		case "unknown":
			// fall through to AC-adapter inference
		default:
			return StateUnknown
		}
	}
	// Fallback: any sibling Mains adapter that is online?
	if siblingMainsOnline(inst.sys) {
		if percent >= 100 {
			return StateFull
		}
		return StateCharging
	}
	return StateDischarging
}

func (inst *Collector) readPower(dev string) (watts float32) {
	if v, err := inst.sys.ReadString(filepath.Join(dev, "power_now")); err == nil {
		if n, perr := strconv.ParseFloat(v, 64); perr == nil {
			// power_now is in microwatts.
			return float32(math.Abs(n) / 1_000_000)
		}
	}
	curr, cerr := inst.sys.ReadString(filepath.Join(dev, "current_now"))
	volt, verr := inst.sys.ReadString(filepath.Join(dev, "voltage_now"))
	if cerr != nil || verr != nil {
		return 0
	}
	cN, cp := strconv.ParseFloat(curr, 64)
	vN, vp := strconv.ParseFloat(volt, 64)
	if cp != nil || vp != nil {
		return 0
	}
	// current_now in microamps; voltage_now in microvolts;
	// W = (uA / 1e6) * (uV / 1e6).
	return float32(math.Abs(cN*vN) / 1e12)
}

// readSecondsRemaining returns time-to-empty (toEmpty=true) or time-to-full.
//
// provenance: btop src/linux/btop_collect.cpp:937-984.
func (inst *Collector) readSecondsRemaining(dev string, toEmpty bool) (seconds int64, ok bool) {
	// Prefer the kernel-computed fields when present.
	if toEmpty {
		if v, err := inst.sys.ReadString(filepath.Join(dev, "time_to_empty_now")); err == nil {
			if n, perr := strconv.ParseInt(v, 10, 64); perr == nil && n > 0 {
				return n, true
			}
		}
	} else {
		if v, err := inst.sys.ReadString(filepath.Join(dev, "time_to_full_now")); err == nil {
			if n, perr := strconv.ParseInt(v, 10, 64); perr == nil && n > 0 {
				return n, true
			}
		}
	}
	// Fall back to numerical derivation: seconds = X / power * 3600.
	for _, pair := range [...][3]string{
		{"energy_now", "energy_full", "power_now"},
		{"charge_now", "charge_full", "current_now"},
	} {
		now, nerr := inst.sys.ReadString(filepath.Join(dev, pair[0]))
		full, ferr := inst.sys.ReadString(filepath.Join(dev, pair[1]))
		rate, rerr := inst.sys.ReadString(filepath.Join(dev, pair[2]))
		if nerr != nil || ferr != nil || rerr != nil {
			continue
		}
		nowN, p1 := strconv.ParseFloat(now, 64)
		fullN, p2 := strconv.ParseFloat(full, 64)
		rateN, p3 := strconv.ParseFloat(rate, 64)
		if p1 != nil || p2 != nil || p3 != nil {
			continue
		}
		if math.Abs(rateN) < 1 {
			continue
		}
		var hours float64
		if toEmpty {
			hours = nowN / math.Abs(rateN)
		} else {
			hours = (fullN - nowN) / math.Abs(rateN)
		}
		if hours <= 0 {
			continue
		}
		return int64(math.Round(hours * 3600)), true
	}
	return
}

// siblingMainsOnline scans /sys/class/power_supply for any Mains-type
// supply with online=1. Used only as a fallback when a battery's status
// file is "unknown".
func siblingMainsOnline(sys *sysfs.Reader) (online bool) {
	names, err := sys.ListDir(PowerSupplyDir)
	if err != nil {
		return false
	}
	for _, n := range names {
		typ, terr := sys.ReadString(filepath.Join(PowerSupplyDir, n, "type"))
		if terr != nil || typ != "Mains" {
			continue
		}
		v, oerr := sys.ReadString(filepath.Join(PowerSupplyDir, n, "online"))
		if oerr == nil && v == "1" {
			return true
		}
	}
	return false
}

func clamp01(n int64) (out uint8) {
	switch {
	case n < 0:
		return 0
	case n > 100:
		return 100
	default:
		return uint8(n)
	}
}
