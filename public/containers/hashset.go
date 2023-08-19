package containers

type HashSet[T comparable] struct {
	data map[T]struct{}
}

func NewHashSet[T comparable](estimatedCard int) *HashSet[T] {
	return &HashSet[T]{data: make(map[T]struct{}, estimatedCard)}
}

func (inst *HashSet[T]) Add(val T) {
	inst.data[val] = struct{}{}
}

func (inst *HashSet[T]) Remove(val T) {
	delete(inst.data, val)
}

func (inst *HashSet[T]) Has(val T) bool {
	_, h := inst.data[val]
	return h
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

func (inst *HashSet[T]) Slice() []T {
	r := make([]T, 0, inst.Size())
	inst.ForEach(func(v T) {
		r = append(r, v)
	})
	return r
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
