//go:build linux && gpu_rocm && llm_generated_opus47

package rocm

import (
	"context"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu"
)

// Generic returns a vendor-neutral [gpu.Snapshot] view of s.
// All AMD-specific fields map directly into [gpu.Device] — there's no
// per-engine richness to collapse, so this is a one-to-one copy.
func (s Snapshot) Generic() (out gpu.Snapshot) {
	out.SampledAtUnixMs = s.SampledAtUnixMs
	out.Devices = make([]gpu.Device, 0, len(s.Devices))
	for i, d := range s.Devices {
		out.Devices = append(out.Devices, gpu.Device{
			Vendor:           gpu.VendorAMD.String(),
			Index:            int32(i),
			Name:             d.Name,
			PCIID:            d.PCIID,
			BusyPercent:      d.BusyPercent,
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
// vendor-neutral [gpu.SamplerI].
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
