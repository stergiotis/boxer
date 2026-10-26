package capmapcorpus

import (
	"strconv"
	"time"
)

// A claim is one assessment of a competence (ADR-0337 §SD2): a dimension, a
// rating on that dimension's scale, the anchors the rating rests on, who
// claimed it and when. The two bare scores ADR-0168 carried — `maturity` and
// `pain` — had none of the last three, which is why they were never filled; a
// claim with an empty basis is an affirmation, and the lint lens says so.

// Claim is one assessment on one dimension.
type Claim struct {
	// Dimension names the scale, one of [AllDimensions].
	Dimension string
	// Rating is the position on the scale, as the vault spells it: a
	// numeral for a ranked dimension, a word for an enumerated one. Kept as
	// text so one column carries every dimension; [DimensionSpec.Rank]
	// orders it.
	Rating string
	// Basis lists the anchors the rating rests on, in authored order. Each
	// is also a [RelationKindBasis] relation, which is where its resolution
	// lives.
	Basis []Anchor
	// By is the assessor's handle; At is the date the claim was made. Either
	// may be empty, and the lint lens lists a claim missing one.
	By string
	At time.Time
	// Problems lists what the lint found wrong with the claim, as
	// [ClaimProblemE] values. Empty for a claim with a basis that resolves,
	// an assessor and a date. Computed on parse, never written to the vault.
	Problems []ClaimProblemE
}

// ClaimProblemE is one thing the parse found wrong with a claim. The names
// are stored and queried by, so they are stable tokens.
type ClaimProblemE string

const (
	// ClaimProblemEmptyBasis is a claim with no anchors: an affirmation.
	ClaimProblemEmptyBasis ClaimProblemE = "empty-basis"
	// ClaimProblemUnresolvedBasis is a claim at least one of whose anchors
	// the tree does not carry.
	ClaimProblemUnresolvedBasis ClaimProblemE = "unresolved-basis"
	// ClaimProblemRetiredBasis is a claim resting on a retired decision.
	ClaimProblemRetiredBasis ClaimProblemE = "retired-basis"
	// ClaimProblemNoAssessor is a claim without a `by`.
	ClaimProblemNoAssessor ClaimProblemE = "no-assessor"
	// ClaimProblemNoDate is a claim without a readable `at`.
	ClaimProblemNoDate ClaimProblemE = "no-date"
	// ClaimProblemDuplicateDimension is a second claim on a dimension the
	// competence already claimed; the first is kept and the second carries
	// this.
	ClaimProblemDuplicateDimension ClaimProblemE = "duplicate-dimension"
)

// DimensionSpec defines one assessment dimension: its scale and what each
// position on it means. The vocabulary is registered and small, and open on
// the ADR-0158 precedent — adding a dimension is an entry here, changing what
// one means is a decision.
type DimensionSpec struct {
	Name string
	// Ranked dimensions take an integer rating in [Min, Max]; enumerated
	// ones take one of Values, in rank order.
	Ranked   bool
	Min, Max int
	Values   []string
	// Rungs names each position, index 0 being Min or Values[0].
	Rungs []string
	// Summary is one line on what the dimension measures.
	Summary string
}

// The four dimensions ADR-0337 §SD2 registers.
const (
	DimensionReadiness  = "readiness"
	DimensionEvolution  = "evolution"
	DimensionImportance = "importance"
	DimensionPain       = "pain"
)

// ReadinessRungs names the nine readiness rungs, adapted from TRL's software
// column to this tree's artefacts. Index 0 is rung 1.
var ReadinessRungs = []string{
	"named in a Context or a survey",
	"decided; milestones unticked",
	"built in part; default-lane tests pass",
	"built end to end behind a surface",
	"exercised against the real engine: integration lane or a scene",
	"documented for a consumer",
	"positioned; every clause rests on a record",
	"measured by a trial's citable claim",
	"operated as the toolkit's own workload with its facts to show it",
}

// PainRungs names the pain scale. Index 0 is 0.
var PainRungs = []string{
	"none",
	"a known rough edge",
	"costs a maintainer time monthly",
	"costs a maintainer time weekly",
	"blocks a consumer's task without a workaround",
	"blocks the toolkit's own workload",
}

var dimensions = []DimensionSpec{
	{Name: DimensionReadiness, Ranked: true, Min: 1, Max: 9, Rungs: ReadinessRungs,
		Summary: "how far the competence has travelled from named to operated"},
	{Name: DimensionEvolution, Values: []string{"genesis", "custom", "product", "commodity"},
		Rungs:   []string{"novel and uncertain", "built to order", "productised", "a utility"},
		Summary: "what kind of thing the competence is in the world; not a maturity"},
	{Name: DimensionImportance, Values: []string{"foundational", "differentiating", "innovating"},
		Rungs:   []string{"the toolkit cannot stand without it", "why a consumer picks this toolkit", "a bet"},
		Summary: "why the toolkit carries the competence, read against the premises"},
	{Name: DimensionPain, Ranked: true, Min: 0, Max: 5, Rungs: PainRungs,
		Summary: "how much the competence costs the people around it"},
}

// AllDimensions lists the registered dimensions in presentation order.
func AllDimensions() (specs []DimensionSpec) {
	return append([]DimensionSpec(nil), dimensions...)
}

// LookupDimension finds a dimension by name.
func LookupDimension(name string) (spec DimensionSpec, ok bool) {
	for _, d := range dimensions {
		if d.Name == name {
			return d, true
		}
	}
	return DimensionSpec{}, false
}

// Rank orders a rating on this dimension's scale: the integer itself for a
// ranked dimension, the value's index for an enumerated one. ok is false for a
// rating the scale does not hold.
func (inst DimensionSpec) Rank(rating string) (rank int, ok bool) {
	if inst.Ranked {
		n, err := strconv.Atoi(rating)
		if err != nil || n < inst.Min || n > inst.Max {
			return 0, false
		}
		return n, true
	}
	for i, v := range inst.Values {
		if v == rating {
			return i, true
		}
	}
	return 0, false
}

// RungName is the name of a rating's position, or empty for one the scale does
// not hold.
func (inst DimensionSpec) RungName(rating string) (name string) {
	rank, ok := inst.Rank(rating)
	if !ok {
		return ""
	}
	idx := rank
	if inst.Ranked {
		idx = rank - inst.Min
	}
	if idx < 0 || idx >= len(inst.Rungs) {
		return ""
	}
	return inst.Rungs[idx]
}

// HasProblem reports whether the claim carries p.
func (inst Claim) HasProblem(p ClaimProblemE) (has bool) {
	for _, q := range inst.Problems {
		if q == p {
			return true
		}
	}
	return false
}

// ClaimOn returns the competence's claim on a dimension, if it made one.
func (inst Competence) ClaimOn(dimension string) (claim Claim, ok bool) {
	for _, c := range inst.Assessment {
		if c.Dimension == dimension && !c.HasProblem(ClaimProblemDuplicateDimension) {
			return c, true
		}
	}
	return Claim{}, false
}
