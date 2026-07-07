---
type: adr
status: proposed
date: 2026-07-06
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0110: leeway marshall — carrier-channel memberships in dynamic tuples

## Context

[ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) gave the `lw:`
front-ends the **dynamic-membership tuple** (a slice-of-struct field whose
elements each emit one attribute carrying its own membership) and restricted an
element to **one `@membership` field on a verbatim channel**, deferring two
things it listed as options: ref channels (its Open question 1) and
**per-element carriers** (its option **O2**, rejected as an Alternative).
[ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) resolved the
first — one-or-more memberships on **verbatim or ref** channels, the id carried
directly per element, byte-identical across both front-ends — because the wire
and DML already carried it; the gap was only the front-end rejection.

The **one membership channel family still barred from a tuple is the carrier
(parametrized) family**: `mixedLowCardRef` (id + params), `mixedLowCardVerbatim`
(name + params), and the params-only `…Parametrized` channels
([ADR-0072](0072-leeway-membership-carriage.md) carriage axes;
[ADR-0008](0008-leeway-marshall-extensions.md) Cut-2). Two rejections enforce
it: `goplan.PlanBuilder.AddTupleSliceField` / `AddNestedSliceField` refuse a
carrier channel on an `@membership` / marker field *and* a carrier-typed value
sibling
([`build.go`](../../public/semistructured/leeway/marshall/go/goplan/build.go)),
and `mappingplan.TupleMembership.Channel` documents the same bar.

A consumer now needs it. An entity/graph-integration model places, per row, **N
attributes each holding a value under a membership that also carries per-row
parameters** — the **reified-edge / named-graph-quad / per-value-provenance**
shape: a value qualified by its source dataset, language, or first-seen; or an
edge whose endpoint is qualified. That qualifier is precisely the membership
**params** axis (ADR-0072), and it is per-attribute, N per entity — a tuple.

As in ADR-0109, the **wire, DML, and RA already carry it per attribute.**
`AddMembershipMixedLowCardRefP(id, params)` /
`AddMembershipMixedLowCardVerbatimP(name, params)` /
`AddMembership…ParametrizedP(params)` append one carrier membership per
attribute; the read side exposes the identity
(`GetMembValueMixedLowCardRef` / `…Verbatim`) and the params
(`GetMembValueMixedRefHighCardParameters` / `…VerbatimHighCardParameters`) per
`(entity, attribute)`. The carrier structs already exist as method-free plain
data the codec recognises by type
([`marshalltypes.go`](../../public/semistructured/leeway/marshall/marshalltypes/marshalltypes.go)).
So, exactly as ADR-0109 found for refs, the gap is **only** the two front-end
rejections and the single-channel-family assumption in the codec.

**Why ADR-0103 rejected O2, and why that no longer binds.** O2 scored `−−` on
wire fidelity (C2) and was said to contradict ADR-0101 D2. Both objections were
placement-specific, not fundamental:

- *C2* — that consumer's committed schema declared a plain verbatim (`lv`)
  column, so emitting carrier columns (`lmv` + params) would have broken
  byte-identity with its probe. A consumer that **wants** the params dimension
  declares a carrier-channel section; carrier columns are then the target, not a
  regression. C2 is measured against the *carrier* section's DML loop.
- *[ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) D2* bars carriers as
  **sub-columns**. Here the carrier rides the **membership** axis, which ADR-0109
  already dispatches per-channel independently of sub-columns (the value fields
  keep the first membership's channel only for `SectionGroup.Channel()`
  well-definedness). D2 is untouched.

The residual O2 objection — "the carrier struct duplicates what a plain element
field states directly" — holds only when there are **no** params, exactly the
case ADR-0109 already covers with a plain ref/verbatim field. It does not cover
params, which no plain field can state.

**The base this extends now ships.** ADR-0109's ref/verbatim tuple markers landed
in *both* front-ends (reflect and codegen), along with the additive
`MarkerGoType` plan field the codec uses to bridge a marker's as-written Go type
to its wire type
([`mappingplan.TupleMembership`](../../public/semistructured/leeway/mappingplan/plan.go)).
So this ADR is a **fourth channel family on a live mechanism** — the tuple drivers
(`marshalTupleSection` / `writeTupleSectionDriver`), the per-channel read
distribution, and the marker bridge all exist for ref/verbatim; carriers add one
struct-shaped marker and the params Seq. The nested how-to's
[Carriers](../howto/leeway-marshalling-nested.md#carriers) section is written to
defer to this ADR.

## Design space (QOC)

**Q1 — how does a per-element carrier membership spell its `(identity, params)`
in the element struct?**

- **A1a — a carrier-typed `@membership` field.** The field's Go type is the
  matching `marshalltypes` carrier struct
  (`MixedLowCardRef{Id, Params}`, `MixedLowCardVerbatim{Name, Params}`,
  `Parametrized{Params}`) — or, in the nested front-end
  ([leeway-marshalling-nested.md](../howto/leeway-marshalling-nested.md), whose
  ref/verbatim markers already ship in both codecs), a new carrier marker
  (`lw.MixedRef[P]` / `lw.MixedVerbatim[P]` / `lw.RefParams[P]`).
  One field carries both parts; the codec recognises the carrier by Go type — as
  it already does for the flat sibling — and emits `AddMembership<Carrier>P(…)`.
  No lookup; both front-ends emit the identical call.
- **A1b — a paired carrier value-sibling** (O2's literal spelling: an id
  `@membership` field plus a `[]marshalltypes.Mixed…` sibling sharing the tag).
  Re-introduces the sibling-pairing the tuple grammar exists to remove; two
  fields for one membership.

A1a **dissolves** the deferral the way ADR-0109 A1a did for refs — carry it on
the field, recognised by type — and needs no sibling.

**Q2 — how many carrier memberships per attribute in this cut?**

- **A2a — exactly one (fixed)**, composing heterogeneously with ADR-0109's
  ref/verbatim channels. Covers the reified edge (one ref predicate + one carrier
  qualifier) and per-value provenance (one carrier), the consumer shapes.
- **A2b — also repeated (`[]carrier`).** Physically supported (per-channel
  append), but no consumer needs it, and splitting a repeated carrier's shared
  per-attribute id+params Seqs back into elements needs a length oracle
  (ADR-0109 D4's deferred case).

A2a wins; A2b is an Open question.

## Decision

Extend both `lw:` front-ends and the shared plan layer so a tuple element's
`@membership` field **may be on a carrier channel**, resolving ADR-0103's
option **O2** and completing ADR-0109's channel-family coverage. One carrier
membership per attribute, identity + params carried directly on the field,
composing with ADR-0109's ref/verbatim channels. Implemented once in
`goplan` + `mappingplan`, driven byte-identically by both front-ends.

### D1 — Grammar

An `@membership` field may name a **carrier channel flag**
(`,mixedLowCardRef` / `,mixedLowCardVerbatim` / `,lowCardRefParametrized` /
`,highCardRefParametrized`); its Go type is the matching `marshalltypes` carrier
struct (flat front-end) or the nested marker. Type ↔ channel is
checked as in ADR-0109 D1: a mixed-ref channel wants `MixedLowCardRef` /
`lw.MixedRef[P]`, a mixed-verbatim channel `MixedLowCardVerbatim` /
`lw.MixedVerbatim[P]`, a params-only channel `Parametrized` / `lw.RefParams[P]`;
a mismatch (e.g. a bare `uint64` on a mixed channel, or a carrier struct on a
verbatim channel) is a `PlanFor` error. **Exactly one** carrier `@membership`
field per attribute (A2a); a repeated `[]carrier` is rejected (Open question 1).

### D2 — Direct carriage, no lookup (resolves ADR-0103 O2)

The codec calls `AddMembership<Carrier>P(field.Id/Name, field.Params)` per
attribute — the existing DML method — taking both parts from the field. No
`kindXxx` symbol, no `LookupI`; both front-ends emit the identical call (the flat
sibling already worked this way; ADR-0109 established direct carriage for refs).
`Params` is wire-emitted even when empty (ADR-0008 SD8); carrier **presence** is
the "attribute is here" signal, aligned with the tuple element's own presence.

### D3 — Read model (extends ADR-0109 D3)

Each attribute decodes to one element. A carrier `@membership` field takes its
identity from the channel's identity Seq (`GetMembValueMixedLowCardRef` /
`…Verbatim`) and its params from the params Seq
(`GetMembValueMixedRefHighCardParameters` / `…VerbatimHighCardParameters`) for
that `(entity, attribute)`, assembled into the carrier struct; a params-only
carrier reads its whole identity from the single params Seq. Heterogeneous
channels distribute independently (ADR-0109 D5). Zero attributes → nil slice.

### D4 — Membership axis, not a sub-column (ADR-0101 D2 preserved)

The carrier rides the **membership** axis; value sub-columns are unchanged, and
ADR-0101 D2's bar on carriers as **sub-columns** stands untouched — this ADR
never places a carrier in a sub-column. The carrier **value-sibling** inside a
tuple (`goplan` `build.go` `CarrierType != ""`, A1b) also **stays rejected**:
the tuple grammar carries a carrier only as a membership field (A1a).

### D5 — Validate / drivers

`marshallreflect.Validate` checks the section's DML exposes
`AddMembership<Carrier>P` for the declared carrier channel — a carrier channel
absent from the section's spec fails `Validate`, not at marshal time (ADR-0109
D5). `marshallgen.writeTupleSectionDriver` and
`marshallreflect.marshalTupleSection` emit the carrier call in `@membership`
declaration order alongside any ref/verbatim fields; every tuple channel site
dispatches on the memberships list, the value fields carrying the first
membership's channel only for `SectionGroup.Channel()` (ADR-0109 D6, unchanged).

## Verification

1. **Carrier round-trip** over a carrier-declaring anchor section (one whose spec
   exposes `AddMembershipMixedLowCardRefP`): **N = 0 / 1 / 3** attributes, each
   one `MixedLowCardRef` membership (id + params, including the empty-params SD8
   case), byte-identical to an explicit DML loop (`AddMembershipMixedLowCardRefP`
   ×1 per attribute); `array.RecordEqual` + IPC bytes; `Unmarshal` restores
   id + params in order (N = 0 → nil).
2. **Verbatim + params-only variants**: `MixedLowCardVerbatim` (name + params)
   and `Parametrized` (params-only), each byte-identical and round-tripping.
3. **Heterogeneous** (composes with ADR-0109): one ref predicate + one carrier
   qualifier on one attribute — the reified-edge shape — over the mixed-shape
   `text` section, byte-identical to its DML loop and round-tripping.
4. **Negatives** (both front-ends, shared builder / `Validate`): a carrier
   value-sibling in a tuple → `PlanFor` (build.go, unchanged); a carrier
   `@membership` whose field type mismatches the channel → `PlanFor`; a repeated
   `[]carrier` → `PlanFor` (Open question 1); a carrier channel absent from the
   section's DML → `Validate`. The former ADR-0103 "carrier channel rejected"
   tuple test flips to accepted for the membership form.
5. **gen ≡ reflect**: a `codecdemo` demo carrying a carrier tuple —
   `RecordEqual` + IPC + both cross-decodes.
6. **Byte-stability**: every in-tree `.out.go` regenerates wire-stable; the
   ADR-0103/0109 verbatim/ref tuple paths are untouched.

## Alternatives

- **A1b — paired carrier value-sibling.** Reintroduces the sibling the tuple
  grammar removed (two fields per membership); kept only as the non-tuple
  spelling for static sections. Rejected inside a tuple.
- **Keep O2 rejected; hand-drive the DML.** Leaves the front-ends permanently
  weaker than the DML for the params axis — ADR-0103 O4's weakness, now the sole
  remaining channel-family gap.
- **Repeated carriers now (A2b).** No consumer shape; the shared per-attribute
  id+params Seq split needs a length oracle (ADR-0109 D4). Deferred.
- **Model provenance as extra scalar sub-columns.** Puts per-value provenance in
  the value tuple, not the membership-params dimension — the wrong physical
  column, and it cannot ride an existing carrier-declaring section. The
  membership-params axis (ADR-0072) is the designed home.

## Consequences

### Positive

- Reified edges, named-graph quads, and per-value provenance / qualifiers
  marshal natively into a tuple; the `lw:` front-ends reach DML parity for the
  **last** membership channel family (carriers), closing ADR-0103 O2 and
  completing the coverage ADR-0109 began.
- The nested front-end's carrier marker (`lw.MixedRef[P]` in `[]Attr`, deferred in
  the shipped cut) becomes expressible on the same footing as its shipped
  ref/verbatim markers — this ADR is the carrier extension the how-to's Carriers
  section defers to.

### Negative

- A carrier `@membership` is a struct / marker, a heavier field than ADR-0109's
  scalar; the read grows a params-collect beside the identity-collect; one more
  channel form in the tuple driver / `Validate` matrix.

### Neutral

- ADR-0101 D2 (carriers as sub-columns) and the ADR-0103/0109 verbatim/ref tuple
  paths are untouched; the carrier value-sibling stays a non-tuple-only spelling;
  static and multi-sub-column sections are unaffected.

## Open questions

1. **Repeated carrier memberships** (`[]carrier`, N per attribute) — needs the
   ADR-0109 D4 positional-split rule for the shared per-attribute id + params
   Seqs. Lift on a consumer need.
2. **`ReadRow` / recordstore + plan-derived DDL** — unchanged from ADR-0103 OQ2 /
   ADR-0109 OQ2: tuple kinds (carriers included) stay excluded from
   `<Kind>ReadRow`, and the plan → DDL path has no tuple mapping.
3. **Params-only `Parametrized` read** — its identity *is* the params; confirm
   the D3 assembly reads a params-only carrier as one field (one Seq, no separate
   identity Seq) with the same nil/empty semantics.

## Status

Proposed (2026-07-06). Resolves ADR-0103 option **O2** (per-element carriers) and
completes ADR-0109's channel-family coverage; **not yet implemented** — the
nested how-to's Carriers section defers to it. The infrastructure it extends —
ADR-0109's ref/verbatim tuple markers, the codegen tuple path, and the
`MarkerGoType` bridge — now ships in both front-ends, so this is a fourth channel
family on a live mechanism rather than a new build. Driven behind the shared plan
layer (goplan + mappingplan + marshallgen + marshallreflect), the verification
suite above green and every in-tree `.out.go` wire-stable.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) — dynamic-membership
  tuples; this ADR resolves its option **O2** (per-element carriers).
- [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) — multi-membership
  + ref tuples; this ADR adds the carrier channel family under the same plan
  layer, read model, and byte-parity argument.
- [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) — mixed-shape
  sections; D2's carrier-as-sub-column bar is preserved (D4).
- [ADR-0072](0072-leeway-membership-carriage.md) — membership carriage: the
  identity × params axes the carrier channels realise.
- [ADR-0008](0008-leeway-marshall-extensions.md) — Cut-2 parametrized / mixed
  channels and the SD8 "params wire-emitted even when empty" presence rule.
- [ADR-0074](0074-leeway-marshall-package-layout.md) — marshall / marshalltypes layout.
- [`goplan/build.go`](../../public/semistructured/leeway/marshall/go/goplan/build.go)
  (`AddTupleSliceField` — lift the `@membership`-carrier rejection at `:662`, keep
  the value-sibling rejection at `:631`),
  [`goplan/grouping.go`](../../public/semistructured/leeway/marshall/go/goplan/grouping.go)
  (`TupleSpec.Memberships` / `Channels()`),
  [`goplan/lwtag.go`](../../public/semistructured/leeway/marshall/go/goplan/lwtag.go)
  (carrier channel flag tokens),
  [`mappingplan/plan.go`](../../public/semistructured/leeway/mappingplan/plan.go)
  (`TupleMembership` — carrier `Channel` + the carrier Go-type).
- [`marshalltypes/marshalltypes.go`](../../public/semistructured/leeway/marshall/marshalltypes/marshalltypes.go)
  (`MixedLowCardRef` / `MixedLowCardVerbatim` / `Parametrized`);
  [`marshallgen/emit.go`](../../public/semistructured/leeway/marshall/go/marshallgen/emit.go),
  [`marshallreflect/marshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/marshal.go)
  / [`unmarshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/unmarshal.go)
  / [`validate.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/validate.go).
- Design-target front-end: [leeway-marshalling-nested.md](../howto/leeway-marshalling-nested.md)
  (the nested `lw.MixedRef[P]` spelling this ADR unblocks).
