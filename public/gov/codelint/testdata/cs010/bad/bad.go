package bad

import "iter"

// Single iterator with a non-canonical name — flagged.
type Store struct{}

func (inst *Store) Iterate() iter.Seq[int] { // want CS010 here
	return func(yield func(int) bool) {}
}

// Suppressed sole-iterator case.
type Suppressed struct{}

func (inst *Suppressed) Stream() iter.Seq[int] { //boxer:lint disable=CS010 reason="testdata coverage of suppression"
	return func(yield func(int) bool) {}
}
