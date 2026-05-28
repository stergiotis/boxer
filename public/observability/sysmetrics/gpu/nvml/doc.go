//go:build linux && gpu_nvml && llm_generated_opus47

// Package nvml samples NVIDIA GPU state via the NVIDIA Management
// Library (NVML). Symbols are loaded at runtime via
// `github.com/ebitengine/purego` — no cgo dependency, matching btop's
// dlopen-and-degrade pattern.
//
// # Build tag
//
// This package only compiles when the `gpu_nvml` build tag is set.
// Per [ADR-0019] SD5 the default `./tags` does not include it; consumers
// who want NVIDIA support add the tag (or pass it explicitly via
// `go build -tags="$(cat tags | tr -d $'\n'),gpu_nvml"`).
//
// # Required runtime
//
//   - `libnvidia-ml.so.1` (or `libnvidia-ml.so`) installed and loadable.
//   - The NVIDIA kernel module loaded.
//   - Per-device permission to read NVML counters; on most distros this
//     is open to non-root via the udev rules shipped in the driver.
//
// When the library is unloadable or `nvmlInit_v2` returns non-success,
// [New] returns [ErrNVMLUnavailable] (wrapped for diagnostics) so
// callers can branch via `errors.Is(err, ErrNVMLUnavailable)` and leave
// the GPU slot empty in the broader Bundle.
//
// # Provenance
//
// btop opens NVML via `dlopen`/`dlsym` at btop_collect.cpp:1230-1318.
// We use the same symbol set: nvmlInit_v2, nvmlShutdown,
// nvmlDeviceGetCount_v2, nvmlDeviceGetHandleByIndex_v2,
// nvmlDeviceGetName, nvmlDeviceGetUtilizationRates,
// nvmlDeviceGetMemoryInfo, nvmlDeviceGetPowerUsage,
// nvmlDeviceGetTemperature, nvmlDeviceGetClockInfo,
// nvmlDeviceGetPciInfo_v3.
//
// [ADR-0019]: ../../../../doc/adr/0019-observability-sysmetrics-linux-collector.md
package nvml
