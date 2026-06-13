//go:build linux && gpu_rocm

package imztop

import (
	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
	"github.com/stergiotis/boxer/public/observability/sysmetrics/gpu/rocm"
)

// wireGPUSampler attaches the AMD ROCm sysfs sampler to the bundle. A
// failure to construct the sampler is logged and silently dropped — the
// most common cause is a host with no AMD device, which is the explicit
// graceful-no-hardware path called out in ADR-0020 SD9.
func wireGPUSampler(opts *sysmetrics.BundleOptions) {
	s, err := rocm.NewGenericSampler(rocm.Options{})
	if err != nil {
		log.Debug().Err(err).Msg("imztop: rocm GPU sampler unavailable")
		return
	}
	opts.GPU = s
}
