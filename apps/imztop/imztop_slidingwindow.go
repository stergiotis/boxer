//go:build llm_generated_opus47

package imztop

// SlidingWindow is a fixed-capacity buffer that drops the oldest value
// on overflow. Push is O(1) until full, then O(cap) per push because
// the implementation memmoves the data slice down by one slot rather
// than tracking a head index. At the sampler's 1Hz cadence against
// cap≈600 this is ~5 KB/s of memcpy on 8-byte floats — negligible. A
// true head+length ring (O(1) per push) is a defensible upgrade if
// profiling ever flags it; see ADR-0020 §SD5 plus the 2026-05-21
// honesty update.
//
// Values returns the slice in chronological order (oldest first). The
// returned slice aliases the backing array; callers that need a
// stable view across mutation must copy.
//
// Concurrency: SlidingWindow is not safe for concurrent use. The sampler
// owns each window exclusively and copies values into the published
// snapshot on each tick.
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
