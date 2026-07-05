package caching

import (
	"context"
	"io"
	"iter"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/functional"
)

// primaryItem is optimized for memory padding.
type primaryItem[V any] struct {
	value    V
	ver      int64  // monotonic order (WithVersioning, or the internal counter)
	stamp    int64  // freshness stamp, ns on the cache clock (WithFreshnessTTL)
	lastSeen uint64 // Epoch for pinning
	slot     int    // this entry's slot in the SIEVE insertion ring
	stale    bool   // Set by MarkAsStale or by age; a stale read queues a refresh.
	pinned   bool   // Sticky dirty-window pin (Pin/Unpin): immune to eviction.
	visited  bool   // SIEVE access bit: set on hit, cleared by the hand.
}

// isStale reports the entry's effective staleness: the explicit flag, or —
// under WithFreshnessTTL — an age beyond the TTL.
func (inst *ReadThroughCache[K, V, W]) isStale(item primaryItem[V], now time.Time) bool {
	if item.stale {
		return true
	}
	return inst.freshTTL > 0 && now.UnixNano()-item.stamp > inst.freshTTL.Nanoseconds()
}

// versionOf assigns an insert's order: intrinsic via orderOf, or the
// internal counter (strictly increasing, so every insert is "newest" —
// exactly last-insert-wins).
func (inst *ReadThroughCache[K, V, W]) versionOf(v V) int64 {
	if inst.orderOf != nil {
		return inst.orderOf(v)
	}
	inst.verSeq++
	return inst.verSeq
}

// putBounded inserts into a side table, dropping a random entry when the
// bound is reached. Dropping is benign: a lost breaker entry allows an
// early retry, a lost absent mark an early refetch.
func putBounded[K comparable](m map[K]time.Time, k K, deadline time.Time, bound int) {
	if _, exists := m[k]; !exists && len(m) >= bound {
		for victim := range m {
			delete(m, victim)
			break
		}
	}
	m[k] = deadline
}

// stillBlocked reports whether the side table holds an unexpired deadline
// for k, deleting expired entries as it goes.
func stillBlocked[K comparable](m map[K]time.Time, k K, now time.Time) bool {
	deadline, ok := m[k]
	if !ok {
		return false
	}
	if now.After(deadline) {
		delete(m, k)
		return false
	}
	return true
}

// NewReadThroughCache creates a new dependency-aware batching cache.
//
// Parameters:
//   - capacity: The maximum number of items in the Primary (L1) store.
//     Must be >= 1.
//   - fetcher: The implementation for partition-aware data retrieval.
//     Must not be nil.
//   - criteria: The batching thresholds (Min/Max keys, partitions, etc.).
//   - opts: Optional configuration (WithStash, WithMetrics,
//     WithErrorBackoff, WithNegativeCaching).
func NewReadThroughCache[K comparable, V any, W comparable](
	capacity int,
	fetcher ItemFetcherI[K, V],
	criteria FetchCriteria,
	opts ...CacheOption[K, V, W],
) *ReadThroughCache[K, V, W] {
	if capacity < 1 {
		log.Panic().Int("capacity", capacity).Msg("caching: NewReadThroughCache requires capacity >= 1")
	}
	if fetcher == nil {
		log.Panic().Msg("caching: NewReadThroughCache requires a non-nil fetcher")
	}

	c := &ReadThroughCache[K, V, W]{
		fetcher:       fetcher,
		cap:           capacity,
		currentEpoch:  1,
		fetchCriteria: criteria,

		primaryStore:     make(map[K]primaryItem[V], capacity),
		keysToFetch:      make(map[uint64][]K),
		keysToFetchSet:   make(map[K]struct{}),
		pendingWorkItems: make(map[W]struct{}),
		errorUntil:       make(map[K]time.Time),

		metrics:         &noopMetrics{}, // Avoid nil checks
		errorBackoffDur: 5 * time.Second,
		sideTableBound:  max(capacity, 64),
		nowFn:           time.Now,
	}

	for _, opt := range opts {
		opt(c)
	}

	if c.absentTTL > 0 {
		c.absentUntil = make(map[K]time.Time)
	}

	// If the user did not provide a Stash via WithStash, initialize a
	// default one: SliceStash (memory dense) at 50% of L1 capacity.
	if c.stash == nil {
		c.stash = NewSliceStash[K, V](max(1, capacity/2))
	}

	return c
}

// SetErrorBackoff adjusts the circuit-breaker window at runtime; it is the
// mutable twin of WithErrorBackoff (kept deliberately — useful for tests
// and live tuning).
func (inst *ReadThroughCache[K, V, W]) SetErrorBackoff(d time.Duration) {
	inst.errorBackoffDur = d
}

func (inst *ReadThroughCache[K, V, W]) AdvanceEpoch() {
	inst.currentEpoch++
}

// WorkItem marks wk as the active work item for the duration of the loop
// body; misses inside it register wk as pending. The previous context is
// restored on exit — including early break and panic — so nesting is safe.
func (inst *ReadThroughCache[K, V, W]) WorkItem(wk W) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		prevW, prevHas := inst.currentWorkItem, inst.hasCurrentWork
		inst.currentWorkItem = wk
		inst.hasCurrentWork = true
		defer func() {
			inst.currentWorkItem = prevW
			inst.hasCurrentWork = prevHas
		}()
		yield(functional.NilIteratorValueType{})
	}
}

// Get retrieves an item. Lookup order: L1 → L2 → miss. On miss the key is
// queued for the next batch fetch and the current work item (if any) is
// marked pending. A stale entry is a miss (the refresh is queued); a key
// inside the circuit-breaker or negative-cache window misses without
// queueing.
func (inst *ReadThroughCache[K, V, W]) Get(k K) (v V, has bool) {
	v, has, _ = inst.getInternal(k, false)
	return
}

// GetAcceptStale retrieves an item, allowing stale data (soft hit). A stale
// entry is served with stale=true while its refresh queues in the
// background — including while the circuit breaker holds the refresh back.
func (inst *ReadThroughCache[K, V, W]) GetAcceptStale(k K) (v V, has bool, stale bool) {
	return inst.getInternal(k, true)
}

func (inst *ReadThroughCache[K, V, W]) getInternal(k K, acceptStale bool) (v V, has bool, stale bool) {
	return inst.lookup(k, acceptStale, true)
}

// lookup implements Get/GetAcceptStale. mayFlush allows one synchronous
// Max-criteria flush: when the flush fires, the lookup re-routes ONCE on
// the post-flush state — the refresh (or an absent verdict) that landed
// within this very call must win over the pre-flush snapshot. The retry
// pass runs with mayFlush=false, bounding the recursion at depth two.
// Every terminal path records exactly one hit or miss.
func (inst *ReadThroughCache[K, V, W]) lookup(k K, acceptStale bool, mayFlush bool) (v V, has bool, stale bool) {
	now := inst.nowFn()

	item, inL1 := inst.primaryStore[k]
	fromStash := false
	if !inL1 {
		// Check Stash (L2). A hit is promoted into L1 with its state —
		// version, freshness stamp, stale flag — intact, then routed
		// exactly like a native L1 entry. Promotion bypasses the version
		// gate: a tier move is not an upstream answer, so it must not
		// count as an equal-version confirmation.
		if e, found := inst.stash.GetAndRemove(k); found {
			item = primaryItem[V]{
				value: e.Value, ver: e.Ver, stamp: e.Stamp, stale: e.Stale,
				lastSeen: inst.currentEpoch,
			}
			inst.place(k, item) // may spill back to L2 if L1 is pinned-full
			inL1, fromStash = true, true
		}
	}

	if inL1 {
		// Pin to the current epoch and mark the SIEVE access bit. Write
		// back only when something changes and the entry was not just
		// (re-)inserted by the promotion above.
		if !fromStash && (item.lastSeen != inst.currentEpoch || !item.visited) {
			item.lastSeen = inst.currentEpoch
			item.visited = true
			inst.primaryStore[k] = item
		}

		if !inst.isStale(item, now) {
			inst.metrics.RecordHit(!fromStash, false)
			return item.value, true, false
		}

		// Stale entry: queue a refresh unless the circuit breaker is open.
		if !stillBlocked(inst.errorUntil, k, now) {
			inst.queueForFetch(k)
			if mayFlush && inst.checkCriteria(context.Background(), true) {
				return inst.lookup(k, acceptStale, false)
			}
		}
		if acceptStale {
			inst.metrics.RecordHit(!fromStash, true)
			return item.value, true, true
		}
		// Strict mode: stale is a miss.
		inst.registerPendingWork()
		inst.metrics.RecordMiss()
		return v, false, false
	}

	// Full miss. Negative cache first: a key known to be absent upstream
	// misses without queueing and without suspending the work item —
	// nothing will arrive, so a replay would loop forever.
	if inst.absentUntil != nil && stillBlocked(inst.absentUntil, k, now) {
		inst.metrics.RecordMiss()
		return v, false, false
	}

	// Circuit breaker: within the backoff window the work item suspends,
	// but no fetch is queued.
	if stillBlocked(inst.errorUntil, k, now) {
		inst.registerPendingWork()
		inst.metrics.RecordMiss()
		return v, false, false
	}

	inst.queueForFetch(k)
	if mayFlush && inst.checkCriteria(context.Background(), true) {
		return inst.lookup(k, acceptStale, false)
	}
	inst.registerPendingWork()
	inst.metrics.RecordMiss()
	return v, false, false
}

// MarkAsStale flags a cached entry as stale in whichever tier it resides:
// the next strict Get misses and queues a refresh, while GetAcceptStale
// keeps serving the old value until fresh data lands. Unknown keys are a
// no-op.
func (inst *ReadThroughCache[K, V, W]) MarkAsStale(k K) {
	if item, ok := inst.primaryStore[k]; ok {
		item.stale = true
		inst.primaryStore[k] = item
		return
	}
	// Stash-resident entries round-trip with the flag set, state intact.
	if e, found := inst.stash.GetAndRemove(k); found {
		e.Stale = true
		inst.stash.Add(k, e)
	}
}

// MarkAsStaleIfOlder is the version-carrying external-writer signal: it
// stales the cached entry only when its order is below ver, so a
// redundant signal for data the cache already holds is free. Without
// WithVersioning the cached orders are internal counters, incomparable to
// the caller's domain, and the signal degrades to an unconditional
// MarkAsStale.
func (inst *ReadThroughCache[K, V, W]) MarkAsStaleIfOlder(k K, ver int64) {
	if inst.orderOf == nil {
		inst.MarkAsStale(k)
		return
	}
	if item, ok := inst.primaryStore[k]; ok {
		if item.ver < ver {
			item.stale = true
			inst.primaryStore[k] = item
		}
		return
	}
	if e, found := inst.stash.GetAndRemove(k); found {
		if e.Ver < ver {
			e.Stale = true
		}
		inst.stash.Add(k, e)
	}
}

// Pin makes the key's entry immune to eviction and demotion until Unpin —
// the write-through dirty-window latch: the writer pins at Commit and
// unpins at Flush, so a written-but-unflushed version can never be
// evicted and then shadowed by an older durable refetch (see the nopin
// counterfactual in verification/formal/caching). A stash-resident entry
// is hoisted into L1; pinned entries may hold L1 beyond its capacity,
// bounded by the caller's flush cadence. Note that epoch pinning
// compounds with this: while every resident entry is read within the
// current epoch, the overshoot cannot drain — an AdvanceEpoch cadence is
// what lets insert pressure bring L1 back to capacity. Pinning an
// uncached key is a no-op. Idempotent.
//
// Delete and Clear remove pinned entries too: explicit invalidation
// overrides the latch. Do not invalidate keys with unflushed local
// writes.
func (inst *ReadThroughCache[K, V, W]) Pin(k K) {
	if item, ok := inst.primaryStore[k]; ok {
		if !item.pinned {
			item.pinned = true
			inst.primaryStore[k] = item
		}
		return
	}
	if e, found := inst.stash.GetAndRemove(k); found {
		inst.placeL1(k, primaryItem[V]{
			value: e.Value, ver: e.Ver, stamp: e.Stamp, stale: e.Stale,
			lastSeen: inst.currentEpoch, pinned: true,
		})
	}
}

// Unpin releases a Pin; the entry becomes ordinarily evictable again.
// No-op for unpinned or uncached keys. Idempotent.
func (inst *ReadThroughCache[K, V, W]) Unpin(k K) {
	if item, ok := inst.primaryStore[k]; ok && item.pinned {
		item.pinned = false
		inst.primaryStore[k] = item
	}
}

func (inst *ReadThroughCache[K, V, W]) queueForFetch(k K) {
	if _, exists := inst.keysToFetchSet[k]; exists {
		return
	}
	inst.keysToFetchSet[k] = struct{}{}
	p := inst.fetcher.DeterminePartition(k)
	inst.keysToFetch[p] = append(inst.keysToFetch[p], k)
}

func (inst *ReadThroughCache[K, V, W]) registerPendingWork() {
	if inst.hasCurrentWork {
		inst.pendingWorkItems[inst.currentWorkItem] = struct{}{}
	}
}

// IterateReadyWorkItems yields work items that are ready for replay. With a
// non-empty queue this means the fetch criteria are met (the flush happens
// first); with an empty queue any pending items are replayed directly —
// their keys were already flushed synchronously by a Max threshold. (Items
// pending on an open circuit breaker replay too and re-suspend; that is
// bounded by the backoff window.)
func (inst *ReadThroughCache[K, V, W]) IterateReadyWorkItems(ctx context.Context) iter.Seq[W] {
	return func(yield func(W) bool) {
		if len(inst.keysToFetchSet) == 0 {
			inst.yieldPending(yield)
			return
		}
		if !inst.checkCriteria(ctx, false) {
			return
		}
		inst.yieldPending(yield)
	}
}

// IterateRestWorkItems forces a fetch of all pending keys and yields
// remaining work.
func (inst *ReadThroughCache[K, V, W]) IterateRestWorkItems(ctx context.Context) iter.Seq[W] {
	return func(yield func(W) bool) {
		inst.performFetch(ctx)
		inst.yieldPending(yield)
	}
}

// yieldPending drains pendingWorkItems and yields each one to the caller's
// replay loop. While a work item is being replayed the cache restores its
// work-item context (currentWorkItem / hasCurrentWork) so that a cascading
// Get miss inside the replay re-queues the same item via registerPendingWork
// rather than being silently dropped. The context is reset when the
// iterator exits; on an early break the not-yet-yielded items are re-queued
// as pending so they are not lost.
func (inst *ReadThroughCache[K, V, W]) yieldPending(yield func(W) bool) {
	if len(inst.pendingWorkItems) == 0 {
		return
	}
	items := make([]W, 0, len(inst.pendingWorkItems))
	for w := range inst.pendingWorkItems {
		items = append(items, w)
	}
	clear(inst.pendingWorkItems)

	prevW, prevHas := inst.currentWorkItem, inst.hasCurrentWork
	defer func() {
		inst.currentWorkItem = prevW
		inst.hasCurrentWork = prevHas
	}()

	for i, w := range items {
		inst.currentWorkItem = w
		inst.hasCurrentWork = true
		if !yield(w) {
			for _, rest := range items[i+1:] {
				inst.pendingWorkItems[rest] = struct{}{}
			}
			return
		}
	}
}

func (inst *ReadThroughCache[K, V, W]) checkCriteria(ctx context.Context, onlyMax bool) bool {
	c := inst.fetchCriteria
	nKeys := len(inst.keysToFetchSet)
	nParts := len(inst.keysToFetch)
	nWork := len(inst.pendingWorkItems)

	if nKeys == 0 {
		return false
	}

	trigger := false
	if (c.MaxKeys > 0 && nKeys >= c.MaxKeys) ||
		(c.MaxPartitions > 0 && nParts >= c.MaxPartitions) ||
		(c.MaxWorkItems > 0 && nWork >= c.MaxWorkItems) {
		trigger = true
	}

	if !onlyMax && !trigger {
		if (c.MinKeys > 0 && nKeys >= c.MinKeys) ||
			(c.MinPartitions > 0 && nParts >= c.MinPartitions) ||
			(c.MinWorkItems > 0 && nWork >= c.MinWorkItems) {
			trigger = true
		}
		if c.MinKeys == 0 && c.MinPartitions == 0 && c.MinWorkItems == 0 {
			trigger = true
		}
	}

	if trigger {
		inst.performFetch(ctx)
		return true
	}
	return false
}

func (inst *ReadThroughCache[K, V, W]) performFetch(ctx context.Context) {
	if inst.fetching {
		// Re-entrant flush: a fetcher called back into the cache and
		// tripped a Max threshold. Keys queued during a flush are fetched
		// on the next flush instead of corrupting the running one.
		return
	}
	if len(inst.keysToFetch) == 0 {
		return
	}

	inst.fetching = true
	inst.fetchAdded = make(map[K]struct{})
	defer func() {
		inst.fetching = false
		inst.fetchAdded = nil
	}()

	start := inst.nowFn()

	for p, keys := range inst.keysToFetch {
		if ctx.Err() != nil {
			// Unprocessed partitions stay queued for the next flush;
			// nothing is silently dropped.
			log.Warn().Msg("caching: context cancelled during batch fetch")
			break
		}

		// Filter against the live set: Delete may have dequeued keys, and
		// re-queues after a Delete may have produced slice duplicates.
		// Consuming set entries as we go also dedups.
		live := keys[:0]
		for _, k := range keys {
			if _, ok := inst.keysToFetchSet[k]; ok {
				delete(inst.keysToFetchSet, k)
				live = append(live, k)
			}
		}
		delete(inst.keysToFetch, p)
		if len(live) == 0 {
			continue
		}

		// Panic Recovery & Error Handling Wrapper
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Msg("caching: panic in ItemFetcher")
					inst.markKeysFailed(live)
					inst.metrics.RecordFetchError(len(live))
				}
			}()

			err := inst.fetcher.FetchItemSinglePartition(ctx, p, live, inst)
			if err != nil {
				log.Error().Err(err).Uint64("partition", p).Msg("caching: fetch failed")
				inst.markKeysFailed(live)
				inst.metrics.RecordFetchError(len(live))
				return
			}

			// Negative caching: requested keys the fetcher did not deliver
			// on a clean return are authoritatively absent for the TTL.
			// Unlike a fetch FAILURE (which preserves a stale value for
			// stale-while-revalidate), the absent verdict also drops any
			// cached remnant — otherwise a surviving stale entry would
			// re-queue the key on every read, defeating the mark.
			if inst.absentUntil != nil {
				until := inst.nowFn().Add(inst.absentTTL)
				for _, k := range live {
					if _, delivered := inst.fetchAdded[k]; !delivered {
						putBounded(inst.absentUntil, k, until, inst.sideTableBound)
						delete(inst.primaryStore, k)
						inst.stash.Delete(k)
					}
				}
			}
		}()
	}

	inst.metrics.RecordFetchDuration(inst.nowFn().Sub(start))
}

// markKeysFailed opens the circuit breaker for queued keys the fetcher did
// not deliver. Keys the fetcher populated before erroring (recorded in
// fetchAdded) keep their fresh values — a fetcher may legitimately AddItem
// some keys and then fail on the rest, and that partial progress must be
// preserved. A stale value whose refresh failed stays resident and stale:
// GetAcceptStale keeps serving it while the breaker suppresses re-queues.
func (inst *ReadThroughCache[K, V, W]) markKeysFailed(keys []K) {
	until := inst.nowFn().Add(inst.errorBackoffDur)
	for _, k := range keys {
		if _, delivered := inst.fetchAdded[k]; delivered {
			continue
		}
		putBounded(inst.errorUntil, k, until, inst.sideTableBound)
	}
}

// AddItem inserts a value — a fetch delivery or a write-through
// population. Under WithVersioning admission is gated by the value's
// intrinsic order (see the option doc); without it, last insert wins.
// Either way the delivery clears the key's breaker and absent marks.
func (inst *ReadThroughCache[K, V, W]) AddItem(k K, v V) {
	inst.admit(k, v)
}

// admit runs the three-outcome version gate and places the winner.
func (inst *ReadThroughCache[K, V, W]) admit(k K, v V) {
	// Delivery bookkeeping is unconditional: the upstream (or the writer)
	// answered for this key, whatever the gate decides about the value —
	// an absent mark or an open breaker must not survive a delivery, and
	// a gate-rejected key was still delivered for the running flush.
	delete(inst.errorUntil, k)
	if inst.absentUntil != nil {
		delete(inst.absentUntil, k)
	}
	if inst.fetchAdded != nil {
		inst.fetchAdded[k] = struct{}{}
	}

	newVer := inst.versionOf(v)
	now := inst.nowFn().UnixNano()

	existing, have := inst.primaryStore[k]
	if !have && inst.orderOf != nil {
		// The gate must compare against ANY cached copy, including a stash
		// shadow; pull it out — the winner lands in L1 either way. Skipped
		// in internal-counter mode, where a fresh insert always wins.
		if e, found := inst.stash.GetAndRemove(k); found {
			existing = primaryItem[V]{value: e.Value, ver: e.Ver, stamp: e.Stamp, stale: e.Stale}
			have = true
		}
	}

	if have {
		switch {
		case newVer > existing.ver:
			// Newer: replace, fresh. The sticky pin survives replacement —
			// a second Commit before Flush keeps its dirty-window latch.
			inst.place(k, primaryItem[V]{
				value: v, ver: newVer, stamp: now,
				lastSeen: inst.currentEpoch, pinned: existing.pinned,
			})
		case newVer == existing.ver:
			// Equal: a revalidation confirmed the cached value is current —
			// keep it, clear staleness, restart the freshness clock.
			existing.stale = false
			existing.stamp = now
			existing.lastSeen = inst.currentEpoch
			inst.place(k, existing)
		default:
			// Older: reject — the raced fetch returning a pre-write row
			// bounces off. Re-place the existing entry (it may have been
			// pulled out of the stash above).
			existing.lastSeen = inst.currentEpoch
			inst.place(k, existing)
		}
		return
	}

	inst.place(k, primaryItem[V]{value: v, ver: newVer, stamp: now, lastSeen: inst.currentEpoch})
}

// place puts an entry into L1 — or spills it to the stash when L1 is at
// capacity with only pinned entries left. A sticky-pinned entry never
// spills: the dirty window may exceed the configured capacity, bounded by
// the writer's flush cadence.
func (inst *ReadThroughCache[K, V, W]) place(k K, item primaryItem[V]) {
	if _, exists := inst.primaryStore[k]; !exists {
		if len(inst.primaryStore) >= inst.cap {
			if inst.ensureSpaceByEvictingOne() && !item.pinned {
				// L1 is full of pinned items; spill the new value directly
				// to L2. No L1 item was demoted here, so the (toStash=true)
				// eviction metric does NOT fire — only a (toStash=false) if
				// the stash itself displaced something.
				if dropped := inst.stash.Add(k, StashEntry[V]{
					Value: item.value, Ver: item.ver, Stamp: item.stamp, Stale: item.stale,
				}); dropped {
					inst.metrics.RecordEviction(false)
				}
				return
			}
		}
	}

	inst.placeL1(k, item)
}

// placeL1 writes an entry into the primary store, giving a key that is new
// to L1 a fresh SIEVE ring slot (an existing entry keeps its slot; its
// access bit travels inside the item the caller derived from it). Stale
// slots are reclaimed by lazy compaction once they outnumber live ones.
func (inst *ReadThroughCache[K, V, W]) placeL1(k K, item primaryItem[V]) {
	if old, exists := inst.primaryStore[k]; exists {
		item.slot = old.slot
	} else {
		if len(inst.order) >= 8 && len(inst.order) >= 2*(len(inst.primaryStore)+1) {
			inst.compactOrder()
		}
		item.slot = len(inst.order)
		inst.order = append(inst.order, k)
	}
	inst.primaryStore[k] = item
}

// compactOrder rebuilds the SIEVE ring in place, keeping insertion order
// and dropping stale slots. The hand restarts at the oldest entry — a
// rare, bounded fairness blip.
func (inst *ReadThroughCache[K, V, W]) compactOrder() {
	live := inst.order[:0]
	for idx, k := range inst.order {
		if it, ok := inst.primaryStore[k]; ok && it.slot == idx {
			it.slot = len(live)
			inst.primaryStore[k] = it
			live = append(live, k)
		}
	}
	inst.order = live
	inst.hand = 0
}

// AddItemSlice inserts parallel key/value slices; the lengths must match.
func (inst *ReadThroughCache[K, V, W]) AddItemSlice(k []K, v []V) {
	if len(k) != len(v) {
		log.Panic().Int("keys", len(k)).Int("values", len(v)).Msg("caching: AddItemSlice length mismatch")
	}
	for i := range k {
		inst.AddItem(k[i], v[i])
	}
}

func (inst *ReadThroughCache[K, V, W]) AddItemIter2(it iter.Seq2[K, V]) {
	for k, v := range it {
		inst.AddItem(k, v)
	}
}

// ensureSpaceByEvictingOne demotes at most one unpinned L1 victim to L2,
// carrying its full state (version, freshness stamp, stale flag). Returns
// useStash=true if every L1 entry is pinned — to the current epoch or
// stickily (dirty window) — and false if a slot was freed (or L1 was
// already under capacity).
func (inst *ReadThroughCache[K, V, W]) ensureSpaceByEvictingOne() (useStash bool) {
	// SIEVE victim selection (Zhang et al., NSDI '24): the hand walks the
	// insertion ring from older toward newer entries, clearing the access
	// bit of visited entries (retaining them) and demoting the first
	// unvisited one. Epoch- and sticky-pinned entries are immune and are
	// skipped WITHOUT touching their bits — pins are protection, not
	// policy signals. Two full passes suffice: the first may clear every
	// access bit, the second must then find a victim — unless everything
	// live is pinned.
	n := len(inst.order)
	for scanned := 0; scanned < 2*n; scanned++ {
		if inst.hand >= len(inst.order) {
			inst.hand = 0
		}
		idx := inst.hand
		inst.hand++
		k := inst.order[idx]
		item, ok := inst.primaryStore[k]
		if !ok || item.slot != idx {
			continue // stale slot (the entry left L1)
		}
		if item.lastSeen == inst.currentEpoch || item.pinned {
			continue // Pinning Protection: immune, bits untouched
		}
		if item.visited {
			item.visited = false
			inst.primaryStore[k] = item
			continue
		}
		inst.demoteToStash(k, item)
		return false
	}

	// A full double sweep found no victim: everything live is Pinned.
	return true
}

// demoteToStash moves the L1 entry for k into the stash, carrying its
// full state, and records the eviction metrics. The caller has
// established that the entry exists and is unpinned.
func (inst *ReadThroughCache[K, V, W]) demoteToStash(k K, v primaryItem[V]) {
	dropped := inst.stash.Add(k, StashEntry[V]{
		Value: v.value, Ver: v.ver, Stamp: v.stamp, Stale: v.stale,
	})
	inst.metrics.RecordEviction(true) // L1 -> L2 demotion (preserved).
	if dropped {
		inst.metrics.RecordEviction(false) // Stash displaced an older item (data loss).
	}
	delete(inst.primaryStore, k)
}

// Delete removes the key from both tiers, clears its breaker and absent
// marks, and dequeues any pending fetch for it — a deleted key must not be
// resurrected by an in-flight batch.
func (inst *ReadThroughCache[K, V, W]) Delete(k K) {
	delete(inst.primaryStore, k)
	inst.stash.Delete(k)
	delete(inst.errorUntil, k)
	if inst.absentUntil != nil {
		delete(inst.absentUntil, k)
	}
	delete(inst.keysToFetchSet, k) // performFetch filters slices against the set
}

// Clear drops every cached entry and all in-flight bookkeeping (queued
// keys, pending work items, breaker and absent marks). Call it between
// frames, not with suspended work items.
func (inst *ReadThroughCache[K, V, W]) Clear() {
	clear(inst.primaryStore)
	clear(inst.keysToFetch)
	clear(inst.keysToFetchSet)
	clear(inst.pendingWorkItems)
	clear(inst.errorUntil)
	if inst.absentUntil != nil {
		clear(inst.absentUntil)
	}
	inst.order = inst.order[:0]
	inst.hand = 0
	inst.stash.Clear()
}

// Close releases resources held by the stash (disk-backed stashes hold
// file locks); in-memory stashes make it a no-op. The cache must not be
// used afterwards.
func (inst *ReadThroughCache[K, V, W]) Close() error {
	if closer, ok := inst.stash.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// Len returns the number of entries resident in the primary (L1) store.
func (inst *ReadThroughCache[K, V, W]) Len() int { return len(inst.primaryStore) }

// StashLen returns the number of entries resident in the stash (L2).
func (inst *ReadThroughCache[K, V, W]) StashLen() int { return inst.stash.Len() }

// QueuedKeys returns the number of keys queued for the next batch fetch.
func (inst *ReadThroughCache[K, V, W]) QueuedKeys() int { return len(inst.keysToFetchSet) }

// PendingWorkItems returns the number of work items suspended on misses.
func (inst *ReadThroughCache[K, V, W]) PendingWorkItems() int { return len(inst.pendingWorkItems) }

// No-op implementation for metrics
type noopMetrics struct{}

func (n *noopMetrics) RecordHit(l1 bool, stale bool)       {}
func (n *noopMetrics) RecordMiss()                         {}
func (n *noopMetrics) RecordFetchError(count int)          {}
func (n *noopMetrics) RecordEviction(toStash bool)         {}
func (n *noopMetrics) RecordFetchDuration(d time.Duration) {}
