package caching

import "github.com/rs/zerolog/log"

/*
S3-FIFO-derived victim cache (Yang et al., SOSP '23), adapted to the
StashBackendI victim-cache semantics: entries leave on their FIRST hit
(GetAndRemove promotes to L1), so the in-queue access bits of the original
are dead weight here — the signal that remains is REINSERTION. A key that
was evicted from the probationary small queue and comes back (the
demote/drop/refetch bounce) is a returning object and enters the main
queue directly, via the ghost set of recently evicted keys.

Two adaptations from the paper, both deliberate:
  - Lazy eviction at TOTAL capacity (the paper evicts when the small queue
    fills): an exact-capacity cache must hold cap entries before dropping
    any (the stash conformance contract).
  - Main-queue evictions do not ghost: with promote-on-hit there is no
    in-main access frequency to defend; the ghost exists to route bounces.

The ghost is key-only metadata (capacity entries) and must never be
confused with the cache's negative-cache table: a ghost hit means "admit
to main", an absent mark means "do not fetch".
*/

type s3Node[K comparable, V any] struct {
	key        K
	entry      StashEntry[V]
	prev, next *s3Node[K, V]
	inMain     bool
}

type S3FIFOStash[K comparable, V any] struct {
	nodes     map[K]*s3Node[K, V]
	small     s3List[K, V] // probationary FIFO (new keys)
	main      s3List[K, V] // returning keys (ghost hits)
	ghost     map[K]struct{}
	ghostFifo []K
	ghostPtr  int
	capacity  int
}

// s3List is a minimal intrusive deque with sentinel-free head/tail.
type s3List[K comparable, V any] struct {
	head, tail *s3Node[K, V]
	n          int
}

func (l *s3List[K, V]) pushHead(nd *s3Node[K, V]) {
	nd.prev, nd.next = nil, l.head
	if l.head != nil {
		l.head.prev = nd
	} else {
		l.tail = nd
	}
	l.head = nd
	l.n++
}

func (l *s3List[K, V]) unlink(nd *s3Node[K, V]) {
	if nd.prev != nil {
		nd.prev.next = nd.next
	} else {
		l.head = nd.next
	}
	if nd.next != nil {
		nd.next.prev = nd.prev
	} else {
		l.tail = nd.prev
	}
	nd.prev, nd.next = nil, nil
	l.n--
}

func NewS3FIFOStash[K comparable, V any](capacity int) *S3FIFOStash[K, V] {
	if capacity < 1 {
		log.Panic().Int("capacity", capacity).Msg("caching: NewS3FIFOStash requires capacity >= 1")
	}
	return &S3FIFOStash[K, V]{
		nodes:     make(map[K]*s3Node[K, V], capacity),
		ghost:     make(map[K]struct{}, capacity),
		ghostFifo: make([]K, 0, capacity),
		capacity:  capacity,
	}
}

func (s *S3FIFOStash[K, V]) listOf(nd *s3Node[K, V]) *s3List[K, V] {
	if nd.inMain {
		return &s.main
	}
	return &s.small
}

func (s *S3FIFOStash[K, V]) GetAndRemove(key K) (e StashEntry[V], found bool) {
	nd, ok := s.nodes[key]
	if !ok {
		return e, false
	}
	s.listOf(nd).unlink(nd)
	delete(s.nodes, key)
	// A promotion is a success, not an eviction: no ghost credit.
	return nd.entry, true
}

func (s *S3FIFOStash[K, V]) ghostAdd(key K) {
	if _, in := s.ghost[key]; in {
		return
	}
	if len(s.ghostFifo) < s.capacity {
		s.ghostFifo = append(s.ghostFifo, key)
	} else {
		delete(s.ghost, s.ghostFifo[s.ghostPtr])
		s.ghostFifo[s.ghostPtr] = key
		s.ghostPtr = (s.ghostPtr + 1) % s.capacity
	}
	s.ghost[key] = struct{}{}
}

// evictOne drops exactly one resident entry: the probationary tail when
// the small queue is non-empty (its key ghosts — a return earns main
// residency), otherwise the main tail.
func (s *S3FIFOStash[K, V]) evictOne() {
	if s.small.tail != nil {
		victim := s.small.tail
		s.small.unlink(victim)
		delete(s.nodes, victim.key)
		s.ghostAdd(victim.key)
		return
	}
	if s.main.tail != nil {
		victim := s.main.tail
		s.main.unlink(victim)
		delete(s.nodes, victim.key)
	}
}

func (s *S3FIFOStash[K, V]) Add(key K, e StashEntry[V]) bool {
	// Update-in-place: an existing key is overwritten, never duplicated,
	// never moved, and an update never evicts (contract).
	if nd, ok := s.nodes[key]; ok {
		nd.entry = e
		return false
	}
	evicted := false
	if len(s.nodes) >= s.capacity {
		s.evictOne()
		evicted = true
	}
	nd := &s3Node[K, V]{key: key, entry: e}
	if _, returning := s.ghost[key]; returning {
		nd.inMain = true
		s.main.pushHead(nd)
		// The credit is spent: a later eviction must be re-earned.
		delete(s.ghost, key)
	} else {
		s.small.pushHead(nd)
	}
	s.nodes[key] = nd
	return evicted
}

func (s *S3FIFOStash[K, V]) Delete(key K) {
	if nd, ok := s.nodes[key]; ok {
		s.listOf(nd).unlink(nd)
		delete(s.nodes, key)
	}
	// Invalidation revokes readmission credit too — a deleted key that
	// comes back is new data, not a bouncing victim.
	delete(s.ghost, key)
}

func (s *S3FIFOStash[K, V]) Len() int { return len(s.nodes) }
func (s *S3FIFOStash[K, V]) Cap() int { return s.capacity }

func (s *S3FIFOStash[K, V]) Clear() {
	clear(s.nodes)
	clear(s.ghost)
	s.small = s3List[K, V]{}
	s.main = s3List[K, V]{}
	s.ghostFifo = s.ghostFifo[:0]
	s.ghostPtr = 0
}
