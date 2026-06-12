// Version comparison. A version is a set of patch hashes that is
// downward-closed under dependencies — which every applied log is, by
// the engine's dependency gate. "More advanced" is set inclusion, a
// PARTIAL order: Diverged is a verdict, not a failure. The union of
// two versions is always materializable (sync both ways and the
// replicas converge, with any collisions surfacing as conflict state),
// so the data-level resolution of Diverged is the join, never a
// clock or counter tiebreak.
package exchange

import (
	t "github.com/stergiotis/boxer/public/algebraicarch/pushout/graggle/types"
)

// RelationE classifies how version a relates to version b under set
// inclusion.
type RelationE uint8

const (
	RelationEqual    RelationE = iota // same patch set
	RelationAhead                     // a strictly contains b: a is more advanced
	RelationBehind                    // b strictly contains a: b is more advanced
	RelationDiverged                  // each side holds patches the other lacks
)

func (inst RelationE) String() (s string) {
	switch inst {
	case RelationEqual:
		s = "equal"
	case RelationAhead:
		s = "ahead"
	case RelationBehind:
		s = "behind"
	case RelationDiverged:
		s = "diverged"
	default:
		s = "unknown"
	}
	return
}

// Comparison is Compare's result: the order verdict plus the exact
// difference in both directions.
type Comparison struct {
	Relation RelationE
	OnlyA    []t.PatchHash // a \ b, in a's order
	OnlyB    []t.PatchHash // b \ a, in b's order
}

// Compare relates two versions given as patch-hash lists. The verdict
// is pure set algebra; reading Ahead/Behind as "more advanced" relies
// on the inputs being downward-closed under dependencies, which holds
// for applied logs. Input order never affects the verdict — commuting
// peers hold the same set in different apply orders and compare
// Equal — but it is preserved in the diffs: when the inputs are
// applied logs, OnlyB is exactly what a must fetch from b, already in
// b's apply order (dependencies before dependents), and symmetrically
// for OnlyA.
func Compare(a, b []t.PatchHash) (c Comparison) {
	c.OnlyA = minus(a, b)
	c.OnlyB = minus(b, a)
	switch {
	case len(c.OnlyA) == 0 && len(c.OnlyB) == 0:
		c.Relation = RelationEqual
	case len(c.OnlyB) == 0:
		c.Relation = RelationAhead
	case len(c.OnlyA) == 0:
		c.Relation = RelationBehind
	default:
		c.Relation = RelationDiverged
	}
	return
}
