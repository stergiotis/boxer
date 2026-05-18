package bad

type Status uint8 // want CS006 here

const (
	StatusDraft  Status = 1
	StatusStable Status = 2
)

type weekday uint8 // want CS006 here

const (
	weekdayMonday weekday = iota
	weekdayTuesday
	weekdayWednesday
)

type Suppressed uint8 //boxer:lint disable=CS006 reason="testdata coverage of suppression"

const (
	SuppressedA Suppressed = 1
	SuppressedB Suppressed = 2
)

type SoloOK int // single-value, not detected — no finding expected

const SoloOnlyOne SoloOK = 1
