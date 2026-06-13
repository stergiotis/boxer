//go:build linux && gpu_rocm

// Package rocm samples AMD GPU state via the kernel's amdgpu sysfs
// surface. Each [Sample] reads the per-card scalars and emits one
// [Device] per detected AMD GPU. The collector is stateless — every
// call performs a fresh sysfs read; there is no prior-tick state and
// [Close] is a no-op.
//
// # Build tag
//
// This package only compiles when the `gpu_rocm` build tag is set.
// Per [ADR-0019] SD8 the default `./tags` does not include it; consumers
// who want AMD GPU support add the tag (or pass it explicitly via
// `go build -tags="$(cat tags | tr -d $'\n'),gpu_rocm"`).
//
// # Strategy: pure sysfs first
//
// btop calls into ROCm-SMI (`librocm_smi64.so`) for AMD GPU telemetry.
// Modern kernels expose enough state via sysfs alone that the SDK
// dependency is unnecessary for the common case:
//
//   - `/sys/class/drm/card*/device/gpu_busy_percent` — overall busy %
//   - `/sys/class/drm/card*/device/mem_info_vram_{total,used}` — VRAM bytes
//   - `/sys/class/drm/card*/device/hwmon/hwmon*/temp1_input` — junction T (millidegrees C)
//   - `/sys/class/drm/card*/device/hwmon/hwmon*/power1_average` — power (microwatts)
//   - `/sys/class/drm/card*/device/hwmon/hwmon*/freq1_input` — graphics clock (Hz)
//   - `/sys/class/drm/card*/device/pp_dpm_sclk` — graphics clock states (Mhz, marker)
//
// Pure-sysfs avoids the v5/v6 ROCm-SMI ABI fork (btop_collect.cpp:253-254)
// and the libstdc++ dependency. The ROCm-SMI fallback path is
// intentionally deferred to a follow-up — every metric we need on
// consumer GPUs is already exposed via sysfs on amdgpu kernels.
//
// [ADR-0019]: ../../../../doc/adr/0019-observability-sysmetrics-linux-collector.md
package rocm
