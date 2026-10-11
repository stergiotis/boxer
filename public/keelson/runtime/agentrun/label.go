package agentrun

import (
	"slices"
	"strings"
)

// The label (ADR-0311 §SD1): an integrity level, a TLP 2.0 tier and two
// set-valued provenance marks, a join-semilattice propagated by Join
// through projections and checked at sinks; lowered only by an endorse
// entry, on the integrity axis alone.

// LevelE is the integrity level: the higher, the less trusted.
type LevelE uint8

const (
	// LevelPerson is the person and harness text.
	LevelPerson LevelE = 0
	// LevelSystem is deterministic system data and same-organisation peer
	// content not otherwise marked.
	LevelSystem LevelE = 1
	// LevelExternal is external content: fetched, files, captures.
	LevelExternal LevelE = 2
	// LevelOwnModel is this run's model output returning through storage.
	LevelOwnModel LevelE = 3
	// LevelOtherModel is another run's model output.
	LevelOtherModel LevelE = 4
)

func (inst LevelE) String() (s string) {
	switch inst {
	case LevelPerson:
		return "person"
	case LevelSystem:
		return "system"
	case LevelExternal:
		return "external"
	case LevelOwnModel:
		return "own-model"
	case LevelOtherModel:
		return "other-model"
	}
	return "unknown"
}

// TierE is the TLP 2.0 confidentiality tier: a mirror of leeway's
// exclusive tlp use-aspect family, ordered as it is, so a label joins by
// maximum; nothing here reads the family.
type TierE uint8

const (
	TierClear TierE = iota
	TierGreen
	TierAmber
	TierAmberStrict
	TierRed
)

var tierNames = []string{"CLEAR", "GREEN", "AMBER", "AMBER+STRICT", "RED"}

func (inst TierE) String() (s string) {
	if int(inst) < len(tierNames) {
		return tierNames[inst]
	}
	return "unknown"
}

// ParseTier is the tier a name spells, case-insensitively; false for none.
func ParseTier(s string) (t TierE, ok bool) {
	for i, n := range tierNames {
		if strings.EqualFold(n, s) {
			return TierE(i), true
		}
	}
	return
}

// Label is the label of one entry, one message of a projection, or one
// run.
type Label struct {
	Level LevelE
	Tier  TierE
	// Exposure names the remote providers the content was sent to; Origin
	// the remote routes whose output it derives from. Both are sets,
	// sorted, joined by union, recorded for audit and read by no guard by
	// default (ADR-0311 §SD7).
	Exposure []string
	Origin   []string
}

// Join is the least upper bound: the higher level, the higher tier, the
// union of the marks.
func (inst Label) Join(other Label) (l Label) {
	l.Level = max(inst.Level, other.Level)
	l.Tier = max(inst.Tier, other.Tier)
	l.Exposure = unionMarks(inst.Exposure, other.Exposure)
	l.Origin = unionMarks(inst.Origin, other.Origin)
	return
}

// JoinAll folds Join over labels; the zero label is the identity.
func JoinAll(labels ...Label) (l Label) {
	for _, x := range labels {
		l = l.Join(x)
	}
	return
}

// Equal says the two labels are the same point of the lattice.
func (inst Label) Equal(other Label) bool {
	return inst.Level == other.Level && inst.Tier == other.Tier &&
		slices.Equal(unionMarks(inst.Exposure, nil), unionMarks(other.Exposure, nil)) &&
		slices.Equal(unionMarks(inst.Origin, nil), unionMarks(other.Origin, nil))
}

// AtOrBelow says the label is at or below the other on both ordered
// axes; marks are not ordered against a clearance.
func (inst Label) AtOrBelow(other Label) bool {
	return inst.Level <= other.Level && inst.Tier <= other.Tier
}

// WithExposure is the label with a provider added to its exposure mark.
func (inst Label) WithExposure(provider string) (l Label) {
	l = inst
	l.Exposure = unionMarks(inst.Exposure, []string{provider})
	return
}

// WithOrigin is the label with a route added to its origin mark.
func (inst Label) WithOrigin(route string) (l Label) {
	l = inst
	l.Origin = unionMarks(inst.Origin, []string{route})
	return
}

// Endorsed is the label lowered on the integrity axis alone, to level:
// the only lowering (ADR-0311 §SD3); a level above the label's leaves it.
func (inst Label) Endorsed(level LevelE) (l Label) {
	l = inst
	l.Level = min(inst.Level, level)
	return
}

func (inst Label) String() (s string) {
	s = inst.Level.String() + "/" + inst.Tier.String()
	if len(inst.Exposure) > 0 {
		s += " exposure=" + strings.Join(inst.Exposure, ",")
	}
	if len(inst.Origin) > 0 {
		s += " origin=" + strings.Join(inst.Origin, ",")
	}
	return
}

// MarkValid says a mark can be stored: not empty, and without the comma
// the columns join marks with, which would read back as two marks and
// break the chain over the canonical form's mark arrays.
func MarkValid(s string) bool {
	return s != "" && !strings.Contains(s, ",")
}

// unionMarks is the sorted union without duplicates, empties or marks the
// columns cannot hold (MarkValid); nil for none, so an empty mark reads
// the same whichever way it was built. The writer refuses an entry with
// such a mark (checkEntry); here it is dropped, so a label never carries
// what the record cannot keep.
func unionMarks(a []string, b []string) (out []string) {
	for _, s := range a {
		if MarkValid(s) {
			out = append(out, s)
		}
	}
	for _, s := range b {
		if MarkValid(s) {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// MarksString joins a mark for a column; ParseMarks reads it back.
func MarksString(marks []string) string { return strings.Join(unionMarks(marks, nil), ",") }

// ParseMarks reads a mark column.
func ParseMarks(s string) []string {
	if s == "" {
		return nil
	}
	return unionMarks(strings.Split(s, ","), nil)
}
