---
type: adr
status: proposed
date: 2026-09-05
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0221: pushout storage seam — batch verbs, a delta retention ledger, a batch acceptor, and history behind a contract

## Context

[ADR-0220](./0220-pushout-storage-capabilities-and-retention-mode.md)
let a store declare what it persists, and named the store shape the
downstream dewmdm design wants: structured rows, no frames, no
snapshot (hackathon_2026 ADR-0007, Updates 2026-09-05). Reading the
seam from that shape's side found four places where the API still
assumed a directory of files, each costing a round trip or a rewrite
that an append-only or row store does not need:

- **Half-batched writes.** `AppendAppliedBatch` made the log append one
  write, but `Repo.ApplyEnvelopes` (ADR-0079 update 2026-09-04) still
  put envelopes one `PutEnvelope` at a time. On ClickHouse that is one
  synchronous insert and one part per patch — the part-pressure risk
  the downstream soak (hackathon_2026 ADR-0007 OQ1) names as its
  primary engineering risk. ADR-0079's own update called a batched put
  "the next step".
- **Recovery was n point reads.** `Open` fetched every envelope to
  replay through `GetEnvelope`. A store that disclaims snapshots
  replays its whole history that way, one round trip per patch, while
  a row store can answer the applied set in one keyed query.
- **The ledger was rewritten whole.** `SaveRetention` replaced the
  entire ledger on every delete-bearing patch, every `Unrecord`, every
  `Sweep` and every reconciling `Open`. That is the filestore's atomic
  file replace leaking into the contract: on an append-only store it
  costs one row per live tombstone per delete, and the engine had to
  compute the full set to call it.
- **The transport was single-envelope.** `exchange.AcceptorI` offered
  only `ApplyEnvelope`, so `Pull` and `Push` applied one at a time and
  the batch verb was reachable only by a transport that bypassed
  `exchange` — which `exchangetest` then did not cover.

Beside these, the engine kept every decoded patch it had ever seen in
an unstructured map filled eagerly at `Open`, documented as "lazy" and
reached from three places. A repo without snapshots therefore held its
whole decoded history for the life of the handle, and nothing said
which part of that was contract and which was implementation.

## Decision

Make the seam symmetric in batches, write the ledger as deltas, give
the transport the batch verb, and put decoded history behind a stated
contract — without changing what a consumer relies on.

**SD1 — batch envelope verbs.** `StorageI` gains
`PutEnvelopes(ctx, []Envelope)` with the contract of consecutive
`PutEnvelope` calls (durable on return; a crash may persist any
subset, which is harmless because the log is the commit point), and
`LoadEnvelopes(ctx, hs) iter.Seq2[Envelope, error]`, yielding one
envelope per requested hash in request order and ending with
`ErrEnvelopeNotFound` at the first miss. `ApplyEnvelopes` puts its batch
in one call; `Open` replays through one `LoadEnvelopes` over the
uncovered log; `Repo.EncodedEnvelopes` and `Unrecord`'s dependent scan
read the same way. Two exported helpers, `PutEnvelopesOneByOne` and
`LoadEnvelopesOneByOne`, are the conformant implementation for a store
with no batch path.

**SD2 — the retention ledger is written as deltas.** `SaveRetention`
is replaced by `UpdateRetention(ctx, RetentionDelta)`, where a delta is
the entries to upsert and the nodes to remove. The verb is atomic,
durable and idempotent; `LoadRetention` returns the folded set. The
engine computes the delta between the committed graph and the one
about to commit (`DiffRetention`) and writes nothing when it is empty —
so a delete-bearing patch writes one upsert, a `Sweep` writes its purge
markers, and a reconciling `Open` writes only what the replay changed.
`ApplyRetentionDelta` is the fold for a store that keeps the set whole.

**SD3 — the acceptor takes a batch.** `exchange.AcceptorI` gains
`ApplyEnvelopes(ctx, [][]byte) (repo.BatchReport, error)`; `Pull` and
`Push` ship the missing envelopes as one batch and let the applying
side sort. A transport must carry the report back intact — Applied,
Duplicates, and Pending with each pending envelope's Missing list.
`Stats` gains `Pending`; a run that leaves envelopes pending ends with
`repo.ErrMissingDependency`, so a caller that classified errors before
still does. `exchangetest` gains three checks: the batch verb across
the carrier, a reordered peer converging in one run, and partial-
failure accounting from a peer that withholds a dependency.

**SD4 — decoded history has a contract, and one implementation.** The
engine's decoded-patch cache becomes a component (`repo/history.go`)
whose contract is all `Repo.PatchInfo`, `ViewI.PatchInfo` and
`Repo.Unrecord` promise: the envelopes in storage are the system of
record; the engine holds some subset decoded, chosen by the component;
a lookup may read and decode from storage, so it can fail with a
storage error and is not O(1). The shipped implementation keeps
everything it decodes, as before. A bounded or lazy one — an LRU, a
reverse-dependency index, a store-side query for dependents — replaces
it without touching `Repo`'s API: `remember` may drop, `lookup` may
fetch, `dependents` may answer from an index; `LoadEnvelopes` is what
makes the fetch path one round trip.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `repo.StorageI` | `PutEnvelopes`, `LoadEnvelopes` added; `SaveRetention` → `UpdateRetention` | `filestore`, `pushoutstore`, every test double, `storagetest` |
| `repo` types | `Envelope`, `RetentionDelta`, `ApplyRetentionDelta`, `DiffRetention`, the one-by-one helpers | — |
| `repo.Repo` | `EncodedEnvelopes` added; `Open`, `ApplyEnvelopes`, `Unrecord`, `Sweep` use the batch and delta verbs | `RecoveredEvent` unchanged |
| `repo/history.go` | the decoded-history component and its contract | `PatchInfo` documentation |
| `exchange.AcceptorI`, `Stats` | `ApplyEnvelopes`, `Pending` | `inproc`, `exchangetest`, the pijul backend (unchanged API, batched underneath) |
| `filestore` | batch put fsyncs each shard directory once; ledger folded in memory and replaced as before | ledger line format unchanged |
| `pushoutstore` | one keyed scan plus one insert per batch put; keyed scans in chunks for the batch load; one delta row per ledger update, compacted every 32 into a tombstone plus one full row; the `purges` key is gone | `Retention` DTO gains `Ops`; schema gains `retOp`; regenerated |
| `repo/storagetest` | `CheckEnvelopeBatch`; `CheckRetention` checks folding, idempotence and removal | every conformance run |

## Alternatives

- **Keep `SaveRetention` and add an upsert beside it.** Rejected: two
  verbs for one fact leave the engine choosing per call, and the
  whole-set verb would remain the one every store must make atomic.
  The delta is the general form; the whole set is a delta from empty.
- **Make `LoadEnvelopes` return a slice.** Rejected: an iterator lets a
  store stream a large history and lets the engine apply as it reads;
  the slice is the caller's to build when it wants one
  (`EncodedEnvelopes` does).
- **A `dependents(h)` verb on the seam.** Deferred: the dependent scan
  is now one batch read behind the history component; a store-side
  reverse index is that component's business, not the seam's, until a
  store wants to offer one.
- **Chunking inside `Pull`/`Push`.** Deferred to ADR-0079 OQ-1: the v1
  protocol already fetched every missing envelope at once, so one batch
  changes memory nothing; what a round carries is the frontier design's
  question.
- **Making the ledger delta the applied log's shape too** (retraction
  entries for `Unrecord`). Deferred, as in ADR-0220: it changes the
  log's meaning and is its own decision.

## Consequences

### Positive

- One engine verb is one storage write on a store with a batch path:
  `ApplyEnvelopes` is one envelope insert, one ledger row, one log
  append.
- A snapshot-less recovery is one keyed read of the applied set.
- Ledger writes are proportional to the change; the ledger on an
  append-only store is bounded by compaction.
- The transport owes no delivery order, and `exchangetest` proves it.
- The decoded-history cost is named, and replaceable behind `Repo`.

### Negative

- Every `StorageI` implementation gains two methods and changes one;
  every `AcceptorI` gains one.
- `DiffRetention` walks every tombstone of both graphs per verb —
  O(tombstones) CPU where the I/O is now O(changed). A graph that
  reports the tombstones a verb touched removes that without touching
  the seam.
- The pushoutstore ledger is a fold over up to 32 rows at first use
  rather than one `Latest`; its mirror holds the ledger in memory.

### Neutral

- Identity, the wire, `PeerI`, `Guarantees`, the retention modes and
  every ADR-0220 flag are untouched.
- The filestore's on-disk layout is unchanged; its batch put is a
  different fsync schedule, not a different file.

## Migration — Tier 1

- **Breaks.** `StorageI` implementations without `PutEnvelopes`,
  `LoadEnvelopes` and `UpdateRetention` stop compiling, as do
  `AcceptorI` implementations without `ApplyEnvelopes`.
- **Path.** Delegate the batch verbs to the one-by-one helpers, or
  implement them natively; turn `SaveRetention(entries)` into
  `UpdateRetention(delta)` with `ApplyRetentionDelta` over the stored
  set; delegate `AcceptorI.ApplyEnvelopes` to `repo.ApplyEnvelopes`.
  Run `storagetest` and `exchangetest`.
- **Regeneration.** `pushoutstore` regenerated (`TestGeneratePushoutStore`);
  the table's DDL changed, and under ADR-0079's stated premise (no
  persisted pushout data to preserve) it is recreated, not migrated.
- **Old shape.** `SaveRetention` and the `purges` key removed outright.

## Verification plan — Tier 1

- **Lane.** Default `go test`: `storagetest` over the filestore, the
  recordstore adapter, the no-snapshot and no-ledger decorators and the
  in-memory double (`CheckEnvelopeBatch`, the delta `CheckRetention`,
  `CheckReopenDurability`); `repo`'s `TestSeamUsage_BatchVerbsAndDeltas`
  pinning one batch put per ingest, one batch load per full replay and
  one-upsert deltas per delete; `pushoutstore`'s compaction test across
  two compactions and a reopen, and its one-insert / two-chunk batch
  test with an insert-counting executor; `exchangetest`'s three new
  checks over `inproc`; the pijul state-machine harness unchanged.
- **What would fail.** A store whose batch verb disagrees with its
  one-by-one verb; a delta re-applied that changes the ledger; an
  engine verb that reaches for a single-envelope verb on a batch path;
  a transport that drops the pending list; a recovery that reads
  envelopes one at a time.
- **Gap.** No store yet offers a bounded history; SD4's contract is
  exercised only by the eager implementation. No re-encoding store is
  in-tree (ADR-0220's gap stands). The Quint crash-recovery model
  abstracts the ledger as a set and is unchanged: a delta applied
  atomically is the same step.

## Status

Proposed — implementation landed with this record for review together;
awaiting review by the pushout code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0220](./0220-pushout-storage-capabilities-and-retention-mode.md) — capabilities, the purge-carrying ledger, retention modes.
- [ADR-0079](./0079-pushout-production-storage-codec-exchange.md) — the storage seam; update 2026-09-04 (`ApplyEnvelopes`) and OQ-1.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the recordstore adapter (S3).
- hackathon_2026 `doc/workspace/sprint1/storagei-layer-design.md` §2.13 — the downstream reading that found the four idioms.
