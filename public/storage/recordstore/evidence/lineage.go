package evidence

import (
	"sync"

	"github.com/rs/zerolog/log"
)

// SchemedRowI is a row that records the scheme its digest and commitments
// were made under (Scheme.Name). A row that does not implement it, or
// records nothing, is read under the scheme the verifier was handed.
type SchemedRowI interface {
	RowI
	SchemeName() string
}

// Retired is a scheme a store no longer writes under and still verifies.
//
// Item is the canonical item a row of this scheme was digested over, for a
// scheme whose canonical item the current build computes differently; it is
// handed the row unwrapped from Memoised. nil when the row's own
// CanonicalItem is still that item. It is verify-only code kept for as long as rows of the scheme may exist — the one place the
// house rule against compatibility shims gives way (CODINGSTANDARDS § Write
// the modern language).
type Retired struct {
	Scheme Scheme
	Item   func(r RowI) ([]byte, error)
}

// Lineage is the schemes of one store: the one it writes under, and the
// retired ones its rows may still record. A verifier dispatches on the
// scheme each row records, so a store that moves to a new scheme keeps
// verifying the rows written before the move — and a chain that crosses
// the move verifies as one chain.
type Lineage struct {
	current Scheme
	retired map[string]Retired
}

// NewLineage is the lineage writing under current and verifying the retired
// schemes as well. A retired scheme with current's name is a programming
// error and panics.
func NewLineage(current Scheme, retired ...Retired) (inst Lineage) {
	inst = Lineage{current: current, retired: make(map[string]Retired, len(retired))}
	for _, r := range retired {
		if r.Scheme.Name() == current.Name() {
			log.Panic().Str("scheme", current.Name()).Msg("evidence: a lineage cannot retire the scheme it writes under")
		}
		inst.retired[r.Scheme.Name()] = r
	}
	return
}

// Current is the scheme the store writes under.
func (inst Lineage) Current() Scheme { return inst.current }

// Knows says rows recording name are verifiable under this lineage: the
// current scheme or a retired one.
func (inst Lineage) Knows(name string) bool {
	if name == inst.current.Name() {
		return true
	}
	_, ok := inst.retired[name]
	return ok
}

// Names lists the schemes this lineage verifies, current first, then the
// retired ones in no particular order.
func (inst Lineage) Names() (names []string) {
	names = make([]string, 0, 1+len(inst.retired))
	names = append(names, inst.current.Name())
	for n := range inst.retired {
		names = append(names, n)
	}
	return
}

// resolve is the scheme row was written under and its canonical item, or
// ok false when the row records a scheme this lineage does not know.
func (inst Lineage) resolve(r RowI) (scheme Scheme, item []byte, ok bool, err error) {
	name := ""
	if s, is := r.(SchemedRowI); is {
		name = s.SchemeName()
	}
	if name == "" || name == inst.current.Name() {
		item, err = r.CanonicalItem()
		return inst.current, item, true, err
	}
	ret, known := inst.retired[name]
	if !known {
		return
	}
	if ret.Item != nil {
		for {
			w, wraps := r.(interface{ Unwrap() RowI })
			if !wraps {
				break
			}
			r = w.Unwrap()
		}
		item, err = ret.Item(r)
	} else {
		item, err = r.CanonicalItem()
	}
	return ret.Scheme, item, true, err
}

var (
	lineagesMu sync.Mutex
	lineages   = map[string]Lineage{}
)

// RegisterLineage makes l the lineage Verify and VerifyHeads use when they
// are handed l's current scheme, typically from the defining store's init.
// A second lineage for one current scheme panics.
func RegisterLineage(l Lineage) {
	lineagesMu.Lock()
	defer lineagesMu.Unlock()
	name := l.current.Name()
	if _, have := lineages[name]; have {
		log.Panic().Str("scheme", name).Msg("evidence: lineage registered twice")
	}
	lineages[name] = l
}

// LineageOf is the lineage registered for current, or the lineage of
// current alone when none is.
func LineageOf(current Scheme) (l Lineage) {
	lineagesMu.Lock()
	defer lineagesMu.Unlock()
	l, ok := lineages[current.Name()]
	if !ok {
		l = NewLineage(current)
	}
	return
}
