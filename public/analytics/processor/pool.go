package processor

import "sync"

func NewSlicePool[T any](capacity int) *SlicePool[T] {
	return &SlicePool[T]{
		capacity: capacity,
		internal: &sync.Pool{
			New: func() any {
				// Pre-allocate slice with desired capacity.
				// Stored as *[]T so sync.Pool sees a pointer-like value and
				// avoids an allocation on every Put (staticcheck SA6002).
				s := make([]T, 0, capacity)
				return &s
			},
		},
	}
}

func (inst *SlicePool[T]) Get() []T {
	return *inst.internal.Get().(*[]T)
}

func (inst *SlicePool[T]) Put(s []T) {
	// Reset length to 0, keep capacity, to prevent data leakage between users.
	// Note: We intentionally do not nil out the elements for performance,
	// assuming T does not contain pointers that need GC (like our HnComment struct).
	// If T contains pointers, you should loop and nil them here.
	s = s[:0]
	inst.internal.Put(&s)
}
