package good

import "iter"

type Store struct {
	ids   []uint64
	names []string
}

func (inst *Store) All() iter.Seq2[uint64, string] {
	return func(yield func(uint64, string) bool) {
		for i, id := range inst.ids {
			if !yield(id, inst.names[i]) {
				return
			}
		}
	}
}

func (inst *Store) Values() iter.Seq[string] {
	return func(yield func(string) bool) {
		for _, n := range inst.names {
			if !yield(n) {
				return
			}
		}
	}
}

func (inst *Store) Keys() iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for _, id := range inst.ids {
			if !yield(id) {
				return
			}
		}
	}
}

func (inst *Store) Backward() iter.Seq[string] {
	return func(yield func(string) bool) {
		for i := len(inst.names) - 1; i >= 0; i-- {
			if !yield(inst.names[i]) {
				return
			}
		}
	}
}

// Free function returning iter.Seq is out of scope.
func freeSeq() iter.Seq[int] {
	return func(yield func(int) bool) {}
}

// Non-iter method is fine.
func (inst *Store) Count() (n int) {
	n = len(inst.ids)
	return
}
