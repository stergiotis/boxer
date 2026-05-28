//go:build llm_generated_opus47

// Package gpu hosts the vendor-neutral GPU types — [Device], [Snapshot],
// and the [SamplerI] interface — that [sysmetrics.Bundle] wires for
// cross-vendor consumers. Vendor-specific richness (per-engine breakdown
// for Intel, encoder utilization for NVIDIA, ...) stays in the per-
// vendor sub-packages under this directory.
//
// The flow is:
//
//   - The richer per-vendor [Collector] returns a vendor-specific
//     Snapshot from its Sample method.
//   - A `GenericSampler` adapter (e.g. [intel.GenericSampler]) wraps
//     the collector and exposes a [gpu.SamplerI]-shaped Sample that
//     returns the unified [Snapshot]. Vendor-specific fields are
//     collapsed (Intel per-engine busy% becomes a single BusyPercent
//     equal to the max across engines).
//   - Bundle wires the adapter via [sysmetrics.BundleOptions.GPU].
//
// Build-tag-gated vendor packages (gpu/intel under `gpu_intel`, etc.)
// implement the adapter only under their tag; the unified types in this
// package compile under the default tag set.
//
// [intel.GenericSampler]: ../intel/
// [sysmetrics.Bundle]: ../../bundle.go
// [sysmetrics.BundleOptions.GPU]: ../../bundle.go
package gpu
