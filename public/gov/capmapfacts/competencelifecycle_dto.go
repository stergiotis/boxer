package capmapfacts

// CompetenceLifecycle is who recorded each lifecycle phase: one attribute
// per phase, the phase riding as the membership's parameter so who stays
// attached to which phase.
type CompetenceLifecycle struct {
	_ struct{} `kind:"capmapCompetenceLifecycle"`

	ID uint64 `lw:",id"`

	By []CompetencePhaseBy `lw:"capmapCompetenceLifecycleBy,symbol,mixedLowCardRef"`
}

// CompetencePhaseBy is one phase and who recorded it.
type CompetencePhaseBy struct {
	Phase string `lw:"@params"`
	By    string
}
