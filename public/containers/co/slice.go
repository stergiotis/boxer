package co

import (
	"cmp"
	"iter"
	"slices"
	"sort"
)

// coSorter adapts a lead slice plus a caller swap callback to
// sort.Interface; desc flips the comparison direction so ascending and
// descending co-sorts share one adapter.
type coSorter[K cmp.Ordered] struct {
	swap  func(i int, j int)
	slice []K
	desc  bool
}

func (inst *coSorter[T]) Len() int {
	return len(inst.slice)
}

func (inst *coSorter[T]) Less(i, j int) bool {
	slice := inst.slice
	if inst.desc {
		return slice[j] < slice[i]
	}
	return slice[i] < slice[j]
}

func (inst *coSorter[T]) Swap(i, j int) {
	inst.swap(i, j)
	slice := inst.slice
	slice[j], slice[i] = slice[i], slice[j]
}

var _ sort.Interface = (*coSorter[int])(nil)

type sorter struct {
	less func(i, j int) bool
	swap func(i, j int)
	n    int
}

func (inst sorter) Len() int {
	return inst.n
}

func (inst sorter) Less(i, j int) bool {
	return inst.less(i, j)
}

func (inst sorter) Swap(i, j int) {
	inst.swap(i, j)
}

var _ sort.Interface = sorter{}

func SortUnstable(n int, less func(i, j int) bool, swap func(i, j int)) {
	sort.Sort(sorter{
		n:    n,
		less: less,
		swap: swap,
	})
}

// CoSortSlices sorts slice ascending, calling swap(i, j) on every
// element exchange so co-indexed slices can be kept aligned. A nil swap
// sorts the lead slice alone. The sort is unstable: the relative order
// of co-values under equal keys is unspecified.
func CoSortSlices[K cmp.Ordered](slice []K, swap func(i int, j int)) {
	coSortSlices(slice, swap, false)
}

// CoSortSlicesReverse is [CoSortSlices] with descending order.
func CoSortSlicesReverse[K cmp.Ordered](slice []K, swap func(i int, j int)) {
	coSortSlices(slice, swap, true)
}

func coSortSlices[K cmp.Ordered](slice []K, swap func(i int, j int), desc bool) {
	switch len(slice) {
	case 0, 1:
		return
	}
	if swap == nil {
		if desc {
			slices.SortFunc(slice, func(a, b K) int { return cmp.Compare(b, a) })
		} else {
			slices.Sort(slice)
		}
		return
	}
	sort.Sort(&coSorter[K]{
		slice: slice,
		swap:  swap,
		desc:  desc,
	})
}

func IterateSliceGrouped[K comparable, V any](sortedSliceKeys []K, coSliceVals []V) iter.Seq2[K, []V] {
	return func(yield func(K, []V) bool) {
		if len(sortedSliceKeys) == 0 {
			return
		}
		last := 0
		lastK := sortedSliceKeys[0]
		for i := 1; i < len(sortedSliceKeys); i++ {
			k := sortedSliceKeys[i]
			if k != lastK {
				if !yield(lastK, coSliceVals[last:i]) {
					return
				}
				last = i
				lastK = k
			}
		}
		if !yield(lastK, coSliceVals[last:]) {
			return
		}
	}
}
func IterateSliceGroupedFunc[K any, V any](sortedSliceKeys []K, coSliceVals []V, cmpKey func(K, K) int) iter.Seq2[K, []V] {
	return func(yield func(K, []V) bool) {
		if len(sortedSliceKeys) == 0 {
			return
		}
		last := 0
		lastK := sortedSliceKeys[0]
		for i := 1; i < len(sortedSliceKeys); i++ {
			k := sortedSliceKeys[i]
			if cmpKey(k, lastK) != 0 {
				if !yield(lastK, coSliceVals[last:i]) {
					return
				}
				last = i
				lastK = k
			}
		}
		if !yield(lastK, coSliceVals[last:]) {
			return
		}
	}
}

func MergeSliceSorted[K any, V any](sortedSliceReadIn []K, coSliceWriteIn []V, key K, val V, cmpKey func(K, K) int, merge func(old V, new V) V) (idx int, existed bool, sortedSliceRead []K, coSliceWrite []V) {
	sortedSliceRead = sortedSliceReadIn
	coSliceWrite = coSliceWriteIn
	idx, existed = slices.BinarySearchFunc(sortedSliceRead, key, cmpKey)
	if existed {
		coSliceWrite[idx] = merge(coSliceWrite[idx], val)
	} else {
		coSliceWrite = slices.Insert(coSliceWrite, idx, val)
		sortedSliceRead = slices.Insert(sortedSliceRead, idx, key)
	}
	return
}
func InsertSliceSortedFunc[K any, V any](sortedSliceReadIn []K, coSliceWriteIn []V, key K, val V, cmpKey func(K, K) int) (idx int, existed bool, sortedSliceRead []K, coSliceWrite []V) {
	sortedSliceRead = sortedSliceReadIn
	coSliceWrite = coSliceWriteIn
	idx, existed = slices.BinarySearchFunc(sortedSliceRead, key, cmpKey)
	if existed {
		coSliceWrite[idx] = val
	} else {
		coSliceWrite = slices.Insert(coSliceWrite, idx, val)
		sortedSliceRead = slices.Insert(sortedSliceRead, idx, key)
	}
	return
}
func InsertSliceSorted[K cmp.Ordered, V any](sortedSliceReadIn []K, coSliceWriteIn []V, key K, val V) (idx int, existed bool, sortedSliceRead []K, coSliceWrite []V) {
	sortedSliceRead = sortedSliceReadIn
	coSliceWrite = coSliceWriteIn
	idx, existed = slices.BinarySearch(sortedSliceRead, key)
	if existed {
		coSliceWrite[idx] = val
	} else {
		coSliceWrite = slices.Insert(coSliceWrite, idx, val)
		sortedSliceRead = slices.Insert(sortedSliceRead, idx, key)
	}
	return
}

// CoIterateFilter yields, for every element of s1 equal to v, the
// source index i and the co-indexed value s2[i], in source order.
// Requires len(s2) >= len(s1): a shorter s2 panics at the first match
// beyond its length.
func CoIterateFilter[K comparable, V any](s1 []K, v K, s2 []V) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		for i, u := range s1 {
			if u == v {
				if !yield(i, s2[i]) {
					return
				}
			}
		}
	}
}

// CoIterateFilterFunc yields, for every element of s1 accepted by
// filterFunc, the source index i and the co-indexed value s2[i], in
// source order. Requires len(s2) >= len(s1): a shorter s2 panics at the
// first match beyond its length.
func CoIterateFilterFunc[K any, V any](s1 []K, filterFunc func(a K) (keep bool), s2 []V) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		for i, u := range s1 {
			if filterFunc(u) {
				if !yield(i, s2[i]) {
					return
				}
			}
		}
	}
}
