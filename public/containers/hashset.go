package containers

import (
	"iter"

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

func (inst *HashSet[T]) Has(val T) bool {
	_, h := inst.data[val]
	return h
}
func (inst *HashSet[T]) All() iter.Seq[T] {
	return func(yield func(T) bool) {
		for m, _ := range inst.data {
			if !yield(m) {
				return
			}
		}
	}
}

func (inst *HashSet[T]) ForEach(handler func(v T)) {
	for v, _ := range inst.data {
		handler(v)
	}
}

func (inst *HashSet[T]) Until(handler func(v T) bool) {
	for v, _ := range inst.data {
		if handler(v) {
			return
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
	if in == nil || cap(in) < inst.Size() {
		out = make([]T, 0, inst.Size())
	}
	inst.ForEach(func(v T) {
		out = append(out, v)
	})
	return out
}

func (inst *HashSet[T]) Size() int {
	return len(inst.data)
}

func (inst *HashSet[T]) UnionMod(other *HashSet[T]) {
	other.ForEach(func(v T) {
		inst.Add(v)
	})
}

func (inst *HashSet[T]) DifferenceMod(other *HashSet[T]) {
	other.ForEach(func(v T) {
		inst.Remove(v)
	})
}

func (inst *HashSet[T]) IntersectMod(other *HashSet[T]) {
	n := make(map[T]struct{}, len(inst.data))
	other.ForEach(func(v T) {
		if inst.Has(v) {
			n[v] = struct{}{}
		}
	})
	inst.data = n
}

func (inst *HashSet[T]) Equal(other HashSet[T]) bool {
	sa := inst.Size()
	sb := other.Size()
	if sa == sb {
		l := 0
		other.Until(func(v T) bool {
			if inst.Has(v) {
				l++
				return false
			}
			return true
		})
		return l == sa
	}
	return false
}
