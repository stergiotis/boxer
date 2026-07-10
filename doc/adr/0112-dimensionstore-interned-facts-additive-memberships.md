---
type: adr
status: proposed
date: 2026-07-10
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0112: DimensionStore — interned dimension facts stamped as additive memberships

## Context

A generated record store (ADR-0100) writes append-only leeway facts to
ClickHouse. A recurring need sits just outside its verb set: attaching *ambient
context* to written data — which host, which code path, later which tenant,
trace or causing command produced each value — without changing the payload
schema and without paying for the heavy context bytes on every row.

The motivating instance is **provenance**: for each attribute (or component)
written through a store's builder, record the writer's **hostname** and **Go
call-stack**, so a reader can attribute data to its producer. Host is one value
per process; a call-stack is high-cardinality and large — Verbatim-copying
either onto every attribute of every entity would bloat the log by orders of
magnitude.

The pieces to do this cheaply already exist:

- **Memberships (leeway).** An attribute carries memberships; the id-bearing
  flavours are the **Ref** memberships (`AddMembershipHighCardRefP(uint64)`,
  `…LowCardRefP`, `dml/runtime/lw_dml_types.go`). A section's membership pack
  has **one lane per flavour** (`GetMembValueHighCardRef` / `…LowCardRef` / …),
  so an attribute can hold a `LowCardRef` *kind* membership **and** a
  `HighCardRef` membership at once, in disjoint lanes. Crucially, the generated
  typed decode (`<Kind>ReadRow`) switches on the *known* kind ids and **ignores
  every other membership** — so extra memberships in a disjoint lane ride along
  invisibly to the entity decode and its presence gate. Ref channels are the
  subject of ADR-0103/ADR-0109/ADR-0110; ADR-0109 already contemplates a
  `LookupI` resolving a ref id.
- **Tagged ids (`identifier`, ADR-0106).** A `TaggedId` is `tag | body`; the
  fibonacci tag lets several id domains coexist in one lane, each
  self-identifying (`TaggedId.GetTag`, `SameTag`).
- **Interning id generation (`identgen`).** `IdGeneratorI.GetId(ctx,
  naturalKey) (id TaggedId, fresh, err)` dedupes by key and reports whether the
  id was newly minted (the natural key is built via the leeway natural-key
  encoder, `leeway/stopa/naturalkey`). Its global-uniqueness strategy — per-host tag, or one shared
  tag with block-leased body ranges — and the durable/networked leasing behind
  it are the subject of the concurrent **ADR-0111** (the `AllocatorI` seam).
- **Batched resolution (`caching`).** `ReadThroughCache` gives read-through /
  version-gated write-through point lookups — exactly the `id → descriptor`
  resolve path, and it is already what a generated store composes.

What is missing is the small composition that ties these together and a place
to name it, such that provenance is *one instance* of the pattern rather than a
bespoke feature.

## Design space (QOC)

**Question.** How should open-ended, possibly high-cardinality *context* be
attached to written leeway facts — cheaply, at attribute granularity, without
changing the payload schema or perturbing the typed decode?

**Options.**

- **O1 — Pass-through columns on the payload schema** (one per context axis).
  Kill: a schema change on every payload store per axis (host, stack, tenant,
  trace, …), most of them high-cardinality; and a column is per-row, so there
  is no per-attribute granularity. ADR-0100 SD2 pass-through is for a fixed
  envelope, not an open-ended set.
- **O2 — A sidecar table of context rows, joined to the payload** by
  (key, order). Kill: resolution becomes a join per read; still per-row, not
  per-attribute; and the descriptor (stack frames) duplicates per referencing
  row unless itself normalised — which is O3.
- **O3 — Additive memberships carrying a surrogate id, resolved via a
  normalised dimension fact store** (chosen). The id rides the existing
  membership lanes (no payload schema change; per-attribute granularity;
  ignored by the typed decode); the heavy descriptor is stored **once** in the
  dimension store; resolution is a cacheable point lookup. In warehouse terms
  the payload is the fact table and the dimension store is a dimension on a
  surrogate key — hence the name.

## Decision

Adopt **O3**. The general capability is **`DimensionStore`**; provenance is its
first instance. Specific decisions:

### SD1 — `DimensionStore`: the capability

A `DimensionStore[D]` (package `public/storage/recordstore/dimension`; the Go
type is `dimension.Store[D]` to avoid stutter) composes three parts behind a
**minimal** surface:

```go
// Reference interns key, emitting the descriptor fact D exactly once (on the
// first sight of key), and returns the surrogate id to stamp as a membership.
func (s *Store[D]) Reference(ctx, key []byte, describe func() D) (id identifier.TaggedId, err error)
// Resolve is the read side: surrogate id -> descriptor, via the cache.
func (s *Store[D]) Resolve(ctx, id identifier.TaggedId) (d D, found bool, err error)
```

Internally: `Reference` calls the injected `IdGeneratorI.GetId(ctx, key)`; when
`fresh`, it `describe()`s and writes one row through a **generated
`recordstore` fact store** for `D` (SD placement below), keyed by the id; the
store's attached `caching` view answers `Resolve` and doubles as the hot
"already emitted this session" set. Nothing about `Store[D]` is
provenance-specific.

The descriptor store is **not a new store kind** — it is an ordinary ADR-0100
generated store over the `D` schema. "Provenance's store is an example" is
therefore literally true: a second dimension generates its own descriptor store
the same way. The `DimensionStore` type stays a thin wrapper (intern + resolve);
per-dimension knobs are refused here (see Deferred — the framework).

### SD2 — The stamping seam: ambient memberships in the leeway DML runtime

The surrogate id must land on each attribute *inside the write path* — the
generated codec opens and closes each attribute itself (`BeginAttribute … 
AddMembership…P … EndAttributeP`), so there is no post-hoc retag. Two
mechanisms were considered:

- **M1 — Ambient memberships on the DML builder** (chosen). The entity/section
  builder carries a small stack of ambient memberships that `BeginAttribute`
  applies until popped:
  `dml.PushMembershipHighCardRef(id) … AddSections(dml, row) … dml.PopMemberships(n)`.
  The **codecs are unchanged** — every existing `AddSections` gains the
  membership for free, and so does any other leeway write consumer. The stamp is
  additive by construction and inert (byte-identical Arrow) when nothing is
  pushed. This is pure membership machinery, so it belongs *in* leeway — unlike
  caching / ClickHouse concerns, which ADR-0100 kept out.
- **M2 — A decorator threaded through the codec.** `marshallgen` emits
  `AddSections(dml, row, dec)` and calls `dec` per attribute. Kill: changes the
  `marshallgen` signature and the emitted attribute **interface constraint**
  (widen to the full membership family, or leak the concrete type) across both
  emit modes and every call site — a much wider blast radius in generated code
  for a strictly less reusable result.

The id goes in a **Ref lane disjoint from the kind lane** (kinds are
`LowCardRef`; dimension ids are `HighCardRef`), and its fibonacci tag
self-identifies it, so a reader separates dimension ids from any other
`HighCardRef` membership with a cheap `tag.SameTag(id)` and the typed
`<Kind>ReadRow` never sees them.

The generated store gains a stamping seam mirroring the existing
`onWrite`/`onFlush` slices: a `stampers []ReferenceStamper` field consulted per
`Begin`/`Add<Kind>`, registered at runtime, inert (byte-identical output) when
none is set. `ReferenceStamper` is the general seam
(`Current(ctx) iter.Seq2[identifier.TaggedId, error]`); the provenance capture
is its first registrant.

Multiple dimensions **compose** as this slice: a `Composite` combines several
`ReferenceStamper`s through the same interface (a leaf yields one id, a
composite yields many), so provenance plus a later trace / causation / tenant
dimension stack with no new machinery — the Composite pattern over `[]TaggedId`.
One detail lands with the seam rather than the leaves: invoked from inside a
generated `Add<Kind>`, a stamper's stack capture must skip the builder frames
that the standalone S1 `Recorder` (called directly) does not see, so the
capture-skip depth is an S2 seam concern, not a leaf's.

### SD3 — Id generation is an injected dependency

`DimensionStore` takes an injected `identifier.IdGeneratorI` and is **agnostic**
to the global-uniqueness strategy — per-host tag or shared-tag block-leasing
(ADR-0111). It requires only a three-line contract of whatever generator is
wired in:

1. **Internalizing** — dedupe by the natural key so `fresh` marks first sight
   (a `seq` generator, which ignores the key and always mints, would re-emit
   every call — wrong).
2. **Stable key ↔ id across restarts** — so a surrogate id on an old durable
   payload row still resolves later. (A purely in-memory interner reuses body
   values after a restart and mis-resolves; the durable/leased backends are
   ADR-0111's concern.)
3. **Globally-unique `TaggedId`** — by tag or by leased block; not the
   `DimensionStore`'s business.

**Recursion guard:** the descriptor store's own writes run with stamping
**off** — interning a fact must not try to intern its own write's stack.

### SD4 — Granularity: entity-level default, attribute-level opt-in

Memberships are per-attribute; there is no entity-level membership lane. Two
tiers, entity-level the default:

- **Entity-level** (default) — one **synthetic single-attribute dimension
  section** per row carries the id(s): one extra attribute per row, cheap,
  and enough to attribute the whole entity.
- **Attribute-level** (opt-in) — the ambient id is applied to *every*
  attribute the entity writes (via M1), for field-level attribution at the cost
  of one id per payload attribute.

Kill "attribute-level always": on a fat event log it multiplies the membership
count per row for attribution most consumers want only per entity.

### SD5 — Flush ordering: dimension durable no later than payload

A payload row can flush referencing an id whose descriptor fact has not — a
transient dangling reference. Default is **ordered**: the payload store flushes
its bound `DimensionStore`(s) **before** its own insert, over the same executor,
so the descriptor is durable ≤ the referencing row. A **best-effort** relaxation
(dimension flushes on its own cadence; resolution self-heals) is configurable
for throughput, accepting a dangling window. The coupling — the payload store
holds and drives the dimension flush — is the price of the default.

### SD6 — Provenance: the first instance

`provenance` (package `recordstore/dimension/provenance`) is a
`DimensionStore[Provenance]` plus capture, **off by default** (byte-identical
output and behaviour for existing stores until enabled):

```go
type Provenance struct {
    _     struct{} `kind:"provenance"`
    ID    uint64   `lw:",id"`               // = the TaggedId body carried as the membership
    Host  string   `lw:"provHost,symbol"`
    Stack []string `lw:"provStack,symbolArray"` // symbolicated once, on fresh
}
```

Capture: `Host` once at construction; the **call-stack via `runtime.Callers` at
`Begin`/`Add<Kind>`** — the meaningful frame is the caller of the builder, and
per-attribute stacks within one `AddSections` are identical modulo codec frames,
so capturing per component and applying to its attributes is both correct and
the cheap choice. The natural key is `(hostId, pcs)`; symbolication runs once,
on `fresh`. `runtime.Callers` per component is real hot-path cost, so provenance
is gated behind a store option (with room for a host-only tier and sampling).

## Usage sketch

```go
prov := provenance.New(idGen, provExec, alloc)                  // DimensionStore[Provenance]
st   := NewDeviceStore(exec, alloc, cfg,
            recordstore.WithStamper(prov.Stamper()))            // opt-in; ordered flush binds prov
st.Begin(id, ts, env).AddIdentity(Identity{...}).Commit()      // Identity's attrs carry the prov id
st.Flush(ctx)                                                  // prov flushed first, then payload

// read side
ent, _, _ := st.Latest(ctx, id)                                // typed decode: prov ids invisible
who, _, _ := prov.Resolve(ctx, provID)                         // host + frames, cached
```

## Consequences

### Positive

- Cheap hot path: an 8-byte surrogate id per stamped attribute (or one per
  row at entity granularity); the heavy descriptor is stored once. Host ids
  are near-free under ClickHouse RLE; distinct stacks stay bounded.
- No payload schema change; the typed decode and its presence gate are
  unaffected (disjoint lane, ignored memberships).
- Reuses `identifier` (ids/tags), `identgen`/ADR-0111 (interning + leasing) and
  `caching` (resolution) — the interner is not hand-rolled.
- Provenance is one instance of a reusable capability: a second dimension is
  "register another stamper + generate its descriptor store", not a redesign.
- The general primitives land in the right layers: additive memberships in
  leeway (provenance-blind), the wrapper in `recordstore`, capture in the
  instance.

### Negative

- Adds mutable ambient state to the single-goroutine DML builder, which must
  interleave correctly with the frame state machine and `EndAttribute`'s
  membership-support bookkeeping.
- The ordered-flush default couples the payload store's `Flush` to the bound
  `DimensionStore`'s.
- `runtime.Callers` per component is a real cost on the ingest path — hence
  opt-in, and a sampling / host-only tier under Deferred.
- One descriptor store per dimension, each with its own table and cache.

### Neutral

- Surrogate ids are dense (leased) or per-host (tagged) per ADR-0111; the
  `DimensionStore` does not observe the difference.
- Resolution requires the descriptor store reachable; an id whose fact was lost
  (best-effort mode, pre-flush crash) resolves as absent — acceptable for
  observability-shaped context.

## Alternatives

- **O1 / O2** above (payload columns; joined sidecar) — killed for schema
  churn / join cost / no per-attribute granularity.
- **Content-hash surrogate ids** (`fnv64a(host, pcs)` under a fixed tag),
  coordination-free and cross-host stable, was considered as the id strategy.
  It is a valid `IdGeneratorI` behind SD3's seam, but the chosen direction —
  per-host or block-leased **dense** ids (ADR-0111) — was preferred for dense,
  gap-free id spaces; SD3 keeps the `DimensionStore` agnostic either way.
- **A generic dimension registry / framework** (N dimensions, per-dimension
  capture-source and lifecycle interfaces, config surface) — deferred. With one
  consumer the per-dimension interface would be guessed, and the repo has been
  burned by an abstraction with no second consumer (the withdrawn `minibatch`).
  The capability ships general; the *framework* waits for dimension #2.

## Relationship to prior ADRs

- **ADR-0100 (recordstore).** `DimensionStore` is a recordstore fact store plus
  a stamping seam on the generated builder; it adds no new store kind and keeps
  the append-only, single-goroutine model.
- **ADR-0103 / ADR-0109 / ADR-0110 (membership + ref-channel tuples).** The
  surrogate id rides the Ref membership lanes those ADRs define; a
  `DimensionStore.Resolve` is the natural backing of ADR-0109's `LookupI` for a
  ref id.
- **ADR-0106 (identifier).** Supplies the tagged-id scheme that lets dimension
  ids self-identify in a shared lane.
- **ADR-0111 (leased id generation).** Owns the `IdGeneratorI` global-uniqueness
  strategy (per-host tag vs shared-tag block-leasing) and its durable/networked
  leasing. This ADR consumes that seam and does not duplicate its decisions.
- **ADR-0102 (table-clause seam)** and the `caching` versioned write-through
  are reused unchanged by the descriptor store.

## Deferred

- **The dimension framework** — a registry, N-dimension config, and
  per-dimension capture-source/lifecycle interfaces — until a second dimension
  (trace context, CQRS causation, tenant/actor audit) pins the shape.
- **Sampling and a host-only provenance tier** to bound `runtime.Callers` cost.
- **Query verbs over dimension memberships** (e.g. "attributes produced by host
  H") beyond raw SQL over the membership lanes — wants a readback artefact,
  like ADR-0066's Filter.
- **Symbolication format and build-id anchoring** for cross-build stack
  resolution.
- **Content-addressed id strategy** as a shipped `IdGeneratorI` (SD3 admits it;
  not built).

## Slices

- **S1** — the `DimensionStore[D]` runtime plus the `provenance` instance,
  standalone: the intern → emit-once → resolve loop over an injected
  `IdGeneratorI` and a `recordstore`-generated descriptor store (ADR-0100
  as-is — no leeway DML or generator changes). Provenance capture: host,
  `runtime.Callers` at the call site, symbolication on `fresh`. Round-trip
  test at the `DimensionStore` level: the first `Reference((host, stack))`
  returns `fresh` and emits one `Provenance` fact; `Flush` + `Resolve(id)`
  returns the host and symbolicated frames; a second `Reference` of the same
  stack returns the same id, not `fresh`, and emits nothing. Deliberately
  decoupled from the stamping path — it touches neither the shared DML
  subsystem nor the M1 decision, consuming only the `IdGeneratorI` seam — so
  it is buildable and verifiable ahead of, and independently of, S2.
  **Done** (`public/storage/recordstore/dimension` + `dimension/provenance`):
  `dimension.Store[D]` over a `DescriptorSink[D]`; the provenance `Recorder`
  over a generated `ProvenanceStore` (id/ts envelope, `symbol` + `symbolArray`
  component). `TestS1ProvenanceRoundTrip` is green against clickhouse-local —
  same-call-site dedup (one emit, one id), distinct-call-site distinctness,
  resolve-after-flush (host + this test's frame), and absent-id. v1 uses a
  host-plus-raw-pcs natural key; the ASLR-robust / cross-build key is recorded
  as a caveat in `provenance.key` (Deferred).
- **S2** — the stamping path: the ambient-membership primitive (M1) in the
  leeway DML runtime/generator, the `ReferenceStamper` seam on the generated
  store (gated, inert when off), entity-level granularity (SD4) and ordered
  flush (SD5). Wire the `provenance` `DimensionStore` from S1 as the first
  stamper. End-to-end test: write with provenance on → flush → payload
  attributes carry the surrogate id (typed decode unaffected) → `Resolve`
  attributes the row; a store with provenance off is byte- and
  behaviour-identical to today.
  The **M1 primitive is done** (`dml/lw_dml_generator.go`): the entity carries
  an ambient HighCardRef stack (`PushMembershipHighCardRef` /
  `PopMembershipsHighCardRef`, always-exported attribute surface) replayed onto
  each attribute at the top of `EndAttribute`/`EndSection` — before the state
  transition, while `AddMembership*` still passes its `InAttribute` guard, and
  before `completeAttribute` records the per-attribute membership count.
  `applyAmbientMemberships` is body-emitted only for sections that declare a
  HighCardRef membership column (so a carrier schema must declare that channel
  on the sections meant to carry the stamp; others no-op). All 17 DML consumers
  were regenerated — behaviour-inert with nothing pushed (every consumer suite
  passes) — and a DML→read-access test (`example/internal/lowlevel`) proves a
  pushed id lands in the HighCardRef lane alongside the codec's own membership.
  Remaining S2: the `ReferenceStamper` seam on the generated store, the
  capture-skip handling, entity-vs-attribute scope (SD4), and ordered flush
  (SD5). Other membership flavours beyond HighCardRef are a mechanical repeat
  (Deferred).
- **S3** — attribute-level opt-in (SD4); a readback artefact for querying by
  dimension membership; the host-only / sampled provenance tiers.
- **S4** — a second dimension (the first real trace/causation/tenant consumer)
  extracts the registry, with its interface earned rather than guessed.

## Status

Proposed. Open forks for review: the M1 ambient-membership surface on the DML
builder (SD2); the entity-vs-attribute default (SD4); and whether ordered flush
is the default or opt-in (SD5). Depends on ADR-0111 for the id-generation seam.
S1 is scoped to the `DimensionStore` runtime and the provenance instance — the
intern/emit/resolve loop — keeping the shared DML-subsystem change (M1) and its
tighter ADR-0111 coupling in S2, so none of those three forks blocks S1. S1 is
now **implemented and tested** (round-trip green against clickhouse-local); the
ADR stays proposed pending human review — flip to accepted with a reviewer once
the S2 forks are settled.

## References

- ADR-0100 — recordstore.
- ADR-0102 — table-clause seam.
- ADR-0103 / ADR-0109 / ADR-0110 — membership and ref-channel tuples.
- ADR-0106 — identifier (fibonacci-tagged ids).
- ADR-0111 — technology-neutral leased id generation.
- `public/caching` — read-through / versioned write-through cache.
