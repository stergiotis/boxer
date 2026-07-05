package caching

import "github.com/rs/zerolog/log"

/*
This uses a Go map for O(1) access.
To handle eviction without scanning, it uses a simplified random eviction (Go map iteration order is random).
Best for: Large Stashes (> 1,000 items) where scan time is non-negotiable.
*/

type MapStash[K comparable, V any] struct {
	data     map[K]StashEntry[V]
	capacity int
}

func NewMapStash[K comparable, V any](capacity int) *MapStash[K, V] {
	if capacity < 1 {
		log.Panic().Int("capacity", capacity).Msg("caching: NewMapStash requires capacity >= 1")
	}
	return &MapStash[K, V]{
		data:     make(map[K]StashEntry[V], capacity),
		capacity: capacity,
	}
}

func (s *MapStash[K, V]) GetAndRemove(key K) (e StashEntry[V], found bool) {
	e, found = s.data[key]
	if found {
		delete(s.data, key)
	}
	return e, found
}

func (s *MapStash[K, V]) Add(key K, e StashEntry[V]) bool {
	// Update-in-place: an existing key is overwritten and never evicts
	// (contract), regardless of the fill level.
	if _, exists := s.data[key]; exists {
		s.data[key] = e
		return false
	}
	if len(s.data) < s.capacity {
		s.data[key] = e
		return false
	}

	// Needs Eviction. Pick a random victim.
	// Since Go map iteration is randomized, taking the "first" item is random.
	for victimKey := range s.data {
		delete(s.data, victimKey)
		break // Evict one
	}

	s.data[key] = e
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
