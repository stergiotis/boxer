package caching

import "github.com/rs/zerolog/log"

/*
This uses a Go map for O(1) access.
To handle eviction without scanning, it uses a simplified random eviction (Go map iteration order is random).
Best for: Large Stashes (> 1,000 items) where scan time is non-negotiable.
*/

type mapStashEntry[V any] struct {
	value V
	stale bool
}

type MapStash[K comparable, V any] struct {
	data     map[K]mapStashEntry[V]
	capacity int
}

func NewMapStash[K comparable, V any](capacity int) *MapStash[K, V] {
	if capacity < 1 {
		log.Panic().Int("capacity", capacity).Msg("caching: NewMapStash requires capacity >= 1")
	}
	return &MapStash[K, V]{
		data:     make(map[K]mapStashEntry[V], capacity),
		capacity: capacity,
	}
}

func (s *MapStash[K, V]) GetAndRemove(key K) (value V, stale bool, found bool) {
	e, ok := s.data[key]
	if !ok {
		return value, false, false
	}
	delete(s.data, key)
	return e.value, e.stale, true
}

func (s *MapStash[K, V]) Add(key K, value V, stale bool) bool {
	// Update-in-place: an existing key is overwritten and never evicts
	// (contract), regardless of the fill level.
	if _, exists := s.data[key]; exists {
		s.data[key] = mapStashEntry[V]{value: value, stale: stale}
		return false
	}
	if len(s.data) < s.capacity {
		s.data[key] = mapStashEntry[V]{value: value, stale: stale}
		return false
	}

	// Needs Eviction. Pick a random victim.
	// Since Go map iteration is randomized, taking the "first" item is random.
	for victimKey := range s.data {
		delete(s.data, victimKey)
		break // Evict one
	}

	s.data[key] = mapStashEntry[V]{value: value, stale: stale}
	return true
}

func (s *MapStash[K, V]) Delete(key K) {
	delete(s.data, key)
}

func (s *MapStash[K, V]) Len() int { return len(s.data) }
func (s *MapStash[K, V]) Cap() int { return s.capacity }

func (s *MapStash[K, V]) Clear() {
	clear(s.data)
}
