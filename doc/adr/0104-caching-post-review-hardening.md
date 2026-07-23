---
type: adr
status: proposed
date: 2026-07-04
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0104: caching — post-review hardening

## Context

An adversarial review of `public/caching` (2026-07-04) confirmed fifteen
defects via runnable repros. The package predates the repo's
design-before-code practice: it was imported in one commit (2026-02-17)
with tests written alongside the code, and had received one targeted patch
cycle but never a hostile review. The defects clustered around two
structural decisions:

- **Item state lived only in L1.** The stash interface trafficked in bare
  `(K, V)`, so every L1→stash demotion silently erased the state machine.
  A failed fetch left a zero-value `ItemStateError` placeholder that
  eviction demoted and a later `Get` served as a legitimate hit — for the
  recordstore instantiation (`V = *Entity`) a `(nil, true)` result that
  panics `GetLive`. Staleness marks were likewise stripped in transit, and
  `MarkAsStale` could not reach stash-resident entries at all.
- **The circuit breaker stored its bookkeeping as fake value items.** A
  failing wide fetch inserted one placeholder per failed key, bypassing the
  capacity check (unbounded L1 growth), and a failed refresh flipped a
  still-held stale value into an unreachable error entry — breaking
  stale-while-revalidate exactly during upstream outages.

The remainder were protocol and validation defects: a `break` out of the
`WorkItem` iterator leaked the work-item context; a `break` out of a replay
loop dropped the un-yielded pending items; `Delete` did not dequeue, so an
in-flight batch resurrected invalidated keys; the documented "Max
thresholds fire synchronously from inside Get" held only on the cold-miss
path; `IterateReadyWorkItems` starved ready items when the key queue was
empty; the three stash implementations disagreed on `Add` of an existing
key (the default `SliceStash` duplicated and later served the older copy);
zero-capacity stashes panicked at first use or over-admitted; absent
upstream keys were re-fetched forever (already recorded as an ADR-0100
deferral).

## Decision

**Question.** How does item state survive tiering, where does failure
bookkeeping live, and how far does the API cut go?

- **D1 — the stale flag travels through the stash** (chosen).
  `StashBackendI.Add` takes and `GetAndRemove` returns the stale bit; a
  stash hit is promoted into L1 preserving it and routed like a native L1
  entry. Alternatives killed:
  - *Drop stale entries at demotion* — simplest, but loses
    stale-while-revalidate for demoted entries; rejected because keeping
    SWR through eviction is a requirement.
  - *Track staleness in a cache-side side table* — leaks: stash evictions
    are invisible to the cache (no victim reporting), so flags for
    silently-dropped keys accumulate unboundedly, and bounding the table
    would silently un-stale entries.
- **D2 — the circuit breaker is a value-free side table.**
  `errorUntil map[K]time.Time`, bounded by `max(capacity, 64)` with
  random-drop on overflow (a lost entry merely allows an early retry). No
  placeholder ever enters the value store; a stale value whose refresh
  failed stays resident and servable to `GetAcceptStale` while the breaker
  suppresses re-queues. Breaker state deliberately does not travel through
  the stash: it carries no value, and its TTL bounds the table without
  victim reporting.
- **D3 — negative caching is opt-in** (`WithNegativeCaching(ttl)`), closing
  the ADR-0100 deferral. After a clean fetch, requested-but-undelivered
  keys are marked absent for the TTL in a second bounded side table; a Get
  on a marked key misses without queueing and without suspending the work
  item, so flush-until-quiet replay loops terminate. An absent verdict is
  authoritative and also drops any cached remnant — unlike a fetch
  *failure*, which preserves the stale value; a surviving stale entry would
  otherwise re-queue the key on every read (found by the model test, not
  the review). Default off: absent keys then re-probe per flush, exactly
  as before.
- **D4 — a Max-triggered synchronous flush re-routes the triggering
  lookup.** Criteria are checked on every queueing path (cold miss and
  stale refresh), and when the flush fires the lookup re-runs once on the
  post-flush state: the value fetched within the call is served instead of
  the pre-flush snapshot (or a miss). Without this, `GetAcceptStale` could
  report a value as stale that the cache had already replaced.
- **D5 — one breaking API set** (verified: nothing outside the package
  references any of it): `RecordHit(l1, stale)` makes SWR traffic
  observable; `ItemTargetI` slims to `AddItem`; `StashBackendI` gains
  `Clear()` and an explicit update-in-place contract for `Add`; the unused
  exported `ItemStateE` constants are gone (state is an internal bool).
  The cache gains `Clear()`, `Close() error` (closes an `io.Closer`
  stash), and `Len`/`StashLen`/`QueuedKeys`/`PendingWorkItems`
  introspection.
- **D6 — protocol hygiene.** `WorkItem` and the replay iterator restore
  context via `defer` (break- and nesting-safe); un-yielded replay items
  are re-queued on early exit; `Delete` dequeues; a cancelled flush leaves
  unprocessed partitions queued; a nested flush (fetcher re-entering the
  cache) is a no-op with the keys deferred to the next flush; constructors
  validate capacities and the fetcher.

## Alternatives

Per-decision alternatives are recorded inline above — each `D*` lists the
options it rejected and why. Two package-level alternatives were weighed
before choosing to harden in place:

- **Rewrite `public/caching` from scratch.** Rejected: the fifteen defects
  are localised to tiering, breaker bookkeeping, and iterator protocol; the
  cache's structure (L1 + stash + work-item batching) survived the review.
  A rewrite would discard working code and its test corpus to fix bounded,
  understood faults.
- **Document the defects and defer.** Rejected: several are silent
  correctness faults — a `(nil, true)` hit that panics `GetLive`, SWR lost
  during an outage — that violate the cache's stated contract, so they
  cannot sit behind a "known issues" note.

## Consequences

- The generated recordstore caches (`ADR-0100` SD5) get the documented
  `MarkStale` / accept-stale semantics they already claim, with no
  regeneration: the consumed surface is unchanged. Adopting `Clear()` for
  `InvalidateAll` and negative caching for `HasEnvelope` are recorded
  follow-ups.
- The disk stashes' on-disk value encoding changed (a CBOR envelope
  carrying the stale bit). Pre-existing cache directories decode as
  misses, which the best-effort stash contract tolerates; `cleanStart`
  resets.
- Failure asymmetry is now part of the contract: fetch *failure* preserves
  stale values (best guess during an outage), an absent *verdict* removes
  them (authoritative answer).

## Test strategy

Four layers, replacing confirmatory-only coverage:

- **Regression suite** (`caching_review_regressions_test.go`): each review
  defect inverted into an assertion of the remediated behavior.
- **Stash conformance suite** (`stashtest` package): one contract runner
  over all four backends — update-in-place, stale-bit round-trip, removal,
  idempotent delete, eviction honesty, `Clear`, unbounded mode.
- **Model-based randomized test**: seeded op sequences against an
  independent oracle (deterministic via an injected clock); invariants
  cover value integrity (no phantom, superseded, or invalidated values),
  staleness honesty, structure bounds, breaker/absent window suppression
  (checked fetcher-side), and a metrics ledger. The oracle found two
  defects the review missed (the stale-requeue absent bypass and the
  pre-flush snapshot serve).
- **Fuzz target** (`FuzzCacheOps`): the same driver fed by raw bytes,
  configuration included; the seed corpus runs as unit tests.

## Status

Proposed (2026-07-04) — pre-human-review, as the banner above states. The
decision is under consideration and not yet accepted; treat this ADR as a
living snapshot until it is.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.
