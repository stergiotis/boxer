//go:build linux && gpu_intel

package intel

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// Generic returns a vendor-neutral [sysmsnap.GPUSnapshot] view of s. Per-engine
// detail is collapsed into a single [sysmsnap.GPUDevice.BusyPercent] equal to
// the maximum across the four engines. Frequency passes through as
// [sysmsnap.GPUDevice.FreqMHz] (the actual GT clock).
//
// PowerWatts, TempC, and the memory fields are left at zero — the i915
// PMU does not expose them. Consumers that need per-engine richness
// should use [Collector.Sample] directly and cast to the rich [Snapshot].
func (s Snapshot) Generic() (out sysmsnap.GPUSnapshot) {
	out.SampledAtUnixMs = s.SampledAtUnixMs
	out.Devices = make([]sysmsnap.GPUDevice, 0, len(s.Devices))
	for i, d := range s.Devices {
		busy := d.RenderBusyPercent
		if d.CopyBusyPercent > busy {
			busy = d.CopyBusyPercent
		}
		if d.VideoBusyPercent > busy {
			busy = d.VideoBusyPercent
		}
		if d.VideoEnhanceBusyPercent > busy {
			busy = d.VideoEnhanceBusyPercent
		}
		out.Devices = append(out.Devices, sysmsnap.GPUDevice{
			Vendor:      sysmsnap.VendorIntel.String(),
			Index:       int32(i),
			Name:        d.Name,
			PCIID:       d.PCIID,
			BusyPercent: busy,
			FreqMHz:     d.ActualFreqMHz,
		})
	}
	return
}

// GenericSampler wraps a [Collector] and exposes its sample via the
// vendor-neutral [gpu.SamplerI]. This is the type to wire into
// [sysmetrics.BundleOptions.GPU] when the consumer wants Intel GPU
// support inside the Bundle aggregator.
type GenericSampler struct {
	Inner *Collector
}

// NewGenericSampler is a convenience constructor that builds the inner
// [Collector] and wraps it in a [GenericSampler].
func NewGenericSampler(opts Options) (inst *GenericSampler, err error) {
	c, cerr := New(opts)
	if cerr != nil {
		err = cerr
		return
	}
	inst = &GenericSampler{Inner: c}
	return
}

// Sample reads the inner Collector and converts its rich [Snapshot] to
// the unified [sysmsnap.GPUSnapshot].
func (inst *GenericSampler) Sample(ctx context.Context) (snap sysmsnap.GPUSnapshot, err error) {
	var s Snapshot
	s, err = inst.Inner.Sample(ctx)
	if err != nil {
		return
	}
	snap = s.Generic()
	return
}

// Close releases the inner Collector's perf-event fds.
func (inst *GenericSampler) Close() (err error) {
	return inst.Inner.Close()
}

var _ gpu.SamplerI = (*GenericSampler)(nil)
