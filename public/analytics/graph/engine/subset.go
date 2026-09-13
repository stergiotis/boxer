package engine

import "slices"

// Subset is a vertex subset — Ligra's vertexSubset — held either as a sorted
// slot list (sparse) or as a membership bitmap (dense). Both forms may be
// present; conversions are cached until the next mutation.
type Subset struct {
	n      int
	sparse []int32
	dense  []bool
	hasS   bool
	hasD   bool
	count  int
}

// NewSubset returns an empty subset over n slots.
func NewSubset(n int) *Subset {
	return &Subset{n: n}
}

// FromSlots returns a subset holding the given slots; they are sorted and
// deduplicated so the representation is canonical.
func FromSlots(n int, slots []int32) *Subset {
	s := NewSubset(n)
	s.SetSparse(slots)
	return s
}

// SetSparse replaces the membership with slots (copied, sorted, deduplicated).
func (s *Subset) SetSparse(slots []int32) {
	s.sparse = append(s.sparse[:0], slots...)
	slices.Sort(s.sparse)
	s.sparse = compactSlots(s.sparse)
	s.count = len(s.sparse)
	s.hasS, s.hasD = true, false
}

// SetDense replaces the membership with the bitmap (copied).
func (s *Subset) SetDense(bits []bool) {
	if cap(s.dense) < s.n {
		s.dense = make([]bool, s.n)
	}
	s.dense = s.dense[:s.n]
	copy(s.dense, bits)
	s.count = 0
	for _, b := range s.dense {
		if b {
			s.count++
		}
	}
	s.hasS, s.hasD = false, true
}

// Len is the member count.
func (s *Subset) Len() int { return s.count }

// IsEmpty reports an empty subset.
func (s *Subset) IsEmpty() bool { return s.count == 0 }

// Sparse returns the members as an ascending slot list, converting from the
// bitmap if needed. Shared; do not modify.
func (s *Subset) Sparse() []int32 {
	if !s.hasS {
		s.sparse = s.sparse[:0]
		for v, b := range s.dense {
			if b {
				s.sparse = append(s.sparse, int32(v))
			}
		}
		s.hasS = true
	}
	return s.sparse
}

// Dense returns the membership bitmap, converting from the list if needed.
// Shared; do not modify.
func (s *Subset) Dense() []bool {
	if !s.hasD {
		if cap(s.dense) < s.n {
			s.dense = make([]bool, s.n)
		}
		s.dense = s.dense[:s.n]
		clear(s.dense)
		for _, v := range s.sparse {
			s.dense[v] = true
		}
		s.hasD = true
	}
	return s.dense
}

// Contains reports membership of v.
func (s *Subset) Contains(v int32) bool {
	if s.hasD {
		return s.dense[v]
	}
	_, ok := searchSlots(s.sparse, v)
	return ok
}

func compactSlots(a []int32) []int32 {
	if len(a) < 2 {
		return a
	}
	w := 1
	for i := 1; i < len(a); i++ {
		if a[i] != a[w-1] {
			a[w] = a[i]
			w++
		}
	}
	return a[:w]
}

func searchSlots(a []int32, v int32) (int, bool) {
	lo, hi := 0, len(a)
	for lo < hi {
		mid := int(uint(lo+hi) >> 1)
		if a[mid] < v {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo, lo < len(a) && a[lo] == v
}
