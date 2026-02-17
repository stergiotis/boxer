//go:build llm_generated_gemini3pro

package caching

/*
This uses a Go map for O(1) access.
To handle eviction without scanning, it uses a simplified random eviction (Go map iteration order is random).
Best for: Large Stashes (> 1,000 items) where scan time is non-negotiable.
*/

type MapStash[K comparable, V any] struct {
	data     map[K]V
	capacity int
}

func NewMapStash[K comparable, V any](capacity int) *MapStash[K, V] {
	return &MapStash[K, V]{
		data:     make(map[K]V, capacity),
		capacity: capacity,
	}
}

func (s *MapStash[K, V]) GetAndRemove(key K) (V, bool) {
	val, ok := s.data[key]
	if ok {
		delete(s.data, key)
	}
	return val, ok
}

func (s *MapStash[K, V]) Add(key K, value V) bool {
	if len(s.data) < s.capacity {
		s.data[key] = value
		return false
	}

	// Check if updating existing
	if _, exists := s.data[key]; exists {
		s.data[key] = value
		return false
	}

	// Needs Eviction. Pick a random victim.
	// Since Go map iteration is randomized, taking the "first" item is random.
	for victimKey := range s.data {
		delete(s.data, victimKey)
		break // Evict one
	}

	s.data[key] = value
	return true
}

func (s *MapStash[K, V]) Delete(key K) {
	delete(s.data, key)
}
func (s *MapStash[K, V]) Len() int { return len(s.data) }
func (s *MapStash[K, V]) Cap() int { return s.capacity }
