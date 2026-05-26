---
type: adr
status: proposed
date: 2026-05-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0008: leeway marshall\* extensions — stacked entities, attribute ordering, full channel coverage

## Context

The sibling pair `public/semistructured/leeway/marshallgen` (codegen)
and `public/semistructured/leeway/marshallreflect` (runtime reflection)
ships an annotated-Go-DTO → leeway DML / RA codec built on a shared
`marshallgen.Plan` vocabulary. The current grammar supports

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

We add a `marshallreflect.MarshalStack` entry point that drives one
`BeginEntity` / `CommitEntity` frame per entity, with sections from
multiple DTOs interleaved between the frame markers. The codegen path
gains no equivalent in this ADR; stacking is a runtime composition and
its natural home is the reflect-side API.

`MarshalStack` accepts a `dml`, a `lookup`, and a heterogeneous list of
row-batches (typically passed as `[]any` where each element is a
`[]TX`). At call time it:

1. Resolves the `marshallgen.Plan` for every DTO type in the batch list
   via the existing `PlanFor[T]()` cache.
2. Cross-checks that every plan declares the same plain-column set —
   same column names, same Go types per column, same field-name
   convention. Any disagreement is an error.
3. Cross-checks that every batch has the same row count.
4. For each row index `i`, emits one `BeginEntity` frame, writes the
   plain columns once (read from any plan — they all agree by step 2),
   iterates the DTO batches in order and emits each DTO's section
   groups, then `CommitEntity`.

Sections may repeat across DTOs without protocol violation: the DML
state machine permits `InEntity → InSection → InEntity → InSection`
re-entry, so two DTOs both declaring section `Foo` cleanly produce
two `BeginSectionFoo`…`EndSection` cycles in the same entity. We do
**not** attempt to merge them at marshal time — each DTO's section run
is emitted as the DTO declares it.

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

- **SD3 — Stacked DTOs' plain columns are cross-checked at marshal
  time, not at PlanFor time.** Each `PlanFor[T]()` builds the plan in
  isolation; the cross-DTO agreement check lives in `MarshalStack`'s
  prelude. This keeps `Plan` per-type-pure (no implicit dependencies
  between DTOs) and lets a DTO appear in multiple stacks with
  different siblings.

- **SD4 — Stacked DTOs may share section names.** Two DTOs in a stack
  both declaring section `Foo` produce two cycles
  (`BeginSectionFoo`…`EndSection`) per entity. We do not deduplicate
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

- **Stacked entities as a Go interface tag on each DTO instead of a
  cross-check at marshal time.** Each DTO would declare which other
  DTOs it composes with via a type-level constraint. Rejected as too
  coupling: it forces every DTO to know its peers, defeating the
  composability win. The cross-check is a per-call concern, not a
  per-DTO one.

- **Sort by section instead of within section (cross-section
  reordering).** Visually clearer in the wire dump but interacts
  badly with D1 (stacked DTOs would have to re-cluster their sections
  to land in the global order), and the reordering benefit is
  marginal because attribute locality matters per section, not per
  entity. Rejected as gratuitous.

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

- **Single Marshal entry point with options instead of MarshalStack.**
  `Marshal(dml, rows, lookup, MarshalOpts{Stack: [...]})`. Rejected on
  API legibility — the stacked path is meaningfully different
  (heterogeneous rows, cross-DTO validation, plain-column agreement);
  a dedicated entry point makes the call site read better.

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

- **Plain-column cross-check is a runtime error path with potentially
  surprising failure messages.** When two DTOs in a stack disagree on
  plain shape, the failure surfaces at `MarshalStack` call time, not
  at compile time. Mitigation: clear error messages naming both DTOs
  and the disagreeing column.
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
4. **Mixed channels under `,explode`.** The shape matrix admits
   `,explode` on `[]MixedLowCardRef`; the per-element emit calls the
   matching `AddMembershipMixed…P` once per element. Unexercised
   until Cut 2 lands; first consumer to need it should add the test.
5. **Read-side multi-membership fix (the existing asymmetry).** Not in
   scope here; tracked in the EXPLANATION.md "Read-side asymmetry"
   section.

## Status

Proposed — awaiting review.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [`../../public/semistructured/leeway/marshallgen/`](../../public/semistructured/leeway/marshallgen/) — codegen side of the codec pair.
- [`../../public/semistructured/leeway/marshallreflect/`](../../public/semistructured/leeway/marshallreflect/) — runtime-reflection sibling.
- [`../../public/semistructured/leeway/marshallgen/EXPLANATION.md`](../../public/semistructured/leeway/marshallgen/EXPLANATION.md) — package-level explainer, including the existing multi-membership read asymmetry note.
- [`../../public/semistructured/leeway/dml/runtime/lw_dml_types.go`](../../public/semistructured/leeway/dml/runtime/lw_dml_types.go) — write-side membership channel interface (`AddMembership*P`).
- [`../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go`](../../public/semistructured/leeway/readaccess/runtime/lw_ra_rt_types.go) — read-side `GetMembValue*` accessors that D3's read-side dispatch consumes.
- [`../../public/semistructured/leeway/dml/statemachine.dot`](../../public/semistructured/leeway/dml/statemachine.dot) — DML state machine; the `InEntity → InSection → InEntity` cycle is the protocol basis for D1's repeating-section behaviour (SD4).
