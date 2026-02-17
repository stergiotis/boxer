//go:build llm_generated_gemini3pro

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
