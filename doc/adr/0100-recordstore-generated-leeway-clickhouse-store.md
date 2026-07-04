---
type: adr
status: accepted
date: 2026-07-01
reviewed-by: "@spx"
reviewed-date: 2026-07-04
---

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

- **SD1 — Package layout.** New package `public/storage/recordstore` —
  under the `storage/` grouping, beside `storage/blob`: a record store and
  a blob store are one family, and neither the representation-format
  grouping (`semistructured/`: cbor, leeway, markdown) nor the per-engine
  tooling grouping (`db/`) names what this is. Subpackages:
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
  them: `Key` (required; the KV access key; Go type must be comparable —
  the generator derives it from the column's canonical type and supports
  `uint64` and `string`, emitting a per-store SQL-literal renderer),
  `Order` (optional; numeric/time; enables `Latest` and `Replay`) and
  `Lifecycle` (optional; carries the live/tombstone marker; together with
  Key and Order it enables the state-view verbs). Roles bind from
  `PlainItemTypeE` (`EntityId` → Key, `EntityTimestamp` → Order,
  `EntityLifecycle` → Lifecycle); a schema in which two plain columns
  claim one role is a generation error, not a silent last-wins (explicit
  role configuration is deferred). The generator also gates the role
  column shapes it hard-assumes: Key must derive to `uint64` or `string`,
  Order to the z64 timestamp lane (`DateTime64(9)` — `Replay` compares
  in nanoseconds), Lifecycle to `uint8`. Remaining plain columns pass
  through as ordinary envelope fields. Callers must keep Order values
  **strictly monotonic per key**: `Latest`, `GetLatest` and the cached
  fetch pick one row among equal-Order ties arbitrarily (`Scan` alone is
  tie-deterministic — it orders by (Order, Key)).
- **SD3 — Append-only primitive.** Rows are immutable once committed.
  `Latest` and `Replay` are query verbs; duplicates are legal (idempotent
  re-puts of identical bytes are harmless by construction). Overwrite and
  tombstone *semantics* exist only as views over appended rows — the
  generated state view (SD4) is one such view — and retention belongs to
  layers above.
- **SD4 — Verb set.** Sketch (generated names per store; `Drone` as the
  running example):

  ```go
  st := NewDroneStore(exec, alloc, cfg)      // cfg: cache capacity, FetchCriteria, DDL tail
  st.EnsureTable(ctx)                        // DDL via ddl/clickhouse + cfg.DDLTail

  b := st.Begin(id, ts)                      // envelope roles → typed args
  b.AddIdentity(Identity{...})               // component appenders, same entity frame
  b.AddBattery(Battery{...})
  b.Raw()                                    // *InEntityDroneTable: direct attribute manipulation
  b.Commit()                                 // CommitEntity; buffered (rolls back on error)
  b.Rollback()                               // abandon the open frame instead

  st.IngestIdentity(ts, rows []Identity)     // whole-entity batches per kind
  st.Flush(ctx)                              // TransferRecords → Arrow IPC → InsertArrow;
                                             // durable when it returns (synchronous insert);
                                             // retryable — a failed insert retains the records
  st.DiscardPending()                        // …or drop everything unflushed ("never happened")

  has, ent := st.Get(k)                      // batched KV via caching.ReadThroughCache
  for range st.WorkItem(w) { ... }           // caching's suspend/replay contract, re-exposed
  st.AdvanceEpoch()                          // cache pinning epoch, once per frame/batch
  ent, ok := st.Latest(ctx, k)               // ORDER BY order DESC LIMIT 1 BY key; uncached v1
  for ent := range st.Replay(ctx, k, from)   // ordered rows for key, order ≥ from
  rows := st.ScanIdentity(ctx, extra)        // per-kind: baked ADR-0066 Filter artefact
                                             // (presence AND validator, ids as literals)
                                             // + optional raw extra predicate

  // State view — emitted only when Key, Order AND Lifecycle roles are bound:
  b := st.Put(id, ts)                        // append a new version (Begin, lifecycle=live)
  b.AddIdentity(...); b.Commit()
  st.Delete(id, ts)                          // append a tombstone row (Lifecycle column)
  ent, ok := st.GetLatest(ctx, k)            // Latest + tombstone interpretation:
                                             // newest row wins; tombstone ⇒ absent
  ```

  `Get` (the cached path) is intended for **immutable-by-key** access —
  content-addressed or unique-keyed records — where cache entries can never
  go stale. Local writes are coherent regardless: `Commit`, `Put` and
  `Delete` invalidate the written key's cache entry, and the key stays
  uncacheable (fetched but not retained) until the write flushes, so a
  fetch inside the dirty window cannot resurrect the pre-write row. Only
  **external** writers can leave `Get` stale. `Latest` and `GetLatest`
  stay uncached in v1 (see Deferred). `Flush` is retryable — a failed
  insert retains the transferred records for the next attempt — and
  `DiscardPending` drops every buffered row instead, for callers whose
  per-operation contract is "a failed operation never happened" (the
  pushout adapter and the CQRS command path both use it).

  The **state view** (`Put`/`Delete`/`GetLatest`) supplies the
  update/delete ergonomics of a data-management hub without breaking the
  append-only substrate: `Put` appends a new version, `Delete` appends a
  tombstone, `GetLatest` reads newest-row-wins with tombstones read as
  absent — `chstore`'s `WriteState`/`DeleteState`/`LatestState`
  generalized and generated. The verb ladder is: append-only primitives →
  state view → consumer layers (CQRS, pushout), all on one substrate.
- **SD5 — Cache wiring.** The generator emits an
  `caching.ItemFetcherI[K, *DroneEntity]` implementation: one
  `SELECT * FROM t WHERE <key> IN (…) FORMAT Arrow` per partition via the
  executor, decoded client-side through the generated read-access classes
  into the **entity bag**. (`SELECT *`, not a projection: the RA readers
  bake schema-order column indices, so the fetch must return every column
  in schema order; a needed-columns projection with index rebinding is
  deferred.)

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
  single-row access), so every marshallgen kind now carries it. S2
  upstreamed the presence-gated read twin the same way:
  `<Kind>ReadRow(i, attrs, membs, …) (row, present, err)` — the
  FillFromArrow decode middles (accumulators, membership-match loops)
  shared, with presence-tolerant tails (a row carrying none of the kind's
  memberships is `present=false`, never an error; a duplicated **scalar**
  field errors, while duplicated container memberships concatenate). Two
  write/read asymmetries are inherited from marshallgen and worth knowing:
  an **empty container** field writes no membership (len-gated) and reads
  back as if absent — "present with empty list" is unrepresentable — and
  a row carrying only some of a kind's fields still decodes as `present`
  with the missing fields zero-valued (only the `Scan` filter enforces
  full conformance). The store generator's decode is one `ReadRow` call
  per component; kinds whose shapes ReadRow does not cover are skipped at
  emission and rejected by the store generator through the same exported
  gate (`marshallgen.ReadRowSupported`), so the two cannot disagree.
  Components must own **disjoint sections** — membership ids are assigned
  per kind, so two kinds writing one section would alias each other's
  memberships and silently cross-decode; the generator enforces the
  invariant at generation time.
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

Implemented as `recordstore/pushoutstore` (slice S3), passing
`repo/storagetest` against clickhouse-local. One store, one fact table;
the envelope `Key` is a string column carrying namespaces —
`"env/<hex hash>"`, `"log"`, `"snapshot"`, `"retention"` — and the Order
column carries a synthetic per-key sequence (Unix nanos 1, 2, 3, …), so
append order is total without wall clocks. As-implemented mapping (the
pre-S3 sketch differed in two places, corrected below):

| `StorageI` method | store verbs | notes |
| --- | --- | --- |
| `PutEnvelope(h, framed)` | existence check via `Latest`, then `Begin("env/"+hex).AddEnvelope(…).Commit()` + `Flush` | first-write-wins must hold even for **different** bytes under the same hash (the suite tests it), so read-before-insert — race-free because StorageI writes are engine-locked; "duplicate rows are harmless" was not enough |
| `GetEnvelope(h)` / `HasEnvelope(h)` | cached `Get` (miss → one forced batch fetch → hit), falling back to uncached `Latest` | the fallback gives the authoritative absent-vs-error answer (cache misses swallow fetch errors); immutable-by-key — the ideal cache case |
| `AppendApplied(h)` | `Begin("log", seqTs).AddLogEntry(hex).Commit()` + `Flush` | torn tail = row never inserted = "never acknowledged" |
| `LoadApplied()` | `Replay("log", 0)`, keeping entries after the **last tombstone** | no generation marker needed — the state-view tombstone is the log reset |
| `ReplaceApplied(hs)` | `Delete("log", seqTs)` (tombstone) + new entries, **one** `Flush` | a single Arrow insert: readers observe the old or the new log, never a mixture |
| `SaveSnapshot` / `LoadSnapshot` | append one row + `Latest` | prefix-gating stays in the engine |
| `SaveRetention` / `LoadRetention` | the whole ledger as one row of three aligned arrays (hash, index, nanos) + `Latest` | whole-set replace needs no generation pattern either |

Durability: honest only over a durable engine (MergeTree with synchronous
inserts), not `ENGINE = Memory`; every mutating method flushes before
returning. The adapter serializes all methods behind one mutex — the
store and cache are single-goroutine, and the contract's concurrent
reads become trivially safe.

### CQRS event sourcing

Implemented as `recordstore/cqrsexample` (slice S4), documentation-grade:
an event-sourced account ledger whose lifecycle test is the executable
walkthrough. The ledger schema deliberately binds no Lifecycle role —
closing an account is a domain event, not a storage tombstone — which
also exercises the generator's no-state-view emission path.

| ES concept | store concept |
| --- | --- |
| aggregate id | envelope `Key` (string, e.g. `"acct/7"`) |
| event sequence | envelope `Order`, caller-assigned synthetic nanos (single-writer per aggregate in v1) |
| event type | archetype (the one populated component names the event) |
| event payload | components |
| append events | `Begin(id, seqTs).Add*(…).Commit()` + `Flush` per command |
| rehydrate | newest snapshot via `Latest` on the sibling key `"acct/7/snap"`, then `Replay(id, afterSnapshot)` + fold — the short-circuit is observable (the example counts folded events) |
| snapshot | a state component under the derived sibling key + `Latest` (outside the event stream, so `Replay` never sees it) |
| optimistic concurrency | **not provided** — needs CAS the substrate lacks; deferred (see below) |
| projections / read models | out of scope as a framework; the example feeds a cross-aggregate projection straight from `Scan<Kind>` |

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
- **Negative caching.** Absent keys are never recorded, so every `Get` of
  a missing key re-queries — the pushout adapter's `HasEnvelope(absent)`
  pays a forced fetch plus the authoritative `Latest` on every probe.
  Needs a `public/caching` feature (absent-entries with a TTL), not a
  store-side hack.
- **Explicit role configuration** (SD2). Role binding is by
  `PlainItemTypeE` only; a schema needing to pick among several
  same-typed columns cannot express it. Ambiguity is a generation error
  today.
- **Projection fetch.** Point lookups are `SELECT *` (SD5); a
  needed-columns projection requires rebinding the RA readers' column
  indices at generation time.
- **`IngestArrow`** (schema-checked Arrow passthrough, sketched in early
  SD4 drafts). Needs a key-column decode for cache invalidation and a
  buffered-count story; no consumer asked yet.
- **Optimistic concurrency / CAS** for the event-store expected-sequence
  check. ClickHouse inserts cannot express it; a serializing layer (e.g. a
  single-writer bus owner per aggregate) is the likely answer, above this
  package.
- **Exactly-once / dedup.** ClickHouse block-level insert deduplication may
  give cheap idempotence for retried flushes; not relied on in v1.
- **Streaming `Replay`** (chunked Arrow decode) — v1 buffers whole
  results; the executor seam is where streaming lands.
- **Generator shape coverage.** With `<Kind>ReadRow` upstreamed (S2), the
  store decode covers scalar, scalar-single (`unit`), slice-container,
  Option-scalar, roaring and multi-sub-column shapes on the non-carrier
  channels. Carrier (mixed / parametrized) channels and exploded fields
  remain uncovered — `marshallgen.ReadRowSupported` is the single gate —
  pending a consumer (the keelson facts kinds would be the trigger).
- **Projections / read models**, multi-table stores, nested component
  structs (blocked on the deferred marshallreflect nested-struct feature),
  and a CLI wrapper for the generator.

## Slices

- **S1** — `recordstore` runtime + `recordstore/gen` emitting a store for an
  `ecsdemo`-shaped schema; unit-level round-trip (build → flush →
  clickhouse-local → Get/Latest/Replay, plus the state view
  Put/Delete/GetLatest) green. **Done** (see `public/storage/recordstore`): the
  example package's store is fully generated by one `gen.Input.Generate`
  call, and the round-trip test that pinned the hand-written reference
  passes unchanged against the generated store. v1 shape limits recorded
  under Deferred.
- **S2** — cache integration exercised end-to-end (miss → batch fetch →
  replay work items), plus `Scan` via readback artefacts. **Done**: the
  example's cache tests cover Min-threshold batching across work items
  (one `IN (…)` query serves several frames), the fetch-error circuit
  breaker, and local-write invalidation; per-kind `Scan<Kind>` verbs embed
  the generation-time Filter artefacts (a single SELECT — the Filter uses
  ClickHouse built-ins only, so the initially-prepended helper UDFs were
  dropped post-review and the executor contract stays "one statement");
  `<Kind>ReadRow` upstreamed lifted the shape limits to
  everything non-carrier/non-explode, proven by a multi-sub-column
  `Located` component and an Option scalar in the example.
- **S3** — pushout `StorageI` adapter passing `repo/storagetest` against
  clickhouse-local. **Done** (`recordstore/pushoutstore`): all five
  conformance checks pass, including reopen durability across executor
  processes. The slice fed back as anticipated: SD2 gained string keys
  (with the generator-emitted SQL-literal renderer), and the consumer
  mapping was corrected — first-write-wins needs read-before-insert, and
  the state-view tombstone replaces the sketched generation-marker
  pattern for both the applied log and the retention ledger.
- **S4** — minimal CQRS worked example (commands → events → replay-fold →
  state), documentation-grade. **Done** (`recordstore/cqrsexample`): the
  account-ledger lifecycle test covers guarded commands (overdraw and
  closed-account rejections), snapshot-accelerated rehydration with the
  replay short-circuit asserted, close-as-domain-event, the ordered
  archetype history, and a cross-aggregate `Scan` projection. The slice
  fed back once more: kind consts are now keyed on the membership name
  (schema-global) instead of the Go field name, so several kinds sharing
  field names (`Amount`, `Owner`) generate into one package without
  collisions — with the corollary, validated in the shared PlanBuilder,
  that ref-channel membership names must be Go identifiers.

## Status

Accepted — 2026-07-04 (reviewed by @spx). The decision in force:
`public/storage/recordstore` is the generated, append-only store
composing leeway, the read-through cache and ClickHouse — SD1–SD9 as
written, with all four slices delivered and the two consumer adapters
(pushout `StorageI` passing `repo/storagetest`, the CQRS ledger example)
as the acceptance evidence. Deferred items (table-clause seam, cached
`Latest`, CAS, carrier/explode ReadRow coverage, streaming `Replay`)
remain open with their triggers recorded under Deferred.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
From acceptance on, this document changes only via dated `## Update`
sections; see `doc/DOCUMENTATION_STANDARD.md` for the edit-policy tiers.

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
