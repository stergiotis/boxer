package containers

import (
	"iter"
	"slices"

	"golang.org/x/exp/maps"
)

type HashSet[T comparable] struct {
	data map[T]struct{}
}

func NewHashSet[T comparable](estimatedCard int) *HashSet[T] {
	return &HashSet[T]{data: make(map[T]struct{}, estimatedCard)}
}

func (inst *HashSet[T]) AddEx(val T) (existed bool) {
	if inst.Has(val) {
		existed = true
		return
	}
	inst.Add(val)
	return
}
func (inst *HashSet[T]) Add(val T) {
	inst.data[val] = struct{}{}
}
func (inst *HashSet[T]) AddMany(vals iter.Seq[T]) (added int) {
	for v := range vals {
		inst.data[v] = struct{}{}
		added++
	}
	return
}
func (inst *HashSet[T]) AddExMany(vals iter.Seq[T]) (existing int, nonExisting int) {
	for v := range vals {
		_, has := inst.data[v]
		if has {
			existing++
		} else {
			inst.data[v] = struct{}{}
			nonExisting++
		}
	}
	return
}

func (inst *HashSet[T]) Remove(val T) {
	delete(inst.data, val)
}
func (inst *HashSet[T]) RemoveEx(val T) (had bool) {
	had = inst.Has(val)
	delete(inst.data, val)
	return
}

func (inst *HashSet[T]) Has(val T) bool {
	_, h := inst.data[val]
	return h
}
func (inst *HashSet[T]) IterateAll() iter.Seq[T] {
	return func(yield func(T) bool) {
		for m, _ := range inst.data {
			if !yield(m) {
				return
			}
		}
	}
}

func (inst *HashSet[T]) Clear() {
	maps.Clear(inst.data)
}
func (inst *HashSet[T]) Slice() []T {
	return inst.SliceEx(nil)
}
func (inst *HashSet[T]) SliceEx(in []T) (out []T) {
	out = slices.Grow(in, len(inst.data))
	for k, _ := range inst.data {
		out = append(out, k)
	}
	return
}

func (inst *HashSet[T]) Size() int {
	return len(inst.data)
}

func (inst *HashSet[T]) UnionMod(other *HashSet[T]) {
	maps.Copy(inst.data, other.data)
}

func (inst *HashSet[T]) DifferenceMod(other *HashSet[T]) {
	for v := range other.IterateAll() {
		inst.Remove(v)
	}
}

func (inst *HashSet[T]) IntersectMod(other *HashSet[T]) {
	n := make(map[T]struct{}, len(inst.data))
	for v := range other.IterateAll() {
		if inst.Has(v) {
			n[v] = struct{}{}
		}
	}
	inst.data = n
}

func (inst *HashSet[T]) Equal(other HashSet[T]) bool {
	sa := inst.Size()
	sb := other.Size()
	if sa == sb {
		return maps.Equal(inst.data, other.data)
	}
	return false
}
