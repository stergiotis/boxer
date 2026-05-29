//go:build llm_generated_opus47 && (!linux || !gpu_rocm)

package imztop

import (
	"github.com/stergiotis/boxer/public/observability/sysmetrics"
)

// wireGPUSampler is a no-op when no GPU vendor build tag is active.
func wireGPUSampler(opts *sysmetrics.BundleOptions) {
	_ = opts
}
