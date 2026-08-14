package sysmfacts

import "time"

// SysGpu is one GPU sample of one host, column-major: one array element per
// device across every vendor. Index i of every array describes the same device
// — see [SysNet] for the alignment contract.
//
// Only fields meaningful across vendors are carried, matching the collector's
// unified device shape; per-vendor richness stays off this plane.
type SysGpu struct {
	_ struct{} `kind:"sysGpu"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindGpu,symbol"`
	Host string `lw:"sysmGpuHost,symbol"`

	// Vendor is the canonical vendor string; Index is 0-based within a vendor,
	// so neither alone identifies a device.
	Vendor []string `lw:"sysmGpuVendor,symbolArray"`
	Index  []int32  `lw:"sysmGpuIndex,i32Array"`
	Name   []string `lw:"sysmGpuName,symbolArray"`
	PCIID  []string `lw:"sysmGpuPciId,symbolArray"`

	// BusyPercent is vendor-defined: the max across exposed engines on Intel,
	// the utilization sample on NVIDIA, gpu_busy_percent on AMD. Comparable
	// over time for one device, not across vendors.
	BusyPercent []uint8 `lw:"sysmGpuBusyPct,u8Array,ct=u8h"`

	// These are 0 where the vendor exposes no accounting — the collector does
	// not distinguish that from a genuine zero, so neither can a reader.
	MemoryUsedBytes  []uint64  `lw:"sysmGpuMemoryUsedBytes,u64Array"`
	MemoryTotalBytes []uint64  `lw:"sysmGpuMemoryTotalBytes,u64Array"`
	PowerWatts       []float32 `lw:"sysmGpuPowerWatts,f32Array"`
	TempC            []float32 `lw:"sysmGpuTempC,f32Array"`
	FreqMHz          []uint32  `lw:"sysmGpuFreqMhz,u32Array"`
}
