//go:build llm_generated_gemini3pro

package caching

/*
(Memory Dense)
 It is CPU-heavy (O(N) scan) but extremely memory-efficient (dense arrays, minimal pointers).
Best for: Small Stashes (< 1,000 items) or simple scalar types.
*/

type SliceStash[K comparable, V any] struct {
	keys     []K
	values   []V
	evictPtr int
	capacity int
}

func NewSliceStash[K comparable, V any](capacity int) *SliceStash[K, V] {
	return &SliceStash[K, V]{
		keys:     make([]K, 0, capacity),
		values:   make([]V, 0, capacity),
		capacity: capacity,
	}
}

func (s *SliceStash[K, V]) GetAndRemove(key K) (V, bool) {
	var zero V
	// Linear Scan
	for i, k := range s.keys {
		if k == key {
			val := s.values[i]
			// Swap Remove
			lastIdx := len(s.keys) - 1
			s.keys[i] = s.keys[lastIdx]
			s.values[i] = s.values[lastIdx]
			s.keys = s.keys[:lastIdx]
			s.values = s.values[:lastIdx]
			return val, true
		}
	}
	return zero, false
}

func (s *SliceStash[K, V]) Add(key K, value V) bool {
	if len(s.keys) < s.capacity {
		s.keys = append(s.keys, key)
		s.values = append(s.values, value)
		return false
	}
	// Round-Robin Eviction
	s.keys[s.evictPtr] = key
	s.values[s.evictPtr] = value
	s.evictPtr = (s.evictPtr + 1) % s.capacity
	return true
}

func (s *SliceStash[K, V]) Delete(key K) {
	s.GetAndRemove(key) // Reuse logic
}

func (s *SliceStash[K, V]) Len() int { return len(s.keys) }
func (s *SliceStash[K, V]) Cap() int { return s.capacity }
