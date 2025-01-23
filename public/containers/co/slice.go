package co

import (
	"cmp"
	"slices"
)

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
