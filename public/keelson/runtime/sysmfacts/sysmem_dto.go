package sysmfacts

import "time"

// SysMem is one memory sample of one host, as a `boxer.facts` row.
//
// Every figure is absolute bytes: the collector scales the kB lines of
// /proc/meminfo, so nothing downstream has to know which unit a field arrived
// in.
type SysMem struct {
	_ struct{} `kind:"sysMem"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindMem,symbol"`
	Host string `lw:"sysmMemHost,symbol"`

	TotalBytes uint64 `lw:"sysmMemTotalBytes,u64Array,unit"`
	FreeBytes  uint64 `lw:"sysmMemFreeBytes,u64Array,unit"`
	// AvailableBytes is MemAvailable, or Free+Cached on kernels without it.
	AvailableBytes uint64 `lw:"sysmMemAvailableBytes,u64Array,unit"`
	BuffersBytes   uint64 `lw:"sysmMemBuffersBytes,u64Array,unit"`
	// CachedBytes includes the ZFS ARC size when the collector was built with
	// ARC accounting and arcstats is present.
	CachedBytes    uint64 `lw:"sysmMemCachedBytes,u64Array,unit"`
	SwapTotalBytes uint64 `lw:"sysmMemSwapTotalBytes,u64Array,unit"`
	SwapFreeBytes  uint64 `lw:"sysmMemSwapFreeBytes,u64Array,unit"`

	// UsedBytes and SwapUsedBytes are the collector's own derivations. They are
	// stored rather than left to the reader because Used encodes which fallback
	// the collector applied — Total minus Available, or minus Free — and that
	// choice is not recoverable from the raw fields.
	UsedBytes     uint64 `lw:"sysmMemUsedBytes,u64Array,unit"`
	SwapUsedBytes uint64 `lw:"sysmMemSwapUsedBytes,u64Array,unit"`

	// ZFS ARC; 0 when disabled or absent.
	ARCSizeBytes uint64 `lw:"sysmMemArcSizeBytes,u64Array,unit"`
	ARCMinBytes  uint64 `lw:"sysmMemArcMinBytes,u64Array,unit"`
}
