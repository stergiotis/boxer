package sysmsnap

// CPUSnapshot is a single sample of CPU state.
type CPUSnapshot struct {
	SampledAtUnixMs int64

	// TotalPercent is total CPU busy percentage [0..100], rounded.
	// First sample returns 0.
	TotalPercent uint8
	// PerCorePercent is per-logical-CPU busy percentage, in /proc/stat
	// order. First sample returns all zeros.
	PerCorePercent []uint8

	// PerCoreFreqMHz is the current frequency of each logical CPU in MHz,
	// 0 when cpufreq is not available for that core.
	PerCoreFreqMHz []uint32

	// LoadAvgN are the kernel-reported 1/5/15-minute load averages.
	LoadAvg1, LoadAvg5, LoadAvg15 float32

	// UsageWatts is the average package power over the most recent sample
	// interval, derived from the Intel RAPL energy_uj counter. Valid only
	// when UsageWattsAvailable is true.
	UsageWatts          float32
	UsageWattsAvailable bool

	// ActiveCPUs is the cgroup v2 effective cpuset (cpuset.cpus.effective)
	// or nil when the cgroup file is absent. Indices are logical CPU ids.
	ActiveCPUs []int32

	// ModelName is the /proc/cpuinfo "model name" field, copied at collector
	// construction.
	ModelName string

	// LogicalCores is the number of logical CPUs detected at construction.
	LogicalCores int32
}
