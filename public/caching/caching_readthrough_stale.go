//go:build llm_generated_gemini3pro

package caching

import (
	"context"
	"iter"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/stergiotis/boxer/public/functional"
)

const (
	// ItemStateReadThroughPending: Item is queued for fetch.
	ItemStateReadThroughPending ItemStateE = 0
	// ItemStateInCache: Item is valid and ready to use.
	ItemStateInCache ItemStateE = 1
	// ItemStateStale: Item is valid but expired; triggers background refresh.
	ItemStateStale ItemStateE = 2
	// ItemStateError: Last fetch failed; item is in cooling-off period.
	ItemStateError ItemStateE = 3
)

// primaryItem is optimized for memory padding.
type primaryItem[V any] struct {
	value      V
	errorUntil time.Time  // Used if State == ItemStateError
	lastSeen   uint64     // Epoch for pinning
	state      ItemStateE // uint8
}

// NewReadThroughCache creates a new dependency-aware batching cache.
//
// Parameters:
//   - capacity: The maximum number of items in the Primary (L1) store.
//   - fetcher: The implementation for partition-aware data retrieval.
//   - criteria: The batching thresholds (Min/Max keys, partitions, etc.).
//   - opts: Optional configurations (Stash, Metrics, Logger, etc.).
func NewReadThroughCache[K comparable, V any, W comparable](
	capacity int,
	fetcher ItemFetcherI[K, V],
	criteria FetchCriteria,
	opts ...CacheOption[K, V, W],
) *ReadThroughCache[K, V, W] {

	// 1. Initialize with Defaults
	c := &ReadThroughCache[K, V, W]{
		fetcher:       fetcher,
		cap:           capacity,
		currentEpoch:  1,
		fetchCriteria: criteria,

		// Map Initialization
		primaryStore:     make(map[K]primaryItem[V], capacity),
		keysToFetch:      make(map[uint64][]K),
		keysToFetchSet:   make(map[K]struct{}),
		pendingWorkItems: make(map[W]struct{}),

		// Safe Defaults
		metrics:         &noopMetrics{}, // Avoid nil checks
		errorBackoffDur: 5 * time.Second,
	}

	// 2. Apply Options
	for _, opt := range opts {
		opt(c)
	}

	// 3. Post-Configuration Defaults
	// If the user did not provide a Stash via WithStash, we initialize a default one.
	if c.stash == nil {
		// Default Strategy: SliceStash (Memory Dense).
		// Default Size: 50% of L1 Capacity (Arbitrary heuristic, but safe).
		defaultStashSize := max(1, capacity/2)
		c.stash = NewSliceStash[K, V](defaultStashSize)
	}

	return c
}

func (inst *ReadThroughCache[K, V, W]) SetErrorBackoff(d time.Duration) {
	inst.errorBackoffDur = d
}

func (inst *ReadThroughCache[K, V, W]) AdvanceEpoch() {
	inst.currentEpoch++
}

func (inst *ReadThroughCache[K, V, W]) WorkItem(wk W) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.currentWorkItem = wk
		inst.hasCurrentWork = true
		if !yield(functional.NilIteratorValueType{}) {
			return
		}
		inst.hasCurrentWork = false
	}
}

// Get retrieves an item. Logic: L1 -> L2 -> Miss/Error/Pending.
func (inst *ReadThroughCache[K, V, W]) Get(k K) (has bool, v V) {
	return inst.getInternal(k, false)
}

// GetAcceptStale retrieves an item, allowing stale data (soft hit).
func (inst *ReadThroughCache[K, V, W]) GetAcceptStale(k K) (has bool, stale bool, v V) {
	h, val := inst.getInternal(k, true)
	if !h {
		return false, false, val
	}
	// Check if it was stale
	if item, ok := inst.primaryStore[k]; ok {
		if item.state == ItemStateStale {
			return true, true, val
		}
	}
	return true, false, val
}

func (inst *ReadThroughCache[K, V, W]) getInternal(k K, acceptStale bool) (has bool, v V) {
	// 1. Check Primary (L1)
	if item, ok := inst.primaryStore[k]; ok {
		// Optimization: Avoid Write-Back if already pinned
		if item.lastSeen != inst.currentEpoch {
			item.lastSeen = inst.currentEpoch
			inst.primaryStore[k] = item // Write back only when necessary
		}

		switch item.state {
		case ItemStateInCache:
			inst.metrics.RecordHit(true)
			return true, item.value
		case ItemStateStale:
			inst.queueForFetch(k)
			if acceptStale {
				inst.metrics.RecordHit(true)
				return true, item.value
			}
			// Strict mode: Stale is a miss
		case ItemStateError:
			// Circuit Breaker: Check if backoff expired
			if time.Now().After(item.errorUntil) {
				// Retry allowed
				inst.queueForFetch(k)
			} else {
				// Still cooling off. Treat as missing, but do NOT queue.
				// Just register work item as blocked.
				inst.registerPendingWork()
				inst.metrics.RecordMiss()
				return false, v
			}
		case ItemStateReadThroughPending:
			// Already queued
		}

		inst.registerPendingWork()
		inst.metrics.RecordMiss()
		return false, v
	}

	// 2. Check Stash (L2) via Interface
	if val, found := inst.stash.GetAndRemove(k); found {
		inst.metrics.RecordHit(false) // L2 Hit
		inst.AddItem(k, val)          // Promote to L1
		return true, val
	}

	// 3. Miss
	inst.queueForFetch(k)
	inst.registerPendingWork()
	inst.metrics.RecordMiss()
	inst.checkCriteria(context.Background(), true) // Check Max criteria (optimistic ctx)
	return false, v
}

func (inst *ReadThroughCache[K, V, W]) MarkAsStale(k K) {
	if item, ok := inst.primaryStore[k]; ok {
		item.state = ItemStateStale
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

// IterateReadyWorkItems yields work items that are ready for retry.
func (inst *ReadThroughCache[K, V, W]) IterateReadyWorkItems(ctx context.Context) iter.Seq[W] {
	return func(yield func(W) bool) {
		// Attempt fetch
		if !inst.checkCriteria(ctx, false) {
			return
		}
		inst.yieldPending(yield)
	}
}

// IterateRestWorkItems forces a fetch of all pending keys and yields remaining work.
func (inst *ReadThroughCache[K, V, W]) IterateRestWorkItems(ctx context.Context) iter.Seq[W] {
	return func(yield func(W) bool) {
		inst.performFetch(ctx)
		inst.yieldPending(yield)
	}
}

func (inst *ReadThroughCache[K, V, W]) yieldPending(yield func(W) bool) {
	if len(inst.pendingWorkItems) == 0 {
		return
	}
	items := make([]W, 0, len(inst.pendingWorkItems))
	for w := range inst.pendingWorkItems {
		items = append(items, w)
	}
	for k := range inst.pendingWorkItems {
		delete(inst.pendingWorkItems, k)
	}
	for _, w := range items {
		if !yield(w) {
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
	if len(inst.keysToFetch) == 0 {
		return
	}

	start := time.Now()

	for p, keys := range inst.keysToFetch {
		if len(keys) == 0 {
			continue
		}

		if ctx.Err() != nil {
			log.Warn().Msg("Context cancelled during batch fetch")
			break
		}

		// Panic Recovery & Error Handling Wrapper
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Error().Interface("panic", r).Msg("Panic in ItemFetcher")
					inst.markKeysAsError(keys)
					inst.metrics.RecordFetchError(len(keys))
				}
			}()

			err := inst.fetcher.FetchItemSinglePartition(ctx, p, keys, inst)
			if err != nil {
				log.Error().Err(err).Uint64("partition", p).Msg("Fetch failed")
				inst.markKeysAsError(keys)
				inst.metrics.RecordFetchError(len(keys))
			}
		}()
	}

	inst.metrics.RecordFetchDuration(time.Since(start))

	// Clean up queues
	for k := range inst.keysToFetch {
		delete(inst.keysToFetch, k)
	}
	for k := range inst.keysToFetchSet {
		delete(inst.keysToFetchSet, k)
	}
}

func (inst *ReadThroughCache[K, V, W]) markKeysAsError(keys []K) {
	until := time.Now().Add(inst.errorBackoffDur)
	for _, k := range keys {
		// If exists, update state. If not, insert placeholder.
		if _, exists := inst.primaryStore[k]; !exists {
			// Ensure space only if strictly necessary, but usually errors
			// don't carry values, so strictly we might just want to track state.
			// Here we insert a placeholder.
			inst.primaryStore[k] = primaryItem[V]{
				state:      ItemStateError,
				errorUntil: until,
				lastSeen:   inst.currentEpoch,
			}
		} else {
			item := inst.primaryStore[k]
			item.state = ItemStateError
			item.errorUntil = until
			inst.primaryStore[k] = item
		}
	}
}

func (inst *ReadThroughCache[K, V, W]) AddItem(k K, v V) {
	// 1. Check Primary Store existence
	if _, exists := inst.primaryStore[k]; !exists {
		// 2. If Primary is full, try to make space
		if len(inst.primaryStore) >= inst.cap {
			useStash := inst.ensureSpaceByEvictingRandomly(1)
			if useStash {
				// 3. Primary is full of Pinned items. We MUST use Stash.

				dropped := inst.stash.Add(k, v)
				if dropped {
					inst.metrics.RecordEviction(false) // L2 Drop (Data Loss)
				}
				inst.metrics.RecordEviction(true) // L1 -> L2 Move
				return
			}
		}
	}

	// Insert into Primary
	inst.primaryStore[k] = primaryItem[V]{
		value:    v,
		state:    ItemStateInCache,
		lastSeen: inst.currentEpoch,
	}
}

func (inst *ReadThroughCache[K, V, W]) AddItemSlice(k []K, v []V) {
	for i := range k {
		inst.AddItem(k[i], v[i])
	}
}
func (inst *ReadThroughCache[K, V, W]) AddItemIter2(it iter.Seq2[K, V]) {
	for k, v := range it {
		inst.AddItem(k, v)
	}
}

func (inst *ReadThroughCache[K, V, W]) ensureSpaceByEvictingRandomly(n int) (useStash bool) {
	if n <= 0 {
		return false
	}
	evictedCount := 0

	// We iterate the map to find victims
	for k, v := range inst.primaryStore {
		// Pinning Protection
		if v.lastSeen == inst.currentEpoch {
			continue
		}

		// Found a victim in L1. Move to Stash.
		// Move to Stash via Interface
		dropped := inst.stash.Add(k, v.value)

		if dropped {
			inst.metrics.RecordEviction(false) // L2 Drop (Data Loss)
		}
		inst.metrics.RecordEviction(true) // L1 -> L2 Move

		// Remove from Primary
		delete(inst.primaryStore, k)

		evictedCount++
		if evictedCount >= n {
			return false
		}
	}

	// If we get here, everything in Primary was Pinned.
	return true
}

func (inst *ReadThroughCache[K, V, W]) Delete(k K) {
	delete(inst.primaryStore, k)
	inst.stash.Delete(k)
}

// No-op implementation for metrics
type noopMetrics struct{}

func (n *noopMetrics) RecordHit(l1 bool)                   {}
func (n *noopMetrics) RecordMiss()                         {}
func (n *noopMetrics) RecordFetchError(count int)          {}
func (n *noopMetrics) RecordEviction(toStash bool)         {}
func (n *noopMetrics) RecordFetchDuration(d time.Duration) {}
