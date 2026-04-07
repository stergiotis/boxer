package processor

import "sync"

func NewSlicePool[T any](capacity int) *SlicePool[T] {
	return &SlicePool[T]{
		capacity: capacity,
		internal: &sync.Pool{
			New: func() any {
				// Pre-allocate slice with desired capacity
				return make([]T, 0, capacity)
			},
		},
	}
}

func (inst *SlicePool[T]) Get() []T {
	// Type assertion is safe here because New is strictly controlled
	return inst.internal.Get().([]T)
}

func (inst *SlicePool[T]) Put(s []T) {
	// Reset length to 0, keep capacity.
	// This prevents data leakage between users of the pool.
	// Note: We intentionally do not nil out the elements for performance,
	// assuming T does not contain pointers that need GC (like our HnComment struct).
	// If T contains pointers, you should loop and nil them here.
	s = s[:0]
	inst.internal.Put(s)
}
