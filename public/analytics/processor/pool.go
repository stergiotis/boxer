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
	// Drop slices that have grown well past the configured capacity, so the
	// pool's memory footprint stays bounded under spiky batch sizes.
	if inst.capacity > 0 && cap(s) > 2*inst.capacity {
		return
	}
	// Reset length to 0, keep capacity. Elements are not zeroed; if T holds
	// pointers and you need GC to reclaim what they reference, clear them
	// before calling Put.
	s = s[:0]
	inst.internal.Put(&s)
}
