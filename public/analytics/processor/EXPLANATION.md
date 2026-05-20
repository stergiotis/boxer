---
type: explanation
audience: processor package maintainer
status: draft
---

> **Status: draft — pre-human-review.** Reconstructed from the original Gemini 3 design conversation ("Stateful Streaming — Entity Lifecycle Processing") plus the post-review fixes in this repo. Not verified against the current documentation standard. Do not cite as authoritative.

# Stateful streaming over discontinuous batches

The `analytics/processor` package bridges two shapes of data that do not naturally fit together: a producer that yields **fixed-size batches** of rows (typically a SQL cursor) and a consumer that wants to operate **per entity** as a single, continuous stream from first row to last.

## The problem

The original framing — and the reason this package exists at all — is what the design conversation called *Entity-Lifecycle Mapping over Discontinuous Batches*.

A producer (in the source case, a ClickHouse `SELECT ... ORDER BY entity_id, time LIMIT N` paginated query) emits batches of, say, 50 000 rows. The consumer logic is entity-centric and stateful: for each unique entity, it wants to **Init → Stream all rows for this entity → Finalize**. The boundaries do not line up. An entity's rows may begin near the end of batch *N* and continue through batch *N+1*. Rebatching every entity into memory defeats the purpose of streaming; calling the consumer's `Process` once per batch hands it a fragment that doesn't compose into a single stateful pass.

The conversation refers to this as an *impedance mismatch*. The package's job is to absorb it.

## The bridge

```
source (batches)              processor                  consumer (one entity)
─────────────────             ─────────                  ─────────────────────
StreamBatches() ─┐                                       Process(ctx, id,
                 │                                          iter.Seq[V])
                 │ for each batch:                            │
                 │   group by entity ID                       │
                 │   for each contiguous run:                 │
                 │     pool.Get() + copy                      │
                 │     send chunk ──────► chan []V ──────►   for chunk := range ch:
                 │                                              for item := range chunk:
                 │   on entity change:                              yield(item)
                 │     close chan, join goroutine                pool.Put(chunk)
                 │     (Finalize for that entity)
                 │
                 ▼ end-of-stream: same closeCurrent dance
```

The consumer's `Process` runs in a **dedicated goroutine** for the duration of one entity lifecycle. Its `iter.Seq` argument is an iterator backed by a channel of pooled chunks. When the channel is empty the consumer's range over `iter.Seq` blocks; when the next chunk for the same entity arrives, iteration resumes. This is the "suspended iteration" the design called for — the consumer holds a single execution frame across batch boundaries, and the channel bridge synchronises producer and consumer with natural backpressure.

When the main loop sees the entity ID change (or end-of-input), it closes the channel, the consumer's range drains and exits, `Process` returns, and the goroutine joins. Then the next entity starts a fresh goroutine.

## Why each piece looks the way it does

**`iter.Seq` for the consumer, `iter.Seq2` for the source.** Both are Go 1.23 idioms. The source needs to return errors inline (network/I/O failures), so `iter.Seq2[[]V, error]`. The consumer just wants rows, so `iter.Seq[V]`. The blocking-channel-as-iterator pattern is what makes the cross-batch suspend cheap: no manual state machine, just a `for chunk := range ch`.

**Chunked handoff, not row-by-row.** An earlier version of the design pushed one row at a time through the channel. Per-row channel ops cost on the order of tens of nanoseconds; for a stream of millions of rows that's where all the time goes. The processor instead finds each maximal contiguous run of the same entity ID within a batch and sends the whole run as one chunk. Synchronisation cost amortises over the chunk.

**Defensive copy from batch into a pooled chunk.** Production-grade DB drivers reuse the batch's backing array between yields to avoid allocation. If we forwarded the source's slice directly, a slow consumer would read corrupted data the moment the source advanced. The processor always copies into a chunk from its own pool. `TestProcessor_MemorySafety_CopyCheck` mocks exactly this driver pattern.

**`sync.Pool` for the chunks.** Without pooling, every chunk is a fresh allocation — at high throughput, a measurable share of CPU goes to the GC. With pooling, each chunk's backing array is reused. The pool is wrapped in a `SlicePool[T]` to be type-safe and to store the slice header indirectly (`*[]T`) so `sync.Pool` doesn't pay a heap alloc on every Put. The benchmarks show the pool is worth ~2.6× on the throughput path and turns ~265 KB/op of chunk allocations into ~3 KB/op of bookkeeping. The pool also drops slices that have grown well past the configured capacity so spiky batch sizes don't ratchet pool memory upward.

**Generics over a concrete row type.** The producer key type is whatever the source uses (`string` for HN usernames, an `int64` for IDs, …); the value type is whatever the source rows are. `EntityItem[K]` is the one constraint: a row must know which entity it belongs to. The pool is generic too, so chunk arrays are stored unboxed.

**Panic recovery in the consumer goroutine.** A panic inside `Process` would otherwise tear down the whole program. The runner wraps `Process` in `defer recover()`, builds an error via `ph.ConvertPanicToError` (which captures the panic stacktrace), **logs** it via zerolog at the recovery site, and returns it via the done channel so `Run`'s caller sees a normal error. Logging at the recovery site matters because callers that drop the returned error would otherwise silently lose the panic.

**The Prefetcher as an opt-in wrapper, not a built-in.** When the source is slow (network query, decompressed columnar read), the processor would idle waiting for the next batch. Wrapping the source with `Prefetcher(source, depth)` runs the upstream reader in its own goroutine and buffers `depth` batches ahead. The wrapper is decoupled from the processor — it just satisfies `BatchReaderI` — so callers pay the prefetcher's extra goroutine + channel only when it helps. For in-memory sources it's net overhead.

**`Run` may be re-called; it is not internally goroutine-safe.** The chunk pool survives across calls (warm-up amortises), but the consumer instance is shared state. Calling `Run` concurrently on the same `Processor` from different goroutines is only safe if the consumer itself is safe to invoke concurrently — the processor doesn't synchronise on it.

**Metrics are opt-in via `MetricsCollectorI`.** The four hooks — `RecordBatch`, `RecordRows(n)`, `RecordEntityFinalized(ok bool)`, `RecordEntityDuration(d)` — match the "introspectable" requirement from the design conversation. Naming and the `Record<Event>(<variant flag>)` shape mirror `caching.MetricsCollectorI` (`RecordHit(l1 bool)`, `RecordEviction(toStash bool)`, etc.) so the two packages read the same way. The default is a `noopMetrics` no-op, so the hot path pays nothing when nobody is watching. All hooks fire from Run's own goroutine.

## Consumer contract

Three explicit contracts on `Process`:

1. **Honor `ctx`.** On cancellation, `Run` closes the row channel and waits for the consumer goroutine to exit before returning. A `Process` that ignores `ctx` and doesn't return when the channel is closed (because it's blocked on something else) will block `Run` indefinitely. The contract is documented in the `Run` doc comment.
2. **Returning `nil` mid-stream means "done with this entity."** The processor drops the remaining rows for that entity and moves on. The early stop *cannot* mean "abort the whole pipeline" — that's what a non-nil error is for.
3. **Returning a non-nil error aborts the pipeline.** The error is wrapped as `"consumer for entity <id>: <err>"` so multi-entity pipelines can identify the failing entity without instrumenting the consumer.

The first contract is the most consequential and the one a naive consumer is most likely to violate.

## What was not built, and why

The design conversation looked at four classes of existing libraries before deciding to write this one:

- **Reactive (RxGo)** — has `GroupBy` operators, but its `interface{}`-based core defeats both the generic type safety and the zero-allocation chunk handoff. The package wants both.
- **Actor (Proto.Actor)** — the entity-as-actor mapping is conceptually clean. The lifecycle management is also automatic. But spinning up an actor system to drain a SQL cursor is architectural overkill for a single-process pipeline.
- **ETL (Benthos / Redpanda Connect)** — abstracts the `BatchReader` side well, but injecting arbitrary Go consumer logic into the middle of a Benthos pipeline requires writing a Benthos plugin, which is more work than the loop in `processor.go`.
- **Kafka-coupled stream processors (Goka)** — solve the entity-lifecycle problem with the right semantics, but presume a Kafka source. The source here is a SQL cursor.

Three more deliberate non-features:

- **No internal sharding.** A single `Run` is single-threaded across entities. The design discussion sketched a `ShardedReader` (a `cityHash64(entity_id) % N` modulo predicate in the SQL) that lets the caller run `N` `Processor` instances in an `errgroup`. The package doesn't bundle that — it stays single-stream and leaves parallelism to the caller, who knows whether their consumer is per-shard isolated.
- **No checkpointing.** The processor doesn't persist progress; if `Run` dies mid-stream, restart depends on the source's own ability to resume (the canonical reader uses keyset pagination, so it can).
- **No retry, no stuck-consumer watchdog.** The Stage 7 design notes mentioned both as possible additions. Neither is here, deliberately. Retry doesn't fit the consumer's stateful lifecycle: if `Process` fails halfway through entity *N*'s rows, the processor has already forwarded those rows and returned their pool chunks; replaying them would require the consumer to be idempotent over partial state, and the processor has no way to know whether it is. Retry belongs either inside the consumer (which knows what's idempotent at the row or entity level) or inside the source (which knows what's transient at the network/SQL level). Stuck-consumer detection is what `ctx` is for: the contract already says `Process` must honor it, so a per-entity deadline is one `context.WithTimeout` wrapper away on the caller side. Bundling a `MaxEntityDuration` knob would cover for consumers that violate the ctx contract and would trip on legitimately long entities (a year of comments for one heavy poster). If you want to detect stuck consumers in production, wire `RecordEntityDuration` to a histogram and alert on the tail — that gives visibility without coupling the processor to a policy decision that depends on the workload.

## Known limitations

These are not bugs but worth being explicit about:

- **Reappearance creates a new lifecycle.** If the same entity ID shows up after a different entity has been processed, it is treated as a brand-new entity — fresh `Process` invocation, fresh state. This matches the assumption that the source is grouped by entity ID. Unordered input silently produces bad groupings.
- **`Run` blocks on a non-cooperative consumer.** Per the contract above, this is deliberate: the alternative is leaking the goroutine.
- **Pool `Put` allocates the slice header.** The `&s` on a local-variable slice escapes the pool method, costing ~24 B/op on the micro-benchmark. The backing array is reused; only the header escapes. A `Put(*[]T)` signature would fix it but changes a stable API.
- **Pool elements are not cleared by default.** If `V` contains pointers (or strings/slices/maps/interfaces) and you need the references released for GC, construct the pool with `WithZeroOnPut[V]()`. Non-zeroing is the default for performance.

## Historical notes

- The pattern originated as a `StatefulProcessor` inside a Hacker News scraper application (the Gemini "Hacker News API Scraper" session, prompts 30–45). The user observed that `AddMessages` was being called once per batch instead of once per entity, identified the suspended-iterator pattern as the fix, and asked for a standalone unit-tested version. The follow-on session "Stateful Streaming — Entity Lifecycle Processing" extracted it into a generic package.
- The post-Gemini code review surfaced six correctness bugs the design conversation did not catch: a `nil`-error-wrap on the early-exit path (B1), a goroutine leak on `ctx.Done` (B2), a prefetcher producer leak (B3), pool chunks lost on early yield-false (B4), an unused `ChunkPoolI` interface with no injection (B5), and recovered panics that built errors but never logged (B6). These are fixed; the tests for them double as regression coverage.
- The `Prefetcher` factory previously accepted a `ctx` argument it never used; it has since been removed (in-repo callers updated; the `stylometrics` analyzer needed a matching one-line tweak).

## References

- The package's tests under `processor_test.go` and benchmarks under `processor_bench_test.go` exercise every claim above.
- Go 1.23 range-over-function (`iter.Seq`, `iter.Seq2`) — https://tip.golang.org/blog/range-functions
- `sync.Pool` SA6002 pattern (store `*[]T`, not `[]T`) — https://staticcheck.dev/docs/checks/#SA6002
