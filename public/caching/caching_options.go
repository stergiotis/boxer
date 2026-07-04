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
