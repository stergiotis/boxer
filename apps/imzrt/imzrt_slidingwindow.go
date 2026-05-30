//go:build llm_generated_opus48

package imzrt

// SlidingWindow is a fixed-capacity buffer that drops the oldest value on
// overflow. Push is O(1) until full, then O(cap) per push (it memmoves the slice
// down one slot rather than tracking a head index). At the sampler's 1Hz cadence
// against cap≈600 that is a few KB/s of memcpy — negligible.
//
// This is a deliberate verbatim copy of imztop's SlidingWindow (apps/imztop/
// imztop_slidingwindow.go). ADR-0061 SD13 sanctions the duplication for M1 and
// tracks lifting the type into a shared package as a later tidy-up (open
// question 3) — extracting it now would touch imztop and is out of M1's scope.
//
// Concurrency: not safe for concurrent use. The sampler owns each window
// exclusively and copies values into the published snapshot every tick.
type SlidingWindow[T any] struct {
	data []T
	cap  int32
}

func NewSlidingWindow[T any](capacity int32) (inst *SlidingWindow[T]) {
	if capacity < 1 {
		capacity = 1
	}
	inst = &SlidingWindow[T]{
		data: make([]T, 0, capacity),
		cap:  capacity,
	}
	return
}

func (inst *SlidingWindow[T]) Cap() (n int32) {
	n = inst.cap
	return
}

func (inst *SlidingWindow[T]) Len() (n int32) {
	n = int32(len(inst.data))
	return
}

func (inst *SlidingWindow[T]) Push(v T) {
	if int32(len(inst.data)) < inst.cap {
		inst.data = append(inst.data, v)
		return
	}
	copy(inst.data, inst.data[1:])
	inst.data[len(inst.data)-1] = v
}

func (inst *SlidingWindow[T]) Values() (out []T) {
	out = inst.data
	return
}
