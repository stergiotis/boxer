package caching

import (
	"context"
	"iter"
	"time"
)

// MetricsCollectorI defines the observability hooks.
//
// RecordEviction semantics:
//   - toStash=true:  an L1 item was demoted to L2 (preserved, no data loss).
//   - toStash=false: an item was dropped from the cache entirely (data loss),
//     either because the stash overflowed during an L1 demotion or because a
//     freshly fetched value was spilled directly to a full stash.
//
// Both can fire from a single operation if an L1 demotion finds the stash
// full: one (true) for the demoted L1 item, one (false) for the displaced
// stash item.
type MetricsCollectorI interface {
	RecordHit(l1 bool)                   // l1=true (Primary), l1=false (Stash)
	RecordMiss()                         // Item not found, fetch triggered
	RecordFetchError(count int)          // Number of keys failed
	RecordEviction(toStash bool)         // See interface doc for semantics.
	RecordFetchDuration(d time.Duration) // Time taken by fetcher
}

type ItemFetcherI[K comparable, V any] interface {
	DeterminePartition(key K) uint64
	FetchItemSinglePartition(ctx context.Context, partition uint64, keys []K, target ItemTargetI[K, V]) error
}

type ItemTargetI[K comparable, V any] interface {
	AddItem(k K, v V)
	AddItemSlice(k []K, v []V)
	AddItemIter2(it iter.Seq2[K, V])
}

// ReadThroughCache V2: Production-Ready, Context-Aware, Batching Cache.
type ReadThroughCache[K comparable, V any, W comparable] struct {
	fetcher      ItemFetcherI[K, V]
	metrics      MetricsCollectorI
	cap          int
	currentEpoch uint64

	// primaryStore (L1). Uses value types (not pointers) to reduce GC overhead.
	primaryStore map[K]primaryItem[V]

	// Accounting & Batching
	keysToFetchSet   map[K]struct{}
	pendingWorkItems map[W]struct{}
	keysToFetch      map[uint64][]K

	// stash (L2/Victim).
	stash StashBackendI[K, V]

	// Context
	currentWorkItem W
	hasCurrentWork  bool

	// Configuration
	fetchCriteria   FetchCriteria
	errorBackoffDur time.Duration
}

// ItemStateE describes the state of an item in the cache.
type ItemStateE uint8

// StashBackendI acts as the L2/Victim cache storage.
// Implementations handle their own storage layout, eviction policy, and capacity management.
type StashBackendI[K comparable, V any] interface {
	// GetAndRemove attempts to retrieve a value.
	// If found, the item MUST be removed from the stash (atomic promote).
	// Returns:
	//   value: The data
	//   found: true if it existed
	GetAndRemove(key K) (value V, found bool)

	// Add inserts a value into the stash.
	// If the stash is full, the implementation MUST evict an item to make room.
	// Returns:
	//   evicted: true if a valid item was dropped to make space (Data Loss).
	Add(key K, value V) (evicted bool)

	// Delete removes the item if it exists (invalidation).
	Delete(key K)

	// Len returns the current number of items.
	Len() int

	// Cap returns the maximum capacity.
	Cap() int
}

// FetchCriteria controls when a queued batch is flushed to the fetcher.
//
// All Min and Max thresholds are evaluated independently and **OR'd**: any
// single threshold being reached triggers a fetch. Max thresholds fire
// synchronously from inside Get (so a single oversized work item still gets
// chunked); Min thresholds are checked by IterateReadyWorkItems and require
// a follow-up call. IterateRestWorkItems always flushes regardless of
// criteria.
//
// A zero value on a threshold disables it. If all three Min fields are zero,
// IterateReadyWorkItems treats any non-empty queue as ready.
type FetchCriteria struct {
	// MinWorkItems is the minimum number of distinct pending work items
	// before IterateReadyWorkItems will flush. Zero disables.
	MinWorkItems int
	// MaxWorkItems forces a synchronous flush from inside Get once at least
	// this many distinct work items are pending. Zero disables.
	MaxWorkItems int
	// MinKeys is the minimum number of queued keys before
	// IterateReadyWorkItems will flush. Zero disables.
	MinKeys int
	// MaxKeys forces a synchronous flush from inside Get once at least this
	// many keys are queued. Zero disables.
	MaxKeys int
	// MinPartitions is the minimum number of distinct partitions present in
	// the queue before IterateReadyWorkItems will flush. Zero disables.
	MinPartitions int
	// MaxPartitions forces a synchronous flush from inside Get once at
	// least this many distinct partitions are queued. Zero disables.
	MaxPartitions int
}
