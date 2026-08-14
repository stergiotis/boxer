package sysmfacts

import (
	"time"

	"github.com/stergiotis/boxer/public/functional/option"
)

// SysCpu is one CPU sample of one host, as a `boxer.facts` row.
//
// It carries only what moves between ticks; the model name and core count
// that do not are [SysCpuInfo]'s (ADR-0184 §SD3).
type SysCpu struct {
	_ struct{} `kind:"sysCpu"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	// Kind's value is the label; its membership id is what a query filters on.
	Kind string `lw:"sysmKindCpu,symbol"`
	Host string `lw:"sysmCpuHost,symbol"`

	// TotalPercent is whole-percent busy across all CPUs, as the collector
	// reports it. The collector's first sample after start reads 0 because a
	// busy fraction needs two readings of a counter; that zero is a real
	// sample and is stored as one.
	TotalPercent uint8 `lw:"sysmCpuTotalPct,u8Array,unit"`

	// PerCorePercent is per-logical-CPU busy percent in /proc/stat order.
	//
	// The `ct=u8h` override is load-bearing: a top-level []byte / []uint8 field
	// is a scalar variable-length blob by default, not a slice of u8, and the
	// two spellings are one type in Go. Without it this lands in a blob lane
	// and reads back as bytes.
	PerCorePercent []uint8 `lw:"sysmCpuPerCorePct,u8Array,ct=u8h"`

	// PerCoreFreqMHz is each logical CPU's current frequency; 0 where cpufreq
	// does not report for that core.
	PerCoreFreqMHz []uint32 `lw:"sysmCpuPerCoreFreqMhz,u32Array"`

	// The kernel's own load averages, not recomputed here.
	LoadAvg1  float32 `lw:"sysmCpuLoadAvg1,f32Array,unit"`
	LoadAvg5  float32 `lw:"sysmCpuLoadAvg5,f32Array,unit"`
	LoadAvg15 float32 `lw:"sysmCpuLoadAvg15,f32Array,unit"`

	// UsageWatts is average package power over the most recent interval, from
	// the RAPL energy counter. None where RAPL is unavailable — the collector
	// reports availability separately, and absence carries that without a
	// parallel boolean, so "no reading" and "idle" cannot be confused.
	UsageWatts option.Option[float32] `lw:"sysmCpuUsageWatts,f32Array,unit"`

	// ActiveCPUs is the cgroup v2 effective cpuset as logical CPU indices.
	// Empty where the cgroup file is absent.
	ActiveCPUs []int32 `lw:"sysmCpuActiveCpus,i32Array"`
}
