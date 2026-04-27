---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "@stergiotis"
reviewed-date: 2026-04-27
---

# Streaming/persisted/kafka — Interface Contract

This document explains the *why* behind the interface boundary in `types.go`. The choices the package makes — concrete types over interface envelopes, a fire-and-forget ack callback, an iterator-shaped record stream — are not local design preferences. They follow from properties of the upstream Connect contract this package derives from, and from the franz-go vocabulary it is built on.

For the derivation decision itself (Apache-2.0 derivative, Benthos service framework dropped, scope, license posture), see [`doc/adr/0015-streaming-persisted-kafka-from-connect.md`](../../../../../../doc/adr/0015-streaming-persisted-kafka-from-connect.md).

## What this package provides

A franz-go-based Kafka consumer (`ConsumerI`, satisfied by three readers — ordered, unordered, toggled) and producer (`ProducerI`, satisfied by `FranzWriter`). The seam at the application boundary is the `Batch` struct plus the `AckFn` callback. Configuration is plain Go option structs; logging is `zerolog`; lifecycle is `context.Context`.

## Interface boundary

```text
   application code
        │
        │  reads: Batch{Records kgo.Fetches, Ack AckFn}
        │  writes: ProducerI.Write(ctx, *kgo.Record...)
        │
        ▼
   ConsumerI / ProducerI            ← package-local seam (this file)
        │
        ▼
   franz-go *.go (FranzReaderOrdered, FranzReaderUnordered,
                  FranzReaderToggled, FranzWriter)
        │
        ▼
   github.com/twmb/franz-go/pkg/kgo  ← Kafka client library
```

## Ack contract

This is the most subtle piece of the boundary; the upstream contract is unobvious from the type signature alone.

### Signature

```go
type AckFn func(ctx context.Context, processErr error) (err error)
```

### What the franz-go-derived reader actually does

`AckFn` arrives in the application as an opaque callback. Inside the upstream-derived reader (Phase 5) it closes over the reader's per-partition checkpointer; invoking it triggers four side-effects, in order:

1. Acquire the reader's mutex.
2. Update the partition checkpointer to release the offsets covered by this batch.
3. Decrement the reader's in-memory back-pressure cache size by the batch's record-count weight, possibly resuming a previously-paused partition fetch via `kgo.Client.ResumeFetchPartitions`.
4. If the released offsets reach the head of the partition's pending dispatch queue, call `kgo.Client.MarkCommitRecords` so the offset advances on the next periodic commit (or on rebalance).

Both arguments are accepted but not consulted in the current port. The signature shape is preserved verbatim from upstream so the closure form of the franz-reader port is mechanical, and so future implementations may honour either argument without breaking callers.

### Strict in-order, exactly-once-call

Acks must be invoked in the order batches are returned. The reader's pending-dispatch queue blocks `Read` from returning a subsequent batch on the same partition until the prior batch's `AckFn` has fired. Out-of-order calls are a contract violation; the package does not detect them today, but `MarkCommitRecords` will commit the wrong offset if they happen.

`AckFn` must be called *exactly once*. Calling it zero times stalls the partition (eventually triggering a rebalance as the consumer-group session times out). Calling it twice double-decrements the back-pressure cache size and double-releases the checkpointer slot — both bugs we currently detect only in tests.

### What `processErr != nil` *will* mean

The upstream reader is paired with `service.AutoRetryNacks` in the Benthos pipeline, which intercepts processing failures *before* they reach the ack callback and reschedules them. The bare ack thus never sees a failure — the framework handles retry off-band.

This package has no auto-retry framework. A processing failure must therefore have one of three resolutions, chosen by the caller:

- **Process-and-acknowledge.** Caller has handled the failure (logged it, written to a dead-letter sink, etc.) and now wants the offset committed so the partition advances. Caller passes `nil` as `processErr`.
- **Stall.** Caller wants the partition paused until external intervention. Caller does not call `AckFn` at all and lets the consumer-group session time out.
- **Future: NACK.** Caller passes `processErr != nil` and expects the reader to leave the offset uncommitted *and* release the back-pressure slot so reads can continue. Not implemented today; the signature reserves the slot.

The third option is the reason `processErr` is in the signature even though the current implementation ignores it.

### Why not split into Ack() / Nack()

Two reasons:

1. The franz-reader port closes a `func(context.Context, error) error` over the per-batch checkpointer state; rewriting that to a two-method interface multiplies the closure surface and the in-flight tracking by two for no current benefit.
2. The Connect contract empirically does not need `Nack` — the AutoRetryNacks layer above it does. Until pebble2impl grows an analogous retry layer, splitting the ack contract is premature.

If a Nack semantics is added later, the migration is `processErr != nil` → reader-honoured offset suppression. Callers who already pass a faithful `processErr` will get the new behaviour for free.

## Iterator vs slice vs callback

`Batch.Records` is `kgo.Fetches`, the franz-go type, exposed directly. Callers iterate via the methods franz-go provides:

- `Records.RecordsAll()` returns `iter.Seq[*kgo.Record]` — the Go 1.23+ range-over-func form. Idiomatic in pebble2impl (FFFI2 generated code, egui Fetcher, fffi2_rt all use `iter.Seq` heavily) and the natural shape for the application's record-by-record loop.
- `Records.Records()` returns `[]*kgo.Record` — the slice form. Use when the application needs `len(records)` up front or wants index-based access.
- `Records.EachRecord(fn)`, `Records.EachPartition(fn)`, `Records.EachTopic(fn)` — callback forms. Use when the per-partition or per-topic structure of the fetch matters (rare in practice).

The package does not pick one for the caller. Three things make this safe:

- All three forms share the same underlying record values; no copying or rewriting happens between them.
- `kgo.Fetches` is the upstream type; we are not abstracting away from it, so re-exposing its full method set has zero cost.
- Wrapping in our own iterator face would force every caller through one of the three shapes anyway, and would re-create the choice at the caller level for no reduction in API surface.

## Why concrete types not interfaces (Batch, *kgo.Record)

ADR-0015 sketched a `RecordEnvelopeI` interface as a placeholder. Phase 1 reverses that to a concrete `*kgo.Record` for three reasons:

1. **Allocation pressure.** A consumer pulling 10–100k records/s through an interface envelope generates 10–100k interface-conversion allocations per second. franz-go's hot path is allocation-conscious; wrapping its records discards that work.
2. **No second implementation.** The package has exactly one record source (kgo) and exactly one consumer of `Batch` (the application). Tests use real `*kgo.Record` values constructed in-process. There is no mock target to abstract for.
3. **kgo's vocabulary is the upstream-derived contract.** The franz-reader port already speaks `kgo.Fetches`/`*kgo.Record` natively; introducing an envelope creates two parallel record types that must be kept in sync as kgo evolves (new header semantics, KIP-1222 per-record ack, transactional metadata).

`Batch` itself is a struct rather than an interface for the same reasons: one producer of `Batch` values (the readers), one consumer (the application), no mock target.

`ConsumerI` and `ProducerI` *are* interfaces — there are three reader implementations (ordered, unordered, toggled) and the application binds to whichever one its config selects. The interface is the only place the package's client code branches; every other type is concrete.

## Lifecycle

```text
   ┌─────────┐  Connect  ┌──────────┐  Read*N    ┌─────────┐
   │ created │ ────────▶ │ ready    │ ─────────▶ │ closed  │
   └─────────┘           │ (Connect │ Close      └─────────┘
                         │  ed)     │ ────────▶
                         └──────────┘
```

- `Connect(ctx)` brings up the underlying `kgo.Client`. May fail if broker addresses are unresolvable; transient broker errors are retried internally by kgo and not surfaced.
- `Read(ctx)` blocks until at least one record is available, ctx is cancelled, or the reader terminally fails. Returns `ErrNotConnected` if called before `Connect` or after `Close`.
- `Close(ctx)` flushes any in-flight commits, tears down `kgo.Client`, and is idempotent. Safe from any state.

Producers follow the same shape with `Write(ctx, records...)` in the middle slot, returning nil only after every record is broker-acknowledged.

## Relationship to the Benthos service framework (which this package replaces)

The upstream code is shaped against `github.com/redpanda-data/benthos/v4/public/service`. The mapping for code archaeologists:

| Upstream                 | Pebble2impl                              |
|--------------------------|------------------------------------------|
| `service.Input`          | [`ConsumerI`](types.go)                  |
| `service.Output`         | [`ProducerI`](types.go)                  |
| `service.MessageBatch`   | [`Batch.Records`](types.go) (`kgo.Fetches`) |
| `service.Message`        | `*kgo.Record`                            |
| `service.AckFunc`        | [`AckFn`](types.go)                      |
| `service.ErrNotConnected`| [`ErrNotConnected`](types.go)            |
| `service.Resources`      | (split: `*zerolog.Logger`, no metrics yet) |
| `service.ConfigField`    | plain Go option structs (Phase 4)        |

The mapping is mechanical for archaeology purposes; the contracts diverge in ack semantics and configuration shape, as discussed above.

## Open today

- **Metrics.** No package-wide `MetricsI` abstraction; the per-feature callback shape (`LagSinkFn` on `[ConsumerLag]`) is what the package commits to today. Reader-side counters (records/s, rebalance count) still emit through `zerolog` debug logs only — adding them would require a wider seam, which is deferred until a second metric appears. The per-message `kafka_lag` metadata that upstream attached on each delivered record is *not* restored: `*kgo.Record` is exposed directly per ADR-0015, so per-message metadata is the application's job; `ConsumerLag` is a standalone utility composed with the reader via `FranzReaderOrdered.Client` / `FranzReaderUnordered.Client`.
- **Shared client across reader + writer.** `FranzWriter` accepts a caller-supplied `*kgo.Client`, so the application can construct one client and use it across multiple writers. The readers (`FranzReaderOrdered`/`FranzReaderUnordered`/`FranzReaderToggled`) still construct their own client internally via the `clientOpts` factory, so a reader/writer pair currently means two `*kgo.Client` instances. A `NewFranzReaderFromClient` variant would close the gap; deferred until a use case appears (transactions, EOS pipelines).
- **`MessageTransportI` adapter.** `src/go/public/transport/message/MessageTransportI` exposes `Publish([]byte, []byte, identifier.TaggedId) error` — single-message, hash-keyed, opaque-payload. Adapting `ProducerI` to satisfy it (lossy: kgo records carry topic/key/headers/timestamp that don't fit) is deferred until a use case appears.
- **Per-record `Record.Ack(status)` (KIP-1222).** Available in franz-go but not used here; the upstream reader uses the older `MarkCommitRecords` path and we follow that choice.
- **Sarama variant.** Out of scope per ADR-0015 and unlikely to return; franz-go is the strategic client.
