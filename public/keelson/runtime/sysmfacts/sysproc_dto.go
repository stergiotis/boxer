package sysmfacts

import "time"

// SysProc is one process-table sample of one host, column-major: one array
// element per process. Index i of every array describes the same process — see
// [SysNet] for the alignment contract.
//
// # Why column-major and not one entity per process
//
// The alternative — an entity per (host, pid, tick) — makes "this process over
// time" a key lookup instead of an array walk. It also multiplies the row rate
// by the process count: the collector samples up to 256 processes, so at 1 Hz
// that is ~22M rows per day per host against ~86k for this shape. `boxer.facts`
// is shared with runtime facts and ADR-0184 already records volume as the cost
// of that sharing; a 256x multiplier on the busiest domain is not a cost worth
// paying for one query shape that array functions can express anyway.
//
// # What is not here
//
// The command line, user name, uid and gid are [SysProcCmd]'s, and are written
// only when a deployment opts in.
type SysProc struct {
	_ struct{} `kind:"sysProc"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindProc,symbol"`
	Host string `lw:"sysmProcHost,symbol"`

	Pid  []uint32 `lw:"sysmProcPid,u32Array"`
	Ppid []uint32 `lw:"sysmProcPpid,u32Array"`
	// Name is /proc/[pid]/comm: the kernel's own 15-character truncation, not
	// the command line.
	Name []string `lw:"sysmProcName,symbolArray"`
	// State is the single-letter Linux state (R/S/D/Z/T/I/…), stored as the
	// letter rather than its byte value because that is what the kernel
	// documentation and every operator call it.
	State []string `lw:"sysmProcState,symbolArray"`

	// CPUPercent is per-CPU and deliberately unclamped: a process pegging N
	// cores reads N*100, and clamping would erase exactly the processes worth
	// looking at.
	CPUPercent  []float32 `lw:"sysmProcCpuPct,f32Array"`
	RSSBytes    []uint64  `lw:"sysmProcRssBytes,u64Array"`
	VMSizeBytes []uint64  `lw:"sysmProcVmSizeBytes,u64Array"`
	NumThreads  []int32   `lw:"sysmProcNumThreads,i32Array"`
	Nice        []int32   `lw:"sysmProcNice,i32Array"`
	Priority    []int32   `lw:"sysmProcPriority,i32Array"`
	// KernelThread rides a u8 lane as 0/1 — the facts schema has no boolean
	// array.
	KernelThread []uint8 `lw:"sysmProcKernelThread,u8Array,ct=u8h"`
	// StartedAtUnixMs is what makes a pid unambiguous across time: pids are
	// reused, so (pid, startedAt) is the identity a history query needs.
	StartedAtUnixMs []int64 `lw:"sysmProcStartedAtMs,i64Array"`

	// The two ADR-0126 topology marks: the cooperative BOXER_COMPONENT value
	// and the kernel-maintained systemd unit that corroborates it. Empty where
	// the process is unmarked or its environ was unreadable.
	Component  []string `lw:"sysmProcComponent,symbolArray"`
	CgroupUnit []string `lw:"sysmProcCgroupUnit,symbolArray"`
}
