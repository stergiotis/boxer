//go:build linux && gpu_intel

package intel

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/internal/sysfs"
)

const (
	// IntelPCIVendor is the 16-bit PCI vendor id Intel uses across every
	// integrated and discrete GPU SKU. We enumerate /sys/class/drm/card*
	// and accept those whose device/vendor file matches.
	IntelPCIVendor uint16 = 0x8086

	// SysClassDRM is the sysfs-relative class root for DRM cards.
	SysClassDRM = "class/drm"

	// SysDevicesI915Type is the sysfs-relative file that exposes the
	// i915 PMU's perf_event type id. Absent when the i915 module is not
	// loaded.
	SysDevicesI915Type = "devices/i915/type"
)

// ErrPMUUnavailable is returned by [New] when the i915 PMU cannot be
// opened — i915 not loaded, perf_event_paranoid too strict, or no
// Intel GPUs present. Callers should treat this as a soft failure
// (render an empty GPU section, do not abort the broader Snapshot).
var ErrPMUUnavailable = errors.New("intel i915 PMU not available")

// EngineE classifies an Intel GPU engine the busy counters cover.
type EngineE uint8

const (
	EngineRender       EngineE = iota // class 0
	EngineCopy                        // class 1 (blitter)
	EngineVideo                       // class 2 (decode/encode)
	EngineVideoEnhance                // class 3 (post-processing)
)

func (e EngineE) String() (out string) {
	switch e {
	case EngineRender:
		return "render"
	case EngineCopy:
		return "copy"
	case EngineVideo:
		return "video"
	case EngineVideoEnhance:
		return "video-enhance"
	default:
		return "unknown"
	}
}

// AllEngines lists every defined [EngineE] in PMU class order.
var AllEngines = []EngineE{EngineRender, EngineCopy, EngineVideo, EngineVideoEnhance}

// Device is one Intel GPU's sample.
type Device struct {
	// Card is the /sys/class/drm/card* basename ("card0", "card1", ...).
	Card string

	// PCIID is the lowercase hex device id, e.g. "0x9a49".
	PCIID string

	// Name is the human-readable codename derived from PCIID via the
	// built-in lookup table; falls back to "Intel Graphics" when the
	// device id is unknown to this package's table.
	Name string

	// Per-engine busy percentage [0..100]. 0 on first sample (no prior
	// tick to delta against) and on engines whose perf event failed to
	// open at construction time.
	RenderBusyPercent       uint8
	CopyBusyPercent         uint8
	VideoBusyPercent        uint8
	VideoEnhanceBusyPercent uint8

	// ActualFreqMHz is the average GT frequency over the sample window.
	// 0 when the frequency counter failed to open.
	ActualFreqMHz uint32

	// RequestedFreqMHz is the average software-requested frequency.
	RequestedFreqMHz uint32
}

// Snapshot is a single sample of every detected Intel GPU.
type Snapshot struct {
	SampledAtUnixMs int64
	Devices         []Device
}

// CollectorI is the public surface an Intel GPU sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
	Close() (err error)
}

// CounterOpener constructs a single perf event handle. Production code
// uses [DefaultCounterOpener] (which calls [unix.PerfEventOpen]); tests
// inject deterministic counters.
type CounterOpener func(pmuType uint32, config uint64) (Counter, error)

// Counter abstracts one open perf event. [Read] returns the cumulative
// counter value; [Close] releases the underlying fd.
type Counter interface {
	Read() (value uint64, err error)
	Close() (err error)
}

// Options configures a [Collector].
type Options struct {
	Sys *sysfs.Reader

	// NowFunc, when non-nil, overrides the wall+monotonic clock used to
	// derive elapsed-time deltas between Samples.
	NowFunc func() time.Time

	// Opener, when non-nil, overrides the perf_event_open path. Tests
	// supply a stub that returns deterministic Counter values; production
	// callers leave this nil to use [DefaultCounterOpener].
	Opener CounterOpener
}

// Collector samples Intel GPU state via the i915 PMU. Construction opens
// one perf event per (device, engine) tuple and per-device frequency
// counters; [Close] releases all of them.
type Collector struct {
	sys     *sysfs.Reader
	nowFn   func() time.Time
	pmuType uint32
	devices []*deviceState
}

type deviceState struct {
	card  string
	pciID string
	name  string

	counters map[EngineE]Counter // engine busy counters; missing ⇒ engine unsupported
	freqAct  Counter
	freqReq  Counter

	primed bool
	prevAt time.Time
	prev   map[EngineE]uint64
	prevFA uint64
	prevFR uint64
}

// New constructs a Collector. The function probes the host for an i915
// PMU and opens perf events; it returns [ErrPMUUnavailable] (wrapped
// for diagnostics) when no usable GPU is found, so callers can branch
// on `errors.Is(err, intel.ErrPMUUnavailable)` without inspecting the
// underlying syscall error.
func New(opts Options) (inst *Collector, err error) {
	if opts.Sys == nil {
		opts.Sys = sysfs.New("")
	}
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	opener := opts.Opener
	if opener == nil {
		opener = DefaultCounterOpener
	}

	pmuTypeStr, terr := opts.Sys.ReadString(SysDevicesI915Type)
	if terr != nil {
		err = fmt.Errorf("%w: read %s: %w", ErrPMUUnavailable, SysDevicesI915Type, terr)
		return
	}
	pmuType, perr := strconv.ParseUint(strings.TrimSpace(pmuTypeStr), 10, 32)
	if perr != nil {
		err = eb.Build().Str("value", pmuTypeStr).Errorf("parse i915 PMU type: %w", perr)
		return
	}

	devices, derr := discoverIntelDevices(opts.Sys)
	if derr != nil {
		err = eh.Errorf("discover Intel DRM devices: %w", derr)
		return
	}
	if len(devices) == 0 {
		err = fmt.Errorf("%w: no Intel DRM cards under %s", ErrPMUUnavailable, SysClassDRM)
		return
	}

	inst = &Collector{
		sys:     opts.Sys,
		nowFn:   opts.NowFunc,
		pmuType: uint32(pmuType),
		devices: make([]*deviceState, 0, len(devices)),
	}

	var openedAny bool
	for _, dev := range devices {
		ds := &deviceState{
			card:     dev.card,
			pciID:    dev.pciID,
			name:     dev.name,
			counters: map[EngineE]Counter{},
			prev:     map[EngineE]uint64{},
		}

		for _, e := range AllEngines {
			cfg := configEngineBusy(uint64(e), 0)
			c, copErr := opener(uint32(pmuType), cfg)
			if copErr != nil {
				continue
			}
			ds.counters[e] = c
			openedAny = true
		}
		if c, copErr := opener(uint32(pmuType), configFreqActual); copErr == nil {
			ds.freqAct = c
			openedAny = true
		}
		if c, copErr := opener(uint32(pmuType), configFreqRequested); copErr == nil {
			ds.freqReq = c
			openedAny = true
		}

		if len(ds.counters) == 0 && ds.freqAct == nil && ds.freqReq == nil {
			continue
		}
		inst.devices = append(inst.devices, ds)
	}

	if !openedAny {
		// Every counter open failed — strongly suggests perf_event_paranoid > 1.
		closeAll(inst)
		inst = nil
		err = fmt.Errorf("%w: every perf_event_open failed (check kernel.perf_event_paranoid)", ErrPMUUnavailable)
		return
	}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample reads each open counter once and returns the per-engine busy
// percentages and per-device frequencies derived against the prior
// tick.
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}

	now := inst.nowFn()
	snap.SampledAtUnixMs = now.UnixMilli()
	snap.Devices = make([]Device, 0, len(inst.devices))

	for _, ds := range inst.devices {
		d := Device{Card: ds.card, PCIID: ds.pciID, Name: ds.name}

		readings := map[EngineE]uint64{}
		for e, c := range ds.counters {
			v, rerr := c.Read()
			if rerr != nil {
				continue
			}
			readings[e] = v
		}
		var freqAct, freqReq uint64
		var haveFA, haveFR bool
		if ds.freqAct != nil {
			if v, rerr := ds.freqAct.Read(); rerr == nil {
				freqAct = v
				haveFA = true
			}
		}
		if ds.freqReq != nil {
			if v, rerr := ds.freqReq.Read(); rerr == nil {
				freqReq = v
				haveFR = true
			}
		}

		if ds.primed {
			elapsed := now.Sub(ds.prevAt)
			elapsedNs := uint64(elapsed.Nanoseconds())
			elapsedSec := elapsed.Seconds()

			busy := func(e EngineE) (pct uint8) {
				cur, ok := readings[e]
				if !ok || elapsedNs == 0 {
					return 0
				}
				prev := ds.prev[e]
				if cur < prev {
					return 0
				}
				v := (cur - prev) * 100 / elapsedNs
				if v > 100 {
					v = 100
				}
				return uint8(v)
			}
			d.RenderBusyPercent = busy(EngineRender)
			d.CopyBusyPercent = busy(EngineCopy)
			d.VideoBusyPercent = busy(EngineVideo)
			d.VideoEnhanceBusyPercent = busy(EngineVideoEnhance)

			if haveFA && elapsedSec > 0 && freqAct >= ds.prevFA {
				d.ActualFreqMHz = uint32(float64(freqAct-ds.prevFA) / elapsedSec)
			}
			if haveFR && elapsedSec > 0 && freqReq >= ds.prevFR {
				d.RequestedFreqMHz = uint32(float64(freqReq-ds.prevFR) / elapsedSec)
			}
		}

		ds.prev = readings
		ds.prevFA = freqAct
		ds.prevFR = freqReq
		ds.prevAt = now
		ds.primed = true

		snap.Devices = append(snap.Devices, d)
	}

	slices.SortFunc(snap.Devices, func(a, b Device) int {
		return strings.Compare(a.Card, b.Card)
	})
	return
}

// Close releases every open perf event fd. After Close the Collector
// must not be reused.
func (inst *Collector) Close() (err error) {
	closeAll(inst)
	inst.devices = nil
	return
}

func closeAll(inst *Collector) {
	if inst == nil {
		return
	}
	for _, ds := range inst.devices {
		for _, c := range ds.counters {
			_ = c.Close()
		}
		if ds.freqAct != nil {
			_ = ds.freqAct.Close()
		}
		if ds.freqReq != nil {
			_ = ds.freqReq.Close()
		}
	}
}

type discovered struct {
	card  string
	pciID string
	name  string
}

func discoverIntelDevices(sys *sysfs.Reader) (out []discovered, err error) {
	names, lerr := sys.ListDir(SysClassDRM)
	if lerr != nil {
		if errors.Is(lerr, fs.ErrNotExist) {
			return nil, nil
		}
		err = lerr
		return
	}
	for _, name := range names {
		if !strings.HasPrefix(name, "card") {
			continue
		}
		// Skip render nodes ("renderD128") and partition entries ("card0-DP-1").
		if strings.ContainsAny(strings.TrimPrefix(name, "card"), "-D") {
			continue
		}
		vendor, verr := sys.ReadString(filepath.Join(SysClassDRM, name, "device", "vendor"))
		if verr != nil {
			continue
		}
		v, perr := strconv.ParseUint(strings.TrimPrefix(strings.TrimSpace(vendor), "0x"), 16, 16)
		if perr != nil || uint16(v) != IntelPCIVendor {
			continue
		}
		device, derr := sys.ReadString(filepath.Join(SysClassDRM, name, "device", "device"))
		if derr != nil {
			continue
		}
		pciID := strings.TrimSpace(device)
		out = append(out, discovered{
			card:  name,
			pciID: strings.ToLower(pciID),
			name:  pciIDName(pciID),
		})
	}
	return
}
