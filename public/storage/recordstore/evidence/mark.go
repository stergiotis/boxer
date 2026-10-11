package evidence

// MarkE is what a chained row says about absent content (ADR-0327
// (proposed) §SD3). The mark is the profile's, not the domain's: a
// domain's own kind enum stays free, and one verifier reads every store.
type MarkE uint8

const (
	// MarkNone is an ordinary row.
	MarkNone MarkE = 0
	// MarkErased says a vaulted component at the row's target ordinal was
	// erased after it was written: an absent component with a standing
	// commitment there is explained. A mark naming no component names
	// every component of the target.
	MarkErased MarkE = 1
	// MarkDeleted says the scope was deleted and this is its last row:
	// every absent component with a standing commitment in the scope is
	// explained, and nothing appends after it.
	MarkDeleted MarkE = 2
	// MarkKeep says the row survives deletion and cannot be erased: a head
	// entry, a receipt.
	MarkKeep MarkE = 3
)

var markNames = []string{"none", "erased", "deleted", "keep"}

func (inst MarkE) String() (s string) {
	if int(inst) < len(markNames) {
		return markNames[inst]
	}
	return "unknown"
}

// ParseMark is the mark a name spells; false for none.
func ParseMark(s string) (m MarkE, ok bool) {
	for i, n := range markNames {
		if n == s {
			return MarkE(i), true
		}
	}
	return
}

// IsValid says the mark is one of the enum's.
func (inst MarkE) IsValid() bool { return int(inst) < len(markNames) }

// Mark is a row's mark with what it names: the target ordinal of an
// erasure, and the component, empty for all.
type Mark struct {
	Mark      MarkE
	Target    uint64
	Component string
}
