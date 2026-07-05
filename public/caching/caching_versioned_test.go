package caching

// Tests for the versioned write-through semantics (WithVersioning,
// Pin/Unpin, WithFreshnessTTL, MarkAsStaleIfOlder). Each core scenario
// mirrors a machine-checked witness or counterfactual trace from
// verification/formal/caching — the Go test asserts the SAFE outcome the
// spec proves, on the real implementation.
//
// Convention: values are ints and carry their own order (orderOf =
// identity), exactly like the spec, so a value doubles as its version.

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func intOrder(v int) int64 { return int64(v) }

func newVersionedCache(f *MockFetcher, opts ...CacheOption[string, int, int]) *ReadThroughCache[string, int, int] {
	all := append([]CacheOption[string, int, int]{
		WithVersioning[string, int, int](intOrder),
		WithStash[string, int, int](NewSliceStash[string, int](4)),
	}, opts...)
	return NewReadThroughCache[string, int, int](2, f, FetchCriteria{}, all...)
}

// The three admission outcomes: newer replaces, equal confirms (clears
// staleness, restarts the freshness clock), older is rejected.
func TestVersionGate_ThreeOutcomes(t *testing.T) {
	f := NewMockFetcher()
	c := newVersionedCache(f)

	c.AddItem("k", 5)
	v, has := c.Get("k")
	assert.True(t, has)
	assert.Equal(t, 5, v)

	// Older: rejected.
	c.AddItem("k", 3)
	v, _ = c.Get("k")
	assert.Equal(t, 5, v, "older insert must bounce off the gate")

	// Equal: confirms — a stale mark is cleared without a value change.
	c.MarkAsStale("k")
	_, has = c.Get("k")
	assert.False(t, has, "stale strict read misses")
	c.AddItem("k", 5) // revalidation delivered the same version
	v, has = c.Get("k")
	assert.True(t, has, "equal-version confirmation restores freshness")
	assert.Equal(t, 5, v)

	// Newer: replaces.
	c.AddItem("k", 9)
	v, _ = c.Get("k")
	assert.Equal(t, 9, v)
}

// Spec witness racedFetchRejectedTest (versioned_cache.qnt) and the
// unsafe_lww counterfactual, inverted: a fetch queued before a Commit
// delivers the older durable row and must NOT resurrect it.
func TestVersionGate_RacedFetchRejected(t *testing.T) {
	f := NewMockFetcher()
	f.data["k"] = 1 // upstream (durable) version 1
	c := newVersionedCache(f)

	// Cold cache: queue the fetch.
	for range c.WorkItem(1) {
		c.Get("k")
	}
	// Commit v2 lands before the flush (write-through + pin).
	c.AddItem("k", 2)
	c.Pin("k")
	v, has := c.Get("k")
	assert.True(t, has)
	assert.Equal(t, 2, v)

	// The queued fetch now runs and delivers durable=1.
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls)

	v, has = c.Get("k")
	assert.True(t, has, "entry must survive the raced delivery")
	assert.Equal(t, 2, v, "gate must reject the pre-write row (no resurrection)")
}

// Spec witness dirtyEvictionRegressionTest (unsafe_nopin), inverted: the
// pin closes the dirty-eviction hole the gate alone cannot — a pinned
// dirty entry is immune to eviction pressure, so the older durable row is
// never admitted into a cold cache.
func TestPin_DirtyEvictionClosed(t *testing.T) {
	f := NewMockFetcher()
	f.data["k"] = 1 // durable v1
	c := newVersionedCache(f)

	// Commit v2: write-through + pin (dirty window opens).
	c.AddItem("k", 2)
	c.Pin("k")

	// Adversarial pressure: fill L1 (cap 2) across epochs — the pinned
	// dirty entry must never be the victim.
	c.AdvanceEpoch()
	c.AddItem("a", 10)
	c.AdvanceEpoch()
	c.AddItem("b", 11)
	c.AdvanceEpoch()
	c.AddItem("d", 12)

	v, has := c.Get("k")
	assert.True(t, has, "pinned dirty entry must survive eviction pressure")
	assert.Equal(t, 2, v)

	// Flush: durable catches up, the pin releases, eviction may now take it —
	// and a refetch delivers the flushed version, preserving monotonicity.
	f.data["k"] = 2
	c.Unpin("k")
	c.Delete("a")
	c.Delete("b")
	c.Delete("d")
	c.AdvanceEpoch()
	c.AddItem("x", 20)
	c.AddItem("y", 21) // pressure may demote/displace k now

	for range c.WorkItem(2) {
		c.Get("k")
	}
	for range c.IterateRestWorkItems(context.Background()) {
	}
	v, has = c.Get("k")
	assert.True(t, has)
	assert.Equal(t, 2, v, "post-flush refetch serves the flushed version — no regression")
}

// Pin hoists a stash-resident entry into L1, pinned entries may hold L1
// beyond capacity, and Unpin restores ordinary evictability. Pins survive
// a newer write (a second Commit keeps the dirty-window latch).
func TestPin_HoistExceedCapacityAndReplacement(t *testing.T) {
	f := NewMockFetcher()
	c := newVersionedCache(f)

	// Demote an entry into the stash, then Pin: it must hoist back to L1.
	c.AddItem("k", 1)
	c.AdvanceEpoch()
	c.AddItem("a", 10)
	c.AddItem("b", 11) // k demoted (cap 2)
	assert.Equal(t, 1, c.StashLen(), "k is stash-resident")
	c.Pin("k")
	assert.Equal(t, 0, c.StashLen(), "Pin must hoist out of the stash")
	assert.Equal(t, 3, c.Len(), "pinned entries may exceed L1 capacity")

	// A newer write to a pinned key keeps the pin.
	c.AddItem("k", 2)
	c.AdvanceEpoch()
	c.AddItem("x", 20)
	c.AddItem("y", 21)
	v, has := c.Get("k")
	assert.True(t, has, "pin must survive the replacement")
	assert.Equal(t, 2, v)

	// Idempotence and no-ops.
	c.Pin("k")
	c.Unpin("k")
	c.Unpin("k")
	c.Pin("ghost") // uncached: no-op
	assert.Equal(t, 0, c.QueuedKeys())
}

// Spec witnesses swrRevalidationTest + demotionCarriesAgeTest
// (freshness_ttl.qnt): entries age out into stale-while-revalidate, an
// equal-version revalidation restores freshness, and the age stamp
// travels through the stash (a tier round-trip must not rejuvenate).
func TestFreshnessTTL_AgeOutRevalidateAndStashRoundTrip(t *testing.T) {
	f := NewMockFetcher()
	f.data["k"] = 5
	now := time.Unix(1000, 0)
	c := newVersionedCache(f, WithFreshnessTTL[string, int, int](10*time.Second))
	c.nowFn = func() time.Time { return now }

	c.AddItem("k", 5)
	v, has := c.Get("k")
	assert.True(t, has)
	assert.Equal(t, 5, v)

	// Age past the TTL: strict reads miss (and queue), accept-stale serves.
	now = now.Add(11 * time.Second)
	_, has = c.Get("k")
	assert.False(t, has, "aged-out entry must read as stale")
	v, has, stale := c.GetAcceptStale("k")
	assert.True(t, has)
	assert.True(t, stale, "SWR serves the aged value")
	assert.Equal(t, 5, v)

	// The queued refresh delivers the SAME version: equal-confirm restarts
	// the freshness clock without replacing the value.
	for range c.IterateRestWorkItems(context.Background()) {
	}
	assert.Equal(t, 1, f.fetchCalls)
	v, has = c.Get("k")
	assert.True(t, has, "revalidation must restore freshness")
	assert.Equal(t, 5, v)

	// Stash round-trip: demote a fresh entry, age it in the stash, promote —
	// it must still read as stale (the stamp travelled; no rejuvenation).
	c.AddItem("j", 7)
	c.AdvanceEpoch()
	c.AddItem("a", 10)
	c.AddItem("b", 11) // j demoted with its stamp
	now = now.Add(11 * time.Second)
	_, has = c.Get("j") // promotes, then routes as stale
	assert.False(t, has, "a stash round-trip must not reset the age stamp")
	v, has, stale = c.GetAcceptStale("j")
	assert.True(t, has)
	assert.True(t, stale)
	assert.Equal(t, 7, v)
}

// MarkAsStaleIfOlder: a redundant external-writer signal (cache already
// holds >= ver) is free; a genuinely newer signal stales; the stash-
// resident path works; counter mode degrades to unconditional.
func TestMarkAsStaleIfOlder(t *testing.T) {
	f := NewMockFetcher()
	f.data["k"] = 5
	c := newVersionedCache(f)

	c.AddItem("k", 5)
	c.MarkAsStaleIfOlder("k", 5) // redundant: cache already has v5
	v, has := c.Get("k")
	assert.True(t, has, "redundant signal must be free")
	assert.Equal(t, 5, v)

	c.MarkAsStaleIfOlder("k", 6) // newer exists upstream
	_, has = c.Get("k")
	assert.False(t, has, "newer-version signal must stale the entry")

	// Stash-resident path.
	f2 := NewMockFetcher()
	c2 := newVersionedCache(f2)
	c2.AddItem("j", 3)
	c2.AdvanceEpoch()
	c2.AddItem("a", 10)
	c2.AddItem("b", 11) // j demoted
	c2.MarkAsStaleIfOlder("j", 3)
	v, has = c2.Get("j")
	assert.True(t, has, "redundant signal on a stash entry must be free")
	assert.Equal(t, 3, v)
	c2.MarkAsStaleIfOlder("j", 4)
	_, has = c2.Get("j")
	assert.False(t, has, "newer signal reaches stash-resident entries")

	// Counter mode: incomparable domains degrade to unconditional stale.
	f3 := NewMockFetcher()
	c3 := NewReadThroughCache[string, int, int](2, f3, FetchCriteria{},
		WithStash[string, int, int](NewSliceStash[string, int](4)))
	c3.AddItem("k", 1)
	c3.MarkAsStaleIfOlder("k", -999) // meaningless ver in counter mode
	_, has = c3.Get("k")
	assert.False(t, has, "counter mode falls back to MarkAsStale")
}

// The gate compares against a stash shadow too: an older insert while the
// only copy sits in L2 must not shadow it.
func TestVersionGate_StashShadowCompared(t *testing.T) {
	f := NewMockFetcher()
	c := newVersionedCache(f)

	c.AddItem("k", 5)
	c.AdvanceEpoch()
	c.AddItem("a", 10)
	c.AddItem("b", 11) // k (v5) demoted to the stash

	c.AddItem("k", 3) // older than the stash copy: must be rejected
	v, has := c.Get("k")
	assert.True(t, has)
	assert.Equal(t, 5, v, "the stash copy must win over an older insert")
}
