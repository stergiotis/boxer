package sysmsnap

// ProcInfo is one process's sample.
type ProcInfo struct {
	PID  uint32
	PPID uint32

	// Name is /proc/[pid]/comm — the kernel-truncated binary name (15
	// chars max).
	Name string

	// Cmd is /proc/[pid]/cmdline with NUL separators converted to spaces
	// and trailing NUL stripped. Empty for kernel threads.
	Cmd string

	// State is the single-letter Linux process state (R/S/D/Z/T/I/...).
	State byte

	UID  uint32
	GID  uint32
	User string // resolved name; empty when uid is unknown to NSS

	// StartedAtUnixMs is the wall-clock process start time, derived from
	// /proc/uptime + /proc/[pid]/stat starttime.
	StartedAtUnixMs int64

	// CPUPercent is the per-CPU CPU usage. A process pegging one core
	// reads 100; pegging N cores reads N*100. 0 on first sample (no
	// prior tick to delta against).
	//
	// Formula: deltaPidTicks * 100 * NumCPUs / deltaGlobalTicks
	// (matches btop src/linux/btop_collect.cpp:3250).
	CPUPercent float32

	RSSBytes    uint64
	VMSizeBytes uint64

	NumThreads int32
	Nice       int32
	Priority   int32

	// KernelThread is true when PID == 2 or PPID == 2.
	KernelThread bool
}
