package evidence

import "github.com/stergiotis/boxer/public/observability/eh/eb"

// Commitment is one vaulted component of a row as the chain and the vault
// hold it: the commitment the ledger carries, the opening the vault row
// carries, and whether the content is present. A component with neither
// content nor commitment never had content and is not listed.
type Commitment struct {
	Component  string
	Commitment string
	// Opening is the vault row's; empty when the content is absent.
	Opening []byte
	// Present says the content is there to open the commitment with.
	Present bool
}

// RowI is a row of a chained store as the laws read it.
type RowI interface {
	// Ordinal is the row's place in its scope, contiguous from 1.
	Ordinal() uint64
	// Prev is the predecessor's ordinal, zero for the first row.
	Prev() uint64
	// Digest is the row's chain digest.
	Digest() string
	// CanonicalItem is the row's canonical item with the chain fields at
	// zero: what the digest is over.
	CanonicalItem() ([]byte, error)
	// Mark is the row's mark.
	Mark() Mark
	// Commitments are the row's vaulted components.
	Commitments() []Commitment
	// VaultItem is the canonical item of the vault row of component
	// without its opening: what its commitment is over. Called only for a
	// present component.
	VaultItem(component string) ([]byte, error)
}

// HeadRowI is a row that records a shipped head: the ordinal it covers and
// the digest at it, as the anchor store received them.
type HeadRowI interface {
	RowI
	Head() (ordinal uint64, digest string, ok bool)
}

// BreakE says why a chain breaks at a row.
type BreakE uint8

const (
	// BreakNone is a chain that holds.
	BreakNone BreakE = iota
	// BreakForm is a row whose canonical item could not be computed.
	BreakForm
	// BreakPrev is a row that does not name its predecessor.
	BreakPrev
	// BreakDigest is a row whose digest is not the chain's.
	BreakDigest
	// BreakCommitment is a present component that does not open its
	// commitment, or has none.
	BreakCommitment
	// BreakUnexplained is an absent component with a standing commitment
	// that no mark explains.
	BreakUnexplained
	// BreakAfterDeleted is a row after the scope's deleted row.
	BreakAfterDeleted
	// BreakOrdinal is a row out of order or repeating an ordinal.
	BreakOrdinal
)

var breakNames = []string{"none", "form", "prev", "digest", "commitment", "unexplained", "after-deleted", "ordinal"}

func (inst BreakE) String() (s string) {
	if int(inst) < len(breakNames) {
		return breakNames[inst]
	}
	return "unknown"
}

// Verify recomputes the chain over rows in ordinal order from seed and
// returns the first ordinal that breaks it and why, or zero when the chain
// holds (law L31): every digest chains; every present component opens its
// commitment; an absent component whose commitment stands is accepted only
// when the chain explains it — an erased mark naming its ordinal and
// component, or a deleted mark at the chain's end — so content blanked in
// a live scope breaks the chain too; nothing follows a deleted mark. err
// is set when a canonical item could not be computed, with broken naming
// the row.
func Verify(scheme Scheme, seed string, rows []RowI) (broken uint64, why BreakE, err error) {
	if len(rows) == 0 {
		return
	}
	erased := explanations(rows)
	last := rows[len(rows)-1].Mark()
	deleted := last.Mark == MarkDeleted
	prevOrd, prevDigest := uint64(0), seed
	for i, r := range rows {
		ord := r.Ordinal()
		if ord <= prevOrd {
			return ord, BreakOrdinal, nil
		}
		if i > 0 && rows[i-1].Mark().Mark == MarkDeleted {
			return ord, BreakAfterDeleted, nil
		}
		item, ferr := r.CanonicalItem()
		if ferr != nil {
			return ord, BreakForm, eb.Build().Uint64("ordinal", ord).Errorf("canonical item: %w", ferr)
		}
		if r.Prev() != prevOrd {
			return ord, BreakPrev, nil
		}
		if r.Digest() != scheme.ChainDigest(prevDigest, item) {
			return ord, BreakDigest, nil
		}
		if why = commitmentsHold(scheme, r, erased, deleted); why != BreakNone {
			return ord, why, nil
		}
		prevOrd, prevDigest = ord, r.Digest()
	}
	return 0, BreakNone, nil
}

// explanations collects what the erased marks name: (ordinal, component)
// pairs, a component of "" standing for every component of the ordinal.
func explanations(rows []RowI) (erased map[uint64]map[string]bool) {
	erased = make(map[uint64]map[string]bool)
	for _, r := range rows {
		m := r.Mark()
		if m.Mark != MarkErased {
			continue
		}
		if erased[m.Target] == nil {
			erased[m.Target] = make(map[string]bool)
		}
		erased[m.Target][m.Component] = true
	}
	return
}

// commitmentsHold checks a row's vaulted components: present ones open,
// absent ones are explained.
func commitmentsHold(scheme Scheme, r RowI, erased map[uint64]map[string]bool, deleted bool) BreakE {
	ord := r.Ordinal()
	for _, c := range r.Commitments() {
		if c.Present {
			item, err := r.VaultItem(c.Component)
			if err != nil || !scheme.Opens(c.Opening, item, c.Commitment) {
				return BreakCommitment
			}
			continue
		}
		if c.Commitment == "" {
			continue
		}
		if deleted {
			continue
		}
		if e := erased[ord]; e != nil && (e[""] || e[c.Component]) {
			continue
		}
		return BreakUnexplained
	}
	return BreakNone
}

// Chained is the digest a row takes when it follows the predecessor at
// prevDigest: what a writer sets before the append.
func Chained(scheme Scheme, prevDigest string, item []byte) string {
	return scheme.ChainDigest(prevDigest, item)
}
