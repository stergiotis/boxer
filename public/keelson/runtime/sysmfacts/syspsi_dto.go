package sysmfacts

import "time"

// SysPsi is one PSI sample of one host: the share of wall-time tasks spent
// stalled on cpu, memory and io.
//
// Every figure is the kernel's own. The avg windows are already percentages
// and the totals are cumulative microseconds, so nothing here is recomputed
// (ADR-0090 §SD3).
//
// `full` — the "every non-idle task stalled" share — is stored for cpu even
// though most kernels report it as zero there. "The kernel reported zero" and
// "we chose not to store it" are different facts, and only the first is
// recoverable from a row.
type SysPsi struct {
	_ struct{} `kind:"sysPsi"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindPsi,symbol"`
	Host string `lw:"sysmPsiHost,symbol"`

	// Available is false when /proc/pressure is absent — a kernel built
	// without CONFIG_PSI or booted with psi=0. Without it every unsupported
	// host would read as a perfectly unstalled one.
	Available bool `lw:"sysmPsiAvailable,bool"`

	CpuSomeAvg10   float32 `lw:"sysmPsiCpuSomeAvg10,f32Array,unit"`
	CpuSomeAvg60   float32 `lw:"sysmPsiCpuSomeAvg60,f32Array,unit"`
	CpuSomeAvg300  float32 `lw:"sysmPsiCpuSomeAvg300,f32Array,unit"`
	CpuSomeTotalUs uint64  `lw:"sysmPsiCpuSomeTotalUs,u64Array,unit"`
	CpuFullAvg10   float32 `lw:"sysmPsiCpuFullAvg10,f32Array,unit"`
	CpuFullAvg60   float32 `lw:"sysmPsiCpuFullAvg60,f32Array,unit"`
	CpuFullAvg300  float32 `lw:"sysmPsiCpuFullAvg300,f32Array,unit"`
	CpuFullTotalUs uint64  `lw:"sysmPsiCpuFullTotalUs,u64Array,unit"`

	MemorySomeAvg10   float32 `lw:"sysmPsiMemorySomeAvg10,f32Array,unit"`
	MemorySomeAvg60   float32 `lw:"sysmPsiMemorySomeAvg60,f32Array,unit"`
	MemorySomeAvg300  float32 `lw:"sysmPsiMemorySomeAvg300,f32Array,unit"`
	MemorySomeTotalUs uint64  `lw:"sysmPsiMemorySomeTotalUs,u64Array,unit"`
	MemoryFullAvg10   float32 `lw:"sysmPsiMemoryFullAvg10,f32Array,unit"`
	MemoryFullAvg60   float32 `lw:"sysmPsiMemoryFullAvg60,f32Array,unit"`
	MemoryFullAvg300  float32 `lw:"sysmPsiMemoryFullAvg300,f32Array,unit"`
	MemoryFullTotalUs uint64  `lw:"sysmPsiMemoryFullTotalUs,u64Array,unit"`

	IoSomeAvg10   float32 `lw:"sysmPsiIoSomeAvg10,f32Array,unit"`
	IoSomeAvg60   float32 `lw:"sysmPsiIoSomeAvg60,f32Array,unit"`
	IoSomeAvg300  float32 `lw:"sysmPsiIoSomeAvg300,f32Array,unit"`
	IoSomeTotalUs uint64  `lw:"sysmPsiIoSomeTotalUs,u64Array,unit"`
	IoFullAvg10   float32 `lw:"sysmPsiIoFullAvg10,f32Array,unit"`
	IoFullAvg60   float32 `lw:"sysmPsiIoFullAvg60,f32Array,unit"`
	IoFullAvg300  float32 `lw:"sysmPsiIoFullAvg300,f32Array,unit"`
	IoFullTotalUs uint64  `lw:"sysmPsiIoFullTotalUs,u64Array,unit"`
}
