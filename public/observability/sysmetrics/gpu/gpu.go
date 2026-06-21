package gpu

import (
	"context"
	"io"

	"github.com/stergiotis/boxer/public/observability/sysmetrics/sysmsnap"
)

// SamplerI is the vendor-agnostic surface that [sysmetrics.Bundle]
// wires. Vendor packages implement adapters (e.g. intel.GenericSampler)
// that lossy-convert their richer Snapshot into the unified
// [sysmsnap.GPUSnapshot].
//
// Adapters embed [io.Closer] so the Bundle can release per-vendor
// resources (perf-event fds, NVML / ROCm-SMI handles) at shutdown.
type SamplerI interface {
	Sample(ctx context.Context) (snap sysmsnap.GPUSnapshot, err error)
	io.Closer
}
