---
type: adr
status: superseded
date: 2026-05-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Superseded by [ADR-0070](0070-leeway-entity-assembly.md)–[ADR-0073](0073-leeway-membership-role.md) (2026-06-07).** The decisions here stand but have been re-cut onto an orthogonal basis (see [ADR-0070 §Concept basis](0070-leeway-entity-assembly.md)): entity assembly → ADR-0070; value & emission → [ADR-0071](0071-leeway-value-and-emission.md); membership carriage & representation → [ADR-0072](0072-leeway-membership-carriage.md); membership role & param-treatment → ADR-0073. Retained for history; new work references the plane-ADRs.

# ADR-0008: leeway marshall\* extensions — stacked entities, attribute ordering, full channel coverage

## Context

The sibling pair `public/semistructured/leeway/marshallgen` (codegen)
and `public/semistructured/leeway/marshallreflect` (runtime reflection)
ships an annotated-Go-DTO → leeway DML / RA codec built on a shared
`mappingplan.Plan` vocabulary (the DTO model lives in the sibling
`public/semistructured/leeway/mappingplan` package — see Updates). The
current grammar supports

- one DTO per leeway entity (one `BeginEntity`…`CommitEntity` frame per
  row, plain columns are entity-scoped and bound to that single DTO);
- attribute emission in DTO declaration order within a section (no
  rearrangement between `BeginSection` and `EndSection`);
- two of the eight membership channels declared in
  `dml/runtime/lw_dml_types.go` — `LowCardRef` (default) and
  `LowCardVerbatim` (`,verbatim` flag) — with the section-level
  invariant that every field in one section must agree on the channel
  so the read-side dispatch iterates one iterator type per section.

Three constraints arrive together from in-flight schema work and want
relaxing in a coherent way:

1. **Stacked DTOs per entity.** Several callers want to compose a
   logical entity from multiple small DTOs — one carries audit fields,
   one carries the spatial payload, one carries the facts. Today the
   only way to do this is to define a god-DTO that unions every
   contributing field set, which couples otherwise-independent code
   paths.

2. **Wire-form attribute ordering.** Downstream tooling that scans the
   per-section attribute stream benefits from grouping single-value
   scalars before container / explode attributes. Locality of the small
   fixed-size leaves matters more on the read side than write side;
   reordering at write time is mechanical given the existing `Plan`.

3. **Reach the other six membership channels.** The DML interface
   exposes `HighCardRef`, `HighCardRefParametrized`, `HighCardVerbatim`,
   `LowCardRefParametrized`, `MixedLowCardRef`, `MixedLowCardVerbatim`
   in addition to the two currently reachable. Today a DTO author who
   needs any of these has to bypass marshallgen entirely. The grammar
   should cover the protocol's full channel set so the codec is the
   right answer for a complete leeway producer, not just for the
   low-card subset.

Forces the decision must respect:

- **Wire compatibility within a codec generation, parity across the
  pair.** Round-trip through marshallgen-emitted code and through
  marshallreflect must produce byte-identical output for the same DTO,
  and the read sides must decode each other's output. Byte-identity
  with the *current* implementation is not required: the wire format
  is not persisted in content-addressed form at this stage of the
  project (caller confirmed 2026-05-26).
- **`Plan` is the shared vocabulary.** Both packages parse to the same
  `marshallgen.Plan` and drive the same downstream emit / dispatch.
  New grammar surface must extend `Plan` once; both call-sites pick it
  up.
- **Read-side dispatch stays cheap.** The codegen-emitted
  `<Kind>FillFromArrow` switches on membership-id inside an iterator
  loop. The reflect-side equivalent uses pre-resolved id caches.
  Multiplying iterator types per section would force the read side to
  iterate every channel's accessor, which is wasteful when only one
  channel is actually populated. The section-channel-uniformity
  invariant therefore stays — it just generalises from 2 to 8.

## Decision

We will extend the marshall\* pair along three orthogonal axes.

### D1 — Stacked entities (multi-DTO per entity, reflect-only)

We add a `marshallreflect.RowComposer` per-row builder that drives one
`BeginEntity` / `CommitEntity` frame per entity, with sections from
multiple DTOs interleaved between the frame markers. The codegen path
gains no equivalent in this ADR; stacking is a runtime composition and
its natural home is the reflect-side API.

`RowComposer` exposes a five-method state machine:

```go
m := marshallreflect.NewRowComposer(dml, lookup)
for ... {
    if err := m.BeginRow(plainOwner); err != nil { ... }    // opens entity, plain + plainOwner's sections
    if err := m.AddSections(other);   err != nil { ... }    // adds another DTO's sections (zero or more)
    // — optional cardinality-filtered variants of AddSections —
    m.AddSingleValueAttributes(row)                          // emits only size-1 attributes
    m.AddMultiValueAttributes(row)                           // emits only size->1 attributes
    if err := m.CommitRow();          err != nil { ... }    // closes entity
}
```

State transitions: `Initial → InRow` on `BeginRow`; `InRow → InRow`
on any `Add*` call; `InRow → Initial` on `CommitRow`. Any mis-sequenced
call returns a clear error without touching the DML.

`AddSingleValueAttributes` and `AddMultiValueAttributes` partition the
emit by **runtime value-cardinality of each attribute**: scalar fields
and `Option[T]` with `Has=true` always go through the single-value
variant; container / roaring fields contribute to either variant
depending on their runtime length (len 1 → single-value pass; len > 1
→ multi-value pass; empty → no emit either way). Explode-shaped fields
always emit through the single-value variant (each element is its own
size-1 attribute). Sections whose fields all fail to match a given
filter open no `BeginSection` frame in that pass.

Chaining the two variants across multiple DTOs in one row produces
the per-section `1,1,…,>1,>1,…` attribute ordering. This is the
runtime-cardinality refinement of D2's static field-class partition
— D2 sorts fields scalar-first within each section; D1's filtered
emits sort attributes single-value-first within each section across
multiple DTOs in one entity.

Plain-column ownership is **explicit per row**: only `plainOwner`'s
DTO drives plain emission. Other DTOs passed via `AddSections`
contribute only their sections; their plain declarations are ignored.
The cross-DTO plain-shape agreement check of the earlier batch-shaped
draft drops out — each row picks its own plain owner, and rows can
legitimately use different DTOs as the plain owner across the loop.

Sections may repeat across DTOs within one entity without protocol
violation: the DML state machine permits
`InEntity → InSection → InEntity → InSection` re-entry, so two DTOs
both declaring section `Foo` cleanly produce two
`BeginSectionFoo`…`EndSection` cycles in the same entity. We do
**not** attempt to merge them at marshal time — each DTO's section
run is emitted as the DTO declares it. Within one row the order is
"plainOwner's sections first, then each `AddSections` call's sections
in invocation order".

Pointer rows (`*T`) are accepted — `BeginRow` / `AddSections`
dereference once before plan resolution.

### D2 — Within-section scalar-first attribute ordering

Within each section group, we stable-sort fields so

- `shapeScalarBegin` and `shapeScalarBeginSingle` fields appear first
  (single-attribute, single-value emit);
- `shapeContainer` and `shapeExplodeBegin` / `shapeExplodeBeginSingle`
  fields appear last (multi-attribute or multi-value emit);
- consts (always scalar-shaped, single attribute) group with the
  scalars;
- declaration order is preserved within each class (stable sort).

Section ordering inside the entity is unchanged — it still follows
DTO declaration order. The sort lives in the shared `computeGroups`
path (consumed by both marshallgen emit and marshallreflect marshal)
so the two paths cannot drift.

The wire format changes byte-for-byte versus the current
implementation. Existing persisted outputs (none at present per
2026-05-26 confirmation) would have to be re-emitted; in-memory
round-trip parity is preserved.

### D3 — Cover all eight membership channels via static lw: flags

We extend the lw: tag flag set to cover every channel exposed by
`dml.runtime.AddMembership*P`. The current `,verbatim` flag is
retained for backward compatibility and is treated as an alias for
the new `,lowCardVerbatim`. The new flag set:

| Flag                              | Channel                               | DTO carrier model                              | Status in this ADR |
|-----------------------------------|---------------------------------------|------------------------------------------------|--------------------|
| _(none)_                          | `LowCardRef`                          | scalar `T` matching the section's value type    | Cut 1              |
| `,verbatim` / `,lowCardVerbatim`  | `LowCardVerbatim`                     | scalar `T`                                      | Cut 1              |
| `,highCardRef`                    | `HighCardRef`                         | scalar `T`                                      | Cut 1              |
| `,highCardVerbatim`               | `HighCardVerbatim`                    | scalar `T`                                      | Cut 1              |
| `,lowCardRefParametrized`         | `LowCardRefParametrized`              | two-field DTO: value sibling + `Parametrized` carrier sibling      | Cut 2 (deferred)   |
| `,highCardRefParametrized`        | `HighCardRefParametrized`             | two-field DTO: value sibling + `Parametrized` carrier sibling      | Cut 2 (deferred)   |
| `,mixedLowCardRef`                | `MixedLowCardRef(uint64,[]byte)`      | two-field DTO: value sibling + `MixedLowCardRef` carrier sibling   | Cut 2 (deferred)   |
| `,mixedLowCardVerbatim`           | `MixedLowCardVerbatim(uint64,[]byte)` | two-field DTO: value sibling + `MixedLowCardVerbatim` carrier sibling | Cut 2 (deferred) |

The four Cut-1 channels share the same DTO shape: one Go field
carrying the section's value type. The channel flag selects which
`AddMembership<Suffix>P` method receives the membership identity
(`kindXxx` from the lookup registry for Ref channels, `[]byte("name")`
literal for Verbatim channels) — but the DTO grammar is otherwise
unchanged from the existing LowCardRef / LowCardVerbatim implementation.

The four Cut-2 channels are accepted by the parser's flag token
table but rejected with a "not yet implemented" parse-time error
that points back to this ADR. Their wire shape requires the DTO to
surface both a section value (which `BeginAttribute(value)` carries)
and additional membership-side data (the params blob, and for Mixed
channels also a uint64 id, which the chained
`AddMembership<Chan>P(...)` carries). This is not expressible with a
single Go field, so we plan a two-field DTO model — value field + sibling
carrier field sharing the same lw: `(membership, section, channel)`
triple — implemented in a follow-up commit.

Cut-2 carrier struct shapes (non-generic; will live in
`public/semistructured/leeway/marshalltypes`):

```go
type Parametrized        struct { Params []byte }
type MixedLowCardRef     struct { Id uint64; Params []byte }
type MixedLowCardVerbatim struct { Id uint64; Params []byte }
```

In Cut 2 the parser pairs sibling fields by matching lw:
`(membership, section, channel)` and identifies each role by Go field
type (any field whose type is one of the marshalltypes carriers is the
carrier sibling; the other is the value sibling). The codec then
emits

```go
sec.BeginAttribute(c.<ValueField>[i]).
    AddMembership<Chan>P(<id-or-bytes>, c.<CarrierField>[i].Params).
    EndAttributeP()
```

with the per-channel argument list matching the
`AddMembership<Chan>P` signature.

### Subsidiary design decisions

- **SD1 — Section-channel uniformity generalises, doesn't relax.** All
  fields targeting one section must still agree on a single channel.
  The invariant moves from "all Verbatim or all Ref" (2 values) to "all
  fields agree on one of the 8 channels". Mixed sections remain
  rejected; the read-side iterator type is still uniquely determined
  per section.

- **SD2 — Read-side dispatch picks the matching `GetMembValue<Channel>`
  method.** The codegen emitter and the reflect dispatcher both
  consult the section's channel (computed from the Plan once per
  section) and call exactly one of `GetMembValueLowCardRef` /
  `GetMembValueLowCardVerbatim` / … / `GetMembValueMixedLowCardRef` /
  `GetMembValueMixedLowCardVerbatim`. Mixed-channel attributes that
  carry a `(uint64, []byte)` pair are read via the `…HighCardParams`
  Seq2 accessor (`GetMembValueLowCardRefHighCardParams` /
  `GetMembValueLowCardVerbatimHighCardParams`) when params must be
  projected back into the carrier struct; the simpler `Seq[uint64]` /
  `Seq[[]byte]` accessor handles cases where params can be discarded
  on the read side. The read mapping is recorded in the wrapper /
  generator so each channel has exactly one read-time pairing.

- **SD3 — Plain-column ownership is per-row, explicit at BeginRow.**
  `RowComposer.BeginRow(plainOwner)` declares one DTO as the plain
  owner for *that row*; `AddSections` ignores the secondary DTOs'
  plain declarations. `PlanFor[T]()` stays per-type-pure (no implicit
  dependencies between DTOs) and a DTO can serve as plain owner in
  one row, sections-only in another, or both. The earlier
  batch-shaped draft's cross-DTO plain-shape agreement check drops
  out as a consequence — different rows can pick different plain
  owners freely.

- **SD4 — Stacked DTOs may share section names within one row.** Two
  DTOs added to the same entity both declaring section `Foo` produce
  two cycles (`BeginSectionFoo`…`EndSection`). We do not deduplicate
  or merge. Read-back projects each DTO's section content into the
  matching DTO's accumulator independently — order within a section
  cycle reflects the DTO that emitted it, not a stack-level merge.

- **SD5 — Codegen stacking is out of scope for this ADR.** A codegen
  `<Kind>BuildStackedEntities` analogue would need static composition
  of the DTO list, which is unusual in practice; stacked composition
  is dynamic by nature. The reflect path covers the use case at the
  cost of per-row reflection — acceptable because stacked emission
  is a configuration / wiring path, not a hot bulk write path. If a
  hot stacked path appears, ADR-0008 admits a future addition; the
  Plan vocabulary already supports it.

- **SD6 — Ordering is within-section only, by `shapeBegin` class.** We
  do not reorder sections inside the entity, and we do not relax the
  "stable within class" rule. Authors retain control over the relative
  order of two scalar fields and of two container fields; the only
  rearrangement is the partition between the classes. This minimises
  authoring surprise.

- **SD7 — Cut-2 carrier fields still drive one attribute per pair.** A
  paired (value, carrier) DTO emits one attribute per row (or N per
  row under `,explode` on the value field); the carrier's `Id` and
  `Params` are arguments to the same `AddMembership<Chan>P` call, not
  two separate attributes. The read-side projection re-assembles the
  carrier from the Seq / Seq2 iterator.

- **SD8 — Empty / zero `Params` is wire-emitted, not elided
  (Cut 2 semantics).** Unlike empty `[]T` (splice semantics — zero
  attributes), an empty `Params` blob on a present carrier emits the
  attribute with an empty params bytes argument. The carrier-field
  presence is the signal of "this attribute is here"; the params
  being empty is a downstream consumer concern.

- **SD9 — Backward-compatible flag aliasing for `,verbatim`.** Existing
  DTOs using `,verbatim` continue to compile. The parser accepts both
  forms and normalises to a single internal representation
  (`LowCardVerbatim`). Documentation prefers the new explicit form for
  new code.

- **SD10 — Cut 1 / Cut 2 staging is parse-time-enforced, not feature-
  flagged.** The four deferred channels are recognised by the
  parser's flag table so users get a clear "not yet implemented"
  error pointing to this ADR, rather than an obscure mismatch
  downstream. Cut 2 lands as a follow-up that extends the parser
  with sibling-pair recognition and the carrier-struct type set —
  no API churn against Cut 1 callers.

## Alternatives

- **Stacked entities as a Go interface tag on each DTO.** Each DTO
  would declare which other DTOs it composes with via a type-level
  constraint. Rejected as too coupling: it forces every DTO to know
  its peers, defeating the composability win. Composition is a
  per-call concern, not a per-DTO one.

- **Batch-shaped MarshalStack(dml, []any{rowsA, rowsB}, lookup).**
  The earlier draft of D1 took a heterogeneous list of `[]TX`
  batches and emitted one entity per row index across all batches,
  with cross-batch plain-shape agreement and row-count checks.
  Rejected in favour of `RowComposer` because the batches-as-
  rectangles shape constrains callers to materialise every DTO's
  rows up front in matched-length slices. `RowComposer` lets the
  caller drive the row loop and compose each entity independently —
  more flexible for streaming, conditional composition, and
  per-row-varying DTO mixes.

- **Sort by section instead of within section (cross-section
  reordering).** Visually clearer in the wire dump but interacts
  badly with D1 (the per-row composer would have to re-cluster
  sections to land in a global order, defeating its incremental
  nature), and the reordering benefit is marginal because attribute
  locality matters per section, not per entity. Rejected as
  gratuitous.

- **`LeewayNativeLeaf` umbrella as a dynamic per-row channel selector
  (a discriminated-union Go field).** The original sketch from the
  user's first draft considered a sum type that lets the channel vary
  per row. Rejected for now: the static-flag form covers the same
  expressivity at lower complexity (no runtime switch in the inner
  loop, no read-side dispatch over a value-level discriminant). A
  future ADR can introduce a dynamic carrier if a concrete use case
  arrives. The umbrella name is dropped — no Go symbol called
  `LeewayNativeLeaf` is introduced.

- **Generic carriers `Parametrized[T any]` /
  `MixedLowCardRef[T any]` carrying both value and metadata in one
  Go field.** Considered for Cut 2 alongside the two-field sibling
  model. Rejected because generic struct types as DTO field types
  require the reflect classifier to recognise parametric type names
  and extract `T` via FieldByName("Value").Type — workable but
  visibly more complex than the sibling-pair model that reuses the
  existing multi-sub-column pairing machinery. If a future use case
  shows the two-field model is too verbose, generic carriers remain
  on the table as a Cut-3 refinement.

- **Relax the "one channel per section" invariant to allow mixed
  channels in one section.** Lets `MixedLowCardRef` carriers coexist
  with `LowCardRef` scalars in one section. Rejected as too
  expensive: the read-side iterator type becomes section-content-
  dependent, the codegen emit must walk every per-channel accessor
  per section, and the wire-form ambiguity ("which channel produced
  this membership?") would force authors to declare which channel
  each lw: field reads from on the wire — i.e. essentially the same
  per-field flag we already pay, just paid twice.

- **Single Marshal entry point with options instead of a separate
  composer.** `Marshal(dml, rows, lookup, MarshalOpts{Stack: [...]})`.
  Rejected on API legibility — the stacked path is meaningfully
  different (caller-driven row loop, mixed DTO types per entity);
  a dedicated `RowComposer` type makes the call site read better
  and gives the state machine somewhere natural to live.

## Consequences

### Positive

- **Composable DTOs.** Independent fragments (audit, spatial, facts)
  can live in their own DTO and be stacked at marshal time, removing
  the god-DTO anti-pattern.
- **Predictable attribute locality.** Scalars-first ordering gives
  downstream readers a stable layout: fixed-size lookups land at known
  positions; container scans bunch together.
- **Full channel coverage at the codec level.** Every channel the
  protocol exposes is reachable from a DTO. New schemas with
  `HighCardVerbatim` paths or parametrized memberships no longer need
  to bypass marshallgen.
- **Carrier-struct ergonomics.** `MixedLowCardRef{Id: ..., Params: ...}`
  reads naturally at the DTO call site; the field's Go type signals
  intent without sub-column ceremony.

### Negative

- **State-machine errors are runtime, not compile time.** Calling
  `AddSections` before `BeginRow` or `BeginRow` while already in a
  row returns a descriptive error but is a runtime check.
  Mitigation: clear messages naming the missing transition; tests
  cover each mis-sequenced call.
- **Section-channel uniformity check grows from binary to 8-way.** The
  parser's uniformity validation now compares a channel enum, not a
  bool. Marginal complexity, but the failure message must name both
  the offending field and the channel it conflicts with.
- **Codegen stacking is unaddressed.** Hot stacked writes go through
  the reflect path's per-row reflection cost. We accept this on the
  assumption that stacked composition is a wiring concern, not a hot
  loop.
- **Wire bytes change for D2.** Existing in-tree round-trip tests
  must be regenerated; any external consumer holding ad-hoc dumps
  must accept the new layout.

### Neutral

- **`,verbatim` alias coexists with `,lowCardVerbatim`.** Both are
  acceptable; documentation prefers the explicit form. The parser
  normalises to one internal representation so downstream code sees
  one channel value.
- **Splice semantics are unchanged.** Empty slices, empty roaring
  bitmaps, and `Option[T].Has=false` continue to emit zero attributes.
  Mixed carriers' `Params=nil` is *not* splice (the carrier presence
  is the signal, not the params content) — see SD8.
- **Multi-membership read asymmetry between codegen and reflect
  (already documented in marshallgen/EXPLANATION.md) is untouched by
  this ADR.** Codec-wire round-trip parity is preserved; cross-
  producer multi-membership behaviour diverges in the same way it
  does today.

## Open questions

Tracked as named follow-ons, not gates on this ADR:

1. **Codegen path for stacked entities.** Whether the `Plan`
   vocabulary should grow a "stacked group" descriptor so a future
   codegen helper can emit a typed stacked builder. Deferred until a
   hot stacked write surfaces.
2. **Default channel for new schemas.** Whether `,lowCardRef` should
   become explicit (no default) to align with the other six explicit
   flags. Deferred — existing DTOs would all need updating, and the
   default is unambiguous because it's the only one with no flag.
3. **Cut 2: Parametrized / Mixed channels via two-field DTO.** The
   four deferred channels (`LowCardRefParametrized`,
   `HighCardRefParametrized`, `MixedLowCardRef`,
   `MixedLowCardVerbatim`) need a sibling-pair DTO model: one field
   for the section value, one field for the carrier (`Parametrized`
   for non-mixed, `MixedLowCardRef` / `MixedLowCardVerbatim` for
   Mixed). Parser pairs by `(membership, section, channel)` triple
   and identifies roles by Go field type. Read-side reconstruction
   pulls value from `GetAttrValueValue` and the carrier data from
   the matching Seq2 `GetMembValue<Chan>HighCardParams` iterator
   (or Seq for non-Mixed). Specified in this ADR's Cut-2 entry above;
   implementation lands in a follow-up commit.
4. **Mixed channels under `,explode`.** *(Resolved 2026-06-05 — see Updates;
   lifted together with Option / container value shapes.)* The shape matrix
   admits `,explode` on a value slice paired with a `[]MixedLowCardRef` slice
   carrier; the per-element emit calls the matching `AddMembershipMixed…P` once
   per element. Covered by marshallgen emit tests + marshallreflect round-trips.
5. **Read-side multi-membership fix (the existing asymmetry).** Not in
   scope here; tracked in the EXPLANATION.md "Read-side asymmetry"
   section.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## Updates

### 2026-05-31 — DTO model extracted to the `mappingplan` package

Everything plan-related moved out of `marshallgen` into a new sibling
package `public/semistructured/leeway/mappingplan`: the parsed `Plan`, the
`lw:` tag grammar (`SplitLW`), per-field validation and assembly
(`PlanBuilder`), the `MembershipChannel` enum, section grouping
(`ComputeGroups` / `SectionGroup`), and field-shape classification
(`ClassifyBegin`, `IsFixedByteArray`). The decisions in this ADR are
unchanged — only the code's home moved.

`marshallgen` is now the go/ast front-end plus emitter, and
`marshallreflect` the reflect front-end plus runtime codec; both depend on
`mappingplan` and no longer on each other. `marshallreflect` no longer
imports `marshallgen` (nor, transitively, `go/ast` / `go/parser`). Symbols
this ADR spells `marshallgen.Plan` / `marshallgen.SplitLW` /
`marshallgen.MembershipChannel*` now live in `mappingplan` (e.g.
`mappingplan.Plan`).

### 2026-05-31 — Strict 1:1 plain-column mapping

Plain (entity-header) columns now map **1:1** onto the entity builder's
setters: a plain field's Go type *is* the setter argument type, and the
codec inserts no conversion on either side. This makes the three codec
packages schema-agnostic for plain columns the same way they already were
for tagged values — previously the plain path was coupled to the
`runtime.facts` layout.

The setter *names* are the stable leeway entity contract; only their
argument *types* vary per table, taken verbatim from the DTO:

- `id` (+ optional `naturalKey`) → `SetId`. Arity follows the declared
  columns: `SetId(id)` when no `naturalKey` is declared, `SetId(id,
  naturalKey)` when it is. The Go types are whatever the DTO declares
  (e.g. `uint64` id, `[]byte` or `[32]byte` natural key).
- `ts` → `SetTimestamp(ts)`; `expiresAt` → `SetLifecycle(expiresAt)`,
  each with the DTO field's type.

`mappingplan.PlainArrowArrayType` is the single source of truth for which
Go types a plain column may carry and the Arrow array each is read from;
it drives validation, codegen emit, and the reflect read. The supported
set is the scalar leaf types plus `string`, `[]byte`, `time.Time`, and
fixed `[N]byte`. `time.Time` is the one type whose read reconstructs from
Arrow's physical int64-nanos timestamp — that is Arrow's storage form,
not a DTO convenience.

Plain fields are **mandatory**: `option.Option[T]`, slices, and roaring
bitmaps are rejected on a plain column (the splice / zero-or-one semantics
that motivate `Option` belong to tagged values).

What this drops versus the earlier semantics (uninteresting at alpha — no
persisted wire to preserve): the facts-specific type constraints (id must
be `uint64`, ts must be `time.Time`/`int64`, naturalKey `[]byte`/`string`)
and the implicit conversions they implied (int64-nanos→`time.Time`,
`string`→`[]byte`). A DTO targeting `runtime.facts` must now declare the
types the facts builder actually accepts — `SetId(uint64, []byte)`,
`SetTimestamp(time.Time)`, `SetLifecycle(time.Time)` — directly.

Migrated in this change: `capabilitygrant` and `errkind` move their `ts` /
`expiresAt` / `CapturedTs` from `int64` to `time.Time`; `m1fixture` gains a
`naturalKey` so its `SetId` matches the facts 2-arg form.

A follow-up extends the migration to the remaining facts DTOs that
modelled their `ts` as `int64` unix nanos — the `task*` / `watch*` /
`grant*` / `dialogreply` / `persistreply` / `inflightsnapshotreply`
codecs. Each renames its `AtNs int64` plain `ts` to `At time.Time` and
gains a `naturalKey` (nil by default; the old emit special-cased a missing
key as `SetId(id, nil)`, which strict 1:1 no longer does). The
task-supervision stack that produces and consumes these DTOs (`fsbroker`,
`task/{handle,inflight,observer,spawn,supervisor}`, `taskmonitor`) converts
at the DTO boundary — `time.Unix(0, ns)` / `time.UnixMilli(ms)` on
construction, `.UnixNano()` / `.UnixMilli()` / `.IsZero()` on read —
keeping its internal millisecond bookkeeping unchanged.

These DTOs also travel over the keelson bus (sparse CBOR), whose canonical
encoder previously emitted `time.Time` as integer Unix seconds —
truncating the sub-second capture instants several of these events carry.
`buscodec` now encodes time as RFC3339 with nanosecond precision so the bus
round-trip stays lossless (producers normalise to UTC to keep the encoding
deterministic). The leeway facts wire still lands `ts` as a u32-seconds
DateTime; only the bus path preserves nanos.

### 2026-06-04 — Cut-2 design: parametrized / mixed channels (mixedLowCardRef landed first)

Cut-2 of D3 — the four channels parse-rejected since the original decision
(`lowCardRefParametrized`, `highCardRefParametrized`, `mixedLowCardRef`,
`mixedLowCardVerbatim`) — is designed and implemented here. All four now parse
and drive through the shared carrier machinery; the staged-rollout parse
rejection is removed entirely (every recognised `lw:` channel flag is now
implemented).

**Channel shapes (grounded in the runtime interfaces).** The write-side
`InAttributeMembership…PI` constraints in `dml/runtime/lw_dml_types.go` and the
read-side accessors in `readaccess/runtime/lw_ra_rt_types.go` give two distinct
shapes:

- *Parametrized* (`lowCardRefParametrized`, `highCardRefParametrized`): the
  membership is a single opaque blob. Write
  `AddMembership<X>ParametrizedP(blob []byte)`; read
  `GetMembValue<X>Parametrized() iter.Seq[[]byte]`.
- *Mixed* (`mixedLowCardRef`, `mixedLowCardVerbatim`): a `(value, params)`
  pair. Write `AddMembershipMixed…P(idOrName, params []byte)`; read either the
  split `GetMembValueMixed…` + `…HighCardParameters` accessors or the combined
  `GetMembValue…HighCardParams() iter.Seq2`.

This refines the original sketch: the membership identity for these channels is
**per-row carrier data**, not a registry-resolved `kindXxx`. `mixedLowCardRef`
emits `AddMembershipMixedLowCardRefP(carrier.Id, carrier.Params)` with both
taken from the row — there is no lookup, and `KindVar()` returns "" for the
channel.

**Carrier types (`marshalltypes`).** A new sibling package
`public/semistructured/leeway/marshalltypes` holds the plain carriers (no
generics, no methods) — one per channel family:

```go
type MixedLowCardRef      struct { Id   uint64; Params []byte }
type MixedLowCardVerbatim struct { Name []byte; Params []byte }
type Parametrized         struct { Params []byte }
```

Each front-end recognises a carrier by its Go type; the channel's
`CarrierValueField` (`Id` / `Name`, or `""` for the params-only `Parametrized`)
selects which field holds the membership value and discriminates the mixed
channels' `Seq2` read from the parametrized channels' single `Seq`.

**DTO grammar (sibling pair).** A Cut-2 attribute is two Go fields sharing one
`(membership, section, channel)` triple — a value field carrying the section's
value type and a carrier field whose Go type is a `marshalltypes` struct:

```go
Reading  uint32                        `lw:"sensor,u32Array,mixedLowCardRef"`
ReadingC marshalltypes.MixedLowCardRef `lw:"sensor,u32Array,mixedLowCardRef"`
```

`mappingplan.PlanBuilder` recognises the carrier by Go type, pairs it with the
value field on the shared triple (the carrier occupies no value sub-column),
and relaxes the in-DTO `(membership, sub-column)` uniqueness rule for exactly
this pair. The pair becomes one `TaggedField` whose new `CarrierField` /
`CarrierType` name the sibling; the carrier is stored as its own SoA column but
emits no attribute of its own.

**Read-side dispatch — one membership per mixed/parametrized section.** Cut-1's
reader matches each attribute against a *known* membership id/name. A
parametrized membership is an opaque per-row blob and a mixed membership's id is
per-row carrier data, so neither can be matched against a fixed identity. Cut-2
therefore constrains a mixed/parametrized section to a **single value+carrier
membership**: every attribute belongs to that one field, and the reader consumes
them in order — value via `GetAttrValueValue`, carrier via the `Seq2` combined
accessor (`GetMembValueLowCardRefHighCardParams` for mixed-ref). `Finish`
rejects a second membership in such a section. This is stricter than SD1's
general per-section channel uniformity, which still holds.

**Scope — all four Cut-2 channels landed.** The mixed pair (value + params
carrier, `Seq2` read) and the parametrized pair (params-only `Parametrized`
carrier, single-`Seq` read) are implemented across mappingplan + marshallgen +
marshallreflect. `mixedLowCardRef` round-trips end-to-end against anchor's
`symbol` section (anchor declares `MembershipSpecMixedLowCardRefHighCardParameters`).
The other three have no boxer schema pairing a *simple single-value* section
with a matching RA reader — anchor declares only mixed-ref; the example
testtable's mixed-verbatim sections are multi-sub-column / multi-membership with
no reader for its `string` section; and no generated schema declares a
parametrized section at all — so they are verified by the marshallgen emit tests
plus marshallreflect write/read mock tests over the shared carrier path (each
differs from mixed-ref only in the carrier's value field: `Name []byte`, or
params-only).

**Interface fix applied for `mixedLowCardVerbatim`.** The hand-written
`InAttributeMembershipMixedLowCardVerbatimPI` (and its `[A]` variant) declared a
`uint64` first argument, while the generated DML emits `([]byte, []byte)` and
the read side is `Seq2[[]byte, []byte]` — a latent bug with zero users.
Corrected to `([]byte, []byte)` here, alongside a copy-paste fix in the read
interface `…LowCardVerbatimHighCardParamsI` (it embedded the Ref variant). The
carrier is `marshalltypes.MixedLowCardVerbatim{Name []byte, Params []byte}`.

**Unchanged.** SD7 (one attribute per pair), SD8 (empty `Params` is wire-
emitted, not spliced), and the value-side splice semantics hold as written. The
flag table and the Cut-1 / Cut-2 staging mechanism (SD10) are unchanged except
that `mixedLowCardRef` moves from rejected to implemented.

### 2026-06-05 — Carrier value shapes lifted beyond scalar T (resolves OQ#4)

Open question #4 ("Mixed channels under `,explode`") is resolved, and the lift
generalises past explode. A carrier (mixed / parametrized) value field may now
be any of:

- a scalar `T` (unchanged);
- `option.Option[T]` (zero-or-one attribute);
- a container `[]T` (one attribute carrying N values); or
- `[]T,explode` (N attributes, one value each).

`*roaring.Bitmap` stays rejected on a carrier channel: a bitmap iterates in
sorted order with no stable element index, so there is no well-defined
element-wise pairing with a carrier slice. Scalar / Option / container `[]T` /
`,explode` cover the need; roaring does not.

**Carrier multiplicity mirrors the attribute count.** Every shape except
`,explode` emits one carrier per attribute and pairs with a scalar
`marshalltypes.X` carrier field. `,explode` emits N attributes and pairs with a
slice carrier `[]marshalltypes.X`, zipped element-wise with the value slice.
The carrier's slice-ness comes from its Go *type*, not a flag — `,explode`
stays on the value field only. `mappingplan.PlanBuilder.Finish` rejects a
multiplicity mismatch (an exploded value without a slice carrier, or a
non-explode value with a slice carrier).

**Length agreement is a runtime check.** The value and carrier are independent
Go fields, so `len(value) == len(carrier)` cannot be verified at plan time.
Both the generated `<Kind>BuildEntities` and `marshallreflect.Marshal` guard it
per row and return a clear error on mismatch rather than panicking.

**Empty container + carrier follows splice, not SD8.** An empty `[]T` container
value emits zero attributes (the existing splice rule), so its carrier is not
observable on the wire — SD8's "carrier presence is the signal" applies per
*emitted attribute*, and an empty container emits none. `,explode` is
symmetric (empty value slice → zero attributes → empty carrier slice). Scalar
and Option carriers are unaffected.

**Scope.** Landed across `mappingplan` (a `CarrierIsSlice` bit on the shape /
field, the `[]marshalltypes.X` classifier in both front-ends, the
multiplicity + roaring checks in `Finish`) + `marshallgen` + `marshallreflect`.
The change is **additive**: the four pre-existing scalar-carrier code paths
emit byte-identically, so every checked-in `.out.go` regenerates unchanged; the
new shapes are exercised by `marshallgen` emit tests and `marshallreflect`
write/read round-trips over the shared carrier path (no generated boxer schema
pairs a non-scalar value with a carrier reader yet). The one-membership-per-
carrier-section rule (2026-06-04 Cut-2 update) and SD1's per-section channel
uniformity are unchanged.

### 2026-06-07 — Canonical-native field types (the Go DTO becomes one front-end)

*No codec has a persisted or in-tree consumer that must survive, so this is a
clean teardown of the Go-type field model — not a migration. Everything below
deletes API rather than bridging to it.*

A plan field's value type **is a canonical type** (`canonicaltypes.PrimitiveAstNodeI`).
The struct-tag and reflect paths are two front-ends that produce it; the Go DTO
is one implementation, not the definition of a plan's types.

`FieldShape` collapses to the canonical type plus presence — `GoType`,
`IsSlice`, and `IsRoaring` are **removed**:

- array (`[]T`) and set (`*roaring.Bitmap`) are the canonical `ScalarModifier`
  (`HomogenousArray` / `Set`);
- the Go type is *derived* 1:1 from the canonical via
  `canonicaltypes/codegen.GenerateGoCode` — the codec emitter and the playground's
  Go preview both call it, so no Go type is stored anywhere in the plan;
- `IsOption` stays: presence / cardinality is not a value type (there is no
  nullable `ScalarModifier`).

`PlanBuilder.AddField` takes a canonical node, not a Go type. The front-ends do
the Go→canonical (N:1) classification (`marshallgen.classifyType` /
`marshallreflect.classifyReflectType` yield a canonical + the option flag);
`GenerateGoCode` is the canonical→Go (1:1) inverse. Both directions already
exist — the rewiring deletes the Go-type/shape-flag triplet rather than adding
machinery.

Only the **type** axis changes here. `section` (`symbol` / `text` / `blob` / …,
the `<section>` lw segment) and `valueaspects` (value semantics) are untouched;
the orthogonal membership axis is reworked in the next entry.

**Why this is simpler, not more.** Today a field's type is a Go string whose
canonical meaning is resolved downstream against a target schema; canonical-native
puts the one authoritative type *in the plan*, deletes the Go-type + two shape
bits, and reuses two mappings that already exist (`GenerateGoCode` canonical→Go;
front-end classification Go→canonical). The `mappingplanview` editor then authors
the canonical type directly via `canonicaltypeedit` (ADR-0067) — no bridge, no
decomposition.

**Implementation.** `mappingplan` (`FieldShape` / `PlanBuilder` / `Plan`), both
front-ends, and the codec emit are rewritten canonical-native in one pass (no
consumers to migrate; checked-in `.out.go` regenerate). The editor change falls
out of the core change rather than prototyping a separate bridge.

### 2026-06-07 — `membership` package: value vs. representation; primary-locates read

The membership axis is split so the delicate part — mapping strings to
membership values — lives alone, and the write and read paths take their
simplest shapes.

**New `membership` package (value + representation, role-agnostic).** Promote
`MembershipValue` out of `membershiprole`, slimmed to the wire identity
(`Kind` / `LowCard` / `Ref` / `Verbatim` / `Params`) — a comparable struct that
*is* the attribute locator key — with `MembershipKindE` and `IsPlaceholder`.
Add its reason for being: the **string↔value mapping** — a *resolver*
(0..n strings → values) and a *renderer* (value → string, with the
`RefFormatter` / `VerbatimFormatter` injected here). The `HumanReadable*` fields
leave the struct; representation is the renderer's output, not stored state.
That is the value-population vs. string-representation split.

**Write — one membership specifier per field** (already enforced: per-field
uniqueness + per-section channel uniformity; canonical-native plans keep it).
No 0..n on the write path.

**Read — primary locates, secondary annotates.** Only primary memberships are
discriminative, and any one of them locates its attribute by key
`(Kind, Ref | Verbatim [, Params when ParamTreatmentIdentity])`; the reader keys
on the first primary it sees and hangs the value plus secondary memberships off
it. This retires the multi-membership read asymmetry the package carried.

**Role stays in ADR-0007** — primary/secondary (`MembershipRoleE`) and param
treatment (`ParamTreatmentE`), the policy that decides discrimination and what
joins the key, remain the `membershiprole` classifier, now over
`membership.MembershipValue`. Layering is acyclic: `membership` ←
`membershiprole` ← consumers (card / streamreadaccess driver / leewaywidgets).
See [ADR-0007](0007-leeway-membership-role-classifier.md).

## References

- [`../../public/semistructured/leeway/mappingplan/`](../../public/semistructured/leeway/mappingplan/) — shared DTO model: `Plan`, `lw:` grammar, `PlanBuilder`, membership channels, section grouping, shape classification.
- [`../../public/semistructured/leeway/marshallgen/`](../../public/semistructured/leeway/marshallgen/) — codegen front-end + emitter over `mappingplan`.
- [`../../public/semistructured/leeway/marshallreflect/`](../../public/semistructured/leeway/marshallreflect/) — runtime-reflection front-end + codec over `mappingplan`.
- [`../../public/semistructured/leeway/marshallgen/EXPLANATION.md`](../../public/semistructured/leeway/marshallgen/EXPLANATION.md) — package-level explainer, including the existing multi-membership read asymmetry note.
- [`../../public/semistructured/leeway/dml/runtime/lw_dml_types.go`](../../public/semistructured/leeway/dml/runtime/lw_dml_types.go) — write-side membership channel interface (`AddMembership*P`).
- [`../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go`](../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go) — read-side `GetMembValue*` accessors that D3's read-side dispatch consumes.
- [`../../public/semistructured/leeway/dml/statemachine.dot`](../../public/semistructured/leeway/dml/statemachine.dot) — DML state machine; the `InEntity → InSection → InEntity` cycle is the protocol basis for D1's repeating-section behaviour (SD4).
