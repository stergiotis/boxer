//go:build linux && gpu_intel && llm_generated_opus47

// Package intel samples Intel integrated- and discrete-GPU state via the
// i915 driver's perf PMU. Each [Sample] reads the per-engine busy
// counters and the actual / requested GT frequency, computes deltas
// against the prior tick, and returns one [Device] per detected Intel
// GPU.
//
// # Build tag
//
// This package only compiles when the `gpu_intel` build tag is set.
// Per [ADR-0019] SD5 the default `./tags` does not include it; consumers
// who want Intel GPU support add the tag to their tags file or pass it
// explicitly via `go build -tags="$(cat tags | tr -d $'\n'),gpu_intel"`.
//
// # Required kernel surface
//
// The package needs:
//
//   - The i915 driver loaded — `/sys/devices/i915/type` must exist and
//     contain the kernel-assigned PMU type id.
//   - `kernel.perf_event_paranoid <= 1`, or [CAP_PERFMON] / root, so
//     [unix.PerfEventOpen] does not fail with EACCES.
//   - `/sys/class/drm/card*/device/{vendor,device}` for PCI-ID
//     enumeration (vendor 0x8086 = Intel).
//
// When the PMU is unavailable, [New] returns [ErrPMUUnavailable] and
// callers can render an empty GPU section without aborting other
// sysmetrics domains.
//
// # Provenance
//
// btop ships a vendored copy of `igt-gpu-tools/intel_gpu_top` that does
// the same job in C. We do not vendor it; the Go path uses
// `golang.org/x/sys/unix.PerfEventOpen` directly with the same i915 PMU
// config-id encoding. PMU constants and engine-class numbering are
// extracted from `i915_drm.h` (see [config_id_engine_busy] and
// [configFreqActual] / [configFreqRequested]).
//
// [ADR-0019]: ../../../../doc/adr/0019-observability-sysmetrics-linux-collector.md
// [CAP_PERFMON]: https://man7.org/linux/man-pages/man7/capabilities.7.html
package intel
