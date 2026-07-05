package caching

import "github.com/rs/zerolog/log"

/*
(Memory Dense)
CPU-heavy (O(N) scans on every operation, including Add's update-in-place
check) but extremely memory-efficient (dense arrays, minimal pointers).
Best for: Small Stashes (< 1,000 items) or simple scalar types.
*/

type SliceStash[K comparable, V any] struct {
	keys     []K
	entries  []StashEntry[V]
	evictPtr int
	capacity int
}

func NewSliceStash[K comparable, V any](capacity int) *SliceStash[K, V] {
	if capacity < 1 {
		log.Panic().Int("capacity", capacity).Msg("caching: NewSliceStash requires capacity >= 1")
	}
	return &SliceStash[K, V]{
		keys:     make([]K, 0, capacity),
		entries:  make([]StashEntry[V], 0, capacity),
		capacity: capacity,
	}
}

func (s *SliceStash[K, V]) GetAndRemove(key K) (e StashEntry[V], found bool) {
	// Linear Scan
	for i, k := range s.keys {
		if k == key {
			e = s.entries[i]
			// Swap Remove
			lastIdx := len(s.keys) - 1
			s.keys[i] = s.keys[lastIdx]
			s.entries[i] = s.entries[lastIdx]
			s.keys = s.keys[:lastIdx]
			s.entries = s.entries[:lastIdx]
			return e, true
		}
	}
	return e, false
}

func (s *SliceStash[K, V]) Add(key K, e StashEntry[V]) bool {
	// Update-in-place: an existing key is overwritten, never duplicated,
	// and an update never evicts (contract).
	for i, k := range s.keys {
		if k == key {
			s.entries[i] = e
			return false
		}
	}
	if len(s.keys) < s.capacity {
		s.keys = append(s.keys, key)
		s.entries = append(s.entries, e)
		return false
	}
	// Round-Robin Eviction
	s.keys[s.evictPtr] = key
	s.entries[s.evictPtr] = e
	s.evictPtr = (s.evictPtr + 1) % s.capacity
	return true
}

func (s *SliceStash[K, V]) Delete(key K) {
	s.GetAndRemove(key) // Reuse logic
}

func (s *SliceStash[K, V]) Len() int { return len(s.keys) }
func (s *SliceStash[K, V]) Cap() int { return s.capacity }

func (s *SliceStash[K, V]) Clear() {
	s.keys = s.keys[:0]
	s.entries = s.entries[:0]
	s.evictPtr = 0
}
