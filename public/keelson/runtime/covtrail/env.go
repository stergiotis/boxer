package covtrail

import "github.com/stergiotis/boxer/public/config/env"

// Enabled is the opt-in (ADR-0343 §SD1, registered per ADR-0009). It needs
// the sampler of ADR-0169 — an atomic-mode cover build and a sampling
// interval — and a trail recorder; missing either, the host logs one line
// and the feature idles.
var Enabled = env.NewBool(env.Spec{
	Name:        "IMZERO2_COVERAGE_TRAIL",
	Default:     "false",
	Description: "write Go coverage to the audit trail on -cover -covermode=atomic builds (ADR-0343): the build's function table, the run's covered-set growth, and per agent command the units its handler executed",
	Category:    env.CategorySystem,
})
