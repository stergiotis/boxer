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
  adaptation to the emit-mode split (`gen.Input.FullCodecs`, unexported
  per-kind codecs) lands as the next slice of the ADR-0100 batch.

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
  the `runtime.facts` TableDesc (`factsschema.GetSchemaInManipulator`;
  `gen.Input.TableName` "facts" agrees with the schema) and the DTO component
  plans, resolving vdd kind ids — the store-side sibling of
  `codec/factswrapper`. Generated lowlevel codecs use the ADR-0100
  `EmitModeStoreSupport` mode split. CLI wiring lands beside the existing
  generator commands under `public/app/commands`.
- **D3 — Slice 1: the unwritten milestones only.** The durable
  `StorageBackendI` backend (`Get`/`Set`/`Delete` map to the generated
  `GetFetch` / `Begin`+`Commit` / `Delete`-tombstone state view; the backend
  opts the entity cache into versioned write-through so a completed `Set` is
  coherent for the next `Get` without a flush round-trip) and the
  CH-backed `FactsStoreI` for grants, audit and state rows. Grants reuse the
  existing `capabilitygrant` DTO as the plan source; audit and state need new
  DTOs, authored with plain scalar/unit shapes that avoid the gated
  carrier/tuple forms by construction.
- **D4 — Concurrency by confinement.** A generated store instance is
  single-goroutine; `StorageBackendI` promises concurrent safety. The owning
  service confines the store (single owner goroutine or a mutex-guarded
  wrapper at the adapter layer); the store instance does not escape.
- **D5 — `chstore` stays, hollowed opportunistically.** No scheduled rewrite.
  The next kind or schema change lands as a generated store behind the
  existing `chstore.Store` facade, leaving callers untouched. The log and
  run-anchored kinds remain hand-rolled until carrier/tuple read support
  exists (ADR-0100 deferral; ADR-0103).

**Sequencing precondition:** the marshallgen `EmitOpts` mode split is on
`main` (2026-07-05). What remains is the recordstore generator's adaptation
to it, which restores `public/storage/recordstore/gen` at HEAD; slice 1
starts after that slice lands and generates against the adapted `gen.Input`
surface. Slice 1 is independent of ADR-0103's review outcome.

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

### Neutral

- `chstore`'s retirement is explicitly unscheduled; the log/lifecycle kinds
  may stay hand-rolled for a long time without harming the rest.
- The dependency closure of keelson binaries is unchanged apart from the two
  recordstore packages (measured; no new third-party dependencies).

## Status

Accepted (2026-07-05). Implementation not started: slice 1 begins once the
recordstore generator's emit-mode adaptation is on `main` (see the
sequencing precondition under Decision).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0100: recordstore — generated leeway ClickHouse store](0100-recordstore-generated-leeway-clickhouse-store.md) — the producer-side decision and its deferrals.
- [ADR-0026: App runtime and capability subjects](0026-app-runtime-and-capability-subjects.md) — §SD3 persist service, §SD6 facts store; the milestones slice 1 implements.
- [ADR-0042: Generated SoA codec for keelson runtime.facts rows](0042-keelson-leeway-codec-soa-generator.md) — the wire-codec generator; unaffected, shares the emitter core.
- [ADR-0089: Row-DML serialization — keep the bus wire and ClickHouse ingestion separate](0089-rowdml-serialization-clickhouse-native-ingestion.md) — boundary this ADR respects.
- [ADR-0101: leeway marshall — mixed-shape multi-sub-column sections](0101-leeway-marshall-mixed-shape-sections.md) and [ADR-0103: leeway marshall — dynamic-membership tuples](0103-leeway-marshall-dynamic-membership-tuples.md) — shape-coverage work adjacent to the deferred kinds; the tuple codec layer is on `main`, store decode still excluded.
- [`public/caching` README](../../public/caching/README.md) — the cache
  substrate's semantics (post-review hardening, versioned write-through),
  recorded there and in its test suites rather than in an ADR.
