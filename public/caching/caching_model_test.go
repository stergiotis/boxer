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
	everValue map[string]map[int]bool
	admiss    map[string]map[int]bool
	staleExp  map[string]bool
	possibly  map[string]bool
	failedAt  map[string]time.Time
	absentAt  map[string]time.Time
}

func newModelOracle(t testing.TB, negTTL time.Duration) *modelOracle {
	return &modelOracle{
		t:         t,
		negTTL:    negTTL,
		everValue: map[string]map[int]bool{},
		admiss:    map[string]map[int]bool{},
		staleExp:  map[string]bool{},
		possibly:  map[string]bool{},
		failedAt:  map[string]time.Time{},
		absentAt:  map[string]time.Time{},
	}
}

// noteFreshValue records a delivery (fetcher) or manual AddItem: the new
// value supersedes every earlier one (older values are never servable again
// — demotion overwrites stash shadows in place), clears staleness and the
// failure/absence bookkeeping.
func (o *modelOracle) noteFreshValue(k string, v int) {
	if o.everValue[k] == nil {
		o.everValue[k] = map[int]bool{}
	}
	o.everValue[k][v] = true
	o.admiss[k] = map[int]bool{v: true}
	o.staleExp[k] = false
	o.possibly[k] = true
	delete(o.failedAt, k)
	delete(o.absentAt, k)
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
}

// noteAbsentVerdict mirrors the cache's authoritative-absence handling: the
// cached remnant is dropped (no more hits) while the absent window stays.
func (o *modelOracle) noteAbsentVerdict(k string) {
	delete(o.admiss, k)
	o.staleExp[k] = false
	o.possibly[k] = false
	delete(o.failedAt, k)
}

func (o *modelOracle) noteClear() {
	o.admiss = map[string]map[int]bool{}
	o.staleExp = map[string]bool{}
	o.possibly = map[string]bool{}
	o.failedAt = map[string]time.Time{}
	o.absentAt = map[string]time.Time{}
}

func (o *modelOracle) checkStrictHit(op int, k string, v int) {
	if !o.everValue[k][v] {
		o.t.Fatalf("op %d: phantom hit Get(%s)=%d — value was never delivered or added", op, k, v)
	}
	if !o.admiss[k][v] {
		o.t.Fatalf("op %d: Get(%s)=%d served a superseded or invalidated value", op, k, v)
	}
	if o.staleExp[k] {
		o.t.Fatalf("op %d: strict Get(%s) hit but the key is stale-expected", op, k)
	}
}

func (o *modelOracle) checkStaleHit(op int, k string, v int) {
	if !o.everValue[k][v] {
		o.t.Fatalf("op %d: phantom stale hit GetAcceptStale(%s)=%d", op, k, v)
	}
	if !o.staleExp[k] {
		o.t.Fatalf("op %d: GetAcceptStale(%s) reported stale but the key was never marked", op, k)
	}
}

type modelFetcher struct {
	t          testing.TB
	o          *modelOracle
	upstream   map[string]int
	failPart   map[uint64]bool
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
			f.o.noteFreshValue(k, v)
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

	negTTL := time.Duration(0)
	if cfgNeg == 1 {
		negTTL = modelAbsentTTL
	}
	criteria := [3]FetchCriteria{
		{},
		{MinKeys: 3},
		{MaxKeys: 4},
	}[cfgCrit]

	oracle := newModelOracle(t, negTTL)
	now := time.Unix(10_000, 0)
	fetcher := &modelFetcher{
		t:        t,
		o:        oracle,
		upstream: map[string]int{},
		failPart: map[uint64]bool{},
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
	c := NewReadThroughCache[string, int, int](modelCapacity, fetcher, criteria, opts...)
	c.SetErrorBackoff(modelBackoff)
	c.nowFn = func() time.Time { return now }

	getCalls := 0
	strictGet := func(op int, k string) {
		getCalls++
		v, has := c.Get(k)
		if has {
			oracle.checkStrictHit(op, k, v)
		}
	}
	acceptStaleGet := func(op int, k string) {
		getCalls++
		v, has, stale := c.GetAcceptStale(k)
		if has && !stale {
			oracle.checkStrictHit(op, k, v)
		}
		if has && stale {
			oracle.checkStaleHit(op, k, v)
		}
	}
	checkGlobal := func(op int) {
		if got := c.Len(); got > modelCapacity {
			t.Fatalf("op %d: L1 len %d exceeds capacity %d", op, got, modelCapacity)
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
			nextVal++
			c.AddItem(k, nextVal)
			oracle.noteFreshValue(k, nextVal)
		case 5:
			c.MarkAsStale(k)
			oracle.noteMarkStale(k)
		case 6:
			c.Delete(k)
			oracle.noteDelete(k)
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
			// Mutate the upstream: new value, or make the key absent.
			del, _ := src.next(4)
			if del == 0 {
				delete(fetcher.upstream, k)
			} else {
				nextVal++
				fetcher.upstream[k] = nextVal
			}
		case 14:
			p, _ := src.next(modelPartitions)
			fetcher.failPart[uint64(p)] = !fetcher.failPart[uint64(p)]
		case 15:
			c.Clear()
			oracle.noteClear()
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
