package caching

// Regression suite for the 2026-07-04 adversarial review. Each
// test is the inverted form of a confirmed defect repro (R1–R15): it
// asserts the remediated behavior, so a regression re-introducing the
// defect fails here.

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// R1: an open circuit breaker must never be laundered into a phantom
// zero-value hit via stash demotion. (Pre-fix: a failed fetch left a
// zero-value placeholder in L1 that eviction demoted to the stash, and a
// later Get served it as a legitimate hit.)
func TestRegressionR1_NoPhantomHitFromFailedFetch(t *testing.T) {
	f := NewMockFetcher()
	f.failKeys["Fail"] = true
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	c.SetErrorBackoff(time.Hour)

	for range c.WorkItem(1) {
		c.Get("Fail")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 0, c.Len(), "a failed fetch must not materialize L1 entries")

	// Unpin and apply insert pressure — the pre-fix laundering path.
	c.AdvanceEpoch()
	c.AddItem("Other", 7)

	v, has := c.Get("Fail")
	assert.False(t, has, "breaker open: must miss, not serve a phantom")
	assert.Zero(t, v)

	// The breaker must still suppress the fetch.
	priorCalls := f.fetchCalls
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, priorCalls, f.fetchCalls, "no retry within the backoff window")
}

// R2: staleness must survive an L1→stash demotion. A strict Get on the
// demoted entry misses and queues the refresh; GetAcceptStale keeps serving
// the old value (the SWR contract) until fresh data lands.
func TestRegressionR2_StalenessSurvivesDemotion(t *testing.T) {
	f := NewMockFetcher()
	f.data["K"] = 999
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	c.AddItem("K", 1)
	c.MarkAsStale("K")
	c.AdvanceEpoch()
	c.AddItem("Other", 2) // demotes stale K into the stash

	_, has := c.Get("K")
	assert.False(t, has, "strict Get on a demoted stale entry must miss")

	v, has, stale := c.GetAcceptStale("K")
	assert.True(t, has, "SWR must keep serving the demoted stale value")
	assert.True(t, stale)
	assert.Equal(t, 1, v)

	for range c.IterateRestWorkItems(context.Background()) {
	}
	v, has = c.Get("K")
	assert.True(t, has, "refresh queued by the stale read must have landed")
	assert.Equal(t, 999, v)
}

// R3: MarkAsStale must reach stash-resident entries (pre-fix it silently
// no-oped, dropping the external-writer invalidation signal).
func TestRegressionR3_MarkAsStaleReachesStash(t *testing.T) {
	f := NewMockFetcher()
	f.data["K"] = 999
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	c.AddItem("K", 1)
	c.AdvanceEpoch()
	c.AddItem("Other", 2) // K now stash-resident (fresh)
	c.MarkAsStale("K")

	_, has := c.Get("K")
	assert.False(t, has, "strict Get after MarkAsStale must miss and queue")

	for range c.IterateRestWorkItems(context.Background()) {
	}
	v, has := c.Get("K")
	assert.True(t, has)
	assert.Equal(t, 999, v, "the queued refetch must deliver the fresh value")
}

// R4: breaking out of a WorkItem loop must not leak the work-item context
// into later bare Gets; nesting must restore the outer context.
func TestRegressionR4_WorkItemContextRestoredOnBreak(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		break // user bails out early
	}
	c.Get("X") // bare Get: no active work item, miss must not register one

	yielded := 0
	for range c.IterateRestWorkItems(context.Background()) {
		yielded++
	}
	assert.Equal(t, 0, yielded, "no work item may leak past the break")

	// Nesting: the outer context must be restored after the inner loop.
	var got []int
	for range c.WorkItem(2) {
		for range c.WorkItem(3) {
		}
		c.Get("Y") // must register under the OUTER item 2
	}
	for w := range c.IterateRestWorkItems(context.Background()) {
		got = append(got, w)
	}
	assert.Equal(t, []int{2}, got, "outer work-item context must survive nesting")
}

// R5: stash Add of an existing key updates in place — never duplicates,
// and the newest value/flag wins (pre-fix SliceStash appended a duplicate
// and served the OLDER value).
func TestRegressionR5_SliceStashUpdateInPlace(t *testing.T) {
	s := NewSliceStash[string, int](4)
	s.Add("k", StashEntry[int]{Value: 1})
	s.Add("k", StashEntry[int]{Value: 2, Stale: true})
	assert.Equal(t, 1, s.Len(), "no duplicate entries")
	e, found := s.GetAndRemove("k")
	assert.True(t, found)
	assert.Equal(t, 2, e.Value, "newest value wins")
	assert.True(t, e.Stale, "newest stale flag wins")
	assert.Equal(t, 0, s.Len())
}

// R6: a failing wide fetch must not grow L1 (pre-fix: one error placeholder
// per failed key, unbounded); breaker bookkeeping is bounded separately.
func TestRegressionR6_FailedFetchDoesNotGrowL1(t *testing.T) {
	f := NewMockFetcher()
	f.failKeys["k-0"] = true // fails the whole partition-0 batch
	c := NewReadThroughCache[string, int, int](2, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](2)))

	for range c.WorkItem(1) {
		for i := 0; i < 100; i++ {
			c.Get(fmt.Sprintf("k-%d", i))
		}
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 0, c.Len(), "no placeholders in L1")
	assert.LessOrEqual(t, len(c.errorUntil), c.sideTableBound, "breaker table respects its bound")
}

// R7 / R15: in-memory stash constructors reject unusable capacities up
// front (pre-fix: SliceStash(0) panicked on first Add, MapStash(0) held an
// item above its cap and reported a phantom eviction).
func TestRegressionR7R15_StashConstructorsValidateCapacity(t *testing.T) {
	assert.Panics(t, func() { NewSliceStash[string, int](0) })
	assert.Panics(t, func() { NewMapStash[string, int](-1) })
	assert.Panics(t, func() {
		NewReadThroughCache[string, int, int](0, NewMockFetcher(), FetchCriteria{})
	}, "cache constructor validates capacity too")
}

// R8: a work item whose keys were flushed synchronously (another item's Max
// trigger) must be yielded by IterateReadyWorkItems even though the key
// queue is empty.
func TestRegressionR8_ReadyItemsYieldedWhenQueueEmpty(t *testing.T) {
	f := NewMockFetcher()
	f.data["A"], f.data["B"] = 1, 2
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{MaxKeys: 2},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	// Item 1 queues "A" (below MaxKeys) and suspends.
	for range c.WorkItem(1) {
		c.Get("A")
	}
	assert.Equal(t, 0, f.fetchCalls)

	// Item 2's miss on "B" trips MaxKeys=2: the synchronous flush delivers
	// BOTH keys, and the triggering Get re-routes to the fresh value, so
	// item 2 completes inside its discovery pass.
	completed2 := false
	for range c.WorkItem(2) {
		if v, has := c.Get("B"); has {
			assert.Equal(t, 2, v)
			completed2 = true
		}
	}
	assert.True(t, completed2, "Max-triggered flush must satisfy the triggering Get in-call")
	assert.Equal(t, 1, f.fetchCalls)
	assert.Equal(t, 0, c.QueuedKeys())

	// Item 1 is pending, its data is cached, the queue is empty:
	// IterateReadyWorkItems must yield it (pre-fix it yielded nothing).
	completed1 := false
	ready := 0
	for range c.IterateReadyWorkItems(context.Background()) {
		ready++
		if v, has := c.Get("A"); has {
			assert.Equal(t, 1, v)
			completed1 = true
		}
	}
	assert.Equal(t, 1, ready, "ready-with-data item must be yielded")
	assert.True(t, completed1)
	assert.Equal(t, 1, f.fetchCalls, "no extra fetch was needed")
}

// R9: breaking out of a replay loop must not drop the un-yielded pending
// work items — they stay pending for the next iterate call.
func TestRegressionR9_ReplayBreakKeepsPendingItems(t *testing.T) {
	f := NewMockFetcher()
	f.data["A"], f.data["B"] = 1, 2
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		c.Get("A")
	}
	for range c.WorkItem(2) {
		c.Get("B")
	}
	for range c.IterateRestWorkItems(context.Background()) {
		break // consumer bails after the first item
	}
	n := 0
	for range c.IterateRestWorkItems(context.Background()) {
		n++
	}
	assert.Equal(t, 1, n, "the not-yet-yielded work item must survive the break")
}

// R10: with negative caching enabled, a key that does not exist upstream
// quiesces the flush-until-quiet replay loop after ONE upstream probe.
func TestRegressionR10_NegativeCachingQuiescesAbsentKeys(t *testing.T) {
	f := NewMockFetcher() // upstream has no data at all
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)),
		WithNegativeCaching[string, int, int](time.Hour))
	now := time.Unix(1000, 0)
	c.nowFn = func() time.Time { return now }

	step := func() { c.Get("ghost") }
	for range c.WorkItem(1) {
		step()
	}

	quiesced := false
	for round := 0; round < 5; round++ {
		n := 0
		for range c.IterateRestWorkItems(context.Background()) {
			n++
			step()
		}
		if n == 0 {
			quiesced = true
			break
		}
	}
	assert.True(t, quiesced, "replay loop must terminate on an absent key")
	assert.Equal(t, 1, f.fetchCalls, "exactly one upstream probe within the TTL")

	// TTL expiry re-opens the key for probing.
	now = now.Add(2 * time.Hour)
	c.Get("ghost")
	assert.Equal(t, 1, c.QueuedKeys(), "expired absent mark allows a re-queue")
}

// R10 (default): without WithNegativeCaching the pre-existing semantics are
// preserved — absent keys re-probe on every flush. This pins the opt-in
// nature of the feature; if this test starts failing because the loop
// quiesces, negative caching leaked into the default configuration.
func TestRegressionR10_DefaultKeepsReprobingAbsentKeys(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	step := func() { c.Get("ghost") }
	for range c.WorkItem(1) {
		step()
	}
	for round := 0; round < 3; round++ {
		n := 0
		for range c.IterateRestWorkItems(context.Background()) {
			n++
			step()
		}
		assert.Equal(t, 1, n, "work item stays pending by default")
	}
	assert.Equal(t, 3, f.fetchCalls, "one probe per flush by default")
}

// R11: Delete must dequeue — an invalidated key must not be resurrected by
// the in-flight fetch queue.
func TestRegressionR11_DeleteDequeues(t *testing.T) {
	f := NewMockFetcher()
	f.data["K"] = 1
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		c.Get("K")
	}
	c.Delete("K")
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 0, f.fetchCalls, "dequeued key must not reach the fetcher")
	assert.Equal(t, 0, c.Len(), "invalidated key must not reappear")
}

// R12: Max thresholds fire synchronously on the stale-requeue path too,
// matching the documented FetchCriteria contract — and the triggering Get
// re-routes onto the refreshed entry, so it returns the fresh value
// directly instead of a miss.
func TestRegressionR12_StaleRequeueTriggersMaxCriteria(t *testing.T) {
	f := NewMockFetcher()
	f.data["S"] = 2
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{MaxKeys: 1},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	c.AddItem("S", 1)
	c.MarkAsStale("S")
	v, has := c.Get("S")
	assert.Equal(t, 1, f.fetchCalls, "MaxKeys reached via the stale path must flush synchronously")
	assert.True(t, has, "the synchronous refresh satisfies the triggering Get")
	assert.Equal(t, 2, v)

	// The failure variant: the refresh errors, so the strict read stays a
	// miss and the stale value remains for accept-stale readers.
	f.failKeys["S2"] = true
	c.AddItem("S2", 10)
	c.MarkAsStale("S2")
	_, has = c.Get("S2")
	assert.False(t, has, "failed synchronous refresh keeps the strict miss")
	sv, has, stale := c.GetAcceptStale("S2")
	assert.True(t, has)
	assert.True(t, stale)
	assert.Equal(t, 10, sv, "stale value survives the failed synchronous refresh")
}

// R13: a failed refresh must not hide the still-held stale value —
// GetAcceptStale keeps serving it through the whole breaker window
// (stale-while-revalidate survives outages).
func TestRegressionR13_StaleValueServedThroughFailedRefresh(t *testing.T) {
	f := NewMockFetcher()
	f.failKeys["K"] = true
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	c.SetErrorBackoff(time.Hour)
	now := time.Unix(1000, 0)
	c.nowFn = func() time.Time { return now }

	c.AddItem("K", 42)
	c.MarkAsStale("K")
	for range c.WorkItem(1) {
		c.GetAcceptStale("K") // serves 42, queues the refresh
	}
	for range c.IterateRestWorkItems(context.Background()) {
	} // refresh fails; breaker opens
	assert.Equal(t, 1, f.fetchCalls)

	v, has, stale := c.GetAcceptStale("K")
	assert.True(t, has, "stale value must stay servable during the outage")
	assert.True(t, stale)
	assert.Equal(t, 42, v)
	assert.Equal(t, 0, c.QueuedKeys(), "breaker suppresses the re-queue")

	// Outage ends: backoff expires and the upstream recovers.
	now = now.Add(2 * time.Hour)
	delete(f.failKeys, "K")
	f.data["K"] = 100

	v, has, stale = c.GetAcceptStale("K")
	assert.True(t, has)
	assert.True(t, stale, "still stale until the refresh lands")
	assert.Equal(t, 42, v)
	for range c.IterateRestWorkItems(context.Background()) {
	}
	v, has = c.Get("K")
	assert.True(t, has)
	assert.Equal(t, 100, v, "recovered refresh replaces the stale value")
}

// R14: AddItemSlice validates the slice lengths instead of panicking with a
// bare index error deep in the loop.
func TestRegressionR14_AddItemSliceValidatesLengths(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	assert.Panics(t, func() {
		c.AddItemSlice([]string{"a", "b"}, []int{1})
	})
	// Matched lengths still work.
	c.AddItemSlice([]string{"a", "b"}, []int{1, 2})
	v, has := c.Get("b")
	assert.True(t, has)
	assert.Equal(t, 2, v)
}

// reentrantFetcher exercises the nested-flush guard: it reads the cache
// from inside FetchItemSinglePartition, and the miss trips MaxKeys=1.
type reentrantFetcher struct {
	c     *ReadThroughCache[string, int, int]
	calls int
}

func (f *reentrantFetcher) DeterminePartition(string) uint64 { return 0 }
func (f *reentrantFetcher) FetchItemSinglePartition(_ context.Context, _ uint64, keys []string, target ItemTargetI[string, int]) error {
	f.calls++
	for _, k := range keys {
		target.AddItem(k, 1)
	}
	if f.calls == 1 {
		// Re-entrant read; the miss queues "inner" and trips MaxKeys=1,
		// which must NOT corrupt the running flush (nested flush = no-op).
		f.c.Get("inner")
	}
	return nil
}

// Documents the fetcher-reentrancy contract: keys queued during a flush are
// fetched on the NEXT flush.
func TestRegression_ReentrantFetcherQueuesForNextFlush(t *testing.T) {
	f := &reentrantFetcher{}
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{MaxKeys: 1},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	f.c = c

	for range c.WorkItem(1) {
		c.Get("outer") // miss trips MaxKeys=1 → synchronous flush
	}
	assert.Equal(t, 1, f.calls, "nested flush must be suppressed")
	assert.Equal(t, 1, c.QueuedKeys(), "reentrantly queued key waits for the next flush")

	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 2, f.calls)
	v, has := c.Get("inner")
	assert.True(t, has)
	assert.Equal(t, 1, v)
}

// Documents the cancellation contract: a cancelled flush leaves unprocessed
// keys queued instead of silently dropping them.
func TestRegression_CancelledFlushKeepsQueue(t *testing.T) {
	f := NewMockFetcher()
	f.partitionFn = func(k string) uint64 {
		if k == "alpha" {
			return 0
		}
		return 1
	}
	f.data["alpha"], f.data["beta"] = 1, 2
	c := NewReadThroughCache[string, int, int](8, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		c.Get("alpha")
		c.Get("beta")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for range c.IterateRestWorkItems(ctx) {
	}
	assert.Equal(t, 0, f.fetchCalls)
	assert.Equal(t, 2, c.QueuedKeys(), "cancelled flush must keep the queue intact")

	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 2, f.fetchCalls, "both partitions fetched after the retry")
	v, has := c.Get("alpha")
	assert.True(t, has)
	assert.Equal(t, 1, v)
	v, has = c.Get("beta")
	assert.True(t, has)
	assert.Equal(t, 2, v)
}

// Clear drops entries and bookkeeping; Close is a no-op for in-memory
// stashes. (New surface from the breaking set.)
func TestRegression_ClearAndClose(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](2, f, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](2)))

	c.AddItem("a", 1)
	c.AdvanceEpoch()
	c.AddItem("b", 2)
	c.AddItem("c", 3) // forces a demotion into the stash
	for range c.WorkItem(1) {
		c.Get("missing")
	}
	assert.Positive(t, c.Len())
	assert.Positive(t, c.QueuedKeys())
	assert.Equal(t, 1, c.PendingWorkItems())

	c.Clear()
	assert.Equal(t, 0, c.Len())
	assert.Equal(t, 0, c.StashLen())
	assert.Equal(t, 0, c.QueuedKeys())
	assert.Equal(t, 0, c.PendingWorkItems())

	assert.NoError(t, c.Close())
}
