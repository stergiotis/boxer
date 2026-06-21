package sysmsnap

// MemSnapshot is a single sample of system memory state. All byte counts are
// absolute bytes (kB lines from /proc/meminfo are scaled <<10).
type MemSnapshot struct {
	SampledAtUnixMs int64
	TotalBytes      uint64 // MemTotal
	FreeBytes       uint64 // MemFree
	AvailableBytes  uint64 // MemAvailable; falls back to Free+Cached on old kernels
	BuffersBytes    uint64 // Buffers
	CachedBytes     uint64 // Cached (+ ZFS ARC size when EnableZFSArc and arcstats present)
	SwapTotalBytes  uint64
	SwapFreeBytes   uint64
	UsedBytes       uint64 // derived: Total - (Available <= Total ? Available : Free)
	SwapUsedBytes   uint64 // derived: SwapTotal - SwapFree (0 when SwapTotal == 0)
	ARCSizeBytes    uint64 // ZFS ARC size; 0 when disabled or absent
	ARCMinBytes     uint64 // ZFS ARC c_min; 0 when disabled or absent
}
