package caching

// Model-based randomized test: drives a ReadThroughCache through seeded op
// sequences and checks every observable result against an independent
// oracle (the oracle never asks the cache — dsl-review lesson). The same
// driver backs FuzzCacheOps in caching_fuzz_test.go via the opSource seam.
//
// Oracle invariants:
//  1. Value integrity — a hit never returns a value that was never
//     delivered/added (phantom), nor one superseded by a later delivery or
//     invalidated by Delete/Clear.
//  2. Staleness honesty — strict hits never come from stale-expected keys;
//     stale=true hits only from them.
//  3. Bounds — L1 ≤ capacity; breaker/absent side tables ≤ sideTableBound.
//  4. Breaker window — the fetcher never receives a key inside its backoff
//     window (checked fetcher-side).
//  5. Negative-cache window — the fetcher never re-receives a key inside
//     its absent TTL.
//  6. Metrics ledger — hits + misses == number of Get/GetAcceptStale calls.

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// opSource abstracts the entropy source so the seeded model test and the
// fuzz target share the driver.
type opSource interface {
	// next returns a value in [0, bound); ok=false ends the run.
	next(bound int) (v int, ok bool)
}

type randOpSource struct {
	r         *rand.Rand
	remaining int
}

func (s *randOpSource) next(bound int) (int, bool) {
	if s.remaining <= 0 || bound <= 0 {
		return 0, false
	}
	s.remaining--
	return s.r.Intn(bound), true
}

type byteOpSource struct {
	data []byte
	pos  int
}

func (s *byteOpSource) next(bound int) (int, bool) {
	if bound <= 0 || s.pos >= len(s.data) {
		return 0, false
	}
	v := int(s.data[s.pos]) % bound
	s.pos++
	return v, true
}

const (
	modelKeys       = 24
	modelPartitions = 3
	modelCapacity   = 6
	modelStashCap   = 4
	modelBackoff    = 15 * time.Second
	modelAbsentTTL  = 60 * time.Second
	modelFreshTTL   = 30 * time.Second
)

func modelKey(i int) string { return fmt.Sprintf("k%02d", i%modelKeys) }

func modelPartOf(k string) uint64 {
	var i int
	_, _ = fmt.Sscanf(k, "k%d", &i)
	return uint64(i % modelPartitions)
}

type modelOracle struct {
	t         testing.TB
	negTTL    time.Duration // 0 = negative caching off
	freshTTL  time.Duration // 0 = age staleness off
	versioned bool          // versioned mode: values ARE their versions
	everValue map[string]map[int]bool
	admiss    map[string]map[int]bool
	staleExp  map[string]bool
	possibly  map[string]bool
	failedAt  map[string]time.Time
	absentAt  map[string]time.Time
	// versioned-mode state
	servedFloor map[string]int   // MonotoneServe: a key's serves never regress
	lastFresh   map[string]int64 // truth freshness stamp (ns), mirrors the gate
}

func newModelOracle(t testing.TB, negTTL time.Duration, freshTTL time.Duration, versioned bool) *modelOracle {
	return &modelOracle{
		t:           t,
		negTTL:      negTTL,
		freshTTL:    freshTTL,
		versioned:   versioned,
		everValue:   map[string]map[int]bool{},
		admiss:      map[string]map[int]bool{},
		staleExp:    map[string]bool{},
		possibly:    map[string]bool{},
		failedAt:    map[string]time.Time{},
		absentAt:    map[string]time.Time{},
		servedFloor: map[string]int{},
		lastFresh:   map[string]int64{},
	}
}

// noteFreshValue records an ADMITTED delivery (fetcher) or write: in
// counter mode the new value supersedes every earlier one (older values
// are never servable again — demotion overwrites stash shadows in place);
// in versioned mode admissibility is governed by the served floor instead,
// so admissible values accumulate. Clears staleness and the
// failure/absence bookkeeping and restarts the truth freshness stamp.
func (o *modelOracle) noteFreshValue(k string, v int, now time.Time) {
	if o.everValue[k] == nil {
		o.everValue[k] = map[int]bool{}
	}
	o.everValue[k][v] = true
	if o.versioned {
		if o.admiss[k] == nil {
			o.admiss[k] = map[int]bool{}
		}
		o.admiss[k][v] = true
	} else {
		o.admiss[k] = map[int]bool{v: true}
	}
	o.staleExp[k] = false
	o.possibly[k] = true
	o.lastFresh[k] = now.UnixNano()
	delete(o.failedAt, k)
	delete(o.absentAt, k)
}

// ageStale reports whether the key's TRUE age exceeds the freshness TTL.
func (o *modelOracle) ageStale(k string, now time.Time) bool {
	return o.freshTTL > 0 && now.UnixNano()-o.lastFresh[k] > o.freshTTL.Nanoseconds()
}

func (o *modelOracle) noteMarkStale(k string) {
	if o.possibly[k] {
		o.staleExp[k] = true
	}
}

func (o *modelOracle) noteDelete(k string) {
	delete(o.admiss, k)
	o.staleExp[k] = false
	o.possibly[k] = false
	delete(o.failedAt, k)
	delete(o.absentAt, k)
	delete(o.servedFloor, k) // explicit invalidation resets the monotone floor
}

// noteAbsentVerdict mirrors the cache's authoritative-absence handling: the
// cached remnant is dropped (no more hits) while the absent window stays.
func (o *modelOracle) noteAbsentVerdict(k string) {
	delete(o.admiss, k)
	o.staleExp[k] = false
	o.possibly[k] = false
	delete(o.failedAt, k)
	delete(o.servedFloor, k)
}

func (o *modelOracle) noteClear() {
	o.admiss = map[string]map[int]bool{}
	o.staleExp = map[string]bool{}
	o.possibly = map[string]bool{}
	o.failedAt = map[string]time.Time{}
	o.absentAt = map[string]time.Time{}
	o.servedFloor = map[string]int{}
}

func (o *modelOracle) checkStrictHit(op int, k string, v int, now time.Time) {
	if !o.everValue[k][v] {
		o.t.Fatalf("op %d: phantom hit Get(%s)=%d — value was never delivered or added", op, k, v)
	}
	if !o.admiss[k][v] {
		o.t.Fatalf("op %d: Get(%s)=%d served a superseded or invalidated value", op, k, v)
	}
	if o.staleExp[k] {
		o.t.Fatalf("op %d: strict Get(%s) hit but the key is stale-expected", op, k)
	}
	if o.ageStale(k, now) {
		o.t.Fatalf("op %d: strict Get(%s) served as fresh past its true refresh age", op, k)
	}
	o.noteServe(op, k, v)
}

func (o *modelOracle) checkStaleHit(op int, k string, v int, now time.Time) {
	if !o.everValue[k][v] {
		o.t.Fatalf("op %d: phantom stale hit GetAcceptStale(%s)=%d", op, k, v)
	}
	if !o.staleExp[k] && !o.ageStale(k, now) {
		o.t.Fatalf("op %d: GetAcceptStale(%s) reported stale but the key was neither marked nor aged out", op, k)
	}
	o.noteServe(op, k, v)
}

// noteServe enforces MonotoneServe in versioned mode: with the version
// gate and the Commit-pin/Flush-unpin discipline, a key's served value
// (== its version) never regresses. Explicit invalidation (Delete, Clear,
// absent verdict) resets the floor — a sanctioned regression.
func (o *modelOracle) noteServe(op int, k string, v int) {
	if !o.versioned {
		return
	}
	if floor, ok := o.servedFloor[k]; ok && v < floor {
		o.t.Fatalf("op %d: MonotoneServe violated: %s served %d after %d", op, k, v, floor)
	}
	o.servedFloor[k] = v
}

type modelFetcher struct {
	t          testing.TB
	o          *modelOracle
	upstream   map[string]int
	failPart   map[uint64]bool
	dirty      map[string]int // written-not-flushed versions (versioned mode)
	now        func() time.Time
	fetchCalls int
}

func (f *modelFetcher) DeterminePartition(k string) uint64 { return modelPartOf(k) }

func (f *modelFetcher) FetchItemSinglePartition(_ context.Context, p uint64, keys []string, target ItemTargetI[string, int]) error {
	f.fetchCalls++
	now := f.now()
	for _, k := range keys {
		if at, ok := f.o.failedAt[k]; ok && now.Before(at.Add(modelBackoff)) {
			f.t.Fatalf("breaker violation: %q re-fetched %v into its %v backoff window", k, now.Sub(at), modelBackoff)
		}
		if f.o.negTTL > 0 {
			if at, ok := f.o.absentAt[k]; ok && now.Before(at.Add(f.o.negTTL)) {
				f.t.Fatalf("negative-cache violation: %q re-fetched %v into its %v absent window", k, now.Sub(at), f.o.negTTL)
			}
		}
	}
	if f.failPart[p] {
		for _, k := range keys {
			f.o.failedAt[k] = now
		}
		return errors.New("model: partition down")
	}
	for _, k := range keys {
		if v, ok := f.upstream[k]; ok {
			target.AddItem(k, v)
			// Mirror the version gate for the oracle's truth stamp: a
			// delivery older than a pinned dirty version is REJECTED by
			// the cache (the pin guarantees the newer copy is resident),
			// so it must not count as a refresh. Every other delivery is
			// admitted or equal-confirmed. Sound because AheadImpliesDirty:
			// outside the dirty window the cache never runs ahead of the
			// upstream, so a delivery is always >= the cached version.
			if dv, isDirty := f.dirty[k]; !isDirty || v >= dv {
				f.o.noteFreshValue(k, v, now)
			}
		} else if f.o.negTTL > 0 {
			f.o.absentAt[k] = now
			f.o.noteAbsentVerdict(k)
		}
	}
	return nil
}

// runCacheModel drives one configured cache through ops drawn from src
// until the source is exhausted. Configuration (stash kind, criteria,
// negative caching) is drawn from the source too, so fuzzing explores it.
func runCacheModel(t testing.TB, src opSource) {
	cfgStash, ok := src.next(2)
	if !ok {
		return
	}
	cfgCrit, ok := src.next(3)
	if !ok {
		return
	}
	cfgNeg, ok := src.next(2)
	if !ok {
		return
	}
	cfgVersioned, ok := src.next(2)
	if !ok {
		return
	}
	cfgFresh, ok := src.next(2)
	if !ok {
		return
	}

	negTTL := time.Duration(0)
	if cfgNeg == 1 {
		negTTL = modelAbsentTTL
	}
	freshTTL := time.Duration(0)
	if cfgFresh == 1 {
		freshTTL = modelFreshTTL
	}
	versioned := cfgVersioned == 1
	criteria := [3]FetchCriteria{
		{},
		{MinKeys: 3},
		{MaxKeys: 4},
	}[cfgCrit]

	oracle := newModelOracle(t, negTTL, freshTTL, versioned)
	now := time.Unix(10_000, 0)
	dirty := map[string]int{} // written-not-flushed (versioned mode only)
	// pinPlacementsOverCap counts Pin calls issued while L1 was at or over
	// capacity. Each such pin may hoist (or hold) one entry beyond the
	// capacity, and — when everything else is epoch-pinned — the overshoot
	// accumulates until eviction pressure can drain it, so the sound L1
	// bound is capacity + this monotone counter (over-counting pins on
	// already-resident entries only loosens it).
	pinPlacementsOverCap := 0
	fetcher := &modelFetcher{
		t:        t,
		o:        oracle,
		upstream: map[string]int{},
		failPart: map[uint64]bool{},
		dirty:    dirty,
		now:      func() time.Time { return now },
	}
	// Seed some upstream data; the rest of the keyspace starts absent.
	for i := 0; i < modelKeys; i += 2 {
		fetcher.upstream[modelKey(i)] = i
	}

	metrics := &MockMetrics{}
	opts := []CacheOption[string, int, int]{
		WithMetrics[string, int, int](metrics),
	}
	if cfgStash == 0 {
		opts = append(opts, WithStash[string, int, int](NewSliceStash[string, int](modelStashCap)))
	} else {
		opts = append(opts, WithStash[string, int, int](NewMapStash[string, int](modelStashCap)))
	}
	if negTTL > 0 {
		opts = append(opts, WithNegativeCaching[string, int, int](negTTL))
	}
	if freshTTL > 0 {
		opts = append(opts, WithFreshnessTTL[string, int, int](freshTTL))
	}
	if versioned {
		// Values ARE their versions (globally increasing nextVal), exactly
		// like the formal spec.
		opts = append(opts, WithVersioning[string, int, int](func(v int) int64 { return int64(v) }))
	}
	c := NewReadThroughCache[string, int, int](modelCapacity, fetcher, criteria, opts...)
	c.SetErrorBackoff(modelBackoff)
	c.nowFn = func() time.Time { return now }

	getCalls := 0
	strictGet := func(op int, k string) {
		getCalls++
		v, has := c.Get(k)
		if has {
			oracle.checkStrictHit(op, k, v, now)
		}
	}
	acceptStaleGet := func(op int, k string) {
		getCalls++
		v, has, stale := c.GetAcceptStale(k)
		if has && !stale {
			oracle.checkStrictHit(op, k, v, now)
		}
		if has && stale {
			oracle.checkStaleHit(op, k, v, now)
		}
	}
	checkGlobal := func(op int) {
		if got := c.Len(); got > modelCapacity+pinPlacementsOverCap {
			t.Fatalf("op %d: L1 len %d exceeds capacity %d + over-cap pin placements %d", op, got, modelCapacity, pinPlacementsOverCap)
		}
		if got := c.StashLen(); got > modelStashCap {
			t.Fatalf("op %d: stash len %d exceeds capacity %d", op, got, modelStashCap)
		}
		if got := len(c.errorUntil); got > c.sideTableBound {
			t.Fatalf("op %d: breaker table %d exceeds bound %d", op, got, c.sideTableBound)
		}
		if c.absentUntil != nil {
			if got := len(c.absentUntil); got > c.sideTableBound {
				t.Fatalf("op %d: absent table %d exceeds bound %d", op, got, c.sideTableBound)
			}
		}
		if hits := metrics.HitsL1 + metrics.HitsL2; hits+metrics.Misses != getCalls {
			t.Fatalf("op %d: metrics ledger broken: %d hits + %d misses != %d gets",
				op, hits, metrics.Misses, getCalls)
		}
	}

	wid := 0
	nextVal := 1000
	ctx := context.Background()

	for op := 0; ; op++ {
		sel, ok := src.next(16)
		if !ok {
			return
		}
		ki, ok := src.next(modelKeys)
		if !ok {
			return
		}
		k := modelKey(ki)

		switch sel {
		case 0, 1, 2:
			strictGet(op, k)
		case 3:
			acceptStaleGet(op, k)
		case 4:
			// Counter mode: a bare write into the cache (last-insert-wins).
			// Versioned mode: a write-through Commit — populate + pin; the
			// upstream catches up only at the flush op (the dirty window).
			nextVal++
			c.AddItem(k, nextVal)
			if versioned {
				if c.Len() >= modelCapacity {
					pinPlacementsOverCap++
				}
				c.Pin(k)
				dirty[k] = nextVal
			}
			oracle.noteFreshValue(k, nextVal, now)
		case 5:
			c.MarkAsStale(k)
			oracle.noteMarkStale(k)
		case 6:
			// The documented write-through discipline forbids invalidating
			// keys with unflushed local writes; the driver follows it.
			if _, isDirty := dirty[k]; !isDirty {
				c.Delete(k)
				oracle.noteDelete(k)
			}
		case 7:
			c.AdvanceEpoch()
		case 8, 9:
			wid++
			n, _ := src.next(3)
			for range c.WorkItem(wid) {
				for j := 0; j <= n; j++ {
					kj, ok2 := src.next(modelKeys)
					if !ok2 {
						break
					}
					strictGet(op, modelKey(kj))
				}
			}
		case 10:
			for range c.IterateReadyWorkItems(ctx) {
				kj, ok2 := src.next(modelKeys)
				if !ok2 {
					break
				}
				strictGet(op, modelKey(kj))
			}
		case 11:
			for range c.IterateRestWorkItems(ctx) {
				kj, ok2 := src.next(modelKeys)
				if !ok2 {
					break
				}
				strictGet(op, modelKey(kj))
			}
		case 12:
			step, _ := src.next(3)
			now = now.Add([3]time.Duration{2 * time.Second, 20 * time.Second, 90 * time.Second}[step])
		case 13:
			if versioned {
				// Flush: publish every dirty write upstream and release
				// the pins (the recordstore Flush analog). The upstream
				// moves only through here — single-writer discipline.
				for dk, dv := range dirty {
					fetcher.upstream[dk] = dv
					c.Unpin(dk)
					delete(dirty, dk)
				}
			} else {
				// Counter mode: an external writer mutates the upstream —
				// new value, or the key disappears.
				del, _ := src.next(4)
				if del == 0 {
					delete(fetcher.upstream, k)
				} else {
					nextVal++
					fetcher.upstream[k] = nextVal
				}
			}
		case 14:
			p, _ := src.next(modelPartitions)
			fetcher.failPart[uint64(p)] = !fetcher.failPart[uint64(p)]
		case 15:
			// Clear drops pins too; the write-through discipline calls it
			// only with no unflushed writes outstanding.
			if len(dirty) == 0 {
				c.Clear()
				oracle.noteClear()
			}
		}
		checkGlobal(op)
	}
}

func TestCacheModel_SeededInvariants(t *testing.T) {
	for seed := int64(0); seed < 48; seed++ {
		t.Run(fmt.Sprintf("seed%02d", seed), func(t *testing.T) {
			src := &randOpSource{r: rand.New(rand.NewSource(seed)), remaining: 4000}
			runCacheModel(t, src)
		})
	}
}
