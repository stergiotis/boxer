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
	HitsL1, HitsL2, Misses, Errors, EvictsStash, EvictsDrop int
}

func (m *MockMetrics) RecordHit(l1 bool) {
	if l1 {
		m.HitsL1++
	} else {
		m.HitsL2++
	}
}
func (m *MockMetrics) RecordMiss()            { m.Misses++ }
func (m *MockMetrics) RecordFetchError(c int) { m.Errors += c }
func (m *MockMetrics) RecordEviction(stash bool) {
	if stash {
		m.EvictsStash++
	} else {
		m.EvictsDrop++
	}
}
func (m *MockMetrics) RecordFetchDuration(d time.Duration) {}

type MockFetcherV2 struct {
	data           map[string]int
	failKeys       map[string]bool // Keys that trigger error
	panicKeys      map[string]bool // Keys that trigger panic
	fetchCalls     int
	fetchedBatches [][]string
}

func NewMockFetcherV2() *MockFetcherV2 {
	return &MockFetcherV2{
		data:           make(map[string]int),
		failKeys:       make(map[string]bool),
		panicKeys:      make(map[string]bool),
		fetchedBatches: make([][]string, 0),
	}
}

func (m *MockFetcherV2) DeterminePartition(key string) uint64 { return 0 }

func (m *MockFetcherV2) FetchItemSinglePartition(ctx context.Context, partition uint64, keys []string, target ItemTargetI[string, int]) error {
	m.fetchCalls++
	m.fetchedBatches = append(m.fetchedBatches, slices.Clone(keys))

	// Simulate Context Cancel
	if ctx.Err() != nil {
		return ctx.Err()
	}

	for _, k := range keys {
		if m.panicKeys[k] {
			panic(fmt.Sprintf("Panic on key %s", k))
		}
	}

	// Check for failures
	for _, k := range keys {
		if m.failKeys[k] {
			return errors.New("simulated fetch error")
		}
	}

	for _, k := range keys {
		if v, ok := m.data[k]; ok {
			target.AddItem(k, v)
		}
	}
	return nil
}

// --- Tests ---
func TestCircuitBreaker(t *testing.T) {
	// Scenario: Fetching "FailKey" fails.
	// 1. First attempt: Fails, marks as Error. Backoff set.
	// 2. Immediate retry: Should be blocked (No fetch call).
	// 3. Wait Backoff.
	// 4. Retry: Should trigger fetch.

	f := NewMockFetcherV2()
	f.failKeys["Fail"] = true
	m := &MockMetrics{}

	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0},
		WithMetrics[string, int, int](m),
		WithStash[string, int, int](NewSliceStash[string, int](10)))
	c.SetErrorBackoff(100 * time.Millisecond)

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
	// This iterate should NOT trigger a fetch because item is in Error state and backoff active
	for range c.IterateRestWorkItems(ctx) {
	}

	assert.Equal(t, 1, f.fetchCalls, "Should not fetch during backoff")

	// 3. Wait
	time.Sleep(150 * time.Millisecond)

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
	f := NewMockFetcherV2()
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
	has, _ := c.Get("Bomb")
	assert.False(t, has)
}

func TestContextCancellation(t *testing.T) {
	f := NewMockFetcherV2()
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0}, WithStash[string, int, int](NewSliceStash[string, int](10)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	for range c.WorkItem(1) {
		c.Get("A")
	}

	// Should not fetch (or abort inside fetch)
	for range c.IterateRestWorkItems(ctx) {
	}

	// Fetcher might be called but abort immediately or logic prevents it.
	// Our mock checks ctx.Err().
	// If the fetcher is called, it should return error/nil.

	// Note: IterateRestWorkItems calls performFetch.
	// performFetch checks ctx.Err inside the loop.
}

func TestV2BasicReadThrough(t *testing.T) {
	f := NewMockFetcherV2()
	f.data["A"] = 10
	m := &MockMetrics{}
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 0}, WithMetrics[string, int, int](m), WithStash[string, int, int](NewSliceStash[string, int](10)))

	done := false
	for range c.WorkItem(1) {
		h, v := c.Get("A")
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
	f := NewMockFetcherV2()
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
	// Expectation: New should go to Stash?
	// Wait, "ensureSpace" removes unpinned. If everything is pinned, ensureSpace returns true (useStash).
	// So "New" goes to Stash. "Old" stays in Primary.

	for range c.WorkItem(1) {
		c.Get("New")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}

	// Verify "Old" is still in primary (via implementation detail or behavior)
	// We can check behavior by ensuring Get("Old") is L1 hit.
	// But our mock metrics are easier.

	// Let's verify data presence
	h1, v1 := c.Get("Old")
	h2, v2 := c.Get("New")

	assert.True(t, h1)
	assert.Equal(t, 1, v1)
	assert.True(t, h2)
	assert.Equal(t, 99, v2)
}

// --- Coverage Tests ---

func TestGetAcceptStale_Comprehensive(t *testing.T) {
	f := NewMockFetcherV2()
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
		has, stale, val := c.GetAcceptStale("Fresh")
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
		has, stale, val := c.GetAcceptStale("Stale")
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
		has, stale, val := c.GetAcceptStale("Stale")
		assert.True(t, has)
		assert.False(t, stale)
		assert.Equal(t, 200, val)
	}

	// --- Case C: Missing Item ---
	// Should return (false, false, 0), trigger fetch
	for range c.WorkItem(3) {
		has, _, _ := c.GetAcceptStale("Missing")
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
	has, _, val := c.GetAcceptStale("Missing")
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
		has, _, _ := c.GetAcceptStale("Error")
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

	f := NewMockFetcherV2()

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
				has, _ := c.Get(key)
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

	fmt.Printf("Stats: Attempts=%d, Fetches=%d, StashDrops=%d\n", attempts, f.fetchCalls, m.EvictsDrop)
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

	f := NewMockFetcherV2()
	f.data["A"], f.data["B"], f.data["C"] = 1, 2, 3

	// Criteria: MaxKeys=2 (Force chunking), MinKeys=100 (Prevent early flush)
	c := NewReadThroughCache[string, int, int](10, f, FetchCriteria{MinKeys: 100, MaxKeys: 2},
		WithStash[string, int, int](NewSliceStash[string, int](10)))

	workID := 1
	completed := false

	runWork := func() {
		missing := false
		// Accumulate all 3
		if h, _ := c.Get("A"); !h {
			missing = true
		}
		if h, _ := c.Get("B"); !h {
			missing = true
		}
		if h, _ := c.Get("C"); !h {
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

// --- Benchmarks ---

// Minimal fetcher for benchmarks to avoid alloc overhead of the MockFetcherV2
type BenchFetcher struct{}

func (b *BenchFetcher) DeterminePartition(key string) uint64 { return 0 }
func (b *BenchFetcher) FetchItemSinglePartition(ctx context.Context, p uint64, keys []string, t ItemTargetI[string, int]) error {
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
