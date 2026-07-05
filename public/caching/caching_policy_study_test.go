package caching

// Eviction-policy study — the measured gate for the postponed S3-FIFO /
// SIEVE round. Two experiments over the same deterministic Zipf workload
// (the long-running recordstore-view persona: skewed key popularity,
// periodic epoch advances, batched flushes):
//
//  1. THROUGH THE REAL CACHE: identical runs differing only in the stash
//     backend (SliceStash round-robin, MapStash random, S3FIFOStash),
//     measuring where the L2 policy moves hits and upstream fetches.
//  2. SIMULATORS: single-tier policy simulators (random — today's L1 —
//     vs FIFO, LRU, SIEVE) on the raw trace, quantifying the headroom an
//     L1 policy change could buy before touching the core.
//
// Run with -v to see the tables. Assertions are deliberately loose sanity
// bounds — the numbers are the deliverable, and policies are compared, not
// pinned (map iteration randomness makes exact counts nondeterministic).

import (
	"context"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

type intFetcher struct {
	delivered int
}

func (f *intFetcher) DeterminePartition(int) uint64 { return 0 }
func (f *intFetcher) FetchItemSinglePartition(_ context.Context, _ uint64, keys []int, target ItemTargetI[int, int]) error {
	for _, k := range keys {
		target.AddItem(k, k) // every key exists upstream
		f.delivered++
	}
	return nil
}

const (
	studyKeys     = 4096
	studyOps      = 120_000
	studyL1Cap    = 256
	studyStashCap = 128
	studyZipfS    = 1.1
)

func zipfTrace(seed int64) func() int {
	rng := rand.New(rand.NewSource(seed))
	z := rand.NewZipf(rng, studyZipfS, 1, studyKeys-1)
	return func() int { return int(z.Uint64()) }
}

func runStashStudy(t *testing.T, name string, epochEvery int, stash StashBackendI[int, int]) (hitRatio float64) {
	f := &intFetcher{}
	m := &MockMetrics{}
	c := NewReadThroughCache[int, int, int](studyL1Cap, f, FetchCriteria{},
		WithStash[int, int, int](stash), WithMetrics[int, int, int](m))
	ctx := context.Background()
	next := zipfTrace(42)

	for i := 0; i < studyOps; i++ {
		if i%epochEvery == 0 {
			c.AdvanceEpoch()
		}
		c.Get(next())
		if i%50 == 49 {
			for range c.IterateRestWorkItems(ctx) {
			}
		}
	}
	for range c.IterateRestWorkItems(ctx) {
	}

	hits := m.HitsL1 + m.HitsL2
	hitRatio = float64(hits) / float64(hits+m.Misses)
	t.Logf("epoch/%-5d %-12s  L1 hits %7d   L2 hits %6d   misses %6d   hit%% %5.2f   upstream deliveries %6d",
		epochEvery, name, m.HitsL1, m.HitsL2, m.Misses, 100*hitRatio, f.delivered)
	return hitRatio
}

// TestEvictionPolicyStudy_Stash compares L2 stash policies through the
// full cache.
func TestEvictionPolicyStudy_Stash(t *testing.T) {
	t.Logf("Zipf(s=%.2f) keys=%d ops=%d L1=%d stash=%d, flush/50",
		studyZipfS, studyKeys, studyOps, studyL1Cap, studyStashCap)
	// Two epoch cadences: heavy pinning (200) shields the hot set and
	// dampens policy differences; light pinning (2000) exposes them.
	for _, epochEvery := range []int{200, 2000} {
		rrr := runStashStudy(t, "slice-rr", epochEvery, NewSliceStash[int, int](studyStashCap))
		rnd := runStashStudy(t, "map-random", epochEvery, NewMapStash[int, int](studyStashCap))
		s3 := runStashStudy(t, "s3fifo", epochEvery, NewS3FIFOStash[int, int](studyStashCap))
		// Sanity bounds only; the log table is the deliverable.
		require.Greater(t, rrr, 0.5)
		require.Greater(t, rnd, 0.5)
		require.Greater(t, s3, 0.5)
	}
}

// --- single-tier policy simulators (the L1 headroom estimate) ------------

type policySim interface {
	access(k int) bool // true = hit
	name() string
}

type randomSim struct {
	cap  int
	rng  *rand.Rand
	set  map[int]int // key -> index in keys
	keys []int
}

func newRandomSim(capacity int, seed int64) *randomSim {
	return &randomSim{cap: capacity, rng: rand.New(rand.NewSource(seed)), set: map[int]int{}}
}
func (s *randomSim) name() string { return "random" }
func (s *randomSim) access(k int) bool {
	if _, ok := s.set[k]; ok {
		return true
	}
	if len(s.keys) >= s.cap {
		i := s.rng.Intn(len(s.keys))
		victim := s.keys[i]
		last := len(s.keys) - 1
		s.keys[i] = s.keys[last]
		s.set[s.keys[i]] = i
		s.keys = s.keys[:last]
		delete(s.set, victim)
	}
	s.set[k] = len(s.keys)
	s.keys = append(s.keys, k)
	return false
}

type node struct {
	key        int
	visited    bool
	prev, next *node // prev = toward head (newer), next = toward tail (older)
}

type listSim struct {
	label      string
	cap        int
	m          map[int]*node
	head, tail *node
	hand       *node // SIEVE only
	lru        bool  // LRU: move-to-head on hit; SIEVE: set visited
}

func newListSim(label string, capacity int, lru bool) *listSim {
	return &listSim{label: label, cap: capacity, m: map[int]*node{}, lru: lru}
}
func (s *listSim) name() string { return s.label }

func (s *listSim) pushHead(nd *node) {
	nd.prev, nd.next = nil, s.head
	if s.head != nil {
		s.head.prev = nd
	} else {
		s.tail = nd
	}
	s.head = nd
}

func (s *listSim) unlink(nd *node) {
	if nd.prev != nil {
		nd.prev.next = nd.next
	} else {
		s.head = nd.next
	}
	if nd.next != nil {
		nd.next.prev = nd.prev
	} else {
		s.tail = nd.prev
	}
}

func (s *listSim) access(k int) bool {
	if nd, ok := s.m[k]; ok {
		if s.lru {
			s.unlink(nd)
			s.pushHead(nd)
		} else {
			nd.visited = true // SIEVE: no movement on hit
		}
		return true
	}
	if len(s.m) >= s.cap {
		if s.lru || s.hand == nil {
			s.evictTailward()
		} else {
			s.evictSieve()
		}
		if !s.lru && s.hand == nil && len(s.m) >= s.cap {
			s.evictSieve()
		}
	}
	nd := &node{key: k}
	s.pushHead(nd)
	s.m[k] = nd
	return false
}

func (s *listSim) evictTailward() {
	if s.lru {
		victim := s.tail
		s.unlink(victim)
		delete(s.m, victim.key)
		return
	}
	s.evictSieve()
}

// evictSieve: the hand walks from the tail toward the head, clearing
// visited bits and retaining their objects, evicting the first unvisited
// one; it persists across evictions and wraps to the tail.
func (s *listSim) evictSieve() {
	if s.hand == nil {
		s.hand = s.tail
	}
	for {
		if s.hand == nil {
			s.hand = s.tail
		}
		if s.hand.visited {
			s.hand.visited = false
			s.hand = s.hand.prev
			continue
		}
		victim := s.hand
		s.hand = victim.prev
		s.unlink(victim)
		delete(s.m, victim.key)
		return
	}
}

type fifoSim struct{ *listSim }

func newFifoSim(capacity int) *fifoSim { return &fifoSim{newListSim("fifo", capacity, false)} }
func (s *fifoSim) name() string        { return "fifo" }
func (s *fifoSim) access(k int) bool {
	if _, ok := s.m[k]; ok {
		return true // no bits, no movement
	}
	if len(s.m) >= s.cap {
		victim := s.tail
		s.unlink(victim)
		delete(s.m, victim.key)
	}
	nd := &node{key: k}
	s.pushHead(nd)
	s.m[k] = nd
	return false
}

// TestEvictionPolicyStudy_L1Simulators quantifies the headroom of an L1
// policy change (today: random among unpinned) on the same trace.
func TestEvictionPolicyStudy_L1Simulators(t *testing.T) {
	for _, capacity := range []int{192, 384} {
		sims := []policySim{
			newRandomSim(capacity, 7),
			newFifoSim(capacity),
			newListSim("lru", capacity, true),
			newListSim("sieve", capacity, false),
		}
		for _, sim := range sims {
			next := zipfTrace(42) // identical trace for every policy
			hits := 0
			for i := 0; i < studyOps; i++ {
				if sim.access(next()) {
					hits++
				}
			}
			ratio := 100 * float64(hits) / float64(studyOps)
			t.Logf("cap %4d  %-7s hit%% %5.2f", capacity, sim.name(), ratio)
			require.Greater(t, ratio, 10.0)
		}
	}
}
