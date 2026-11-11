package changepoint

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// DefaultCusumAllowance is the reference value k in noise standard
	// deviations: half the shift the chart is tuned to catch, here one σ.
	DefaultCusumAllowance = 0.5
	// DefaultCusumThreshold is the decision interval h in noise standard
	// deviations. With the default allowance it gives a two-sided in-control
	// average run length of roughly 465 samples for independent Gaussian
	// noise with known parameters (Montgomery, Introduction to Statistical
	// Quality Control, the tabular CUSUM chapter).
	DefaultCusumThreshold = 5.0
	// DefaultCusumWarmup is how many samples estimate the in-control mean and
	// scale when the configuration supplies neither.
	DefaultCusumWarmup int32 = 50
)

// CusumConfig parameterizes a [Cusum].
type CusumConfig struct {
	// Warmup is how many samples the in-control mean and standard deviation
	// are estimated from, at the start and again after every alarm. Zero
	// accepts [DefaultCusumWarmup]. Ignored when Sigma is set.
	//
	// The published run lengths assume the in-control parameters are known.
	// Estimating them from a short warm-up makes false alarms more frequent
	// than the table says, by an amount that depends on how lucky the warm-up
	// was; a longer warm-up is the only remedy.
	Warmup int32
	// Mean and Sigma are the in-control parameters when they are known. A
	// positive Sigma disables the warm-up, and an alarm then resets the sums
	// without re-estimating anything.
	Mean, Sigma float64
	// Allowance is k, in standard deviations. Zero accepts
	// [DefaultCusumAllowance].
	Allowance float64
	// Threshold is h, in standard deviations. Zero accepts
	// [DefaultCusumThreshold].
	Threshold float64
}

// CusumReading is the chart's state after one sample.
type CusumReading struct {
	// Upper and Lower are the one-sided sums, both non-negative, in standard
	// deviations. Upper accumulates evidence of an upward shift, Lower of a
	// downward one.
	Upper, Lower float64
	// WarmUp is true while the sample was used to estimate the in-control
	// parameters; the sums are zero and mean nothing.
	WarmUp bool
	// Alarm is true on the sample at which a sum crossed the threshold.
	Alarm bool
	// Up tells which sum raised the alarm.
	Up bool
	// Onset is the stream position where the alarming sum last left zero —
	// Page's estimate of where the change happened. Meaningful only with
	// Alarm. The distance from Onset to the alarm is the detection delay.
	Onset int64
}

// Cusum is the two-sided tabular cumulative-sum chart of Page, "Continuous
// inspection schemes", Biometrika 41 (1954), doi:10.1093/biomet/41.1-2.100.
//
//	z  = (x − μ)/σ
//	S⁺ = max(0, S⁺ + z − k)
//	S⁻ = max(0, S⁻ − z − k)
//
// An alarm is raised when either sum exceeds h. The chart is sequential: each reading
// uses the samples up to it and nothing later, so the readings are what a
// monitor running at the time would have shown.
//
// After an alarm the sums restart, and unless the in-control parameters were
// given, a new warm-up estimates the new regime's. The chart is blind during
// that warm-up, and a second change inside it is absorbed into the estimate
// rather than reported.
//
// A Cusum is not safe for concurrent use.
type Cusum struct {
	cfg       CusumConfig
	allowance float64
	threshold float64
	known     bool

	count int64
	mean  float64
	sigma float64

	// warm-up accumulators (Welford).
	warmN    int32
	warmMean float64
	warmM2   float64
	warming  bool

	upper, lower           float64
	upperOnset, lowerOnset int64
}

// NewCusum validates cfg and returns a chart ready for its first sample.
func NewCusum(cfg CusumConfig) (inst *Cusum, err error) {
	err = checkConfig([]string{"mean", "sigma", "allowance", "threshold"}, cfg.Mean, cfg.Sigma, cfg.Allowance, cfg.Threshold)
	if err != nil {
		return
	}
	if cfg.Sigma == 0 && cfg.Mean != 0 {
		err = eb.Build().Float64("mean", cfg.Mean).Errorf("an in-control mean is given without its standard deviation; give both, or neither to estimate them")
		return
	}
	if cfg.Sigma < 0 || cfg.Allowance < 0 || cfg.Threshold < 0 {
		err = eb.Build().Float64("sigma", cfg.Sigma).Float64("allowance", cfg.Allowance).Float64("threshold", cfg.Threshold).
			Errorf("a scale, allowance or threshold is negative")
		return
	}
	if cfg.Warmup < 0 || (cfg.Sigma == 0 && cfg.Warmup == 1) {
		err = eb.Build().Int32("warmup", cfg.Warmup).Errorf("the warm-up cannot estimate a standard deviation from fewer than two samples")
		return
	}
	inst = &Cusum{cfg: cfg, allowance: cfg.Allowance, threshold: cfg.Threshold}
	if inst.allowance == 0 {
		inst.allowance = DefaultCusumAllowance
	}
	if inst.threshold == 0 {
		inst.threshold = DefaultCusumThreshold
	}
	if cfg.Sigma > 0 {
		inst.known = true
		inst.mean, inst.sigma = cfg.Mean, cfg.Sigma
	} else {
		if inst.cfg.Warmup == 0 {
			inst.cfg.Warmup = DefaultCusumWarmup
		}
		inst.warming = true
	}
	return
}

// checkConfig refuses a configuration value that is not finite, naming the
// first in the order given.
func checkConfig(names []string, vals ...float64) (err error) {
	for i, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			err = eb.Build().Str("field", names[i]).Errorf("a configuration value is not finite")
			return
		}
	}
	return
}

// Threshold returns the decision interval in effect, in standard deviations.
func (inst *Cusum) Threshold() (h float64) {
	return inst.threshold
}

// Push feeds one sample and returns the chart's state after it. A sample that
// is not finite is refused and leaves the chart unchanged.
func (inst *Cusum) Push(v float64) (reading CusumReading, err error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		err = eb.Build().Int64("position", inst.count).Errorf("the sample is not finite")
		return
	}
	pos := inst.count
	inst.count++
	if inst.warming {
		inst.warmN++
		d := v - inst.warmMean
		inst.warmMean += d / float64(inst.warmN)
		inst.warmM2 += d * (v - inst.warmMean)
		reading.WarmUp = true
		// A warm-up that saw no variation cannot scale anything, so it runs
		// on until it has.
		if inst.warmN >= inst.cfg.Warmup && inst.warmM2 > 0 {
			inst.mean = inst.warmMean
			inst.sigma = math.Sqrt(inst.warmM2 / float64(inst.warmN-1))
			inst.warming = false
			inst.upperOnset, inst.lowerOnset = pos+1, pos+1
		}
		return
	}
	z := (v - inst.mean) / inst.sigma
	inst.upper = max(0, inst.upper+z-inst.allowance)
	inst.lower = max(0, inst.lower-z-inst.allowance)
	if inst.upper == 0 {
		inst.upperOnset = pos + 1
	}
	if inst.lower == 0 {
		inst.lowerOnset = pos + 1
	}
	reading.Upper, reading.Lower = inst.upper, inst.lower
	if inst.upper > inst.threshold || inst.lower > inst.threshold {
		reading.Alarm = true
		reading.Up = inst.upper >= inst.lower
		if reading.Up {
			reading.Onset = inst.upperOnset
		} else {
			reading.Onset = inst.lowerOnset
		}
		inst.upper, inst.lower = 0, 0
		inst.upperOnset, inst.lowerOnset = pos+1, pos+1
		if !inst.known {
			inst.warming = true
			inst.warmN, inst.warmMean, inst.warmM2 = 0, 0, 0
		}
	}
	return
}
