---
type: adr
status: accepted
date: 2026-07-05
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-05
---

# ADR-0105: keelson adopts generated record stores for durable facts

## Context

keelson persists runtime facts to ClickHouse through hand-rolled code, and two
of its planned persistence milestones are unwritten:

- [`runtime/factsstore/chstore`](../../public/keelson/runtime/factsstore/chstore)
  (~1.6k lines, wired into the imzero2 host app and `apps/capinspector`)
  hand-rolls per kind what [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md)'s
  generator now emits: the Arrow ship path (`commitAndShip` →
  `chclient.InsertArrow`), blake3 natural keys, latest-state SQL, per-kind row
  parsers, schema setup.
- The durable `persist.StorageBackendI` backend ([ADR-0026](0026-app-runtime-and-capability-subjects.md)
  §SD3, milestone M2.5) and the ClickHouse-backed `factsstore.FactsStoreI`
  (§SD6) exist only as in-memory implementations.

ADR-0100 built `public/storage/recordstore` deliberately *beside* `chstore`
(its Alternatives records the kill-reason: generalizing `chstore` in place
would entangle keelson's runtime with a general-purpose library), and its
deferral list names "the keelson facts kinds" as the trigger for the missing
carrier-channel read support. This ADR is that consumer-side decision.

Forces measured before deciding (2026-07-05, scripted checks):

- **Import graph:** no dependency edges exist in either direction today. The
  only coupling is the shared `marshallgen` emitter core, used by both the
  [ADR-0042](0042-keelson-leeway-codec-soa-generator.md) codecs and
  `recordstore/gen`; the keelson codec golden tests are the byte-identity
  guard on that seam.
- **Dependency cost:** adding the recordstore runtime to keelson's dependency
  closure adds only the two recordstore packages themselves — every
  transitive dependency is already present.
- **Shape coverage:** all 16 `lw`-tagged keelson DTO kinds pass
  `marshallgen.ReadRowSupported` (checked with `factswrapper`'s
  unit-inference preprocessing replicated). The `chstore`-persisted kinds
  (log, heartbeat, run lifecycle, run sessions, env vars) have **no DTOs** —
  they are hand-coded against `factsschema/dml`. The log kind writes typed
  fields via `AddMembershipMixedLowCardRef` with the field name as a runtime
  membership parameter
  ([`chstore.go:295`](../../public/keelson/runtime/factsstore/chstore/chstore.go)),
  and lifecycle rows ride the run id as a high-card membership parameter —
  both shapes sit behind `ReadRowSupported`'s refusal gates (carrier channel;
  dynamic-membership tuples). The
  [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) tuple
  support is on `main` through the codec layer (plan, reflect, emitters),
  but `ReadRowSupported` still excludes tuple fields — for these kinds the
  remaining gap is store decode only, no longer authoring.
- **Transport gap:** `recordstore/chexec` ships only a clickhouse-local
  executor; keelson's transports ([`data/chclient`](../../public/keelson/data/chclient),
  [`data/chlocalbroker`](../../public/keelson/data/chlocalbroker)) have no
  `recordstore.ExecutorI` implementation.
- **Write-path substrate:** [`public/caching`](../../public/caching) ships
  opt-in versioned admission and write-through (`WithVersioning`,
  dirty-window pinning, freshness TTL; README §3.4) — the write coherency a
  read-write KV backend otherwise hand-rolls. The recordstore generator's
  adaptation to the emit-mode split (`gen.Input.FullCodecs`, trimmed
  store-support codecs by default) is on `main`, closing the sequencing
  precondition this ADR originally waited on.

A second pass before implementation start (2026-07-11, code-level checks)
added four forces the Decision now reflects:

- **Membership-id models conflict.** `recordstore/gen` assigns membership
  ids positionally per kind (`marshallgen.MembershipIds`) and bakes them as
  SQL literals into the generated `Scan` filters; the facts kinds resolve
  stable vdd-registry ids via `vdd.MembXxx.GetId()` (the
  [`codec/factswrapper`](../../public/keelson/runtime/codec/factswrapper)
  pattern). `gen.Input` has no seam for externally supplied ids, and
  recordstore must stay keelson-free, so the id map has to cross the
  boundary as caller-supplied data.
- **The disjoint-sections gate rejects the facts schema's design.** The
  generator errors when two components bind one section — correct under
  positional ids, but `boxer.facts` kinds share sections by construction
  (every kind's tag rides the symbol section; `reason` is one column
  joined across six DTOs).
- **The Key role would bind a sequence, not a key.** The store keys
  `Latest`/`GetFetch` on the leading `EntityId`; in `boxer.facts` that is
  `id`, a per-process counter. The access identity (the blake3
  `naturalKey`, or (appId, key) memberships) is the second `EntityId`,
  which passes through as an envelope field; explicit role election is an
  ADR-0100 deferral.
- **No state view can emit against the facts TableDesc.** The Lifecycle
  role requires the first u8 `EntityLifecycle`; the facts schema's
  `expiresAt` is a DateTime, and keelson tombstones are per-kind
  memberships (`MembPersistTombstone`), not envelope lifecycle. The
  original D3 mapping — `Delete` onto the generated tombstone state view —
  could not have been built as written against `boxer.facts`.

## Design space (QOC)

**Question.** How should keelson gain durable persistence for its facts
kinds, given that a generated store now exists?

**Options.**

- **O1** — Milestone-first adoption: build the unwritten milestones (persist
  backend, CH-backed `FactsStoreI`) on generated stores; leave `chstore`
  untouched.
- **O2** — Big-bang: replace `chstore`'s internals with generated stores
  first, then build the milestones on the result.
- **O3** — Status quo: hand-roll M2.5 as originally planned; no recordstore
  dependency.

**Criteria.**

- **C1** — Regression risk to production-wired surfaces (imzero2 host app,
  capinspector).
- **C2** — Hand-written lines added or avoided.
- **C3** — Time-to-value for the unshipped ADR-0026 milestones.
- **C4** — Dependency hygiene (recordstore stays keelson-free; coupling
  one-way).

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −− | ++ |
| C2 | ++ | +  | −− |
| C3 | ++ | −  | −  |
| C4 | +  | +  | ++ |

## Decision

We adopt generated record stores in keelson **by milestone, not by refactor**
(O1). keelson gains a one-way dependency on
[`public/storage/recordstore`](../../public/storage/recordstore); recordstore
stays keelson-free. Concretely:

- **D1 — Executor adapter on the keelson side.** A small package (working
  name `public/keelson/data/storeexec`) implements `recordstore.ExecutorI`
  (`Exec` / `QueryArrow` / `InsertArrow`) over `data/chclient`. Placement in
  keelson keeps the dependency direction one-way — the mirror image of
  ADR-0100's non-entanglement kill-reason. A `chlocalbroker`-backed executor
  is deferred until an in-proc consumer exists.
- **D2 — Store-gen wrapper for the facts schema.** A generation-time package
  (working name `runtime/factsschema/storegen`) feeds `recordstore/gen` with
  the `boxer.facts` TableDesc (`factsschema.GetSchemaInManipulator`;
  `gen.Input.TableName` "facts" agrees with the schema) and the DTO component
  plans, resolving vdd membership ids at generation time — the store-side
  sibling of `codec/factswrapper`. Where `factswrapper` resolves ids at
  init, the store bakes them as generation-time literals (they reach `Scan`
  filter SQL); vdd `TaggedId`s are stable by the vdd contract, and a
  registry change regenerates the store exactly as it regenerates the
  codecs. This needs two `recordstore/gen` features that do not exist yet,
  both keelson-blind: an optional membership-id override on `gen.Input` (a
  name → id map supplied by the caller; positional assignment stays the
  default) and a relaxation of the disjoint-sections gate to id-level
  disjointness when the override is present. Generated lowlevel codecs use
  the ADR-0100 `EmitModeStoreSupport` mode split. CLI wiring lands beside
  the existing generator commands under `public/app/commands`.
- **D3 — Slice 1 split by verb shape.** The two milestones bind different
  tables, because their verbs want different envelopes:
  - **D3a — persist backend on a dedicated store-owned table** (unblocked
    now; needs no generator changes). The durable `StorageBackendI` backend
    binds its own generated table (working name `runtime.persiststate`):
    Key = string `"<appId>/<key>"` (the pushoutstore namespacing pattern),
    Order = the z64 timestamp lane, and a u8 lifecycle column, so the full
    state view emits — `Get`/`Set`/`Delete` map to the generated `GetFetch`
    / `Begin`+`Commit` / `Delete`-tombstone — and the backend opts the
    entity cache into versioned write-through so a completed `Set` is
    coherent for the next `Get` without a flush round-trip. Persist state
    thereby leaves the `boxer.facts` substrate; the `FactsStoreI` state
    verbs (`WriteState`/`DeleteState`/`LatestState`) stay on the legacy
    `chstore` facade until its callers migrate.
  - **D3b — facts-bound store for grants and audit** (gated on the D2
    generator features). The CH-backed `FactsStoreI` ingest and `Scan` for
    grant and audit rows binds the `boxer.facts` TableDesc; no state view
    is expected or possible there — both kinds are append-shaped. Grants
    reuse the existing `capabilitygrant` DTO as the plan source; audit
    needs a new DTO, authored with plain scalar/unit shapes that avoid the
    gated carrier/tuple forms by construction.
- **D4 — Concurrency by confinement.** A generated store instance is
  single-goroutine; `StorageBackendI` promises concurrent safety. The owning
  service confines the store (single owner goroutine or a mutex-guarded
  wrapper at the adapter layer); the store instance does not escape.
- **D5 — `chstore` stays, hollowed opportunistically.** No scheduled rewrite.
  The next kind or schema change lands as a generated store behind the
  existing `chstore.Store` facade, leaving callers untouched. The log and
  run-anchored kinds remain hand-rolled until carrier/tuple read support
  exists (ADR-0100 deferral; ADR-0103).

**Sequencing:** the original precondition — the recordstore generator's
adaptation to the marshallgen `EmitOpts` mode split — is on `main`
(`gen.Input.FullCodecs` / `EmitModeStoreSupport`), so D3a can start now.
D3b additionally waits on the two D2 generator features (membership-id
override, id-level disjointness). Both are independent of ADR-0103's
review outcome.

## Alternatives

- **Big-bang `chstore` replacement (O2).** Rejected: it puts the highest-risk
  step first — regressing production-wired read paths — to chase a ~1k-line
  maintenance payoff the opportunistic path collects anyway.
- **Status quo hand-rolling (O3).** Rejected: M2.5 would duplicate by hand
  exactly what the generator emits (batched keyed fetch, cache wiring, flush
  policy, schema verification), against a cache substrate
  ([`public/caching`](../../public/caching)) that recordstore already
  composes — including the versioned write-through semantics its README and
  regression suites pin.
- **Generalizing `chstore` in place.** Already rejected in ADR-0100; not
  re-opened here.
- **Adding a u8 lifecycle column to `boxer.facts`** so the state view can
  emit against the facts TableDesc. Rejected: a live-table migration plus a
  retrofit of every existing facts writer and reader, for an envelope
  column only the state kind would populate — coexisting confusingly with
  the per-kind membership tombstones (`MembPersistTombstone`) already
  written.
- **Keeping persist state facts-bound, tombstoned by membership.**
  Rejected: without the state view the live read stays hand-written
  leeway-encoded SQL (`composeLatestStateSql` and its cumulative-sum
  membership lookups) — the code class this ADR exists to delete — and the
  persist milestone keeps roughly half its hand-rolled surface.
- **Executor adapter inside `recordstore/chexec`.** Rejected: it would point
  the dependency arrow at keelson and entangle the general-purpose library
  with runtime specifics — the same entanglement ADR-0100 built beside
  `chstore` to avoid.
- **Unifying the bus wire codecs with store ingestion.** Out of scope and
  already rejected by [ADR-0089](0089-rowdml-serialization-clickhouse-native-ingestion.md);
  the 16 wire codec packages are unaffected by this ADR.

## Consequences

### Positive

- The two unshipped ADR-0026 persistence milestones arrive mostly generated;
  hand-written surface shrinks to adapters (rough estimate: 250–400 lines
  against 700–1,100 avoided).
- D3a needs no generator changes at all: string keys, the state view and
  the versioned write-through cache are shipped recordstore features, so
  the persist milestone is purely consumer-side work.
- Durable persistence inherits the read-through cache's staleness controls,
  batching, circuit breaker and opt-in versioned write-through (ordered
  admission, dirty-window pinning, freshness TTL) instead of reimplementing
  them — the coherency a `Set`-then-`Get` KV backend needs.
- Future facts kinds get ingest/read/state-view code for free once their DTO
  exists.

### Negative

- keelson gains its first `recordstore` import; marshallgen evolution now
  serves two in-repo consumers, so generator changes must keep the keelson
  codec goldens byte-identical (this guard already caught a missed call site
  during the `EmitOpts` split).
- Generated store packages add on the order of 1–2.5k generated lines under
  keelson (the ADR-0100 `example` store is ~1k lines for four components).
- Two persistence mechanisms coexist in `factsstore` indefinitely; readers
  must know `chstore` is legacy-by-policy, not legacy-by-replacement.
- Persist state departs the single-substrate reading of ADR-0026 §SD6: once
  the durable backend ships, state rows live in `runtime.persiststate`, not
  `boxer.facts`, and surfaces narrating the facts substrate (e.g.
  capinspector's persist help) must follow.
- `recordstore/gen` grows a membership-id override whose first — and so far
  only — consumer is keelson: generalization pressure ADR-0100 did not
  carry, held in check by keeping positional assignment the default.

### Neutral

- `chstore`'s retirement is explicitly unscheduled; the log/lifecycle kinds
  may stay hand-rolled for a long time without harming the rest.
- The dependency closure of keelson binaries is unchanged apart from the two
  recordstore packages (measured; no new third-party dependencies).

## Status

Accepted (2026-07-05); reconciled in place 2026-07-11, before
implementation start (see Updates). D1 (`data/storeexec`) and D2
(`runtime/factsschema/storegen`) are built. D3a is built and wired — the
persist backend runs on the generated `boxer.persiststate` store, and the
2026-07-31 deviation is discharged; since 2026-08-15 the facts-bound
predecessor and the `FactsStoreI` state verbs are gone. D3b's generator
precondition is met; the facts-bound grants/audit store itself is not built.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-07-31 — the tree deviates from D3a: a facts-bound persist backend shipped

**A durable `StorageBackendI` landed on 2026-07-30 (`66ac54c9`) that is the
alternative this ADR rejects by name.** `persist.FactsBackend` routes
`Get`/`Set`/`Delete` onto `FactsStoreI.LatestState`/`WriteState`/`DeleteState`
— persist state facts-bound, tombstoned by `MembPersistTombstone`, read
through `composeLatestStateSql`. *Alternatives* calls that shape out
specifically, and the kill-reason it gives is unaffected by anything that has
happened since: the live read stays hand-written leeway-encoded SQL, which is
the code class this ADR exists to delete.

The deviation was not a decision. The implementing session did not consult
this ADR, and its commit message asserts that "only the adapter was ever
missing" — untrue, since D3a specifies a different adapter and left it
unbuilt. Recording it here rather than quietly, because the tree and an
accepted decision disagree and the disagreement is not self-evident from
either side.

**D3a is unchanged and remains the target.** Nothing below reopens it.

**What the deviation bought, and why it is not being reverted today.** The
backend closed a real defect: the runtime applet store persists user-authored
documents through `StorageI` under ADR-0132's assumption of facts-backing,
and with the memory backend wired those documents evaporated at every
restart. That is fixed and verified against live ClickHouse. Reverting now
would restore the defect and buy nothing, because D3a cannot be built in its
place today — **D1 (`keelson/data/storeexec`) does not exist**, and
`recordstore/chexec` still ships only the clickhouse-local executor, so no
keelson service has a `recordstore.ExecutorI` to bind.

**The exit is one adapter.** Checked on 2026-07-31: `persist.FactsBackend` is
the *only* production caller of the `FactsStoreI` state verbs — the other
references are comments and the in-memory store's own implementation. D3a's
"until its callers migrate" therefore describes exactly one migration: build
D1, generate the `runtime.persiststate` store, and swap the backend behind
the unchanged `StorageBackendI` seam. No app and no service sees the change,
because the app-facing surface is `StorageI` either way.

**The deviation grew before it was noticed.** ADR-0151's column-width fact
kind (2026-07-31, `7f5e5852`) added roughly 250 further lines of the same
hand-written class — `composeListColumnWidthsSql` with its nested `argMax`
and cumulative-sum membership lookups — for a kind that is state-shaped and
so, by the force this ADR's own reconciliation measured, can never acquire a
state view while it lives on `boxer.facts`. It carries the same target and
the same exit; see [ADR-0151](./0151-table-column-width-overrides.md)'s
Update of the same date.

**A conflicting invariant was also written in the interim, and is corrected
elsewhere.** [ADR-0148](./0148-app-workingsets.md)'s data-centricity Update
(2026-07-30) says state "is stored in the runtime facts table
(`boxer.facts`)", which contradicts D3a's "persist state thereby leaves the
`boxer.facts` substrate". That ADR carries the correction: the invariant's
binding clause is that state be *modelled*, and naming one table
over-specified it. A generated leeway table satisfies the invariant more
completely than an opaque payload on `boxer.facts` does, so D3a and the
invariant agree once the wording is fixed.

Background for all of the above:
[persist-api-surface-recordstore](../adr-background-work/persist-api-surface-recordstore.md).

### 2026-07-11 — reconciled in place (pre-implementation)

A code-level pass before implementation start found the original D3 not
buildable as written and the sequencing precondition already met. The
maintainer authorized reconciling the body in place rather than accreting
a correction-sized dated entry (the ADR-0100 exception pattern); this
entry records what changed:

- **Precondition met.** The recordstore generator's emit-mode adaptation is
  on `main`; Status moved from "waits on the adaptation" to "D3a
  unblocked".
- **Four forces added to Context** (2026-07-11 checks): positional
  membership ids baked as SQL literals with no override seam; the
  disjoint-sections gate vs the facts schema's shared sections; the Key
  role binding the per-process `id` sequence rather than an access key; no
  u8 `EntityLifecycle` in the facts schema, so no state view can emit
  against `boxer.facts`.
- **D3 split by verb shape.** D3a: the persist backend binds a dedicated
  generated table (string Key, u8 lifecycle — full state view plus
  versioned write-through); persist state leaves the facts substrate. D3b:
  the facts-bound store covers grants and audit (append and `Scan`, no
  state view), gated on two new D2 generator features (membership-id
  override on `gen.Input`, id-level disjointness under the override).
- **Alternatives extended** with the kill-reasons for the two rejected
  resolutions (adding a u8 lifecycle column to `boxer.facts`; keeping
  persist state facts-bound with membership tombstones and hand-written
  live reads).

### 2026-08-10 — the two D2 generator features landed

The recordstore generator now takes the membership-id override and
relaxes the disjoint-sections gate under it (ADR-0100's dated entry of
the same day carries the mechanics): `gen.Input.Wrapper` selects a
`marshallgen.WrapperEmitterI` that must state its ids at generation time
(`MembershipIdSourceI`), and `marshallgen.FixedIdsWrapper` carries the
caller-resolved name → id snapshot — the wrapper form subsumes the bare
map this ADR sketched. Under a source claiming globally-unique ids the
gate checks id-level disjointness instead of section ownership.
`factswrapper`'s vocabulary package and qualifier are parameterized for
vocabularies outside boxer's vdd. `recordstore/sharedsection` is the
round-tripped worked example. D3b's generator precondition is thereby
met; the facts-bound store itself — the storegen package resolving vdd
ids into the snapshot — remains to be built.

### 2026-08-14 — D1 and D2 built; D3a built and wired; the 2026-07-31 deviation is discharged

D1 (`keelson/data/storeexec`) and D2 (`keelson/runtime/factsschema/storegen`)
landed for ADR-0184's tee. They are this ADR's D1 and D2 and satisfy what it
specified — so D3a's real precondition, a `recordstore.ExecutorI` that a
keelson service can bind, was met by the first of them.

**D3a is built.** `persist.StoreBackend` runs on a generated store over
`boxer.persiststate` (`runtime/persist/persiststore`): entity key
`"<appId>/<key>"`, the z64 timestamp as Order, a u8 lifecycle column. `Get`
is the cache's `GetFetch` plus a tombstone check, `Set` is `Begin`+`Commit`,
`Delete` is the tombstone — as specified. Every mutating call flushes before
returning and discards its rows on a failed flush, so a failed operation
stays "never happened"; that is `pushoutstore`'s posture, adopted for the
same reason. D4's confinement is the mutex-guarded wrapper rather than an
owner goroutine. The backend's test suite mirrors `FactsBackend`'s case for
case, over `clickhouse-local`, because the migration only holds if the
replacement answers the same contract.

**The 2026-07-31 deviation is discharged.** That entry declined to revert
`persist.FactsBackend` because "D3a cannot be built in its place today — D1
does not exist", and named the exit as exactly one migration. The carousel
now selects `StoreBackend`; `FactsBackend` has no production callers, and
the `FactsStoreI` state verbs have no callers outside it. The status-bar
label moves from `persist:facts` to `persist:store`.

Three corrections the decision did not anticipate:

- **The table is `boxer.persiststate`, not `runtime.persiststate`.** D3a
  predates the runtime→boxer database rename. Same database as the facts
  table it left: D3a moves the substrate, not the deployment.
- **`FactsBackend` is kept, not deleted.** D5 schedules no rewrite, and it
  is the only backend a test can aim at a scratch database — a generated
  store bakes its database at generation time, so
  `sqlapplet_store_durability_integration_test` has no equivalent isolation
  and stays on it. That leaves one test exercising a path production no
  longer takes, which is the smaller cost against pointing it at the live
  table.
- **What the move costs is the §SD6 join.** State rows leave the table
  carrying that app's grant, audit and launch facts, so "an app's state
  beside its other facts" is now a two-table query. ADR-0026 §SD6 argued for
  the single table on that property; D3a takes the trade knowingly, and the
  app id is carried on the new table so the join stays expressible.

Not taken here: removing `WriteState`/`DeleteState`/`LatestState` from
`FactsStoreI`. D3a says they stay "until its callers migrate" and the
callers have, so the removal is now available — but it touches `chstore`,
the in-memory store and that one integration test, and D5's opportunistic
posture argues against scheduling the sweep for its own sake.

### 2026-08-15 — the state verbs leave `FactsStoreI`; a transport defect in D3a fixed

Two things landed together, closing what the 2026-08-14 entry left open
and one thing it did not know.

**The persist store never provisioned itself in production.** `EnsureTable`
shipped its embedded DDL as one two-statement script (`CREATE DATABASE …;
CREATE TABLE …`), and the ClickHouse HTTP interface rejects a multi-statement
body — the very fact `data/storeexec`'s own integration test pins. So the
carousel's `OpenStoreBackend` failed at open through the HTTP executor and
fell back to `MemoryBackend` (`persist:mem`, with a warning), and
`boxer.persiststate` did not exist on the server the 08-14 entry called
"wired" (checked 2026-08-15: `system.tables` had no such table). The
`clickhouse-local` executor the tests use runs scripts, which is why the
suite was green. Fixed at the root: a generated `EnsureTable` now issues one
statement per `Exec` (`recordstore.ProvisioningStatements`; ADR-0100's Update
of this date), verified end to end through `storeexec` against a live server
by the sqlapplet durability test (next paragraph). The live table provisions itself on
the next carousel boot; nothing was written under the old wiring, so there is
nothing to migrate.

**`FactsBackend` and `WriteState`/`LatestState`/`DeleteState` are removed.**
The 08-14 entry kept them only because a generated store baked its table and
the durability integration test needed a scratch database. That is now a
runtime binding — `<Store>StoreConfig.Table` (ADR-0100, same date),
surfaced here as `persist.OpenStoreBackendAt(ctx, exec, alloc, table)` — and
`apps/sqlapplet`'s durability test runs the production backend over a
scratch database through the HTTP executor, which is more faithful than the
facts-bound path it used before. Gone with the verbs: `factsstore.StateRow`,
the in-memory state trail, `chstore.composeLatestStateSql` and its callers.
`MembKindState` / `MembPersistKey` / `MembPersistTombstone` stay registered —
rows carry them, and the tombstone term is still what `DeleteWorkingset` and
`DeleteColumnWidth` write.

## References

- [ADR-0100: recordstore — generated leeway ClickHouse store](0100-recordstore-generated-leeway-clickhouse-store.md) — the producer-side decision and its deferrals.
- [ADR-0026: App runtime and capability subjects](0026-app-runtime-and-capability-subjects.md) — §SD3 persist service, §SD6 facts store; the milestones slice 1 implements.
- [ADR-0042: Generated SoA codec for keelson boxer.facts rows](0042-keelson-leeway-codec-soa-generator.md) — the wire-codec generator; unaffected, shares the emitter core.
- [ADR-0089: Row-DML serialization — keep the bus wire and ClickHouse ingestion separate](0089-rowdml-serialization-clickhouse-native-ingestion.md) — boundary this ADR respects.
- [ADR-0101: leeway marshall — mixed-shape multi-sub-column sections](0101-leeway-marshall-mixed-shape-sections.md) and [ADR-0103: leeway marshall — dynamic-membership tuples](0103-leeway-marshall-dynamic-membership-tuples.md) — shape-coverage work adjacent to the deferred kinds; the tuple codec layer is on `main`, store decode still excluded.
- [`recordstore/pushoutstore`](../../public/storage/recordstore/pushoutstore)
  — the string-key namespacing precedent D3a follows.
- [`public/caching` README](../../public/caching/README.md) — the cache
  substrate's semantics (post-review hardening, versioned write-through),
  recorded there and in its test suites rather than in an ADR.
