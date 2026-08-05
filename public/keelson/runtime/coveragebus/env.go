package coveragebus

import (
	"time"

	"github.com/stergiotis/boxer/public/config/env"
)

// Interval is the coverage sample cadence knob (ADR-0169 §SD4, registered
// per ADR-0009). The sampler only exists on -cover -covermode=atomic
// builds; on those, a zero or negative duration disables sampling.
var Interval = env.NewString(env.Spec{
	Name:        "IMZERO2_COVERAGE_INTERVAL",
	Default:     "5s",
	Description: "coverage sample interval on -cover -covermode=atomic builds (ADR-0169); a zero or negative duration disables sampling",
	Category:    env.CategorySystem,
})

// ParseInterval interprets the knob's raw value: a non-positive duration
// disables sampling, an unparsable value falls back to the default (a
// misconfigured knob must not silently switch the lane off).
func ParseInterval(raw string) (interval time.Duration, enabled bool) {
	d, err := time.ParseDuration(raw)
	if err != nil {
		return DefaultInterval, true
	}
	if d <= 0 {
		return 0, false
	}
	return d, true
}

// IntervalFromEnv reads and interprets the knob.
func IntervalFromEnv() (interval time.Duration, enabled bool) {
	return ParseInterval(Interval.Get())
}
