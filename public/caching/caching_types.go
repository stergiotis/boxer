package caching

import (
	"context"
	"time"
)

// MetricsCollectorI defines the observability hooks.
//
// RecordHit semantics:
//   - l1: true for a Primary (L1) hit, false for a Stash (L2) hit.
//   - stale: true when the served value was marked stale (only possible via
//     GetAcceptStale; strict Get treats stale as a miss).
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
	RecordHit(l1 bool, stale bool)       // See interface doc for semantics.
	RecordMiss()                         // Item not found, fetch queued (or suppressed)
	RecordFetchError(count int)          // Number of keys failed
	RecordEviction(toStash bool)         // See interface doc for semantics.
	RecordFetchDuration(d time.Duration) // Time taken by fetcher
}

// ItemFetcherI retrieves values from the backing source (L3) in
// partition-grouped batches.
//
// Contract:
//   - keys never contains duplicates; every key was queued by a miss (or a
//     stale refresh) since the previous flush.
//   - The fetcher may retain the keys slice after returning; the cache does
//     not reuse it.
//   - The fetcher may call target.AddItem for some keys and then return an
//     error for the rest: delivered keys keep their fresh values, only the
//     undelivered ones enter the circuit-breaker backoff.
//   - Keys the fetcher does not deliver on a nil-error return are treated as
//     absent upstream (recorded only when negative caching is enabled).
//   - DeterminePartition is evaluated once, at queue time — a key's
//     partition is frozen until it is fetched.
//   - The fetcher may read the cache (Get/GetAcceptStale) re-entrantly;
//     misses queued during a flush are fetched on the NEXT flush (a nested
//     flush is a no-op). It must not call Iterate*WorkItems or Clear.
type ItemFetcherI[K comparable, V any] interface {
	DeterminePartition(key K) uint64
	FetchItemSinglePartition(ctx context.Context, partition uint64, keys []K, target ItemTargetI[K, V]) error
}

// ItemTargetI is the sink a fetcher delivers values into.
type ItemTargetI[K comparable, V any] interface {
	AddItem(k K, v V)
}

// ReadThroughCache is a single-goroutine, batching, read-through cache with
// work-item suspend/replay bookkeeping (see the package README).
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

	// Circuit breaker: key → retry-allowed deadline. Value-free side table,
	// bounded by sideTableBound; overflow drops a random entry, which merely
	// allows an early retry.
	errorUntil map[K]time.Time

	// Negative cache: key → absent-mark expiry. nil when negative caching
	// is disabled (the default); bounded like errorUntil, and an overflow
	// drop merely allows an early refetch.
	absentUntil map[K]time.Time

	// Per-flush bookkeeping: guards against nested flushes and records the
	// keys the fetcher actually delivered (fetchAdded is non-nil only while
	// a flush is running).
	fetching   bool
	fetchAdded map[K]struct{}

	// SIEVE eviction state for L1: the insertion-order ring and the hand
	// (see ensureSpaceByEvictingOne). A slot is stale once its entry left
	// L1 (detected via primaryItem.slot) and is reclaimed by lazy
	// compaction in placeL1.
	order []K
	hand  int

	// Context
	currentWorkItem W
	hasCurrentWork  bool

	// Versioning (WithVersioning). orderOf == nil selects the internal
	// counter (verSeq), which degenerates to last-insert-wins.
	orderOf func(V) int64
	verSeq  int64

	// Configuration
	fetchCriteria   FetchCriteria
	errorBackoffDur time.Duration
	absentTTL       time.Duration
	freshTTL        time.Duration // WithFreshnessTTL; 0 = age never stales
	sideTableBound  int
	nowFn           func() time.Time // seam for deterministic tests
}

// StashEntry is the record an entry carries through the L2 tier. Entry
// state — the stale flag, the monotonic version, the freshness stamp —
// must survive demotion and promotion intact (the tier-boundary law: a
// state bit that does not travel with the entry is silently laundered at
// the first demotion; see the formal specs under verification/formal/caching).
type StashEntry[V any] struct {
	Value V
	// Ver is the entry's monotonic order (see WithVersioning); 0 in
	// internal-counter mode is never compared across restarts.
	Ver int64
	// Stamp is the freshness stamp in nanoseconds on the cache clock
	// (see WithFreshnessTTL).
	Stamp int64
	Stale bool
}

// StashBackendI acts as the L2/Victim cache storage.
// Implementations handle their own storage layout, eviction policy, and
// capacity management.
//
// The interface is infallible: disk-backed implementations must degrade a
// storage or codec error into a miss (GetAndRemove) or a silent no-op (Add,
// Delete) — the stash is best-effort by contract, the fetcher remains the
// source of truth.
type StashBackendI[K comparable, V any] interface {
	// GetAndRemove attempts to retrieve an entry.
	// If found, the item MUST be removed from the stash (atomic promote).
	GetAndRemove(key K) (e StashEntry[V], found bool)

	// Add inserts an entry into the stash, carrying its state intact.
	// An existing key MUST be updated in place — never duplicated — and an
	// update MUST NOT evict. If the stash is full and the key is new, the
	// implementation MUST evict an item to make room.
	// Returns:
	//   evicted: true if a valid item was dropped to make space (Data Loss).
	Add(key K, e StashEntry[V]) (evicted bool)

	// Delete removes the item if it exists (invalidation).
	Delete(key K)

	// Len returns the current number of items.
	Len() int

	// Cap returns the maximum capacity (0 = unbounded, disk-backed only).
	Cap() int

	// Clear removes every item.
	Clear()
}

// FetchCriteria controls when a queued batch is flushed to the fetcher.
//
// All Min and Max thresholds are evaluated independently and **OR'd**: any
// single threshold being reached triggers a fetch. Max thresholds fire
// synchronously from inside Get / GetAcceptStale on every queueing path
// (cold miss and stale refresh alike), so a single oversized work item
// still gets chunked. Min thresholds are checked by IterateReadyWorkItems
// and require a follow-up call; when nothing is queued but work items are
// pending (their keys already flushed synchronously), IterateReadyWorkItems
// replays them without fetching. IterateRestWorkItems always flushes
// regardless of criteria.
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
