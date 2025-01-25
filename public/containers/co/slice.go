package co

import (
	"cmp"
	"iter"
	"slices"
)

func IterateSliceGrouped[K any, V any](sortedSliceKeys []K, coSliceVals []V, cmpKey func(K, K) int) iter.Seq2[K, []V] {
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
