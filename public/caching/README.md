---
type: reference
audience: caching user
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# caching — read-through dependency cache

## 1. Overview
The `ReadThroughCache` is a single-threaded read-through cache that accumulates missing keys across pending work items and fetches them in partition-grouped batches.

`Get()` calls declare dependencies; the actual fetch happens later, once enough work items have queued up misses. This trades random-access hit rates (the LRU/LFU target) for sequential-call latency.

**Target use case:** ETL pipelines, build systems, graph traversals, and ML feature engineering — workloads where processing is cheap, I/O is expensive, and the work can be replayed.

## 2. Architecture

The system is a state machine with three storage tiers and a discovery / suspend / replay execution loop.

### 2.1 Storage Hierarchy
1.  **L1 Primary (RAM):** A native Go map optimized for $O(1)$ access. Contains the "Working Set."
2.  **L2 Stash (Victim Cache):** A secondary buffer (Memory or Disk) for items evicted from L1. It handles "Thrashing" scenarios where the working set exceeds L1 capacity.
3.  **L3 Source (Fetcher):** The external truth (DB, S3, API). Accessed only via batch interfaces.

## 2.2 Execution model

Execution proceeds in phases:

1.  **Discovery Phase:** The user code runs inside a `WorkItem` iterator. It requests **all** necessary keys for the current unit of work.
2.  **Accumulation:**
    *   If `Get(Key1)` returns `false` (missing), the cache records the dependency but the user code **should continue** to call `Get(Key2)`, `Get(Key3)`, etc., if possible.
    *   This allows the cache to build a complete picture of the work item's requirements.
3.  **Suspension:** If *any* dependencies were missing, the user code returns (aborts logic execution), effectively pausing the work item.
4.  **Batching:** The cache aggregates all missing keys (Key1, Key2, Key3) across all pending work items.
5.  **Fetch:** The cache groups keys by Partition and performs a bulk fetch.
6.  **Replay:** The user code is re-run (`IterateReadyWorkItems`). This time, data is present, and the logic proceeds to completion.

The `processItem` function must be written to accumulate misses:

### Incorrect (Sequential / N+1 Latency)
```go
// BAD: Aborts immediately. Cache only sees "Key A".
// "Key B" will be discovered only after "Key A" is fetched and the loop restarts.
valA, found := cache.Get("KeyA")
if !found { return }

valB, found := cache.Get("KeyB")
if !found { return }
```

### Correct (Batch / O(1) Latency)
```go
// GOOD: Checks all requirements. Cache sees "Key A" AND "Key B".
// They will be fetched in a single batch.
missing := false

valA, foundA := cache.Get("KeyA")
if !foundA { missing = true }

valB, foundB := cache.Get("KeyB")
if !foundB { missing = true }

// Only abort after registering all needs
if missing { return }

// Proceed with logic
Result = valA + valB
```

## 3. Implementation details

### 3.1 Epoch-Based Pinning
To prevent cache thrashing (evicting data needed by the *current* batch to make room for *other* data in the same batch), the system uses **Epochs**.
*   **Mechanism:** `lastSeen` timestamp on every item.
*   **Invariant:** `ensureSpace` will **never** evict an item marked with the `currentEpoch`.
*   **Usage:** The user calls `AdvanceEpoch()` between logical batches to unpin old data.

### 3.2 Bounded Stash (L2)
When the L1 cache is full of "Pinned" items (Working Set > L1 Capacity), items are spilled to the Stash.
*   **Interface:** `StashBackendI[K, V]`. Entries carry their stale flag, so
    staleness survives the L1→L2 round-trip; `Add` of an existing key
    updates in place (never duplicates, never evicts).
*   **Implementations:**
    *   `SliceStash`: CPU-heavy ($O(N)$ scan), Memory-dense. Good for small L2.
    *   `MapStash`: Memory-heavy, CPU-light ($O(1)$). Good for large RAM L2.
    *   `PogrebStash`/`PebbleStash`: Disk-backed. Good for datasets exceeding
        RAM. Best-effort by contract: storage or codec errors degrade into
        misses or dropped writes, never into failures of the cache itself.
*   **Eviction:** The Stash implements a round-robin or random eviction policy when full, bounding L2 size.
*   **Conformance:** the `stashtest` package runs the contract suite against
    a backend; custom implementations should wire it into their tests.

### 3.3 Circuit Breaker
To prevent cascading failures during outages:
*   **State:** a bounded side table of `key → retry-allowed` deadlines —
    failure bookkeeping never occupies value-store slots.
*   **Behavior:** a missing key inside its backoff window returns `false`
    and **suppresses** the fetch request until the backoff expires. A
    *stale* value whose refresh failed stays resident: `GetAcceptStale`
    keeps serving it through the outage while strict `Get` misses — the
    stale-while-revalidate contract holds exactly when the upstream is
    down.
*   **Safety:** The fetcher is wrapped in `recover()` to prevent panics from crashing the pipeline.

### 3.4 Negative caching (opt-in)
`WithNegativeCaching(ttl)` records an *absent* verdict for requested keys a
clean fetch did not deliver. Within the TTL a `Get` on such a key misses
without queueing and without suspending the current work item, so
flush-until-quiet replay loops terminate instead of re-probing the upstream
forever. The verdict is authoritative: any cached remnant of the key is
dropped (contrast the breaker, which preserves stale values — a failure is
a guess, an absence is an answer). Off by default; without it, absent keys
re-probe on every flush and distinguishing "absent" from "not fetched yet"
is the caller's job.

### 3.5 Partition-Aware Fetching
*   **Interface:** `ItemFetcherI.DeterminePartition(key)`.
*   **Optimization:** Keys are grouped by partition before the fetch call. This allows for optimal connection pooling (e.g., one DB query per shard).
*   **Contract:** batches never contain duplicates; a partition is frozen at
    queue time; a fetcher may `AddItem` some keys and then return an error —
    the delivered keys keep their values, only the rest enter the breaker.
    A fetcher may read the cache re-entrantly (misses queued during a flush
    are fetched on the *next* flush; a nested flush is a no-op), but must
    not call `Iterate*WorkItems` or `Clear`.

## 4. Design Trade-offs

| Feature | Advantage | Trade-off / Constraint |
| :--- | :--- | :--- |
| **Single Threaded** | No mutex contention; simple control flow. | **Must** be owned by a single Goroutine. No concurrent access. |
| **Restart Loop** | Eliminates manual batching complexity. | User logic **must** be idempotent (safe to run multiple times). |
| **Strict Pinning** | Guarantees progress for large batches. | Requires explicit `AdvanceEpoch()` call, or memory will leak (logic-wise). |
| **Staleness** | Supports Stale-While-Revalidate. | Eventual consistency; user must opt-in via `GetAcceptStale`. |

## 5. Usage Guidance

### 5.1 Basic Setup

```go
// 1. Define Fetcher
fetcher := NewMyDbFetcher()

// 2. Configure Cache (L1=10k, L2=1k items)
cache := caching.NewReadThroughCache[string, Data, int](
    10000,
    fetcher,
    caching.FetchCriteria{MinKeys: 100, MaxWorkItems: 50},
    caching.WithStash(caching.NewSliceStash[string, Data](1000)),
)
```

### 5.2 The Processing Loop

```go
workItems := []int{1, 2, 3, ...}

for i, wID := range workItems {
    // 1. SIGNAL NEW BATCH (needed for memory management)
    if i % 100 == 0 {
        cache.AdvanceEpoch()
    }

    // 2. DISCOVERY PHASE
    // If Get() returns false, this loop breaks, and wID is queued.
    for range cache.WorkItem(wID) {
        processItem(wID)
    }

    // 3. EXECUTION PHASE (Replay ready items)
    // Checks if enough items are queued to trigger a batch fetch.
    for readyWID := range cache.IterateReadyWorkItems(ctx) {
        processItem(readyWID)
    }
}

// 4. FLUSH PHASE (Cleanup)
// Forces fetch for any remaining stragglers.
for wID := range cache.IterateRestWorkItems(ctx) {
    processItem(wID)
}
```

The cache restores the active work-item context for each replay
yielded by `IterateReadyWorkItems` / `IterateRestWorkItems`, so a
cascading `Get()` miss inside `processItem` (e.g., one that only
becomes visible after the first dependency resolves) will re-enter
the pending queue and be retried on the next flush — no manual
`WorkItem()` wrap is needed during replay.

### 5.3 User Logic Requirements
The `processItem` function:
1.  **Must be Idempotent:** It may be called multiple times. Side effects (DB writes, increments) should only happen *after* all `Get()` calls succeed.
2.  **Must Fail Fast:** If `cache.Get()` returns `false`, return immediately. Do not attempt to calculate with zero values.
3.  **Should Request All:** Ideally, request all known dependencies upfront to maximize batch size.

## 6. Configuration Options

*   **`WithStash(backend)`**: Swap L2 storage. Built-in options:
    `NewSliceStash` (memory-dense, O(n) scan; good for small L2s),
    `NewMapStash` (O(1), heavier per entry; good for large in-RAM L2s),
    and the disk-backed `diskbacked.NewPogrebStash` /
    `diskbacked.NewPebbleStash` (CBOR-encoded, optional soft cap; good
    when the working set spills beyond RAM).
*   **`WithMetrics(collector)`**: Inject Prometheus/StatsD hooks.
*   **`WithErrorBackoff(duration)`**: Circuit-breaker recovery window
    set at construction time. `SetErrorBackoff(duration)` does the
    same thing at runtime — useful for tests and for tuning live.
*   **`WithNegativeCaching(ttl)`**: absent-key marking (§3.4). Off by
    default.

Lifecycle and introspection: `Clear()` drops every entry and all in-flight
bookkeeping (call between frames, not with suspended work); `Close()`
releases a disk-backed stash's resources; `Len()`, `StashLen()`,
`QueuedKeys()` and `PendingWorkItems()` report occupancy. Constructors
panic on unusable arguments (capacity < 1, nil fetcher).

### 6.1 Fetch threshold semantics

`FetchCriteria` exposes three Min/Max pairs (`Keys`, `Partitions`,
`WorkItems`). All thresholds are evaluated independently and **OR'd** —
any single threshold being reached triggers a fetch:

*   **Max\*** fires *synchronously* from inside `Get()` /
    `GetAcceptStale()`, on every queueing path (cold miss and stale
    refresh alike). The triggering lookup then re-routes onto the
    post-flush state, so it returns the freshly fetched value directly. A
    single oversized work item that requests more than `MaxKeys` keys
    still chunks naturally: the first `MaxKeys` keys flush, then
    discovery continues.
*   **Min\*** is only checked by `IterateReadyWorkItems`. If no Min is
    met, the iterator yields nothing — unless the key queue is empty
    while work items are pending (their keys already flushed
    synchronously): those replay immediately, no fetch needed.
*   **`IterateRestWorkItems`** ignores criteria entirely and always
    flushes whatever is queued.
*   A zero field disables that threshold. If all three `Min*` fields
    are zero, `IterateReadyWorkItems` treats any non-empty queue as
    ready.
*   A cancelled context aborts a flush between partitions; unprocessed
    partitions stay queued for the next flush — nothing is dropped.

## 7. Anti-Patterns

1.  **Sharing across Goroutines:** Never wrap this in a Mutex and share it. Create one Cache instance per Worker Goroutine.
2.  **Ignoring the Boolean:** `val := c.Get(k)`. Never ignore the `found` boolean. If `false`, `val` is zero/garbage.
3.  **Heavy Values in Keys:** Do not use large structs as Keys (`K`). Use IDs or Hashes.
4.  **Replaying keys that don't exist:** a key absent upstream re-probes on
    every flush by default, so a flush-until-quiet loop never quiesces.
    Enable `WithNegativeCaching` (§3.4), or have the fetcher `AddItem` an
    explicit sentinel value for keys it knows are absent.
5.  **Iterating the cache from a fetcher:** reading (`Get`) is allowed;
    calling `Iterate*WorkItems` or `Clear` from inside
    `FetchItemSinglePartition` is not.

Breaking out of `WorkItem` or a replay loop early is safe: contexts are
restored and un-yielded work items stay pending. A work item whose keys
were dropped under memory pressure simply replays and re-queues them —
progress requires the working set to fit L1+L2 (see §4 pinning).