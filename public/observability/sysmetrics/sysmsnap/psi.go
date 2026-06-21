package sysmsnap

// PSIPressure is one PSI line (the "some" or "full" share): the percentage of
// wall-time stalled, averaged over 10/60/300 s windows, plus the cumulative
// stall time in microseconds. avgN are already percentages [0,100].
type PSIPressure struct {
	Avg10   float32
	Avg60   float32
	Avg300  float32
	TotalUs uint64
}

// PSIResource holds the some/full pressure for one PSI resource. "full" is the
// "every non-idle task stalled" share; the cpu resource reports full as
// all-zero on most kernels (only "some" is meaningful for CPU).
type PSIResource struct {
	Some PSIPressure
	Full PSIPressure
}

// PSISnapshot is a single sample of Linux PSI from
// /proc/pressure/{cpu,memory,io}.
type PSISnapshot struct {
	SampledAtUnixMs int64

	CPU    PSIResource
	Memory PSIResource
	IO     PSIResource

	// Available is false when /proc/pressure is absent — the kernel was built
	// without CONFIG_PSI or booted with psi=0. The PSIResource fields are then
	// zero and callers should render "unavailable" rather than "0% pressure".
	Available bool
}
