//go:build llm_generated_gemini3pro

package caching

import (
	"context"
	"iter"
	"time"
)

// MetricsCollectorI defines the observability hooks.
type MetricsCollectorI interface {
	RecordHit(l1 bool)                   // l1=true (Primary), l1=false (Stash)
	RecordMiss()                         // Item not found, fetch triggered
	RecordFetchError(count int)          // Number of keys failed
	RecordEviction(toStash bool)         // Evicted to Stash vs Dropped
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

	stashEvictPtr int
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

type FetchCriteria struct {
	MinWorkItems  int
	MaxWorkItems  int
	MinKeys       int
	MaxKeys       int
	MinPartitions int
	MaxPartitions int
}
