// Package slidingwindow provides a fixed-capacity, drop-oldest ring buffer used
// by the runtime-dashboard samplers (imzrt, imztop) to retain a bounded history
// of per-tick metric values for plotting.
//
// It was lifted out of the two apps, which each held a verbatim copy, per
// ADR-0061 SD13 (open question 3).
package slidingwindow

// Window is a fixed-capacity buffer that drops the oldest value on overflow.
// Push is O(1) until full, then O(cap) per push: it memmoves the backing slice
// down one slot rather than tracking a head index. At a 1 Hz sampler cadence
// against cap≈600 that is a few KB/s of memcpy on 8-byte floats — negligible. A
// true head+length ring (O(1) per push) is a defensible upgrade if profiling
// ever flags it; see ADR-0020 §SD5 and ADR-0061 SD13.
//
// Values returns the backing slice in chronological order (oldest first); it
// aliases the backing array, so callers needing a stable view across mutation
// must copy.
//
// Concurrency: a Window is not safe for concurrent use. The typical owner is a
// single sampler that copies values into a published snapshot each tick.
type Window[T any] struct {
	data []T
	cap  int32
}

// New returns an empty Window holding at most capacity values (clamped to a
// minimum of 1).
func New[T any](capacity int32) (inst *Window[T]) {
	if capacity < 1 {
		capacity = 1
	}
	inst = &Window[T]{
		data: make([]T, 0, capacity),
		cap:  capacity,
	}
	return
}

// Cap returns the maximum number of values the window retains.
func (inst *Window[T]) Cap() (n int32) {
	n = inst.cap
	return
}

// Len returns the number of values currently held (0..Cap).
func (inst *Window[T]) Len() (n int32) {
	n = int32(len(inst.data))
	return
}

// Push appends v, dropping the oldest value once the window is full.
func (inst *Window[T]) Push(v T) {
	if int32(len(inst.data)) < inst.cap {
		inst.data = append(inst.data, v)
		return
	}
	copy(inst.data, inst.data[1:])
	inst.data[len(inst.data)-1] = v
}

// Values returns the held values in chronological order (oldest first). The
// returned slice aliases the backing array.
func (inst *Window[T]) Values() (out []T) {
	out = inst.data
	return
}
