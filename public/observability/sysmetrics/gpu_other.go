//go:build !linux || !gpu_rocm

package sysmetrics

// wireGPU is a no-op when no GPU vendor build tag is active. The active variant
// lives in gpu_rocm.go (//go:build linux && gpu_rocm).
func wireGPU(opts *BundleOptions) {
	_ = opts
}
