//go:build llm_generated_opus47

package gpu

import (
	"context"
	"io"
)

// VendorE classifies the GPU manufacturer. The string form is used in
// [Device.Vendor] for forward-compatibility (third-party adapters can
// stamp arbitrary vendor strings without recompiling this package).
type VendorE uint8

const (
	VendorUnknown VendorE = iota
	VendorIntel
	VendorNVIDIA
	VendorAMD
)

func (v VendorE) String() (out string) {
	switch v {
	case VendorIntel:
		return "intel"
	case VendorNVIDIA:
		return "nvidia"
	case VendorAMD:
		return "amd"
	default:
		return "unknown"
	}
}

// AllVendors lists every defined [VendorE] value.
var AllVendors = []VendorE{VendorUnknown, VendorIntel, VendorNVIDIA, VendorAMD}

// Device is the unified per-GPU sample. Only fields meaningful across
// every vendor live here; per-vendor richness (per-engine busy %,
// encoder utilization, etc.) belongs in the vendor-specific Snapshot.
type Device struct {
	// Vendor is the canonical vendor string ("intel", "nvidia", "amd")
	// or any other label a third-party adapter chooses. See [VendorE].
	Vendor string

	// Index is the 0-based device index within the vendor.
	Index int32

	// Name is the human-readable device codename (e.g. "Tiger Lake-H GT2",
	// "GeForce RTX 4090").
	Name string

	// PCIID is the lowercase hex device id, e.g. "0x9a49".
	PCIID string

	// BusyPercent is the overall device utilization, vendor-defined.
	// Intel: max across exposed engines. NVIDIA: nvmlUtilizationRates.gpu.
	// AMD: gpu_busy_percent. 0..100.
	BusyPercent uint8

	// MemoryUsedBytes / MemoryTotalBytes are in absolute bytes. 0 when
	// the vendor doesn't expose memory accounting (current Intel
	// adapter via PMU does not).
	MemoryUsedBytes  uint64
	MemoryTotalBytes uint64

	// PowerWatts is instantaneous device power. 0 when not exposed.
	PowerWatts float32

	// TempC is the hottest junction temperature in Celsius. 0 when not
	// exposed.
	TempC float32

	// FreqMHz is the graphics-engine clock. 0 when not exposed.
	FreqMHz uint32
}

// Snapshot is a single sample of every visible GPU across vendors.
type Snapshot struct {
	SampledAtUnixMs int64
	Devices         []Device
}

// SamplerI is the vendor-agnostic surface that [sysmetrics.Bundle]
// wires. Vendor packages implement adapters (e.g. intel.GenericSampler)
// that lossy-convert their richer Snapshot into the unified [Snapshot].
//
// Adapters embed [io.Closer] so the Bundle can release per-vendor
// resources (perf-event fds, NVML / ROCm-SMI handles) at shutdown.
type SamplerI interface {
	Sample(ctx context.Context) (snap Snapshot, err error)
	io.Closer
}
