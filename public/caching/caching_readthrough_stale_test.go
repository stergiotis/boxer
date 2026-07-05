package caching

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- Mocks ---

type MockMetrics struct {
	HitsL1, HitsL2, HitsStale, Misses, Errors, EvictsStash, EvictsDrop int
}

func (inst *MockMetrics) RecordHit(l1 bool, stale bool) {
	if l1 {
		inst.HitsL1++
	} else {
		inst.HitsL2++
	}
	if stale {
		inst.HitsStale++
	}
}
func (inst *MockMetrics) RecordMiss()            { inst.Misses++ }
func (inst *MockMetrics) RecordFetchError(c int) { inst.Errors += c }
func (inst *MockMetrics) RecordEviction(stash bool) {
	if stash {
		inst.EvictsStash++
	} else {
		inst.EvictsDrop++
	}
}
func (inst *MockMetrics) RecordFetchDuration(d time.Duration) {}

type MockFetcher struct {
	data              map[string]int
	failKeys          map[string]bool // Keys that trigger error
	panicKeys         map[string]bool // Keys that trigger panic
	partitionFn       func(key string) uint64
	fetchCalls        int
	fetchedBatches    [][]string
	fetchedPartitions []uint64
}

func NewMockFetcher() *MockFetcher {
	return &MockFetcher{
		data:              make(map[string]int),
		failKeys:          make(map[string]bool),
		panicKeys:         make(map[string]bool),
		fetchedBatches:    make([][]string, 0),
		fetchedPartitions: make([]uint64, 0),
	}
}

func (inst *MockFetcher) DeterminePartition(key string) uint64 {
	if inst.partitionFn != nil {
		return inst.partitionFn(key)
	}
	return 0
}

func (inst *MockFetcher) FetchItemSinglePartition(ctx context.Context, partition uint64, keys []string, target ItemTargetI[string, int]) error {
	inst.fetchCalls++
	inst.fetchedBatches = append(inst.fetchedBatches, slices.Clone(keys))
	inst.fetchedPartitions = append(inst.fetchedPartitions, partition)

	// Simulate Context Cancel
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, k := range keys {
		if inst.panicKeys[k] {
			panic(fmt.Sprintf("Panic on key %s", k))
		}
	}

	// Check for failures
	for _, k := range keys {
		if inst.failKeys[k] {
			return errors.New("simulated fetch error")
		}
	}

	for _, k := range keys {
		if v, ok := inst.data[k]; ok {
			target.AddItem(k, v)
		}
	}
	return nil
}

// --- Tests ---
func TestCircuitBreaker(t *testing.T) {
	// Scenario: Fetching "FailKey" fails.
	// 1. First attempt: Fails, breaker opens. Backoff set.
	// 2. Immediate retry: Should be blocked (No fetch call).
	// 3. Advance the clock past the backoff.
	// 4. Retry: Should trigger fetch.

	f := NewMockFetcher()
	f.failKeys["Fail"] = true
	m := &MockMetrics{}

	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](10)))
	c.SetErrorBackoff(100 * time.Millisecond)
	now := time.Unix(1000, 0)
	c.nowFn = func() time.Time { return now }

	ctx := context.Background()

	// 1. Trigger Failure
	for range c.WorkItem(1) {
		c.Get("Fail")
	}
	for range c.IterateRestWorkItems(ctx) {
	} // Trigger fetch

	assert.Equal(t, 1, f.fetchCalls)
	assert.Equal(t, 1, m.Errors)

	// 2. Immediate Retry
	for range c.WorkItem(1) {
		c.Get("Fail")
	}
	// This iterate should NOT trigger a fetch: the breaker is open.
	for range c.IterateRestWorkItems(ctx) {
	}

	assert.Equal(t, 1, f.fetchCalls, "Should not fetch during backoff")

	// 3. Advance past the backoff window.
	now = now.Add(150 * time.Millisecond)

	// 4. Retry after backoff
	for range c.WorkItem(1) {
		c.Get("Fail")
	}
	for range c.IterateRestWorkItems(ctx) {
	}

	assert.Equal(t, 2, f.fetchCalls, "Should fetch after backoff expires")
	assert.Equal(t, 2, m.Errors)
}

func TestPanicRecovery(t *testing.T) {
	f := NewMockFetcher()
	f.panicKeys["Bomb"] = true
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0}, WithStash[string, int, int](NewSliceStash[string, int](10)))
	c.SetErrorBackoff(time.Second)

	// Trigger Panic
	for range c.WorkItem(1) {
		c.Get("Bomb")
	}

	assert.NotPanics(t, func() {
		for range c.IterateRestWorkItems(context.Background()) {
		}
	})

	assert.Equal(t, 1, f.fetchCalls)

	// Verify item is in error state (check via behavior)
	_, has := c.Get("Bomb")
	assert.False(t, has)
}

func TestContextCancellation(t *testing.T) {
	// With ctx already cancelled before IterateRestWorkItems, performFetch's
	// per-partition loop must break before invoking the fetcher.
	f := NewMockFetcher()
	f.data["A"] = 1
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](10)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for range c.WorkItem(1) {
		c.Get("A")
	}
	for range c.IterateRestWorkItems(ctx) {
	}

	assert.Equal(t, 0, f.fetchCalls, "fetcher must not run when ctx is already cancelled")

	// Subsequent attempt with a live ctx should fetch successfully (queue was
	// not corrupted by the cancelled run).
	for range c.WorkItem(1) {
		c.Get("A")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls)
	v, has := c.Get("A")
	assert.True(t, has)
	assert.Equal(t, 1, v)
}

func TestBasicReadThrough(t *testing.T) {
	f := NewMockFetcher()
	f.data["A"] = 10
	m := &MockMetrics{}
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0}, WithMetrics[string, int, int](m), WithStash[string, int, int](NewSliceStash[string, int](10)))

	done := false
	for range c.WorkItem(1) {
		v, h := c.Get("A")
		if !h {
			return
		}
		assert.Equal(t, 10, v)
		done = true
	}
	assert.False(t, done)

	for range c.IterateRestWorkItems(context.Background()) {
	}

	for range c.IterateReadyWorkItems(context.Background()) {
		done = true
	}

	assert.True(t, done)
	assert.Equal(t, 1, m.Misses)
	assert.Equal(t, 1, f.fetchCalls)
}

func TestMemoryOptimizedEviction(t *testing.T) {
	// Verify value-receiver map logic handles pinning correctly
	f := NewMockFetcher()
	f.data["New"] = 99
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{MinKeys: 0}, WithStash[string, int, int](NewSliceStash[string, int](10)))

	// Add Old (Epoch 1)
	c.AddItem("Old", 1)
	c.AdvanceEpoch()

	// Pin Old (Update Epoch to 2)
	for range c.WorkItem(1) {
		c.Get("Old")
	}

	// Fetch New. Cache Cap=1. Old is Pinned.
	// ensureSpaceByEvictingOne only demotes unpinned entries; with everything
	// pinned it reports useStash, so "New" spills to the stash and "Old"
	// stays in Primary.

	for range c.WorkItem(1) {
		c.Get("New")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}

	// Verify "Old" is still in primary (via implementation detail or behavior)
	// We can check behavior by ensuring Get("Old") is L1 hit.
	// But our mock metrics are easier.

	// Let's verify data presence
	v1, h1 := c.Get("Old")
	v2, h2 := c.Get("New")

	assert.True(t, h1)
	assert.Equal(t, 1, v1)
	assert.True(t, h2)
	assert.Equal(t, 99, v2)
}

// --- Coverage Tests ---

func TestGetAcceptStale_Comprehensive(t *testing.T) {
	f := NewMockFetcher()
	f.data["Fresh"] = 1
	f.data["Stale"] = 2
	f.data["Missing"] = 3
	f.failKeys["Error"] = true // Will trigger circuit breaker

	m := &MockMetrics{}
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](10)))
	c.SetErrorBackoff(100 * time.Millisecond)

	// 1. Setup Data
	// Add "Fresh"
	c.AddItem("Fresh", 1)
	// Add "Stale" and mark it
	c.AddItem("Stale", 2)
	c.MarkAsStale("Stale")
	// "Missing" is not in cache yet
	// "Error" will fail on fetch

	ctx := context.Background()

	// --- Case A: Fresh Item ---
	// Should return (true, false, val), no fetch triggered
	for range c.WorkItem(1) {
		val, has, stale := c.GetAcceptStale("Fresh")
		assert.True(t, has)
		assert.False(t, stale)
		assert.Equal(t, 1, val)
	}
	// Verify no fetch triggered
	for range c.IterateRestWorkItems(ctx) {
	}
	assert.Equal(t, 0, f.fetchCalls)

	// --- Case B: Stale Item ---
	// Should return (true, true, val), AND trigger background fetch
	for range c.WorkItem(2) {
		val, has, stale := c.GetAcceptStale("Stale")
		assert.True(t, has)
		assert.True(t, stale)
		assert.Equal(t, 2, val)
	}
	// Verify fetch triggered
	f.data["Stale"] = 200 // Update upstream
	for range c.IterateRestWorkItems(ctx) {
	}
	assert.Equal(t, 1, f.fetchCalls)

	// Verify it is now Fresh
	for range c.WorkItem(2) {
		val, has, stale := c.GetAcceptStale("Stale")
		assert.True(t, has)
		assert.False(t, stale)
		assert.Equal(t, 200, val)
	}

	// --- Case C: Missing Item ---
	// Should return (false, false, 0), trigger fetch
	for range c.WorkItem(3) {
		_, has, _ := c.GetAcceptStale("Missing")
		if !has {
			return
		} // Abort expected
		assert.Fail(t, "Should not find missing item")
	}
	// Verify fetch
	for range c.IterateRestWorkItems(ctx) {
	}
	assert.Equal(t, 2, f.fetchCalls)

	// Check result
	val, has, _ := c.GetAcceptStale("Missing")
	assert.True(t, has)
	assert.Equal(t, 3, val)

	// --- Case D: Error Item (Circuit Breaker) ---
	// 1. Trigger failure
	for range c.WorkItem(4) {
		c.GetAcceptStale("Error")
	}
	for range c.IterateRestWorkItems(ctx) {
	} // Fetch fails here
	assert.Equal(t, 3, f.fetchCalls)

	// 2. Access during backoff (Circuit Breaker Open)
	// Should return false, but NOT trigger new fetch
	f.fetchCalls = 0
	for range c.WorkItem(4) {
		_, has, _ := c.GetAcceptStale("Error")
		assert.False(t, has)
	}
	for range c.IterateRestWorkItems(ctx) {
	}
	assert.Equal(t, 0, f.fetchCalls, "Should obey backoff")
}
func TestOverstretchedCache_Livelock(t *testing.T) {
	// Configuration
	// L1 Capacity: 5
	// L2 Capacity: 5
	// Total Memory: 10 Slots
	l1Size := 5
	l2Size := 5

	f := NewMockFetcher()

	// Create Mock Metrics to spy on Evictions
	m := &MockMetrics{}

	// We use strict limits (using initialStashSize as the Cap)
	c := NewReadThroughCache[string, int, int](l1Size, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](l2Size)))

	// Workload: A single job requires 15 items.
	// This exceeds Total Memory (10).
	requiredItems := 15

	// Setup Fetcher with data
	for i := 0; i < requiredItems; i++ {
		key := fmt.Sprintf("key-%d", i)
		f.data[key] = i
	}

	ctx := context.Background()
	workID := 1
	completed := false

	// We limit the attempts to prevent the test from running forever.
	// If it hasn't finished in 20 passes, it never will.
	maxAttempts := 20
	attempts := 0

	for attempts < maxAttempts {
		attempts++
		c.AdvanceEpoch() // Essential: Allow cache to unpin previous pass

		workDone := false

		// Run Work Item
		for range c.WorkItem(workID) {
			missing := false

			// Request all 15 items
			for i := 0; i < requiredItems; i++ {
				key := fmt.Sprintf("key-%d", i)

				// Accessing the key PINS it to the current Epoch.
				_, has := c.Get(key)
				if !has {
					missing = true
				}
			}

			// If we found everything, we are done (Should be impossible)
			if !missing {
				workDone = true
			}
		}

		if workDone {
			completed = true
			break
		}

		// Trigger Fetch for missing items
		for range c.IterateRestWorkItems(ctx) {
		}
	}

	// --- Assertions ---

	// 1. It must NOT complete. The working set (15) > Cache (10).
	assert.False(t, completed, "Work item should not complete due to insufficient cache capacity")

	// 2. It should have thrashed (dropped items from Stash).
	// We expect L2 Drops (RecordEviction(false)) > 0
	assert.Greater(t, m.EvictsDrop, 0, "Cache should have evicted items from Stash (Data Loss) due to pressure")

	// 3. It should have fetched significantly more than the data size
	// We needed 15 items. After 20 attempts, we likely fetched hundreds of times.
	assert.Greater(t, f.fetchCalls, 10, "Cache should be thrashing (repeatedly fetching evicted items)")
}
func TestMaxKeys_SmallerThanWorkItem_ChunkedFetch(t *testing.T) {
	// Scenario:
	// - Work Item needs 3 keys: A, B, C.
	// - MaxKeys = 2.
	//
	// Expected Flow:
	// 1. Pass 1: Get(A), Get(B) -> Trigger Fetch(A,B). Get(C) -> Queue C. Abort.
	// 2. State: A,B in L1. C in Pending Queue.
	// 3. User calls IterateReady: C does not trigger MaxKeys(2).
	//    If MinKeys is high, C waits.
	// 4. User calls IterateRest: Fetch(C).
	// 5. Pass 2: Get(A,B,C) -> All Hit. Success.

	f := NewMockFetcher()
	f.data["A"], f.data["B"], f.data["C"] = 1, 2, 3

	// Criteria: MaxKeys=2 (Force chunking), MinKeys=100 (Prevent early flush)
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 100, MaxKeys: 2},
		WithStash[string, int, int](NewSliceStash[string, int](10)))

	workID := 1
	completed := false

	runWork := func() {
		missing := false
		// Accumulate all 3
		if _, h := c.Get("A"); !h {
			missing = true
		}
		if _, h := c.Get("B"); !h {
			missing = true
		}
		if _, h := c.Get("C"); !h {
			missing = true
		}

		if !missing {
			completed = true
		}
	}

	ctx := context.Background()

	// --- Phase 1: Discovery & Partial Fetch ---
	for range c.WorkItem(workID) {
		runWork()
	}

	assert.False(t, completed, "Should abort due to missing keys")

	// Assertion: Fetcher should have been called ONCE for A and B (MaxKeys triggered)
	// But NOT for C yet.
	assert.Equal(t, 1, f.fetchCalls, "Should have triggered synchronous fetch for A and B")
	assert.Contains(t, f.fetchedBatches[0], "A")
	assert.Contains(t, f.fetchedBatches[0], "B")
	assert.NotContains(t, f.fetchedBatches[0], "C")

	// --- Phase 2: Iterate Ready (The Stalemate) ---
	// C is pending. Count=1. MaxKeys=2. MinKeys=100.
	// Ideally, this should yield NOTHING because criteria aren't met.
	readyCount := 0
	for range c.IterateReadyWorkItems(ctx) {
		readyCount++
		runWork()
	}
	assert.Equal(t, 0, readyCount, "Should not yield work item yet; C is pending but below thresholds")
	assert.False(t, completed)

	// --- Phase 3: Flush (Iterate Rest) ---
	// This forces the fetch of C.
	restCount := 0
	for range c.IterateRestWorkItems(ctx) {
		restCount++
		runWork()
	}

	assert.True(t, completed, "Work should complete after flushing C")
	assert.Equal(t, 2, f.fetchCalls, "Should have performed 2 fetches total (Chunk 1: A,B; Chunk 2: C)")
	assert.Contains(t, f.fetchedBatches[1], "C")
}

// TestEvictionMetric_L1DemotionCount verifies that when an unpinned L1 item is
// evicted to a non-full stash, exactly one (toStash=true) is recorded and no
// (toStash=false). This exercises the canonical demotion path through
// ensureSpaceByEvictingOne.
func TestEvictionMetric_L1DemotionCount(t *testing.T) {
	f := NewMockFetcher()
	m := &MockMetrics{}
	// Cap=1 forces a demotion on the second insert. Stash large enough to absorb.
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](10)))

	c.AddItem("Old", 1)
	c.AdvanceEpoch() // unpin "Old" so it becomes evictable
	c.AddItem("New", 2)

	assert.Equal(t, 1, m.EvictsStash, "exactly one L1→L2 demotion")
	assert.Equal(t, 0, m.EvictsDrop, "stash has room; no drop")
}

// TestEvictionMetric_StashOverflowOnDemotion verifies that an L1 demotion into
// a full stash records both a (toStash=true) for the demoted L1 item and a
// (toStash=false) for the stash item it displaces.
func TestEvictionMetric_StashOverflowOnDemotion(t *testing.T) {
	f := NewMockFetcher()
	m := &MockMetrics{}
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](1)))

	// Fill stash directly via L1 → L2 demotion: insert, advance, insert.
	c.AddItem("A", 1)
	c.AdvanceEpoch()
	c.AddItem("B", 2) // A demoted; stash now holds {A}
	assert.Equal(t, 1, m.EvictsStash)
	assert.Equal(t, 0, m.EvictsDrop)

	c.AdvanceEpoch()
	c.AddItem("C", 3) // B demoted; stash full, A is dropped to make room

	assert.Equal(t, 2, m.EvictsStash, "two L1→L2 demotions over the run")
	assert.Equal(t, 1, m.EvictsDrop, "stash overflow dropped one item")
}

// TestEvictionMetric_DirectStashSpillDoesNotInflateStashCount verifies the bug
// fix: when L1 is full of pinned items and AddItem must spill the new value
// directly to the stash, we do NOT record a (toStash=true) — no L1 item was
// demoted. A (toStash=false) still fires if the stash itself overflows.
func TestEvictionMetric_DirectStashSpillDoesNotInflateStashCount(t *testing.T) {
	f := NewMockFetcher()
	m := &MockMetrics{}
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](1)))

	// Pin the single L1 slot to currentEpoch.
	c.AddItem("Pinned", 1)
	for range c.WorkItem(1) {
		c.Get("Pinned") // pin to currentEpoch
	}
	assert.Equal(t, 0, m.EvictsStash, "AddItem to empty cache is not an eviction")
	assert.Equal(t, 0, m.EvictsDrop)

	// Now AddItem a different key — L1 is full and pinned, so it must go
	// directly to L2. Stash has 1 cap and is currently empty.
	c.AddItem("Spill1", 2)
	assert.Equal(t, 0, m.EvictsStash, "direct-to-stash spill must NOT count as L1→L2 demotion")
	assert.Equal(t, 0, m.EvictsDrop, "stash had room")

	// Spill again; stash now full, so a drop should be recorded but still no demotion.
	c.AddItem("Spill2", 3)
	assert.Equal(t, 0, m.EvictsStash, "still no demotion")
	assert.Equal(t, 1, m.EvictsDrop, "stash overflow drops one")
}

// TestDelete confirms Delete removes a key from both L1 and L2.
func TestDelete(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](1, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	// Put into L1
	c.AddItem("L1Key", 10)
	c.Delete("L1Key")
	_, has := c.Get("L1Key")
	assert.False(t, has, "L1 entry must be gone (Get queues a fresh fetch)")

	// Put one into L2 by forcing a demotion.
	c.AddItem("InL1", 1)
	c.AdvanceEpoch()
	c.AddItem("Other", 2) // demotes "InL1" into the stash

	// Sanity: "InL1" can be promoted back from L2 via Get
	v, has := c.Get("InL1")
	assert.True(t, has)
	assert.Equal(t, 1, v)

	// Re-demote and then Delete from L2.
	c.AdvanceEpoch()
	c.AddItem("Filler", 99) // demote "InL1" again (it was promoted back to L1 by the Get)
	c.Delete("InL1")
	_, has = c.Get("InL1")
	assert.False(t, has, "L2 entry must be gone after Delete")
}

// TestAddItemSlice_AndIter2 covers the two helper insertion methods.
func TestAddItemSlice_AndIter2(t *testing.T) {
	f := NewMockFetcher()
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	c.AddItemSlice([]string{"a", "b", "c"}, []int{1, 2, 3})
	for _, k := range []string{"a", "b", "c"} {
		v, has := c.Get(k)
		assert.True(t, has, "expected %q present after AddItemSlice", k)
		assert.NotZero(t, v)
	}

	c.AddItemIter2(func(yield func(string, int) bool) {
		if !yield("d", 4) {
			return
		}
		yield("e", 5)
	})
	for k, want := range map[string]int{"d": 4, "e": 5} {
		v, has := c.Get(k)
		assert.True(t, has, "expected %q present after AddItemIter2", k)
		assert.Equal(t, want, v)
	}
}

// TestMarkAsStale_DirectStateTransitions exercises MarkAsStale on its own:
// after marking, strict Get queues a fetch and returns miss; GetAcceptStale
// returns the stale value AND queues; a successful fetch restores Fresh state.
func TestMarkAsStale_DirectStateTransitions(t *testing.T) {
	f := NewMockFetcher()
	f.data["K"] = 100
	c := NewReadThroughCache[string, int, int](4, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	c.AddItem("K", 1)
	c.MarkAsStale("K")

	// MarkAsStale on a missing key is a no-op.
	c.MarkAsStale("DoesNotExist")

	// Strict Get: stale is a miss.
	_, has := c.Get("K")
	assert.False(t, has, "strict Get on stale must be a miss")

	// Stash-clear and let the queued fetch resolve.
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls)

	// Now the item should be fresh again with the upstream value.
	v, has := c.Get("K")
	assert.True(t, has)
	assert.Equal(t, 100, v)
}

// TestPartitionedFetch_OnePerPartition: keys spread across N partitions cause
// exactly N FetchItemSinglePartition calls, each with its own subset.
func TestPartitionedFetch_OnePerPartition(t *testing.T) {
	f := NewMockFetcher()
	f.partitionFn = func(k string) uint64 {
		// "p0-x" → 0, "p1-x" → 1, ...
		var p uint64
		fmt.Sscanf(k, "p%d-", &p)
		return p
	}
	f.data["p0-a"], f.data["p0-b"] = 1, 2
	f.data["p1-a"] = 3
	f.data["p2-a"], f.data["p2-b"] = 4, 5

	c := NewReadThroughCache[string, int, int](20, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](10)))

	for range c.WorkItem(1) {
		for _, k := range []string{"p0-a", "p0-b", "p1-a", "p2-a", "p2-b"} {
			c.Get(k)
		}
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}

	assert.Equal(t, 3, f.fetchCalls, "one fetch per partition")

	// Verify keys ended up in the right partition's batch. Partition order
	// of iteration is map-random, so check the pairing as-is before
	// sorting for the membership assertion.
	for i, p := range f.fetchedPartitions {
		for _, k := range f.fetchedBatches[i] {
			assert.Equal(t, p, f.partitionFn(k),
				"key %q in batch for partition %d but partitionFn says %d", k, p, f.partitionFn(k))
		}
	}
	parts := slices.Clone(f.fetchedPartitions)
	slices.Sort(parts)
	assert.Equal(t, []uint64{0, 1, 2}, parts, "each partition visited exactly once")

	// All values are now present.
	for k, want := range f.data {
		v, has := c.Get(k)
		assert.True(t, has, "expected %q present", k)
		assert.Equal(t, want, v)
	}
}

// TestMaxPartitions_TriggersFetch: with MinKeys high and MaxPartitions=2,
// reaching 2 partitions should force an early flush even though MinKeys is
// not satisfied. The flush dispatches one FetchItemSinglePartition call per
// queued partition.
func TestMaxPartitions_TriggersFetch(t *testing.T) {
	f := NewMockFetcher()
	f.partitionFn = func(k string) uint64 {
		if k == "alpha" {
			return 0
		}
		return 1
	}
	f.data["alpha"], f.data["beta"] = 7, 8

	c := NewReadThroughCache[string, int, int](10, f,
		FetchCriteria{MinKeys: 100, MaxPartitions: 2},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		// First miss queues partition 0 (under Max). Second miss adds
		// partition 1 → nParts=2 → MaxPartitions hit → synchronous fetch
		// that drains both queued partitions.
		c.Get("alpha")
		c.Get("beta")
	}

	assert.Equal(t, 2, f.fetchCalls, "synchronous fetch drained both partitions")
	slices.Sort(f.fetchedPartitions)
	assert.Equal(t, []uint64{0, 1}, f.fetchedPartitions)

	// Both values are now in L1.
	v, has := c.Get("alpha")
	assert.True(t, has)
	assert.Equal(t, 7, v)
	v, has = c.Get("beta")
	assert.True(t, has)
	assert.Equal(t, 8, v)
}

// TestMinPartitions_DoesNotTriggerWhenBelow: when only Min thresholds are set
// and none are met, IterateReadyWorkItems must not fetch.
func TestMinPartitions_DoesNotTriggerWhenBelow(t *testing.T) {
	f := NewMockFetcher()
	f.partitionFn = func(k string) uint64 { return 0 }
	f.data["a"] = 1

	c := NewReadThroughCache[string, int, int](10, f,
		FetchCriteria{MinPartitions: 3, MinKeys: 1000, MinWorkItems: 1000},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	for range c.WorkItem(1) {
		c.Get("a")
	}
	for range c.IterateReadyWorkItems(context.Background()) {
	}
	assert.Equal(t, 0, f.fetchCalls, "no Min threshold met → no fetch")

	// IterateRest forces it.
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls, "IterateRest always flushes")
}

// TestSliceStash_Eviction covers SliceStash directly: round-robin eviction
// preserves insertion-order positions modulo capacity.
func TestSliceStash_Eviction(t *testing.T) {
	s := NewSliceStash[string, int](2)
	assert.False(t, s.Add("a", StashEntry[int]{Value: 1}))
	assert.False(t, s.Add("b", StashEntry[int]{Value: 2}))
	assert.Equal(t, 2, s.Len())
	assert.Equal(t, 2, s.Cap())

	// Full; next Add evicts via round-robin (slot 0 → "a").
	assert.True(t, s.Add("c", StashEntry[int]{Value: 3}))
	_, hasA := s.GetAndRemove("a")
	assert.False(t, hasA, "a should be the round-robin victim")

	// "b" and "c" remain.
	e, has := s.GetAndRemove("b")
	v := e.Value
	assert.True(t, has)
	assert.Equal(t, 2, v)
	ee, has2 := s.GetAndRemove("c")
	v, has = ee.Value, has2
	assert.True(t, has)
	assert.Equal(t, 3, v)
	assert.Equal(t, 0, s.Len())
}

// TestMapStash_Eviction verifies MapStash bounds itself: Add at cap drops one.
func TestMapStash_Eviction(t *testing.T) {
	s := NewMapStash[string, int](2)
	assert.False(t, s.Add("a", StashEntry[int]{Value: 1}))
	assert.False(t, s.Add("b", StashEntry[int]{Value: 2}))
	// Updating existing key at cap does not evict.
	assert.False(t, s.Add("a", StashEntry[int]{Value: 11}))
	ea, has := s.GetAndRemove("a")
	v := ea.Value
	assert.True(t, has)
	assert.Equal(t, 11, v)

	// Refill and overflow.
	s.Add("a", StashEntry[int]{Value: 1})
	assert.True(t, s.Add("c", StashEntry[int]{Value: 3}), "Add beyond cap must evict")
	assert.Equal(t, 2, s.Len())
}

// partialFetcher pre-populates `good` keys before returning an error for
// the `bad` keys in the same batch.
type partialFetcher struct {
	good       map[string]int
	bad        map[string]bool
	fetchCalls int
}

func (f *partialFetcher) DeterminePartition(string) uint64 { return 0 }
func (f *partialFetcher) FetchItemSinglePartition(_ context.Context, _ uint64, keys []string, target ItemTargetI[string, int]) error {
	f.fetchCalls++
	for _, k := range keys {
		if v, ok := f.good[k]; ok {
			target.AddItem(k, v)
		}
	}
	for _, k := range keys {
		if f.bad[k] {
			return errors.New("partial failure: " + k)
		}
	}
	return nil
}

// TestMarkAsError_PreservesAlreadyFetched verifies the P2.1 contract:
// when a fetcher AddItems some keys and then returns an error for others,
// the successful keys must remain readable. Only keys still in the
// pending/queued state get flipped to ItemStateError.
func TestMarkAsError_PreservesAlreadyFetched(t *testing.T) {
	f := &partialFetcher{
		good: map[string]int{"keepA": 1, "keepB": 2},
		bad:  map[string]bool{"failC": true},
	}
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	c.SetErrorBackoff(time.Hour) // long enough that backoff won't expire mid-test

	for range c.WorkItem(1) {
		c.Get("keepA")
		c.Get("keepB")
		c.Get("failC")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls)

	v, has := c.Get("keepA")
	assert.True(t, has, "keepA must survive partial-failure marking")
	assert.Equal(t, 1, v)

	v, has = c.Get("keepB")
	assert.True(t, has, "keepB must survive partial-failure marking")
	assert.Equal(t, 2, v)

	// failC is in error state — Get returns false and the circuit breaker
	// must suppress retries within the backoff window.
	priorCalls := f.fetchCalls
	for range c.WorkItem(2) {
		_, has = c.Get("failC")
		assert.False(t, has)
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, priorCalls, f.fetchCalls, "failC must be suppressed by circuit breaker")
}

// TestReplay_CascadingMiss_ReQueued verifies the P2.2 contract:
// when a work item is yielded from Iterate*WorkItems and its replay logic
// then misses on a key discovered only after the first fetch (a "cascading
// dependency"), the cache must restore the work-item context so the new
// miss re-queues this item rather than dropping it on the floor.
func TestReplay_CascadingMiss_ReQueued(t *testing.T) {
	f := NewMockFetcher()
	f.data["root"] = 100
	f.data["leaf"] = 200

	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](4)))

	completed := false
	step := func() {
		_, hasRoot := c.Get("root")
		if !hasRoot {
			return
		}
		// Cascading dependency: only known once "root" is in hand.
		_, hasLeaf := c.Get("leaf")
		if !hasLeaf {
			return
		}
		completed = true
	}

	// Pass 1: queue "root".
	for range c.WorkItem(42) {
		step()
	}
	assert.False(t, completed)

	// Flush: fetches "root", then yields work item 42 for replay.
	// Replay finds "root" in L1 and queries "leaf" (cascading miss).
	// With the P2.2 fix, that miss must re-enter pendingWorkItems.
	yielded := 0
	for range c.IterateRestWorkItems(context.Background()) {
		yielded++
		step()
	}
	assert.Equal(t, 1, yielded)
	assert.False(t, completed, "leaf still missing after first flush")

	// Pass 2: replay flush. With the cascading miss re-queued, "leaf"
	// gets fetched and the next replay completes the work item.
	for range c.IterateRestWorkItems(context.Background()) {
		step()
	}
	assert.True(t, completed, "cascading dependency must resolve on second flush")
	assert.Equal(t, 2, f.fetchCalls, "exactly two fetches: root then leaf")
}

// --- Benchmarks ---

// Minimal fetcher for benchmarks to avoid alloc overhead of the MockFetcher
type BenchFetcher struct{}

func (inst *BenchFetcher) DeterminePartition(key string) uint64 { return 0 }
func (inst *BenchFetcher) FetchItemSinglePartition(ctx context.Context, p uint64, keys []string, t ItemTargetI[string, int]) error {
	// Minimal allocation fetch
	for _, k := range keys {
		t.AddItem(k, 1)
	}
	return nil
}

// Benchmark: Happy Path (100% L1 Hits)
// Simulates a hot cache where no I/O is needed.
func BenchmarkReadThrough_Hit_L1(b *testing.B) {
	f := &BenchFetcher{}
	c := NewReadThroughCache[string, int, int](1000, f, FetchCriteria{MinKeys: 0},
		WithStash[string, int, int](NewSliceStash[string, int](100)))

	// Pre-warm
	for i := 0; i < 1000; i++ {
		c.AddItem(fmt.Sprintf("key-%d", i), i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Access random key in range
		key := fmt.Sprintf("key-%d", i%1000)
		c.Get(key)
	}
}

// Benchmark: Cold Start (100% Misses)
// Simulates the overhead of batching and fetching.
func BenchmarkReadThrough_Cold_Fetch(b *testing.B) {
	f := &BenchFetcher{}
	// Large capacity to avoid eviction overhead, focusing on Fetch overhead
	c := NewReadThroughCache[string, int, int](b.N+1, f, FetchCriteria{MinKeys: 100},
		WithStash[string, int, int](NewSliceStash[string, int](100)))
	ctx := context.Background()

	b.ResetTimer()

	// We simulate chunks of work
	chunkSize := 100
	for i := 0; i < b.N; i += chunkSize {
		// 1. Queue Work
		for j := 0; j < chunkSize; j++ {
			id := i + j
			key := fmt.Sprintf("key-%d", id)
			for range c.WorkItem(id) {
				c.Get(key)
			}
		}

		// 2. Fetch & Complete
		// IterateReady should trigger the fetch because MinKeys=100
		count := 0
		for range c.IterateReadyWorkItems(ctx) {
			count++
		}
	}
}

// Benchmark: Churn / Thrashing (Working Set > Cache Size)
// Simulates heavy pressure on the Stash and Eviction logic.
// Cache Size: 100. Working Set: 1000.
// This forces 90% of items to flow through the Stash.
func BenchmarkReadThrough_Stash_Thrash(b *testing.B) {
	f := &BenchFetcher{}
	c := NewReadThroughCache[string, int, int](100, f, FetchCriteria{MinKeys: 10}, WithStash[string, int, int](NewSliceStash[string, int](1000)))
	ctx := context.Background()

	// Pre-fill partially to avoid initial cold start noise
	for i := 0; i < 100; i++ {
		c.AddItem(fmt.Sprintf("key-%d", i), 1)
	}

	b.ResetTimer()

	// We process a rolling window of keys
	// WorkItem J needs Key J.
	// Since we iterate sequentially 0..N, we constantly evict 0 to make room for 101, etc.

	// We batch them in groups of 10 to allow *some* batching,
	// but the eviction pressure is constant.
	batchSize := 10
	for i := 0; i < b.N; i += batchSize {
		// Queue
		for j := 0; j < batchSize; j++ {
			id := i + j
			key := fmt.Sprintf("key-%d", id%1000) // Wrap around 1000 keys
			for range c.WorkItem(id) {
				c.Get(key)
			}
		}
		// Fetch
		for range c.IterateRestWorkItems(ctx) {
		}
	}
}

// Benchmark: Many Small Work Items
// Simulates overhead of the WorkItem iterator and state tracking.
// 10,000 work items, each needs 1 key.
func BenchmarkReadThrough_SmallWorkItems(b *testing.B) {
	f := &BenchFetcher{}
	// Use a smaller cache size to trigger eviction logic sooner,
	// or keep it large but ensure we don't hit the O(N*Cap) loop.
	c := NewReadThroughCache[string, int, int](100000, f, FetchCriteria{MinKeys: 100}, WithStash[string, int, int](NewSliceStash[string, int](100)))
	ctx := context.Background()

	b.ResetTimer()

	batchSize := 100
	for i := 0; i < b.N; i += batchSize {
		// Advance the epoch
		// This tells the cache: "The items from the previous batch are no longer
		// strictly required for the current operation, so you may evict them."
		c.AdvanceEpoch()

		// Register 100 tiny work items
		for j := 0; j < batchSize; j++ {
			id := i + j
			key := fmt.Sprintf("key-%d", id)
			for range c.WorkItem(id) {
				c.Get(key)
			}
		}

		// Flush
		for range c.IterateRestWorkItems(ctx) {
		}
	}
}
