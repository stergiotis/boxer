package good

import "iter"

type SoleAll struct {
	items []int
}

func (inst *SoleAll) All() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

type SoleValues struct {
	values []string
}

func (inst *SoleValues) Values() iter.Seq[string] {
	return func(yield func(string) bool) {}
}

// MultiIter is the legitimate multi-iterator case (e.g. graph store).
// Each domain-named iterator is fine because the receiver exposes
// more than one iterator.
type MultiIter struct{}

func (inst *MultiIter) LiveChildren() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

func (inst *MultiIter) ForwardEdges() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

func (inst *MultiIter) BackwardEdges() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

// Free function returning iter.Seq is out of scope.
func freeSeq() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

// Non-iter method is fine.
func (inst *MultiIter) Count() (n int) {
	return
}
