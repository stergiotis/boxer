package app

// MaturityE is what an app promises the people and the apps that depend on
// it (ADR-0375). It is a third classification axis beside [TopicT], what the
// app is about, and [KindE], how it came to exist: a demo says what an app is
// for, a grade says how far to rely on it.
//
// The values are ordered, weakest first, and [MaturityE.Rank] is that order
// as a number, so a predicate over keelson.apps can ask for a grade or
// better.
type MaturityE uint8

const (
	// MaturityUnstated is the zero value: nobody has graded the app. It
	// ranks below every stated grade, so an app that leaves the field out
	// promises nothing.
	MaturityUnstated MaturityE = 0
	// MaturityIncubating is an app that exists to learn what can be built.
	// It promises neither its operations, its stored state nor that it will
	// stay.
	MaturityIncubating MaturityE = 1
	// MaturitySupported is an app other work may depend on: a change that
	// breaks a caller or a person's stored state is announced.
	MaturitySupported MaturityE = 2
	// MaturityCore is an app whose stored data or bus contract other apps
	// or deployments are built on: changing either needs a migration.
	MaturityCore MaturityE = 3
)

// AllMaturities lists the grades, weakest first.
var AllMaturities = []MaturityE{
	MaturityUnstated,
	MaturityIncubating,
	MaturitySupported,
	MaturityCore,
}

// String renders the grade as its wire and display token.
func (inst MaturityE) String() (s string) {
	switch inst {
	case MaturityIncubating:
		s = "incubating"
	case MaturitySupported:
		s = "supported"
	case MaturityCore:
		s = "core"
	default:
		s = "unstated"
	}
	return
}

// Rank is the grade's position in [AllMaturities]: 0 for unstated, rising
// with what the app promises.
func (inst MaturityE) Rank() (r int32) {
	r = int32(inst)
	return
}

// IsValid reports whether inst is a declared grade. The zero value is valid,
// as with [KindE]: an app that states no grade is unstated, and needs no
// line in its manifest to say so.
func (inst MaturityE) IsValid() (ok bool) {
	ok = inst <= MaturityCore
	return
}

// ParseMaturity resolves a token against the grades. An unknown token
// returns ok=false rather than the zero value, so a misspelt grade is not
// read as unstated.
func ParseMaturity(s string) (m MaturityE, ok bool) {
	for _, cand := range AllMaturities {
		if cand.String() == s {
			m = cand
			ok = true
			return
		}
	}
	return
}
