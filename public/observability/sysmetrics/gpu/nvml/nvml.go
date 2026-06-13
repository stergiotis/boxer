//go:build linux && gpu_nvml

package nvml

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
)

// ErrNVMLUnavailable is returned by [New] when the NVML library cannot
// be loaded or initialized — no NVIDIA driver, missing libnvidia-ml.so,
// or `nvmlInit_v2` returned a non-success code. Callers should treat
// this as a soft failure (render an empty GPU section, do not abort
// the broader Snapshot).
var ErrNVMLUnavailable = errors.New("NVML not available")

// Device is one NVIDIA GPU's sample.
type Device struct {
	Index int32
	Name  string
	PCIID string // "0x2206" for an RTX 3080, derived from pciDeviceId

	GPUUtilizationPercent    uint8
	MemoryUtilizationPercent uint8
	MemoryUsedBytes          uint64
	MemoryTotalBytes         uint64
	PowerWatts               float32
	TempC                    float32
	GraphicsClockMHz         uint32
}

// Snapshot is a single sample of every detected NVIDIA GPU.
type Snapshot struct {
	SampledAtUnixMs int64
	Devices         []Device
}

// CollectorI is the public surface an NVIDIA GPU sampler implements.
type CollectorI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
	Close() (err error)
}

// NVMLI is the abstract surface a Collector talks to. Production code
// uses a purego-backed implementation; tests inject a deterministic
// fake.
type NVMLI interface {
	DeviceCount() (count uint32, err error)
	DeviceName(idx uint32) (name string, err error)
	DevicePCIID(idx uint32) (pciID string, err error)
	DeviceUtilization(idx uint32) (gpuPct, memPct uint32, err error)
	DeviceMemory(idx uint32) (total, free, used uint64, err error)
	DevicePowerMilliWatts(idx uint32) (mw uint32, err error)
	DeviceTempC(idx uint32) (c uint32, err error)
	DeviceGraphicsClockMHz(idx uint32) (mhz uint32, err error)
	Close() (err error)
}

// LoaderFunc opens NVML and returns an [NVMLI]. Production uses
// [DefaultLoader] (purego-backed); tests inject a fake.
type LoaderFunc func() (nvml NVMLI, err error)

// Options configures a [Collector].
type Options struct {
	NowFunc func() time.Time

	// Loader, when non-nil, overrides the NVML loader. Defaults to
	// [DefaultLoader] which dlopens libnvidia-ml.so via purego.
	Loader LoaderFunc
}

// Collector samples NVIDIA GPU state.
type Collector struct {
	nvml  NVMLI
	nowFn func() time.Time
}

// New constructs a Collector. It dlopens NVML, calls nvmlInit_v2, and
// queries the device count once. On failure it returns
// [ErrNVMLUnavailable] (wrapped) so the caller can branch via
// errors.Is.
func New(opts Options) (inst *Collector, err error) {
	if opts.NowFunc == nil {
		opts.NowFunc = time.Now
	}
	loader := opts.Loader
	if loader == nil {
		loader = DefaultLoader
	}
	nvml, lerr := loader()
	if lerr != nil {
		err = fmt.Errorf("%w: %w", ErrNVMLUnavailable, lerr)
		return
	}
	inst = &Collector{nvml: nvml, nowFn: opts.NowFunc}
	return
}

var _ CollectorI = (*Collector)(nil)

// Sample queries NVML for every visible NVIDIA GPU. Per-field failures
// are absorbed (the field stays at zero) so a single missing capability
// (e.g. nvmlDeviceGetPowerUsage on a GPU without power telemetry) does
// not abort the whole snapshot.
func (inst *Collector) Sample(ctx context.Context) (snap Snapshot, err error) {
	select {
	case <-ctx.Done():
		err = ctx.Err()
		return
	default:
	}
	snap.SampledAtUnixMs = inst.nowFn().UnixMilli()

	count, cerr := inst.nvml.DeviceCount()
	if cerr != nil {
		err = fmt.Errorf("nvml device count: %w", cerr)
		return
	}
	snap.Devices = make([]Device, 0, count)
	for i := uint32(0); i < count; i++ {
		select {
		case <-ctx.Done():
			err = ctx.Err()
			return
		default:
		}
		d := Device{Index: int32(i)}
		if name, nerr := inst.nvml.DeviceName(i); nerr == nil {
			d.Name = name
		}
		if pciID, perr := inst.nvml.DevicePCIID(i); perr == nil {
			d.PCIID = pciID
		}
		if gpuPct, memPct, uerr := inst.nvml.DeviceUtilization(i); uerr == nil {
			d.GPUUtilizationPercent = clamp01(gpuPct)
			d.MemoryUtilizationPercent = clamp01(memPct)
		}
		if total, _, used, merr := inst.nvml.DeviceMemory(i); merr == nil {
			d.MemoryTotalBytes = total
			d.MemoryUsedBytes = used
		}
		if mw, perr := inst.nvml.DevicePowerMilliWatts(i); perr == nil {
			d.PowerWatts = float32(mw) / 1000
		}
		if c, terr := inst.nvml.DeviceTempC(i); terr == nil {
			d.TempC = float32(c)
		}
		if mhz, ferr := inst.nvml.DeviceGraphicsClockMHz(i); ferr == nil {
			d.GraphicsClockMHz = mhz
		}
		snap.Devices = append(snap.Devices, d)
	}

	slices.SortFunc(snap.Devices, func(a, b Device) int {
		switch {
		case a.Index < b.Index:
			return -1
		case a.Index > b.Index:
			return 1
		}
		return 0
	})
	return
}

// Close shuts NVML down and releases the loaded library.
func (inst *Collector) Close() (err error) {
	return inst.nvml.Close()
}

// Generic returns a vendor-neutral [gpu.Snapshot] view of s. NVML
// already exposes a single device-wide BusyPercent (the gpu field of
// nvmlUtilization_t), so the conversion is direct.
func (s Snapshot) Generic() (out gpu.Snapshot) {
	out.SampledAtUnixMs = s.SampledAtUnixMs
	out.Devices = make([]gpu.Device, 0, len(s.Devices))
	for _, d := range s.Devices {
		out.Devices = append(out.Devices, gpu.Device{
			Vendor:           gpu.VendorNVIDIA.String(),
			Index:            d.Index,
			Name:             d.Name,
			PCIID:            d.PCIID,
			BusyPercent:      d.GPUUtilizationPercent,
			MemoryUsedBytes:  d.MemoryUsedBytes,
			MemoryTotalBytes: d.MemoryTotalBytes,
			PowerWatts:       d.PowerWatts,
			TempC:            d.TempC,
			FreqMHz:          d.GraphicsClockMHz,
		})
	}
	return
}

// GenericSampler wraps a [Collector] and exposes its sample via the
// vendor-neutral [gpu.SamplerI]. Use this when wiring NVIDIA support
// into [sysmetrics.Bundle].
type GenericSampler struct {
	Inner *Collector
}

// NewGenericSampler builds a [Collector] and wraps it in a [GenericSampler].
func NewGenericSampler(opts Options) (inst *GenericSampler, err error) {
	c, cerr := New(opts)
	if cerr != nil {
		err = cerr
		return
	}
	inst = &GenericSampler{Inner: c}
	return
}

func (inst *GenericSampler) Sample(ctx context.Context) (snap gpu.Snapshot, err error) {
	var s Snapshot
	s, err = inst.Inner.Sample(ctx)
	if err != nil {
		return
	}
	snap = s.Generic()
	return
}

func (inst *GenericSampler) Close() (err error) {
	return inst.Inner.Close()
}

var _ gpu.SamplerI = (*GenericSampler)(nil)

func clamp01(v uint32) (out uint8) {
	if v > 100 {
		return 100
	}
	return uint8(v)
}

// formatPCIDevice returns "0xDDDD" given the low 16 bits of the
// nvmlPciInfo_t.pciDeviceId field. Exposed here for the loader's use.
func formatPCIDevice(pciDeviceID uint32) (out string) {
	low := pciDeviceID & 0xFFFF
	return strings.ToLower(fmt.Sprintf("0x%04x", low))
}
