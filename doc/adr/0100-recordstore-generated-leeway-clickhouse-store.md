---
type: adr
status: proposed
date: 2026-07-01
---

> **Status: proposed — pre-human-review.** Design dialogue in progress: this
> records the agreed direction and the reasoning; API surfaces below are
> sketches to iterate on, not commitments. Do not cite as settled.

# ADR-0100: recordstore — a generated store over leeway records in ClickHouse

## Context

Every piece needed to ingest, create, retrieve and persist leeway records
exists today as a proven pairwise seam — but composing them for a new fact
table means hand-wiring roughly ten of them, and two of the pieces have never
been connected at all.

What exists:

- **Physical schema.** `common.TableDesc` built via `TableManipulator`
  ([`public/semistructured/leeway/common`](../../public/semistructured/leeway/common))
  is the single source of truth; `ddl/clickhouse` derives the DDL, the `dml`
  generator derives a typed `InEntity<Table>` fluent builder over an Arrow
  `RecordBuilder` (`BeginEntity … CommitEntity`, `TransferRecords →
  []arrow.RecordBatch`), and `readaccess` derives the section readers.
- **Logical mapping.** A flat `lw:`-tagged Go DTO yields a `mappingplan.Plan`
  through either front-end (`marshallgen.ParsePlan`, go/ast, or
  `marshallreflect.PlanFor[T]`, reflect; ADR-0071/ADR-0074).
  `readback.ValidatePlanAgainstIR` checks a plan against a table's IR without
  emitting SQL.
- **Typed codecs.** `marshallgen` emits per-kind SoA `<Kind>Columns` plus
  `BuildEntities` (drives any generated DML via the `dml/runtime` interfaces)
  and `FillFromArrow` (ADR-0042); `marshallreflect` is the runtime twin with
  `RowComposer` for many-DTOs-per-entity assembly (ADR-0070).
- **ECS composition.** The anchor `ecsdemo` shows entity–component
  composition on one fact table: components are flat DTOs sharing sections;
  `FatRow.Extract[T]` splits a wide row back into components; archetype =
  the set of populated sections (ADR-0075).
- **Read-back SQL.** `marshall/clickhouse/readback` generates
  Presence/Projection/Validator/Filter artefacts per kind from Plan ⋈ IR
  (ADR-0066).
- **Persistence.** ADR-0089 fixed the pivot: the SoA is the unification
  point; ClickHouse ingest is Arrow IPC (`INSERT … FORMAT Arrow`), as
  practised by keelson's `chstore` (`commitAndShip` → `chclient.InsertArrow`).
- **Batched KV.** [`public/caching`](../../public/caching) provides
  `ReadThroughCache[K, V, W]`: a single-threaded read-through cache with an
  `ItemFetcherI` seam (`FetchItemSinglePartition(ctx, partition, keys,
  target)`), work-item suspend/replay, L1/L2 tiers and a circuit breaker. It
  has no ADR and has never been wired to leeway or ClickHouse.

What is missing is the composition: a bind-once object that validates N
component plans against one `TableDesc`, owns the DML batch and flush policy,
plugs a batched `WHERE key IN (…)` fetcher into the cache, and a generator
that emits all of it as typed code.

The design dialogue fixed four course-setting answers:

1. **Consumers.** This is a general-purpose library, but its primitives must
   suffice to build (a) a CQRS event-sourcing event store and (b) a
   [`github.com/stergiotis/boxer/public/algebraicarch/pushout`](../../public/algebraicarch/pushout)
   `repo.StorageI` backend (ADR-0079). Direct use for simple cases is
   expected but secondary.
2. **Semantics.** Consequently the storage primitive is **append-only**;
   "latest state" and "ordered replay" are query verbs over appended rows,
   not storage semantics. There are no update or delete *primitives*;
   update/delete ergonomics are provided as a generated **state view**
   over append (SD4), the way `chstore` layers
   `WriteState`/`DeleteState`/`LatestState` over immutable facts.
3. **Placement.** Outside leeway — leeway remains a data-representation
   pipeline and must not grow caching or ClickHouse-client concerns.
4. **Delivery.** Generator-first: the typed generated store is the primary
   artefact, not a reflection-bound runtime facade.

## Design space (QOC)

**Question.** How should the existing leeway, caching and ClickHouse seams be
composed into one high-level API for ingesting, creating, retrieving
(batched KV) and persisting user-supplied leeway records — such that a CQRS
event store and a pushout `StorageI` backend can be built on the same
primitives?

**Options.**

- **O1 — Status quo.** Each consumer hand-assembles the seams, as `ecsdemo`
  and `chstore` do. Kill: the wiring is ~10 seams and two of them
  (cache ⋈ read-back) have no precedent to copy; every consumer would
  re-derive fetch SQL, decode plumbing, flush policy and error handling, and
  `chstore` shows the per-kind hand-rolled cost.
- **O2 — Runtime-bound facade, reflection-first.** Bind at run time via
  `PlanFor[T]` + `marshallreflect`; add a generator later. Kill: rejected in
  the design dialogue — the typed generated store is the desired artefact;
  per-entity reflection is the wrong hot path for a store meant to back an
  event log (ADR-0042 measured the reflection tax when it rejected
  reflection per row). `marshallreflect` remains available for ad-hoc use.
- **O3 — Latest-state KV store as the primitive.** Model the store as a
  mutable map (update = overwrite, delete = tombstone), the shape
  `chstore.LatestState` implements for one kind. Kill: both target consumers
  are append-shaped (an event log; an immutable content-addressed envelope
  store plus an append log). Latest-state is expressible as a query
  (`ORDER BY <order> DESC LIMIT 1 BY <key>`) and belongs to the read side;
  baking overwrite semantics into the primitive would put the CQRS write
  model at war with its own substrate.
- **O4 — A new package outside leeway: append-only generated store**
  (chosen). A runtime support package plus a generator that composes the
  existing generators and emits the store; consumers adapt it to their
  seams.

## Decision

Adopt **O4**. Specific decisions:

- **SD1 — Package layout.** New top-level package `public/recordstore`:
  - `recordstore` (root) — the runtime the generated code imports: the
    executor seam, envelope-role types, generic cache/fetcher scaffolding,
    flush policy, shared errors. Kept small, mirroring `dml` vs
    `dml/runtime`.
  - `recordstore/gen` — the generator library. Driven the repo-idiomatic way
    (a `gen_test.go` in the target package, like `ecsdemo/stage2`); a CLI
    wrapper may follow.
  - `recordstore/chexec` — the default `ExecutorI` adapter over
    `chclient` (HTTP server) and a `clickhouse-local` adapter for tests.
    Isolated in a subpackage so the root stays dependency-light.
- **SD2 — Envelope roles.** A store binds one `TableDesc`. Its plain columns
  form the **envelope**; generator config names up to three roles among
  them: `Key` (required; the KV access key; Go type must be comparable),
  `Order` (optional; numeric/time; enables `Latest` and `Replay`) and
  `Lifecycle` (optional; carries the live/tombstone marker; together with
  Key and Order it enables the state-view verbs). Roles default from
  `PlainItemTypeE` where unambiguous (`EntityId` → Key,
  `EntityTimestamp` → Order, `EntityLifecycle` → Lifecycle) and are
  explicit otherwise. Remaining plain columns pass through as ordinary
  envelope fields.
- **SD3 — Append-only primitive.** Rows are immutable once committed.
  `Latest` and `Replay` are query verbs; duplicates are legal (idempotent
  re-puts of identical bytes are harmless by construction). Overwrite and
  tombstone *semantics* exist only as views over appended rows — the
  generated state view (SD4) is one such view — and retention belongs to
  layers above.
- **SD4 — Verb set.** Sketch (generated names per store; `Drone` as the
  running example):

  ```go
  st := NewDroneStore(exec, alloc, cfg)      // cfg: cache capacity, FetchCriteria, flush thresholds, DDL tail
  st.EnsureTable(ctx)                        // DDL via ddl/clickhouse + cfg.DDLTail

  b := st.Begin(id, ts)                      // envelope roles → typed args
  b.AddIdentity(Identity{...})               // component appenders, same entity frame
  b.AddBattery(Battery{...})
  b.Raw()                                    // *InEntityDroneTable: direct attribute manipulation
  b.Commit()                                 // CommitEntity; buffered

  st.IngestIdentity(rows []Identity)         // whole-entity batches per kind (BuildEntities)
  st.IngestArrow(recs)                       // schema-checked passthrough
  st.Flush(ctx)                              // TransferRecords → Arrow IPC → InsertArrow;
                                             // durable when it returns (synchronous insert)

  has, ent := st.Get(k)                      // batched KV via caching.ReadThroughCache
  for range st.WorkItem(w) { ... }           // caching's suspend/replay contract, re-exposed
  ent, ok := st.Latest(ctx, k)               // ORDER BY order DESC LIMIT 1 BY key; uncached v1
  for ent := range st.Replay(ctx, k, from)   // ordered rows for key, order ≥ from
  rows := st.Scan(ctx, filter)               // ADR-0066 artefacts: Presence/Filter + decode

  // State view — emitted only when Key, Order AND Lifecycle roles are bound:
  b := st.Put(id, ts)                        // append a new version (Begin, lifecycle=live)
  b.AddIdentity(...); b.Commit()
  st.Delete(id, ts)                          // append a tombstone row (Lifecycle column)
  ent, ok := st.GetLatest(ctx, k)            // Latest + tombstone interpretation:
                                             // newest row wins; tombstone ⇒ absent
  ```

  `Get` (the cached path) is intended for **immutable-by-key** access —
  content-addressed or unique-keyed records — where cache entries can never
  go stale. `Latest` and `GetLatest` stay uncached in v1 (invalidation is a
  real problem; see Deferred).

  The **state view** (`Put`/`Delete`/`GetLatest`) supplies the
  update/delete ergonomics of a data-management hub without breaking the
  append-only substrate: `Put` appends a new version, `Delete` appends a
  tombstone, `GetLatest` reads newest-row-wins with tombstones read as
  absent — `chstore`'s `WriteState`/`DeleteState`/`LatestState`
  generalized and generated. The verb ladder is: append-only primitives →
  state view → consumer layers (CQRS, pushout), all on one substrate.
- **SD5 — Cache wiring.** The generator emits an
  `caching.ItemFetcherI[K, *DroneEntity]` implementation: one
  `SELECT <needed physical columns> FROM t WHERE <key> IN (…) FORMAT Arrow`
  per partition via the executor, decoded client-side through the generated
  read-access classes into the **entity bag**:

  ```go
  type DroneEntity struct {
      ID       uint64                    // envelope
      Ts       time.Time
      Identity option.Option[Identity]   // one option per bound component
      Battery  option.Option[Battery]
      ...
  }
  func (e *DroneEntity) Archetype() []string
  ```

  Cached values are **Arrow-free** plain Go — decode happens eagerly at
  fetch time, because `RecordBatch.Release` lifecycles cannot be tied to
  cache eviction (the cache deliberately has no eviction callbacks).
  `DeterminePartition` is a config hook; v1 default is a single partition
  (one table, one server), with key-hash chunking available for oversized
  batches. Read-back for point lookups deliberately does **not** use the
  ADR-0066 SQL artefacts: raw-column fetch + client-side decode is the
  proven `ecsdemo` path and avoids SQL-side tuple/NULL gymnastics; the
  artefacts serve `Scan`.

  Three facts S1 established bind this decode path: physical **plain**
  column names are leeway-encoded like every other column (e.g.
  `"id:id:u64:2k:0:0:"`) — every SQL fragment quotes names derived from
  the IR at generation time, never bare role names; fetched Arrow must be
  pinned to the shape the read-access classes expect
  (`SETTINGS output_format_arrow_string_as_string=1,
  output_format_arrow_low_cardinality_as_dictionary=0`); and the
  kind-homogeneous decode helpers (`<Kind>FillFromArrow`,
  `marshallreflect.Unmarshal`) **cannot decode fat rows** — they enforce
  exactly-one-occurrence for scalar/unit fields, which rows lacking that
  component legitimately violate. The store therefore decodes with
  presence-gated, membership-matched per-row reads over the RA accessors.
- **SD6 — Generator orchestrates, then glues.** One generator invocation
  takes `{TableDesc, TableRowConfigE, []component DTO sources, roles,
  store name}` and emits a complete package: the DML class, DDL SQL and RA
  classes (by driving the existing `dml`, `ddl/clickhouse` and `readaccess`
  generators), per-component SoA codecs (by driving `marshallgen`), and the
  new glue — the store type, entity bag, fetcher, decoders, and baked SQL
  constants. The one new per-component emission,
  `<Kind>AddSections(dml, row)` — the body of `BuildEntities` minus
  `BeginEntity`/`Set*`/`CommitEntity`, so typed components compose under
  one entity frame the way `RowComposer` does reflectively (ADR-0070) —
  was upstreamed into `marshallgen` itself during S1 (a value-source
  refactor lets the same section drivers render SoA-at-row-i and
  single-row access), so every marshallgen kind now carries it. The
  presence-gated read twin (`<Kind>ReadRow`) is not upstreamed yet; the
  store generator emits specialized decode for the v1 shapes and rejects
  the rest at generation time (see Deferred).
- **SD7 — Executor seam.** `ExecutorI { Exec(ctx, sql) error;
  QueryArrow(ctx, sql) ([]arrow.RecordBatch, error); InsertArrow(ctx, table,
  recs) error }` (exact shape to be settled during implementation; a
  streaming query variant may be needed for `Replay`). `chexec` adapts
  `chclient` and `clickhouse-local`; the bus-hosted `chlocalbroker` can be
  adapted later if a consumer needs it.
- **SD8 — Ownership.** A store instance is single-goroutine, like every
  part it composes (the cache, the DML builders, ADR-0010's codec rule).
  Concurrent-read requirements of adapters (e.g. pushout's "safe for
  concurrent READS") are the adapter's problem, solved with its own lock or
  per-goroutine instances.
- **SD9 — Consumer adapters are validation slices, not v1 scope.** The
  pushout adapter must pass the existing `repo/storagetest` conformance
  suite; that suite is the acceptance gate proving the primitive set
  suffices. The CQRS layer gets a minimal worked example, not a framework.

## Consumer mappings

### pushout `repo.StorageI` (ADR-0079)

One store, one fact table; envelope `Key` is a string (or fixed-byte hash)
column; payload components: a framed-blob component for envelopes, a
hash-list component for log entries, blob components for snapshot/retention.

| `StorageI` method | store verbs | notes |
| --- | --- | --- |
| `PutEnvelope(h, framed)` | `Begin(key=h).AddEnvelope(blob).Commit()` + `Flush` | idempotent: equal bytes for equal hash, duplicate rows harmless; durable-on-return = synchronous `Flush` per op |
| `GetEnvelope(h)` / `HasEnvelope(h)` | `Get` (cached) | immutable-by-key — the ideal cache case; batched closure walks amortize |
| `AppendApplied(h)` | `Begin(key="applied/"+gen, order=seq).AddLogEntry(h).Commit()` + `Flush` | torn tail = row never inserted = "never acknowledged", matching the contract |
| `LoadApplied()` | `Replay(key="applied/"+gen, 0)` | gen = current log generation |
| `ReplaceApplied(hs)` | append full list under gen+1, then a generation-marker row; readers resolve gen via `Latest` | atomic-enough: readers see old gen until the marker row lands (single-block insert) |
| `SaveSnapshot` / `LoadSnapshot` | append + `Latest` | prefix-gating stays in the engine |
| `SaveRetention` / `LoadRetention` | same generation pattern as `ReplaceApplied` | ledger is replica-local; one store per replica |

Durability caveat: this mapping is honest only over a durable engine
(MergeTree with synchronous inserts), not `ENGINE = Memory`, and the adapter
must run with async inserts off.

### CQRS event sourcing

| ES concept | store concept |
| --- | --- |
| aggregate id | envelope `Key` |
| event sequence | envelope `Order`, caller-assigned (single-writer per aggregate in v1) |
| event type | archetype (set of populated components) |
| event payload | components |
| append events | `Begin(id, seq).Add*(…).Commit()` |
| rehydrate | `Replay(id, fromSeq)` + fold |
| snapshot | a snapshot component + `Latest` |
| optimistic concurrency | **not provided** — needs CAS the substrate lacks; deferred (see below) |
| projections / read models | out of scope; separate leeway tables fed by `Replay`/`Scan` |

## Consequences

### Positive

- One bind point replaces ~10 hand-wired seams per fact table; the two
  never-connected pieces (cache ⋈ leeway read-back ⋈ typed decode) get a
  designed home instead of ad-hoc first contact.
- The hot paths are generated and typed end to end (SoA → DML → Arrow;
  Arrow → RA → entity bag), consistent with ADR-0042's rejection of
  per-row reflection.
- The pushout `repo/storagetest` conformance suite acts as an external
  acceptance test for the primitive set — the verbs are validated against a
  consumer this ADR did not invent.
- Append-only primitives keep the substrate honest for both target
  consumers; nothing in the store fights an event log or an immutable
  envelope store.

### Negative

- Generator-first freezes surface area earlier than a runtime-first cut
  would; mitigated by slice ordering (S1–S3 feed back into this ADR while
  it is still pre-acceptance, edited in place).
- A new generator that *orchestrates four existing generators* concentrates
  drift risk: a change in `dml`, `ddl`, `readaccess` or `marshallgen`
  output now has a downstream consumer that must move with it.
- Point-lookup performance depends on the user supplying a sensible
  `DDLTail` (key leading ORDER BY) until the table-clause seam exists.
- Single-goroutine ownership pushes concurrency handling to adapters
  (pushout's concurrent-read contract needs an adapter-level lock).

### Neutral

- leeway itself is untouched — the store is purely a consumer (ADR-0074
  discipline).
- ADR-0010 remains deferred; its RPC-wire niche is neither served nor
  blocked by this package.
- Direct users get verbs shaped by two demanding consumers; for simple
  cases the surface may feel larger than needed.

## Alternatives

The QOC options above carry the rankings; notes below record nuance.

- **O2 — runtime-first.** Not a dead end — `marshallreflect` exists and the
  gen ≡ reflect wire-compat rule would have made it a safe staging step —
  but the generated store is the artefact this effort exists to produce,
  and staging through a reflection facade would design the API around
  reflection's limits (per-entity decode cost, `any`-typed DML) rather than
  around the generated surfaces.
- **O3 — latest-state primitive.** Also rejected for asymmetry of cost:
  latest-on-append is one query idiom (`LIMIT 1 BY`), whereas append-on-
  mutable requires versioning machinery the substrate (ClickHouse parts,
  immutable Arrow batches) does not offer. The cheap direction wins.
- **Building on `chstore` instead of beside it.** keelson's `chstore` is
  the closest production relative (Arrow ship path, `LatestState` SQL,
  blake3 natural keys) but is hand-rolled per fact kind against the facts
  schema specifically; generalizing it in place would entangle keelson's
  runtime with a general-purpose library. Its idioms are templates here,
  not a base.
- **Placement inside leeway** (`leeway/store`, or `marshall/clickhouse/`
  beside `readback`): rejected to keep leeway free of caching and
  CH-client concerns; the store is a consumer of marshall targets, not a
  marshall target.

## Relationship to prior ADRs

- **ADR-0089** governs the persist path: the SoA is the pivot, ingest is
  Arrow IPC. This ADR adds no wire.
- **ADR-0074** governs placement of the pieces this store consumes; the
  store itself lives outside leeway as a consumer and buries nothing.
- **ADR-0070 / ADR-0075** define entity assembly and typed components; SD6's
  `<Kind>AddSections` is their generated-composition completion. The
  flat-DTO limit (no nested component structs) is inherited.
- **ADR-0066** artefacts back the `Scan` verb; point lookups bypass them by
  design (SD5).
- **ADR-0042** set the SoA-primary, batch-shaped codec model this store
  builds on.
- **ADR-0010** (deferred generic CBOR codec) anticipated a generic
  DTO→target-table binding; its territory has since been substantially built
  as `marshallreflect`, and this store serves the "generic high-level
  ingest" need through composition instead. ADR-0010 stays deferred for its
  actual niche — a single-entity RPC wire codec — and is untouched by this
  decision.

## Deferred

- **Table-level DDL clauses** (ENGINE, ORDER BY, PARTITION BY, TTL, skip
  indexes). Still the known leeway gap; v1 takes an opaque `DDLTail` string
  in config. Point-lookup performance on MergeTree wants the key column
  leading ORDER BY — the tail is where the user says so for now. A proper
  seam (which would also close the readback skip-index gap) is its own
  future decision.
- **Caching `Latest`/`GetLatest`.** Requires write-through invalidation
  (`MarkAsStale`/`Delete` on local appends to the same key) and a staleness
  story for external writers. Deferred until a consumer needs it.
- **Optimistic concurrency / CAS** for the event-store expected-sequence
  check. ClickHouse inserts cannot express it; a serializing layer (e.g. a
  single-writer bus owner per aggregate) is the likely answer, above this
  package.
- **Exactly-once / dedup.** ClickHouse block-level insert deduplication may
  give cheap idempotence for retried flushes; not relied on in v1.
- **Streaming `Replay`** (chunked Arrow decode) — v1 buffers whole
  results; the executor seam is where streaming lands.
- **Generator shape coverage.** v1 store decode supports scalar,
  scalar-single (`unit`) and slice-container fields on the LowCardRef
  channel; multi-sub-column sections, carriers, options, roaring and
  explode shapes are a generation-time error. Upstreaming a presence-gated
  `<Kind>ReadRow` into `marshallgen` (beside `AddSections`) is the S2 move
  that closes this without duplicating shape logic.
- **Projections / read models**, multi-table stores, nested component
  structs (blocked on the deferred marshallreflect nested-struct feature),
  and a CLI wrapper for the generator.

## Slices

- **S1** — `recordstore` runtime + `recordstore/gen` emitting a store for an
  `ecsdemo`-shaped schema; unit-level round-trip (build → flush →
  clickhouse-local → Get/Latest/Replay, plus the state view
  Put/Delete/GetLatest) green. **Done** (see `public/recordstore`): the
  example package's store is fully generated by one `gen.Input.Generate`
  call, and the round-trip test that pinned the hand-written reference
  passes unchanged against the generated store. v1 shape limits recorded
  under Deferred.
- **S2** — cache integration exercised end-to-end (miss → batch fetch →
  replay work items), plus `Scan` via readback artefacts.
- **S3** — pushout `StorageI` adapter passing `repo/storagetest` against
  clickhouse-local. Feedback from this slice may amend SD2–SD4 in place
  (this ADR is pre-acceptance).
- **S4** — minimal CQRS worked example (commands → events → replay-fold →
  state), documentation-grade.

## Status

Proposed — 2026-07-01. The design dialogue is ongoing; this document is a
living snapshot edited in place until acceptance. Promote to `accepted`
after the open sketches (SD4 verb signatures, SD7 executor shape) settle
and slice S1 demonstrates the generator end to end.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
ADRs are append-only; supersession is recorded, not deleted.

## References

- [ADR-0042: Keelson leeway codec SoA generator](0042-keelson-leeway-codec-soa-generator.md)
  — the SoA-primary codec model and the reflection kill-reasons.
- [ADR-0066: leeway DQL ClickHouse read-back generator](0066-leeway-dql-clickhouse-readback-generator.md)
  — the Presence/Projection/Validator/Filter artefacts behind `Scan`.
- [ADR-0070: leeway entity assembly](0070-leeway-entity-assembly.md) —
  many-DTOs-per-entity composition; SD6 generates its reflective
  `RowComposer` pattern.
- [ADR-0074: leeway marshall package layout](0074-leeway-marshall-package-layout.md)
  — the tiering this store consumes without disturbing.
- [ADR-0075: leeway typed component views](0075-leeway-typed-component-views.md)
  — components, archetypes and the flat-DTO limit.
- [ADR-0079: pushout production storage, codec, exchange](0079-pushout-production-storage-codec-exchange.md)
  — `repo.StorageI` and the conformance suite gating slice S3.
- [ADR-0089: row-DML serialization vs ClickHouse-native ingestion](0089-rowdml-serialization-clickhouse-native-ingestion.md)
  — the SoA pivot and Arrow-IPC ingest this store persists through.
- [ADR-0010: leeway CBOR RPC codec](0010-leeway-cbor-rpc-codec.md) —
  deferred; adjacent territory, untouched.
- [`public/caching`](../../public/caching) — the read-through cache;
  [`public/algebraicarch/pushout/repo/storage.go`](../../public/algebraicarch/pushout/repo/storage.go)
  — the storage seam;
  [`public/semistructured/leeway/anchor/ecsdemo`](../../public/semistructured/leeway/anchor/ecsdemo)
  — the composition pattern this store generalizes.
