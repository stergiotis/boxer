package containers

import (
	"sort"

	"iter"
	"slices"

	"github.com/stergiotis/boxer/public/containers/co"
)

type BinarySearchGrowingKV[K any, V any] struct {
	keys      []K
	vals      []V
	cmpKey    func(a K, b K) int
	sorted    bool
	compacted bool
}

func (inst *BinarySearchGrowingKV[K, V]) IterateKeys() iter.Seq[K] {
	return func(yield func(K) bool) {
		for _, k := range inst.keys {
			if !yield(k) {
				return
			}
		}
	}
}
func (inst *BinarySearchGrowingKV[K, V]) IterateValues() iter.Seq[V] {
	return func(yield func(V) bool) {
		for _, v := range inst.vals {
			if !yield(v) {
				return
			}
		}
	}
}
func (inst *BinarySearchGrowingKV[K, V]) Iterate() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		vals := inst.vals
		for i, k := range inst.keys {
			if !yield(k, vals[i]) {
				return
			}
		}
	}
}

func (inst *BinarySearchGrowingKV[K, V]) Len() int {
	return len(inst.keys)
}

func (inst *BinarySearchGrowingKV[K, V]) Less(i, j int) bool {
	keys := inst.keys
	return inst.cmpKey(keys[i], keys[j]) < 0
}

func (inst *BinarySearchGrowingKV[K, V]) Swap(i, j int) {
	keys := inst.keys
	vals := inst.vals
	keys[j], keys[i] = keys[i], keys[j]
	vals[j], vals[i] = vals[i], vals[j]
}

func NewBinarySearchGrowingKV[K any, V any](estSize int, cmpKey func(a K, b K) int) (inst *BinarySearchGrowingKV[K, V]) {
	inst = &BinarySearchGrowingKV[K, V]{
		keys:      make([]K, 0, estSize),
		vals:      make([]V, 0, estSize),
		cmpKey:    cmpKey,
		sorted:    true,
		compacted: true,
	}
	return
}

func (inst *BinarySearchGrowingKV[K, V]) ensureSorted() {
	if !inst.sorted {
		sort.Stable(inst)
		inst.sorted = true
	}
	if !inst.compacted {
		// FIXME this is ugly and most likely slow, rewrite compact to keep last instance
		slices.Reverse(inst.keys)
		slices.Reverse(inst.vals)
		inst.compactOldestWins()
		slices.Reverse(inst.keys)
		slices.Reverse(inst.vals)
		inst.compacted = true
	}
}

// compactOldestWins adopted from go stdlib slices.Compact()
func (inst *BinarySearchGrowingKV[K, V]) compactOldestWins() {
	keys := inst.keys
	vals := inst.vals
	if len(keys) < 2 {
		return
	}
	cmp := inst.cmpKey
	for k := 1; k < len(keys); k++ {
		if cmp(keys[k], keys[k-1]) != 0 {
			continue
		}

		keys2 := keys[k:]
		vals2 := vals[k:]
		for k2 := 1; k2 < len(keys2); k2++ {
			if cmp(keys2[k2], keys2[k2-1]) != 0 {
				keys[k] = keys2[k2]
				vals[k] = vals2[k2] // Stable sorted, keeps last value
				k++
			}
		}

		clear(keys[k:])
		clear(vals[k:])
		inst.keys = keys[:k]
		inst.vals = vals[:k]
		return
	}
}

func (inst *BinarySearchGrowingKV[K, V]) compactYoungestWins() {
	slices.Reverse(inst.keys)
	slices.Reverse(inst.vals)
	inst.compactOldestWins()
	slices.Reverse(inst.keys)
	slices.Reverse(inst.vals)
}

func (inst *BinarySearchGrowingKV[K, V]) Has(key K) (has bool) {
	inst.ensureSorted()
	_, has = slices.BinarySearchFunc(inst.keys, key, inst.cmpKey)
	return
}

func (inst *BinarySearchGrowingKV[K, V]) Get(key K) (val V, has bool) {
	inst.ensureSorted()
	var idx int
	idx, has = slices.BinarySearchFunc(inst.keys, key, inst.cmpKey)
	if has {
		val = inst.vals[idx]
	}
	return
}

func (inst *BinarySearchGrowingKV[K, V]) GetDefault(key K, defaultV V) (val V) {
	inst.ensureSorted()
	idx, has := slices.BinarySearchFunc(inst.keys, key, inst.cmpKey)
	if has {
		val = inst.vals[idx]
	} else {
		val = defaultV
	}
	return
}
func (inst *BinarySearchGrowingKV[K, V]) MergeValue(key K, val V, merge func(old V, new V) V) (existed bool) {
	inst.ensureSorted()
	_, existed, inst.keys, inst.vals = co.MergeSliceSorted(inst.keys, inst.vals, key, val, inst.cmpKey, merge)
	return
}

func (inst *BinarySearchGrowingKV[K, V]) UpsertSingle(key K, val V) (existed bool) {
	inst.ensureSorted()
	_, existed, inst.keys, inst.vals = co.InsertSliceSortedFunc(inst.keys, inst.vals, key, val, inst.cmpKey)
	return
}

// UpsertBatch last write wins
func (inst *BinarySearchGrowingKV[K, V]) UpsertBatch(key K, val V) {
	inst.keys = append(inst.keys, key)
	inst.vals = append(inst.vals, val)
	inst.compacted = false
	inst.sorted = false
}

func (inst *BinarySearchGrowingKV[K, V]) Pairs() iter.Seq2[K, V] {
	inst.ensureSorted()
	return func(yield func(K, V) bool) {
		keys := inst.keys
		vals := inst.vals
		for i, k := range keys {
			if !yield(k, vals[i]) {
				return
			}
		}
	}
}

func (inst *BinarySearchGrowingKV[K, V]) Keys() iter.Seq[K] {
	inst.ensureSorted()
	return func(yield func(K) bool) {
		keys := inst.keys
		for _, k := range keys {
			if !yield(k) {
				return
			}
		}
	}
}

func (inst *BinarySearchGrowingKV[K, V]) Values() iter.Seq[V] {
	inst.ensureSorted()
	return func(yield func(V) bool) {
		vals := inst.vals
		for _, v := range vals {
			if !yield(v) {
				return
			}
		}
	}
}

func (inst *BinarySearchGrowingKV[K, V]) Reset() {
	inst.sorted = true
	inst.compacted = true
	clear(inst.keys)
	clear(inst.vals)
	inst.keys = inst.keys[:0]
	inst.vals = inst.vals[:0]
}

var _ sort.Interface = (*BinarySearchGrowingKV[any, any])(nil)
