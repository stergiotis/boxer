package processor

import "sync"

func NewSlicePool[T any](capacity int, opts ...SlicePoolOption[T]) *SlicePool[T] {
	p := &SlicePool[T]{
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
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// WithZeroOnPut configures the pool to clear slice elements before
// returning the slice to the pool. Use this when T contains pointers
// (or string / slice / interface / map values) and you want the
// references to be released for GC instead of being retained in the
// pool's reused backing array.
func WithZeroOnPut[T any]() SlicePoolOption[T] {
	return func(p *SlicePool[T]) {
		p.zero = true
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
	if inst.zero {
		clear(s)
	}
	s = s[:0]
	inst.internal.Put(&s)
}
