package impacttolerance

import (
	"math"
	"strconv"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Model is the services, their tolerances and the recovery estimates,
// columnar: Services and ToleranceHours run in parallel, and RecoveryHours is
// a service-major matrix over Services × Scenarios.
type Model struct {
	// Services are the important business services, drawn top to bottom.
	Services []string
	// ToleranceHours is each service's impact tolerance: the longest
	// disruption it may suffer, in hours.
	ToleranceHours []float64
	// Scenarios name the severe-but-plausible scenarios, in legend order.
	Scenarios []string
	// RecoveryHours is the estimated time to recover service s under scenario
	// k at s*len(Scenarios)+k, in hours; NaN where there is no estimate.
	RecoveryHours []float64
}

// Recovery is service s's estimated recovery under scenario k, NaN for none.
func (inst *Model) Recovery(s, k int) float64 {
	return inst.RecoveryHours[s*len(inst.Scenarios)+k]
}

// Validate checks the shapes agree and every value present is positive —
// the time axis is logarithmic.
func (inst *Model) Validate() (err error) {
	if inst == nil || len(inst.Services) == 0 {
		return eh.Errorf("impacttolerance: the model holds no services")
	}
	ns, nk := len(inst.Services), len(inst.Scenarios)
	if len(inst.ToleranceHours) != ns {
		return eh.Errorf("impacttolerance: ToleranceHours has %d entries for %d services", len(inst.ToleranceHours), ns)
	}
	if len(inst.RecoveryHours) != ns*nk {
		return eh.Errorf("impacttolerance: RecoveryHours has %d entries for %d services × %d scenarios", len(inst.RecoveryHours), ns, nk)
	}
	for s, t := range inst.ToleranceHours {
		if !positive(t) {
			return eh.Errorf("impacttolerance: service %q has tolerance %v; it must be a positive number of hours", inst.Services[s], t)
		}
	}
	for i, v := range inst.RecoveryHours {
		if !math.IsNaN(v) && !positive(v) {
			return eh.Errorf("impacttolerance: service %q under %q has recovery %v; it must be a positive number of hours",
				inst.Services[i/nk], inst.Scenarios[i%nk], v)
		}
	}
	return nil
}

// breaches counts the scenarios whose estimate exceeds service s's tolerance,
// out of those with an estimate.
func (inst *Model) breaches(s int) (over, of int) {
	for k := range inst.Scenarios {
		v := inst.Recovery(s, k)
		if math.IsNaN(v) {
			continue
		}
		of++
		if v > inst.ToleranceHours[s] {
			over++
		}
	}
	return
}

// hourRange is the smallest and largest value the model holds.
func (inst *Model) hourRange() (lo, hi float64) {
	lo, hi = math.Inf(1), math.Inf(-1)
	for _, v := range inst.ToleranceHours {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	for _, v := range inst.RecoveryHours {
		if !math.IsNaN(v) {
			lo, hi = math.Min(lo, v), math.Max(hi, v)
		}
	}
	return
}

func positive(v float64) bool { return v > 0 && !math.IsInf(v, 1) && !math.IsNaN(v) }

// hourLadder is the axis's tick vocabulary: durations people plan in, from a
// second to a year.
var hourLadder = []float64{1.0 / 3600, 10.0 / 3600, 1.0 / 60, 5.0 / 60, 0.25, 1, 4, 12, 24, 72, 168, 336, 720, 2160, 8760}

// axisRange is the ladder span covering [lo, hi]: down to the rung at or
// below lo, up to the rung at or above three times hi, so the region beyond
// the largest tolerance has room to read as a region.
func axisRange(lo, hi float64) (from, to float64) {
	from, to = hourLadder[0], hourLadder[len(hourLadder)-1]
	for _, r := range hourLadder {
		if r <= lo {
			from = r
		}
	}
	for i := len(hourLadder) - 1; i >= 0; i-- {
		if hourLadder[i] >= hi*3 {
			to = hourLadder[i]
		}
	}
	if lo < from {
		from = lo
	}
	if hi > to {
		to = hi
	}
	return
}

// FormatHours is a duration in the unit it is planned in: "30 s", "15 min",
// "4 h", "3 d", "1.5 d".
func FormatHours(h float64) string {
	switch {
	case h < 1.0/60:
		return num(math.Round(h*3600*10)/10) + " s"
	case h < 1:
		return num(math.Round(h*60)) + " min"
	case h < 24:
		return num(math.Round(h*10)/10) + " h"
	}
	return num(math.Round(h/24*10)/10) + " d"
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
