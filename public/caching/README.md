# Batch-Optimized Dependency Cache

## 1. Overview
The `ReadThroughCache` is a specialized, single-threaded synchronization primitive designed for **high-throughput data processing pipelines**.

Unlike traditional caches (LRU/LFU) that focus on hit rates for random access, this system focuses on **Latency Hiding via Batching**. It decouples the *declaration* of data dependencies from the *execution* of fetching them, effectively transforming sequential `Get()` calls into optimized, partition-aware batch I/O.

**Target Use Case:** ETL pipelines, Build Systems, Graph Traversals, and ML Feature Engineering where processing is cheap, but I/O is expensive and restartable.

## 2. Architecture

The system operates as a state machine with three distinct storage tiers and a "Restart Loop" execution model.

### 2.1 Storage Hierarchy
1.  **L1 Primary (RAM):** A native Go map optimized for $O(1)$ access. Contains the "Working Set."
2.  **L2 Stash (Victim Cache):** A secondary buffer (Memory or Disk) for items evicted from L1. It handles "Thrashing" scenarios where the working set exceeds L1 capacity.
3.  **L3 Source (Fetcher):** The external truth (DB, S3, API). Accessed only via batch interfaces.

## 2.2 The "Restart Loop" Pattern (Corrected)

The core mechanism is **Speculative Execution with Accumulation**:

1.  **Discovery Phase:** The user code runs inside a `WorkItem` iterator. It requests **all** necessary keys for the current unit of work.
2.  **Accumulation:**
    *   If `Get(Key1)` returns `false` (missing), the cache records the dependency but the user code **should continue** to call `Get(Key2)`, `Get(Key3)`, etc., if possible.
    *   This allows the cache to build a complete picture of the work item's requirements.
3.  **Suspension:** If *any* dependencies were missing, the user code returns (aborts logic execution), effectively pausing the work item.
4.  **Batching:** The cache aggregates all missing keys (Key1, Key2, Key3) across all pending work items.
5.  **Fetch:** The cache groups keys by Partition and performs a bulk fetch.
6.  **Replay:** The user code is re-run (`IterateReadyWorkItems`). This time, data is present, and the logic proceeds to completion.

The `processItem` function must be written to **Accumulate Misses**:

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

## 3. Key Features & Implementation Details

### 3.1 Epoch-Based Pinning
To prevent cache thrashing (evicting data needed by the *current* batch to make room for *other* data in the same batch), the system uses **Epochs**.
*   **Mechanism:** `lastSeen` timestamp on every item.
*   **Invariant:** `ensureSpace` will **never** evict an item marked with the `currentEpoch`.
*   **Usage:** The user calls `AdvanceEpoch()` between logical batches to unpin old data.

### 3.2 Bounded Stash (L2)
When the L1 cache is full of "Pinned" items (Working Set > L1 Capacity), items are spilled to the Stash.
*   **Interface:** `StashBackend[K, V]`.
*   **Implementations:**
    *   `SliceStash`: CPU-heavy ($O(N)$ scan), Memory-dense. Good for small L2.
    *   `MapStash`: Memory-heavy, CPU-light ($O(1)$). Good for large RAM L2.
    *   `PogrebStash`/`PebbleStash`: Disk-backed. Good for datasets exceeding RAM.
*   **Eviction:** The Stash implements a Round-Robin or Random eviction policy when full, ensuring the system never OOMs.

### 3.3 Circuit Breaker
To prevent cascading failures during outages:
*   **State:** Items track an `ItemStateError` and `errorUntil` timestamp.
*   **Behavior:** Accessing an Error item returns `false` (missing) but **suppresses** the fetch request until the backoff expires.
*   **Safety:** The fetcher is wrapped in `recover()` to prevent panics from crashing the pipeline.

### 3.4 Partition-Aware Fetching
*   **Interface:** `ItemFetcherI.DeterminePartition(key)`.
*   **Optimization:** Keys are grouped by partition before the fetch call. This allows for optimal connection pooling (e.g., one DB query per shard).

## 4. Design Trade-offs

| Feature | Advantage | Trade-off / Constraint |
| :--- | :--- | :--- |
| **Single Threaded** | No Mutex contention; massive throughput; simple code. | **Must** be owned by a single Goroutine. No concurrent access. |
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
    // 1. SIGNAL NEW BATCH (Crucial for memory management)
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

### 5.3 User Logic Requirements
The `processItem` function:
1.  **Must be Idempotent:** It may be called multiple times. Side effects (DB writes, increments) should only happen *after* all `Get()` calls succeed.
2.  **Must Fail Fast:** If `cache.Get()` returns `false`, return immediately. Do not attempt to calculate with zero values.
3.  **Should Request All:** Ideally, request all known dependencies upfront to maximize batch size.

## 6. Configuration Options

*   **`WithStash(backend)`**: Swap L2 storage (Memory vs Disk).
*   **`WithMetrics(collector)`**: Inject Prometheus/StatsD hooks.
*   **`WithErrorBackoff(duration)`**: Tune the circuit breaker recovery time.

## 8. Anti-Patterns

1.  **Sharing across Goroutines:** Never wrap this in a Mutex and share it. Create one Cache instance per Worker Goroutine.
2.  **Ignoring the Boolean:** `val := c.Get(k)`. Never ignore the `found` boolean. If `false`, `val` is zero/garbage.
3.  **Heavy Values in Keys:** Do not use large structs as Keys (`K`). Use IDs or Hashes.
4.  **Infinite Loops:** Ensure your `fetcher` eventually returns distinct errors or data. If it returns `nil` error but no data repeatedly, the cache loop will spin forever.