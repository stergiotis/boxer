//go:build linux && gpu_rocm

package sysmetrics

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/rocm"
)

// wireGPU attaches the AMD ROCm sysfs sampler to the bundle. A failure to
// construct it is logged and dropped — the common cause is a host with no AMD
// device, the graceful-no-hardware path of ADR-0020 SD9.
func wireGPU(opts *BundleOptions) {
	s, err := rocm.NewGenericSampler(rocm.Options{})
	if err != nil {
		log.Debug().Err(err).Msg("sysmetrics: rocm GPU sampler unavailable")
		return
	}
	opts.GPU = s
}
