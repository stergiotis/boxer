package co

import (
	"cmp"
	"iter"
	"slices"
	"sort"
)

type coSorter[K cmp.Ordered] struct {
	swap  func(i int, j int)
	slice []K
}

func (inst *coSorter[T]) Len() int {
	return len(inst.slice)
}

func (inst *coSorter[T]) Less(i, j int) bool {
	slice := inst.slice
	return slice[i] < slice[j]
}

func (inst *coSorter[T]) Swap(i, j int) {
	inst.swap(i, j)
	slice := inst.slice
	slice[j], slice[i] = slice[i], slice[j]
}

var _ sort.Interface = (*coSorter[int])(nil)

type coSorterReverse[K cmp.Ordered] struct {
	swap  func(i int, j int)
	slice []K
}

func (inst *coSorterReverse[T]) Len() int {
	return len(inst.slice)
}

func (inst *coSorterReverse[T]) Less(i, j int) bool {
	slice := inst.slice
	return slice[i] > slice[j]
}

func (inst *coSorterReverse[T]) Swap(i, j int) {
	inst.swap(i, j)
	slice := inst.slice
	slice[j], slice[i] = slice[i], slice[j]
}

var _ sort.Interface = (*coSorterReverse[int])(nil)

type sorter struct {
	n    int
	less func(i, j int) bool
	swap func(i, j int)
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
func CoSortSlices[K cmp.Ordered](slice []K, swap func(i int, j int)) {
	switch len(slice) {
	case 0, 1:
		return
	}
	if swap == nil {
		slices.Sort(slice)
	} else {
		s := &coSorter[K]{
			slice: slice,
			swap:  swap,
		}
		sort.Sort(s)
	}
}
func CoSortSlicesReverse[K cmp.Ordered](slice []K, swap func(i int, j int)) {
	switch len(slice) {
	case 0, 1:
		return
	}
	s := &coSorterReverse[K]{
		slice: slice,
		swap:  swap,
	}
	sort.Sort(s)
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
		var dummyV V
		var dummyK K
		coSliceWrite = append(coSliceWrite, dummyV)
		copy(coSliceWrite[idx+1:], coSliceWrite[idx:])
		coSliceWrite[idx] = val

		sortedSliceRead = append(sortedSliceRead, dummyK)
		copy(sortedSliceRead[idx+1:], sortedSliceRead[idx:])
		sortedSliceRead[idx] = key
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
		var dummyV V
		var dummyK K
		coSliceWrite = append(coSliceWrite, dummyV)
		copy(coSliceWrite[idx+1:], coSliceWrite[idx:])
		coSliceWrite[idx] = val

		sortedSliceRead = append(sortedSliceRead, dummyK)
		copy(sortedSliceRead[idx+1:], sortedSliceRead[idx:])
		sortedSliceRead[idx] = key
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
		var dummyV V
		var dummyK K
		coSliceWrite = append(coSliceWrite, dummyV)
		copy(coSliceWrite[idx+1:], coSliceWrite[idx:])
		coSliceWrite[idx] = val

		sortedSliceRead = append(sortedSliceRead, dummyK)
		copy(sortedSliceRead[idx+1:], sortedSliceRead[idx:])
		sortedSliceRead[idx] = key
	}
	return
}
func CoIterateFilter[K comparable, V any](s1 []K, v K, s2 []V) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		n := 0
		for i, u := range s1 {
			if u == v {
				if !yield(n, s2[i]) {
					return
				}
				n++
			}
		}
	}
}
func CoIterateFilterFunc[K any, V any](s1 []K, filterFunc func(a K) (keep bool), s2 []V) iter.Seq2[int, V] {
	return func(yield func(int, V) bool) {
		n := 0
		for i, u := range s1 {
			if filterFunc(u) {
				if !yield(n, s2[i]) {
					return
				}
				n++
			}
		}
	}
}
func StripIter2Key[K, V any](iter2 iter.Seq2[K, V]) iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range iter2 {
			if !yield(v) {
				return
			}
		}
	}
}
func StripIter2Value[K, V any](iter2 iter.Seq2[K, V]) iter.Seq[K] {
	return func(yield func(K) bool) {
		for k, _ := range iter2 {
			if !yield(k) {
				return
			}
		}
	}
}
func MakeIter2FromIter1[K, V any](iter1 iter.Seq[V], k K) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for v := range iter1 {
			if !yield(k, v) {
				return
			}
		}
	}
}
func MakeIter2FromIter1Func[K, V any](iter1 iter.Seq[V], f func(v V) (k K)) iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for v := range iter1 {
			if !yield(f(v), v) {
				return
			}
		}
	}
}
