package caching

import "time"

// CacheOption defines a functional option for configuring the ReadThroughCache.
type CacheOption[K comparable, V any, W comparable] func(*ReadThroughCache[K, V, W])

// WithStash configures a custom L2/Victim cache backend.
// If not provided, the cache defaults to a memory-dense SliceStash with 50% of the L1 capacity.
func WithStash[K comparable, V any, W comparable](backend StashBackendI[K, V]) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.stash = backend
	}
}

// WithMetrics configures the observability collector.
// If not provided, a no-op collector is used.
func WithMetrics[K comparable, V any, W comparable](m MetricsCollectorI) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.metrics = m
	}
}

// WithErrorBackoff configures the duration a key remains in the Error state
// (Circuit Breaker open) before a retry is allowed.
// Default is 5 seconds.
func WithErrorBackoff[K comparable, V any, W comparable](d time.Duration) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.errorBackoffDur = d
	}
}

// WithVersioning makes admission version-gated: every value carries a
// monotonic order extracted by orderOf (the version is intrinsic to the
// value, so it can never be mismatched against it), and an insert for a
// cached key is admitted only by the three-outcome rule:
//
//   - newer than the cached copy  → replace, mark fresh;
//   - equal to the cached copy    → keep, clear staleness, restart the
//     freshness clock (a revalidation confirmed currency);
//   - older than the cached copy  → reject (a raced fetch returning a
//     pre-write row bounces off instead of resurrecting it).
//
// The order type is int64, chosen deliberately over uint64: it must be
// order-isomorphic with the source's ordering column (recordstore's Order
// is a signed DateTime64 — `ent.Order.UnixNano()` is the canonical
// orderOf). A uint64 conversion of a pre-epoch or zero time would wrap to
// a huge value and permanently poison the key's admission; the same input
// as int64 orders as "very old" and is rejected — exactly the verdict the
// source's own ORDER BY would give that row. Negative orders are fine:
// admission compares only against entries that exist, so no sentinel
// value is reserved.
//
// Without this option the cache uses an internal monotonic counter, which
// is exactly last-insert-wins (the pre-versioning semantics).
//
// Write-through consumers pair the gate with Pin/Unpin: the gate alone
// cannot protect a written-but-unflushed version that gets EVICTED (a
// refetch then compares against nothing) — see the dirty-pin
// counterfactual in verification/formal/caching.
func WithVersioning[K comparable, V any, W comparable](orderOf func(V) int64) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.orderOf = orderOf
	}
}

// WithFreshnessTTL enables age-based staleness onset for
// stale-while-revalidate: an entry older than ttl (since it was admitted
// or last confirmed by an equal-version revalidation) reads as stale —
// strict Get misses and queues a refresh, GetAcceptStale serves it while
// the refresh is in flight. The age stamp travels through the stash, so
// demotion does not reset freshness. Zero (the default) disables age
// staleness; MarkAsStale remains available either way.
func WithFreshnessTTL[K comparable, V any, W comparable](ttl time.Duration) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.freshTTL = ttl
	}
}

// WithNegativeCaching enables absent-key marking: after a clean fetch,
// requested keys the fetcher did not deliver are treated as absent upstream
// for ttl. A Get on an absent-marked key misses without queueing a fetch
// and without suspending the current work item, so replay loops over keys
// that do not exist terminate instead of re-fetching forever.
//
// Disabled by default (ttl <= 0 keeps it off): misses on absent keys then
// re-queue on every discovery pass, and distinguishing "absent" from
// "not fetched yet" is the caller's job.
func WithNegativeCaching[K comparable, V any, W comparable](ttl time.Duration) CacheOption[K, V, W] {
	return func(c *ReadThroughCache[K, V, W]) {
		c.absentTTL = ttl
	}
}
