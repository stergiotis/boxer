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
  - `recordstore` (root) — the small runtime the generated code and
    adapters share: the executor seam (`ExecutorI`), the lifecycle marker
    constants, the `ReplayOpts`/`ScanOpts` option types, the
    synthetic-sequence `SeqTs`/`SeqOf` helpers and shared errors. The
    per-store cache, fetcher and flush machinery is generated, not here —
    keeping the root small, mirroring `dml` vs `dml/runtime`.
  - `recordstore/gen` — the generator library. Driven the repo-idiomatic way
    (a `gen_test.go` in the target package, like `ecsdemo/stage2`); a CLI
    wrapper may follow.
  - `recordstore/chexec` — the default `ExecutorI` adapter over
    `chclient` (HTTP server) and a `clickhouse-local` adapter for tests.
    Isolated in a subpackage so the root stays dependency-light.
- **SD2 — Envelope roles and pass-through fields.** A store binds one
  `TableDesc`; its plain columns form the **envelope**. The generator
  enumerates them in canonical order — the order the DML setters take their
  arguments and the read-access readers expose their fields, both from one
  IR — and binds up to three **roles** by `PlainItemTypeE`:
  - `Key` (required; the KV access key) — the **leading** `EntityId`
    column. Its Go type is derived from the canonical type; `uint64` and
    `string` are supported, with a per-store SQL-literal renderer emitted
    (the unexported `<table>KeyLiteral`).
  - `Order` (optional; enables `Latest`, `Replay` and the state view) —
    the `EntityTimestamp` column. It must derive to the z64 timestamp lane
    (`DateTime64(9)`; `Replay` compares in nanoseconds).
  - `Lifecycle` (optional; enables the state view together with Key and
    Order) — the first `u8` `EntityLifecycle` column, carrying the
    live/tombstone marker (`recordstore.LifecycleLive` /
    `LifecycleTombstone`).

  Every **other** plain column — a second or further `EntityId`, any
  `EntityRouting`, an extra or non-`u8` `EntityLifecycle` — becomes a
  **pass-through field**. The generator emits a `<Store>Envelope` struct
  with one field per pass-through column in canonical order, embedded in
  `<Store>Entity` (so the columns read back as promoted fields) and taken
  as a third `Begin(id, ts, env)` argument; writes flow through the grouped
  DML setters (`SetId`/`SetRouting`/`SetLifecycle`, one call per
  `PlainItemType` group) and reads through the readers' typed accessors (a
  set column surfaces as `[]element`, the accessor shape, not the codec's
  bitmap carrier). A schema with **no** pass-through column emits none of
  this and is byte-identical to the pre-feature output — the feature is
  inert until a schema needs it. `Transaction`/`Opaque` plain columns are
  **rejected** at generation (they carry streaming-group / transaction
  semantics the store glue does not model); their pass-through is deferred.

  The generator also gates the role column shapes it hard-assumes: Key must
  derive to `uint64` or `string`, Order to the z64 timestamp lane, Lifecycle
  to `uint8`. Role binding is by `PlainItemTypeE` and column order alone — a
  schema needing to pick among several same-typed columns for a role cannot
  express which is the role, and explicit role configuration is deferred.
  Callers must keep Order values **strictly monotonic per key**: `Latest`,
  `GetLive` and the cached fetch pick one row among equal-Order ties
  arbitrarily (`Scan` alone is tie-deterministic — it orders by (Order,
  Key)).

  For cross-package handles and external SQL, each store exports its table
  name and role column names — `<Store>TableName` (the qualified
  `<db>.<table>` when a Database is set; SD6) and
  `<Store>ColKey`/`ColOrder`/`ColLifecycle`, the quoted leeway-encoded
  physical names — so `ScanOpts.ExtraPredicate` and hand-written SQL can
  address the columns without re-deriving their encodings.
- **SD3 — Append-only primitive.** Rows are immutable once committed.
  `Latest` and `Replay` are query verbs; duplicates are legal (idempotent
  re-puts of identical bytes are harmless by construction). Overwrite and
  tombstone *semantics* exist only as views over appended rows — the
  generated state view (SD4) is one such view — and retention belongs to
  layers above.
- **SD4 — Verb set.** The store carries append/flush, the query verbs and
  the state view; the batched-KV cache is a separately constructed
  **attached view** (SD5). Sketch (generated names per store; `Drone` as
  the running example):

  ```go
  st := NewDroneStore(exec, alloc, cfg)      // cfg: DDLTail (raw suffix); no cache fields
  st.EnsureTable(ctx)                        // composed CREATE TABLE (+ CREATE DATABASE if qualified) + DDLTail
  st.VerifySchema(ctx)                       // DESCRIBE vs the generated Arrow schema; drift is an error

  b := st.Begin(id, ts)                      // (id, ts, env) when the schema has pass-through fields (SD2)
  b.AddIdentity(Identity{...})               // component appenders, one entity frame
  b.AddBattery(Battery{...})
  b.Raw()                                    // *lowlevel.InEntityDroneTable: attribute escape hatch; frame control walled off (SD6)
  b.Commit()                                 // CommitEntity; buffered (rolls back the frame on error)
  b.Rollback()                               // abandon the open frame instead

  st.IngestIdentity(ts, rows []Identity)     // whole-entity batches per kind; dup key in one call → ErrDuplicateIngestKey
  st.Buffered()                              // rows staged since the last flush
  st.Flush(ctx)                              // TransferRecords → Arrow IPC → InsertArrow;
                                             // durable when it returns (synchronous insert);
                                             // retryable — a failed insert retains the records
  st.DiscardPending()                        // …or drop everything unflushed ("never happened")
  st.Close()                                 // DiscardPending + release the builders (tracking allocators)

  ent, found, err := st.Latest(ctx, k)       // newest row for key (tombstone-blind); uncached
  for ent, err := range st.Replay(ctx, k, from, recordstore.ReplayOpts{To, Limit}) { … }
                                             // ordered rows, from ≤ Order < To; single-use iterator
  for ent, err := range st.ScanIdentity(ctx, recordstore.ScanOpts{ExtraPredicate, Limit}) { … }
                                             // per-kind: baked ADR-0066 Filter artefact + optional extra predicate

  // State view — emitted only when Key, Order AND a u8 Lifecycle role are bound (SD2):
  st.Delete(id, ts)                          // append a tombstone row (Lifecycle column)
  ent, found, err := st.GetLive(ctx, k)      // Latest + tombstone interpretation (tombstone ⇒ absent); uncached
  ```

  The multi-row verbs (`Replay`, `Scan<Kind>`) return
  `iter.Seq2[*Entity, error]` — the repo's fallible-iteration idiom: the
  sequence is single-use, `ctx` must stay valid until iteration completes,
  the query may run at call time or lazily during iteration, and an error
  ends the sequence as a final `(nil, err)` pair. `Latest` and `GetLive`
  stay value-shaped (`(ent, found, err)`). v1 buffers each result set
  internally, so streaming `Replay` becomes an executor-seam change (SD7)
  with no signature impact.

  There is **no `Put`**: appending a new version through `Begin` *is* the
  update. The **state view** (`Delete`/`GetLive`) supplies the
  update/delete ergonomics of a data-management hub without breaking the
  append-only substrate — `Delete` appends a tombstone, `GetLive` reads
  newest-row-wins with tombstones read as absent (`chstore`'s
  `WriteState`/`DeleteState`/`LatestState` generalized and generated).
  `Live` names the interpreted read; the unmarked `Latest` is the raw
  newest-row read. The verb ladder is: append-only primitives → state view
  → consumer layers (CQRS, pushout), all on one substrate.

  `Flush` is retryable — a failed insert retains the transferred records
  for the next attempt — and `DiscardPending` drops every buffered row
  instead, for callers whose per-operation contract is "a failed operation
  never happened" (the pushout adapter and the CQRS command path both use
  it). Reads see only **flushed** rows.
- **SD5 — Cache wiring: an attached view.** The batched-KV cache is a
  generated `<Store>Cache[W comparable]` view, constructed separately over a
  store — `New<Store>Cache[W](st, <Store>CacheConfig{Capacity,
  FetchCriteria})` — so the store itself is non-generic (the `[W]` work-item
  parameter lives only where the cache's suspend/replay machinery needs it;
  consumers that don't use work items instantiate `struct{}`). A store may
  carry several attached views; each registers a local-write invalidation
  hook at construction.

  The view owns a `caching.ReadThroughCache` and an unexported
  `<table>Fetcher` shim implementing `caching.ItemFetcherI[K, *DroneEntity]`
  (a compile-time `var _ caching.ItemFetcherI = …` assertion pins it, and
  keeps `DeterminePartition`/`FetchItemSinglePartition` off the store's
  public surface). The fetcher runs one
  `SELECT * FROM t WHERE <key> IN (…) FORMAT Arrow` per partition — shared as
  `fetchLatestSQL` with `GetFetch` — decoded client-side through the
  generated read-access classes into the **entity bag**:

  ```go
  type DroneEntity struct {
      ID       uint64                    // envelope (SD2); pass-through fields via an embedded DroneEnvelope
      Ts       time.Time
      Identity option.Option[Identity]   // one option per bound component
      Battery  option.Option[Battery]
      …
  }
  func (e *DroneEntity) Archetype() []string
  func (e *DroneEntity) IsTombstone() bool   // state-view stores only
  ```

  `SELECT *`, not a projection: the RA readers bake schema-order column
  indices, so the fetch must return every column in schema order (a
  needed-columns projection with index rebinding is deferred). Cached values
  are **Arrow-free** plain Go — decode happens eagerly at fetch time,
  because `RecordBatch.Release` lifecycles cannot be tied to cache eviction
  (the cache has no eviction callbacks); cached entities are shared, so
  callers must treat them as immutable. `DeterminePartition` is a config
  hook; v1 default is a single partition (one table, one server), with
  key-hash chunking available for oversized batches. Point-lookup read-back
  deliberately does **not** use the ADR-0066 SQL artefacts: raw-column fetch
  + client-side decode is the proven `ecsdemo` path and avoids SQL-side
  tuple/NULL gymnastics; the artefacts serve `Scan`.

  View verbs: `Get(k) (ent, found)` (cached only — a miss does not fetch);
  `GetFetch(ctx, k) (ent, found, err)` (cached, or one immediate batch fetch
  with the fetch error surfaced — `found=false, err=nil` is the
  authoritative absent); `WorkItem`/`IterateReadyWorkItems`/
  `IterateRestWorkItems`/`AdvanceEpoch` (the caching suspend/replay
  contract). On a state-view store the view also carries the **cached
  state-view reads** `GetLive(k) (ent, found)` and
  `GetLiveAcceptStale(k) (ent, found, stale)` — the cached twins of the
  store's uncached authoritative `GetLive`, with the tombstone read as
  absent.

  `Get` is intended for **immutable-by-key** access — content-addressed or
  unique-keyed records where cache entries can never go stale. Local writes
  are coherent regardless: `Commit`/`Delete` invalidate the written key and
  the store's dirty map keeps it uncacheable (fetched but not retained)
  until the write flushes, so a fetch inside the dirty window cannot
  resurrect the pre-write row. Only **external** writers can leave a cached
  read stale, and the caller supplies the signal: `MarkStale(k)` (next
  strict read misses and refetches), `MarkStaleIfOlder(k, order)` (the
  version-carrying signal — redundant for a version already held, the sink
  for a `(key, Order)` invalidation stream), `Invalidate(k)`, and
  `InvalidateAll()` (drops every entry via the cache's `Clear` — the bulk
  signal after e.g. an external import; call between frames, with no
  suspended work). A freshness TTL for auto-staleness remains deferred (it
  belongs in `public/caching` with a clock seam).

  Three facts S1 established bind this decode path: physical **plain** column
  names are leeway-encoded like every other column (e.g.
  `"id:id:u64:2k:0:0:"`, exported per store as `<Store>ColKey` etc.) — every
  SQL fragment quotes names derived from the IR at generation time, never
  bare role names; fetched Arrow must be pinned to the shape the read-access
  classes expect (`SETTINGS output_format_arrow_string_as_string=1,
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

  *Emitted layout and control visibility.* The DML and RA scaffolding (~280
  identifiers: the `InEntity` builder, section classes,
  `ReadAccess`/`MembershipPack` readers) go into `internal/lowlevel` beneath
  `OutDir` as their own package; the store file imports it through the
  required `gen.Input.ImportPath` and qualifies every scaffolding reference.

  The builder's **control set** — the entity-frame lifecycle
  (`BeginEntity`/`CommitEntity`/`RollbackEntity`), the drain
  (`TransferRecords`), the envelope setters, `SetActiveSections`, and the raw
  `array.RecordBuilder` accessor (dropped from the public surface — a
  `ReleaseBuilder` driver covers `Close`'s only use) — is emitted
  **unexported**; the store drives it through exported **free-function
  drivers** in `lowlevel` (`lowlevel.CommitEntity(b)`, …). The
  section/attribute surface (`GetSection*` and the methods `addSections`
  uses) stays exported. So `Raw()` lets an external holder manipulate
  attributes of the frame the store opened, but **cannot** open, commit,
  drain or re-key a frame — it can no longer desync the store's
  buffered-count, dirty-key and cache-invalidation bookkeeping. The default
  layout is **construction-safe** against that bypass: an external package
  can neither import `lowlevel` to call the drivers nor recover the
  unexported methods by casting the `Raw()` value. The control surface moves
  *off the type* rather than behind a narrowed `Raw()` interface for a
  reason — a type assertion to a locally-declared interface recovers any
  control method with a public signature (e.g. `Builder`, `CommitEntity`), so
  interface-narrowing is safe-by-convention only, whereas the import barrier
  and the unexported-method rule are safe-by-construction.

  The per-kind codecs stay in the parent package: they name the hand-written
  DTO types and the parent already imports lowlevel, so moving them would
  cycle — and they reference no concrete DML/RA type, driving them through
  ADR-0042/0070 structural generics. `gen.Input.Flat` opts into the
  single-package layout for a consumer that must name DML/RA types in its own
  signatures; there — as under `FullCodecs` — the control set stays
  **exported** (the wide, unguarded surface: `Raw()` and the nameable builder
  can drive frames), documented as the escape hatch it is. A Flat layout that
  keeps the control set walled (nameable types via in-package unexported
  methods) is deferred; no in-repo package needs it.

  *Trimmed codec emission.* By default the per-component codec is the trimmed
  `marshallgen.EmitModeStoreSupport` product — `addSections` and `readRow`
  with unexported kind prefixes plus their constraint interfaces, and *not*
  `<Kind>Columns`/`BuildEntities`/`FillFromArrow`. Dropping `BuildEntities`
  removes a live coherence bypass: `<Kind>BuildEntities` on `Raw()` would
  drive entity frames past the store's buffered-count, dirty-key and
  cache-invalidation bookkeeping. `gen.Input.FullCodecs` opts a store package
  back into the full exported codec (the SoA batch / bus-wire path, ADR-0089
  territory); the marshallgen goldens verify the mode split byte-identically.
  Because `<Kind>BuildEntities` drives entity frames from the parent package,
  `FullCodecs` **requires the exported control set** (above): it selects the
  wide builder and is mutually exclusive with the construction-safe default —
  by construction, not by a gate. The trimming takes the example package from
  ~320 exported identifiers to ~54.

  *DDL and schema.* The complete `CREATE TABLE` is composed at generation
  time through the ADR-0102 table-clause seam
  (`ddl/clickhouse.ComposeCreateTable`): clause defaults derived from the
  envelope roles (IF NOT EXISTS, `MergeTree()`, `ORDER BY` the Key then Order
  **by column name** — so a composite id leaves the clause unambiguous — and
  the low-cardinality settings) merged with the `gen.Input.DDL` overrides
  (engine, order, indexes, raw PARTITION BY/TTL). `EnsureTable` runs the
  embedded `.out.sql` verbatim; the runtime `DDLTail` survives only as a raw
  suffix appended after the composed statement. When `gen.Input.Database` is
  set, one value qualifies the whole surface — the `<Store>TableName` const
  becomes `"<db>.<table>"` so every runtime statement is database-scoped, and
  the DDL prepends `CREATE DATABASE IF NOT EXISTS <db>;` so `EnsureTable`
  self-provisions. `VerifySchema` compares the live table's `DESCRIBE` column
  names and order against the generated Arrow schema — `EnsureTable` is `IF
  NOT EXISTS` and the decode is positional, so drift would otherwise fail
  late or, for same-typed column swaps, silently.

  The generation step order matters: the store glue — with its role gates
  (duplicate roles, unsupported shapes) — is emitted **before** the DDL
  composition, so a domain-level role error wins over a downstream
  column-reference failure.
- **SD7 — Executor seam.** `ExecutorI { Exec(ctx, sql) error;
  QueryArrow(ctx, sql) iter.Seq2[arrow.RecordBatch, error]; InsertArrow(ctx,
  table, recs) error }`. `QueryArrow` **streams**: the sequence is
  single-use, an error ends it as a final `(nil, err)` pair (the convention
  the store's iterator verbs share), and ownership of each yielded batch
  transfers to the consumer — which must `Release` every batch it receives,
  including one it breaks on; batches never yielded stay the
  implementation's. `InsertArrow` does not retain the records (the caller
  releases them after return) and, over a durable engine with asynchronous
  inserts disabled, returns only once the rows are durable — the property
  the state view and the pushout adapter rely on. The streaming shape was
  fixed before external adapters exist because it cannot be retrofitted
  afterward: a buffered implementation satisfies it trivially (`chexec`
  materializes a slice and iterates it), the reverse is impossible; and the
  generated decode already releases each batch as it is consumed, so
  record-level memory is bounded the moment a streaming executor exists.
  `chexec` adapts `chclient` (HTTP server) and `clickhouse-local`; the
  bus-hosted `chlocalbroker` can be adapted later if a consumer needs it.
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
| `GetEnvelope(h)` / `HasEnvelope(h)` | one cache-view `GetFetch` (cached hit, or an immediate batch fetch with the error surfaced) | `GetFetch` collapsed the earlier Get/force/Get/`Latest` ritual; `found=false, err=nil` is the authoritative absent; immutable-by-key — the ideal cache case |
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
- Point-lookup performance depends on the `ORDER BY` (Key leading); the
  generator derives that default and `gen.Input.DDL` overrides it, with
  `DDLTail` surviving only as a raw suffix (the table-clause seam landed as
  ADR-0102).
- Single-goroutine ownership pushes concurrency handling to adapters
  (pushout's concurrent-read contract needs an adapter-level lock).

### Neutral

- leeway's data-representation pipeline is untouched (ADR-0074 discipline) —
  no data semantics change; but the store now drives one leeway *generator*
  capability, the dml generator's optional control-method visibility (SD6)
  that makes `Raw()` construction-safe, as it drove the ADR-0102 table-clause
  seam.
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
- **ADR-0102** supplies the table-clause seam SD6 composes the full
  `CREATE TABLE` through; the runtime `DDLTail` is demoted to a raw suffix.
- **ADR-0042** set the SoA-primary, batch-shaped codec model this store
  builds on.
- **ADR-0010** (deferred generic CBOR codec) anticipated a generic
  DTO→target-table binding; its territory has since been substantially built
  as `marshallreflect`, and this store serves the "generic high-level
  ingest" need through composition instead. ADR-0010 stays deferred for its
  actual niche — a single-entity RPC wire codec — and is untouched by this
  decision.

## Deferred

- **Explicit role configuration** (SD2). Role binding is by
  `PlainItemTypeE` and column order; a schema with several same-typed
  columns cannot elect which one is the role — the leading `EntityId` is the
  Key and the rest pass through, with no way to name a different one.
- **Pass-through of `Transaction`/`Opaque` plain columns** (SD2). Every
  other plain-item type passes through today; these two carry
  streaming-group / transaction semantics the store glue does not model and
  are rejected at generation.
- **Negative caching.** Absent keys are never recorded, so every `GetFetch`
  of a missing key re-queries — the pushout adapter's `HasEnvelope(absent)`
  pays a forced fetch on every probe. Needs a `public/caching` feature
  (absent-entries with a TTL), not a store-side hack.
- **Freshness TTL for cached reads** (SD5). Auto-staleness after a bound, so
  a multi-writer deployment need not arrange explicit `MarkStale` signals;
  it belongs in `public/caching` and wants a clock seam there for
  testability.
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
- **A streaming executor.** The executor seam and the store verbs already
  stream (SD7 — the decode releases each batch as consumed), but `chexec`
  buffers whole result sets, so streaming `Replay` end-to-end is an
  executor-implementation change with no signature impact.
- **Index-selection defaults and structured PARTITION BY / TTL** beyond the
  raw overrides `gen.Input.DDL` takes today — which columns deserve a skip
  index without being asked, and typed partition/TTL clauses (both with
  ADR-0102).
- **Generator shape coverage.** With `<Kind>ReadRow` upstreamed (S2), the
  store decode covers scalar, scalar-single (`unit`), slice-container,
  Option-scalar, roaring and multi-sub-column shapes on the non-carrier
  channels. Carrier (mixed / parametrized) channels and exploded fields
  remain uncovered — `marshallgen.ReadRowSupported` is the single gate —
  pending a consumer (the keelson facts kinds would be the trigger).
- **Flat-safe layout** (SD6). `Flat` today exports the whole builder,
  control set included — the wide, unguarded surface. A Flat variant that
  keeps the types nameable but the control set walled (in-package unexported
  methods, no import barrier to lean on) is deferred: the default
  internal/lowlevel layout already delivers construction-safety, and no
  consumer needs nameable-and-safe yet.
- **Projections / read models**, multi-table stores, nested component
  structs (blocked on the deferred marshallreflect nested-struct feature),
  and a CLI wrapper for the generator.

## Slices

- **S1** — `recordstore` runtime + `recordstore/gen` emitting a store for an
  `ecsdemo`-shaped schema; unit-level round-trip (build → flush →
  clickhouse-local → cache Get, Latest/Replay, plus the state view
  Delete/GetLive) green. **Done** (see `public/storage/recordstore`): the
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

Accepted — 2026-07-04 (reviewed by @spx); reconciled in place 2026-07-10
(see Updates). The decision in force: `public/storage/recordstore` is the
generated, append-only store composing leeway, the read-through cache and
ClickHouse — SD1–SD9 as written, with all four slices delivered and the two
consumer adapters (pushout `StorageI` passing `repo/storagetest`, the CQRS
ledger example) as the acceptance evidence. Open items (explicit roles, CAS,
carrier/explode ReadRow coverage, a streaming executor, negative caching)
remain recorded under Deferred.

Status lifecycle: `Proposed → Accepted → (Deprecated | Superseded by ADR-XXXX)`.
From acceptance on, this document normally changes only via dated `## Update`
sections; the 2026-07-10 reconciliation is a maintainer-authorized
consolidation exception (the SD text now states current truth and the dated
updates are compacted to the changelog below). See
`doc/DOCUMENTATION_STANDARD.md` for the edit-policy tiers.

## Updates

### 2026-07-10 — reconciled in place

The Decision (SD1–SD9), Consumer mappings, Consequences, Deferred and
References above were rewritten to state the store's **current** shape
directly, folding in the dated updates that had accreted between acceptance
(2026-07-04) and now. This is a one-time consolidation authorized by the
maintainer — the accepted-ADR edit policy otherwise appends dated updates
rather than rewriting SD text (see `doc/DOCUMENTATION_STANDARD.md`). The
changelog below preserves what changed when; the prose those entries carried
now lives in the SDs.

Changelog (all landed; see git history for the commits):

- **2026-07-04** — post-acceptance API-surface corrections: `Get`
  value-first, `Scan<Kind>(ScanOpts)`, the fetcher shim moved off the
  store's method set, `recordstore.SeqTs`/`SeqOf` canonicalized. (SD2/SD4/SD5)
- **2026-07-04** — `Replay`/`Scan<Kind>` became `iter.Seq2` iterators; the
  store de-generified and the cache became a separately constructed
  `<Store>Cache[W]` attached view. (SD4/SD5)
- **2026-07-04** — table-clause seam (ADR-0102): the full `CREATE TABLE` is
  composed at generation time; `DDLTail` demoted to a raw suffix. (SD6)
- **2026-07-04** — DML/RA scaffolding moved to `internal/lowlevel`;
  `gen.Input.ImportPath` required, `Flat` opts out. (SD6)
- **2026-07-04** — cached state-view reads (`GetLive`/`GetLiveAcceptStale`)
  and explicit external-writer staleness controls landed on the view. (SD5)
- **2026-07-04** — external-review batch 1 (additive): zero-time `Replay`
  fix, exported `<Store>TableName`/`Col*` and `LifecycleLive`/`Tombstone`
  handles, `GetFetch`, the `Ingest<Kind>` duplicate gate, `Close`,
  `VerifySchema`. (SD2/SD4/SD5/SD6)
- **2026-07-04** — external-review batch 2 (breaking): the state-view read
  is `GetLive` (was `GetLatest`), `Put` removed (Begin is the update),
  `Replay` takes `ReplayOpts{To, Limit}`, `ExecutorI.QueryArrow` streams.
  (SD4/SD7)
- **2026-07-04** — external-review batch 3: trimmed per-kind codec emission
  (`EmitModeStoreSupport` default, `gen.Input.FullCodecs` opt-out), removing
  the `BuildEntities`-on-`Raw()` coherence bypass. (SD6)
- **2026-07-09** — pass-through envelope fields: non-role plain columns
  become a `<Store>Envelope` taken by `Begin(id, ts, env)`, superseding the
  interim rejection of non-role columns. (SD2)
- **2026-07-09** — optional `gen.Input.Database` qualification of the whole
  generated surface, with `CREATE DATABASE IF NOT EXISTS` self-provisioning.
  (SD2/SD6)
- **2026-07-10** — dml control-method visibility (this refinement, in place):
  the default layout emits the builder's control set (frame lifecycle, drain,
  envelope setters, `Builder`) unexported with import-walled free-function
  drivers, making `Raw()` construction-safe; `Flat`/`FullCodecs` select the
  exported (wide) control set. Interface-narrowing was rejected as
  cast-defeatable. A dml-generator capability drove this. (SD4/SD6)

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
- [ADR-0102: leeway ClickHouse table-clause seam](0102-leeway-clickhouse-table-clause-seam.md)
  — the composed `CREATE TABLE` SD6 emits; `DDLTail` demoted to a raw suffix.
- [ADR-0010: leeway CBOR RPC codec](0010-leeway-cbor-rpc-codec.md) —
  deferred; adjacent territory, untouched.
- [`public/caching`](../../public/caching) — the read-through cache;
  [`public/algebraicarch/pushout/repo/storage.go`](../../public/algebraicarch/pushout/repo/storage.go)
  — the storage seam;
  [`public/semistructured/leeway/anchor/ecsdemo`](../../public/semistructured/leeway/anchor/ecsdemo)
  — the composition pattern this store generalizes.
