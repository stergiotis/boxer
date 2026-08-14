package sysmfacts

import "time"

// SysCpuInfo is the CPU descriptor of one host: the facts that do not change
// between ticks.
//
// It is written once on first sight of a host rather than on every sample —
// the descriptor/sample split of ADR-0184 §SD3, following the shape ADR-0169
// §SD6 uses for coverage. Re-writing it is harmless (the kind is append-shaped
// like every other, so a later row is simply a later observation), which is
// what makes "on first sight" a cheap heuristic rather than a correctness
// requirement.
type SysCpuInfo struct {
	_ struct{} `kind:"sysCpuInfo"`

	Id         uint64    `lw:",id"`
	NaturalKey []byte    `lw:",naturalKey"`
	Ts         time.Time `lw:",ts"`

	Kind string `lw:"sysmKindCpuInfo,symbol"`
	Host string `lw:"sysmCpuInfoHost,symbol"`

	// ModelName is /proc/cpuinfo's "model name", read at collector
	// construction.
	ModelName string `lw:"sysmCpuModelName,symbol"`
	// LogicalCores is the number of logical CPUs detected at construction. It
	// bounds the length of every per-core array in SysCpu.
	LogicalCores int32 `lw:"sysmCpuLogicalCores,i32Array,unit"`
}
