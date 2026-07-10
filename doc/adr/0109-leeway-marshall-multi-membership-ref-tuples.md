---
type: adr
status: accepted
date: 2026-07-05
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-10
---

# ADR-0109: leeway marshall — multi-membership + ref-channel dynamic tuples

## Context

[ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) gave the `lw:`
front-ends the **dynamic-membership tuple**: a slice-of-struct field
(`Texts []LabeledText` tagged `lw:"text"`) whose elements each emit one
attribute of a section carrying its own membership. It restricted an element to
**exactly one** `@membership` field, on a **verbatim** channel only, and
deferred two things: ref-channel memberships (its Open question 1) and
multi-membership (rejected as an Alternative — "dynamic ref memberships via
`LookupI`" — for breaking front-end byte-parity).

Both deferrals now have a consumer. An external ontology-integrated entity model
needs, in **one entity**:

1. **type-lineage** — the entity's ancestor-closure kinds as **many ref
   memberships on one attribute**, so a supertype query ("all `LegalEntity`") is
   a membership filter with no join and no hierarchy walk; and
2. **edge aliasing** — a value under a typed predicate **and** a generic graph
   membership (two memberships on one element), and property values under a
   verbatim label **and** a ref type-id (heterogeneous channels).

The leeway **wire model and DML already carry both.** Each
`AddMembership<Channel>P` appends to a per-channel builder and bumps a per-channel
count, recorded as the per-channel *membership-card* at `completeAttribute`
(anchor `…InAttr.AddMembership…P` / `handleMembershipSupportColumns`); several
calls on one channel record card N, calls on different channels write independent
columns. anchor's sections already declare `LowCardRef` + `HighCardRef` specs.
So, as in ADR-0103, the gap is **only** in the DTO front-ends — two rejections in
`goplan.PlanBuilder.AddTupleSliceField` and single-channel assumptions across the
codec sites.

## Design space (QOC)

Two forks decide the shape; the rest follows.

**Q1 — how does a ref membership resolve per element?** ADR-0103 rejected ref
channels because a ref membership resolves through a compile-time `kindXxx`
symbol the generated `BuildEntities` cannot parameterise per element, and a
reflect-only `LookupI` would make the two front-ends emit different bytes.

- **A1a — carry the id directly as `uint64`** on the `@membership` field. The id
  is per-element data, so `AddMembership<Ref>P(field)` needs no symbol and no
  lookup; both front-ends emit the identical call. Matches consumers already
  holding ontology node-ids, and disambiguates homonymous names
  (`unknownLink.subject` ≠ `email.subject`) because ids are globally unique.
- **A1b — per-element `LookupI`.** Reflect-only (no generated lookup surface) →
  breaks the byte-parity invariant; the exact reason ADR-0103 deferred refs.

A1a **dissolves** the deferral rather than working around it, and needs no
`LookupI` at all (Unmarshal skips the ref-id pre-resolve for tuple fields).

**Q2 — how is an attribute's membership set read back?** ADR-0103 D5 read "one
element per membership value". With several memberships per attribute that no
longer maps to one element.

- **A2a — one element per attribute**, gathering all its memberships into the
  element's `@membership` fields. Byte-stable for the single-fixed case (1
  membership ⇒ 1 element either way) and the only model that round-trips an
  element whose sole slice membership is empty (a `type` attribute with an empty
  ancestor set still writes an attribute; it must read back as one element with a
  nil slice, not vanish).
- **A2b — keep element-per-membership.** Cannot represent a zero-membership or a
  multi-membership attribute as one element; incompatible with the goal.

A2a wins; it **supersedes** ADR-0103 D5.

## Decision

Extend both `lw:` front-ends so a tuple element carries **one or more**
memberships, on **verbatim or ref** channels, resolving ADR-0103's Open
question 1 and its single-membership rule. Implemented once in the shared plan
layer (`goplan` + `mappingplan`) and driven byte-identically by both front-ends.

### D1 — Grammar

An element declares **at least one** `@membership` field. Each names a channel
(per field) via an explicit flag, and its Go type fixes the identity form:

- **verbatim** (`,verbatim` / `,lowCardVerbatim` / `,highCardVerbatim`) ⟺ a
  `string` or `[]byte` field — the literal name embeds on the wire;
- **ref** (`,lowCardRef` / `,highCardRef`) ⟺ a `uint64` field — the id is
  carried directly (D2).

A repeated `@membership` field is a `[]T` of the same element type (`[]uint64`,
`[]string`, `[][]byte`). Carrier / parametrized channels are rejected (their
identity is per-row carrier data, not an element field). `lowCardRef` is added as
an explicit flag token so the ref channel — otherwise the empty default — can be
named at the declaration site; a `string`/`[]byte` field on a ref (or the bare
default) channel keeps ADR-0103's "requires an explicit verbatim channel flag"
error, a `uint64` on a verbatim channel is symmetrically rejected.

### D2 — Ref by direct id, no lookup (resolves ADR-0103 OQ1)

A ref `@membership` field's `uint64` is the membership id; the codec calls
`AddMembership<Ref>P(id)` with the field value. No `kindXxx` symbol, no `LookupI`
— both front-ends emit the identical call, preserving byte-parity. Verbatim stays
the ADR-0103 `[]byte`-name path.

### D3 — Read model: one element per attribute (supersedes ADR-0103 D5)

Each attribute decodes to exactly one element. Its sub-column values are read
positionally (ADR-0101 D5, unchanged); its memberships are distributed to the
element's `@membership` fields **one channel at a time**:

- a channel carrying a single **repeated (slice)** field takes the whole
  per-attribute Seq for that channel (nil when empty);
- otherwise the channel's **fixed** fields each take one value in declaration
  order, and the Seq length must equal the field count exactly — a mismatch is an
  error (`membership count mismatch on read`), never an out-of-range panic.

Zero attributes decode to a nil slice.

### D4 — Per-channel arity (the one descope)

On any single channel an element may declare **either** any number of fixed
fields **or** exactly one repeated field — not a mix, and not two repeated
fields. A slice mixed with any other field on one channel could not be split back
off the shared per-attribute Seq unambiguously. This is an authoring constraint,
not a capability loss: put the extra field on a different channel. Mixing fixed
and slice on **one** channel is deferred (Open questions). Both consumer shapes
fit inside it — type-lineage is one slice, edge aliasing is two fixed — as does a
heterogeneous verbatim+ref element (two fixed-mode channels, one field each).

### D5 — Heterogeneous channels

An element's `@membership` fields may span several channels; each is handled
independently (write appends per channel, read distributes per channel — the
cross-channel call order does not affect the wire). The generated `AttrI` embeds
one `InAttributeMembership<Channel>PI` per channel and the `MembsReadI` one
`GetMembValue<Channel>` per channel; `marshallreflect.Validate` checks the DML
exposes `AddMembership<Channel>P` for **each** channel. A ref `@membership` on a
section whose spec lacks that ref channel therefore fails `Validate` (the method
is absent), not at marshal time.

### D6 — Write / read drivers, byte-identity

Both front-ends emit, per element: `BeginAttribute(<scalars…>)`, the zipped
co-containers (ADR-0101 D4, unchanged), then one `AddMembership<Channel>P` per
`@membership` field in declaration order — one call per slice element for a
repeated field — then `EndAttributeP`. `membership-card > 1` is the natural
outcome (per-channel DML builders). `marshallgen.writeTupleSectionDriver` and
`marshallreflect.marshalTupleSection` share the call order; the decode
(`writeTupleSectionDecode` / `unmarshalTupleSection`) shares the D3
distribution. The value fields carry the first membership's channel only to keep
`SectionGroup.Channel()` and the per-section channel-uniformity check
well-defined; every tuple channel site dispatches on the memberships list
instead.

## Verification

1. **Ref round-trip** over anchor `symbol` (S = 1): a slice `@membership`
   (`[]uint64`) with **N = 0 / 1 / 3** ref memberships per attribute,
   byte-identical to an explicit DML loop (`AddMembershipLowCardRefP` ×N);
   `array.RecordEqual` + IPC bytes; Unmarshal restores order (N = 0 → nil).
2. **Both forms**: repeated **fixed** ref fields over `foreignKey` (two
   `lowCardRef`), and a **heterogeneous** verbatim+ref element over the
   mixed-shape `text` section — each byte-identical to its DML loop and
   round-tripping.
3. **Negatives** (both front-ends, shared builder / Validate): a slice
   `@membership` mixed with another field on one channel → `PlanFor`; a
   verbatim-typed field on a ref channel and a ref-typed field on a verbatim
   channel → `PlanFor`; a ref channel absent from a section's DML → `Validate`;
   a membership count mismatch → `Unmarshal` / generated decode. The former
   ADR-0103 "second `@membership`" and "membership on a ref channel" rejections
   are flipped to accepted.
4. **gen ≡ reflect**: a `codecdemo` demo (`LineageDoc`: slice ref + two fixed ref
   + heterogeneous) — `RecordEqual` + IPC + both cross-decodes.
5. **Byte-stability**: the single-verbatim tuple's write path and interfaces
   regenerate unchanged (only the decode grows the per-channel form); every
   in-tree `.out.go` regenerates wire-stable.

## Alternatives

- **Ref via per-element `LookupI` / `kindXxx`.** The generated `BuildEntities`
  has no per-element lookup surface, so only the reflect front-end could honour
  it — different wire bytes per front-end, the invariant ADR-0103 protected by
  deferring refs. The direct-id form (D2) removes the need entirely.
- **Element-per-membership read (ADR-0103 D5, kept).** Cannot represent a
  zero-membership attribute (an empty ancestor set) or a multi-membership
  attribute as a single element; does not compose with D1.
- **Fixed + slice `@membership` on one channel.** A slice adjacent to a fixed
  field on one channel cannot be split back off the shared Seq without a length
  oracle; deferred rather than adding one (put the fields on distinct channels).
- **Defaulting a bare `@membership` on a `uint64` field to verbatim.** A `uint64`
  cannot embed a literal name; the ref default is the only coherent reading, so a
  verbatim channel on a `uint64` field is rejected instead.

## Consequences

### Positive

- The consumer's type-lineage (many ref memberships per attribute) and edge
  aliasing (repeated / heterogeneous memberships) marshal natively; a supertype
  query becomes a membership filter, and ontology node-ids need no lookup.
- Ref-channel tuples are reached without a lookup surface — the deferral in
  ADR-0103 OQ1 is resolved, not merely scheduled.

### Negative

- A second read model to hold: a tuple element now maps to one **attribute**, not
  one (attribute, membership) pair. The generated decode grows a per-channel
  collect + count-guard where it was a single membership loop.
- One authoring constraint (D4): a repeated `@membership` is the sole membership
  on its channel.

### Neutral

- The single-verbatim tuple path is wire-stable (a special case of D1/D3/D6);
  static and multi-sub-column sections are untouched.
- ADR-0103's exclusivity, presence-signal, and per-element zip rules stand.

## Open questions

1. **Fixed + slice `@membership` on one channel** (D4) — needs a positional split
   rule (e.g. fixed-then-slice with the slice taking the tail). Lift when a
   consumer needs both arities on one channel rather than distinct channels.
2. **`ReadRow` / recordstore + plan-derived DDL.** Unchanged from ADR-0103 OQ2/OQ3
   — tuple kinds stay excluded from `<Kind>ReadRow`, and the plan→DDL path has no
   tuple mapping.

## Status

Accepted (2026-07-10; proposed 2026-07-05) — reviewed in the marshall
consolidation dialogue, see
[ADR-0113](0113-leeway-marshall-nested-primary-consolidation.md); the consumer's
type-lineage encoding remains one of its open modelling candidates, which the
acceptance does not pre-empt. Implemented behind the shared plan layer (goplan +
mappingplan + marshallgen + marshallreflect) with the verification suite above
green; the single-verbatim tuple path and every other in-tree `.out.go` regenerate
wire-stable. Resolves the "Dynamic ref memberships" open question of
[ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) and extends its
single-membership rule.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) — dynamic-membership
  tuples; this ADR extends its grammar (D1), supersedes its read model (D3), and
  resolves its Open question 1.
- [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) — mixed-shape
  multi-sub-column sections (the per-element call sequence and zip rule, reused).
- [ADR-0008](0008-leeway-marshall-extensions.md) — membership channels (the
  verbatim / ref pairs this ADR draws on).
- [ADR-0072](0072-leeway-membership-carriage.md) — the channel carriage axes
  (cardinality × identity × params).
- [ADR-0074](0074-leeway-marshall-package-layout.md) — marshall/<target> layout.
- [`goplan/build.go`](../../public/semistructured/leeway/marshall/go/goplan/build.go)
  (`AddTupleSliceField` — multi-membership + ref validation, D4 arity),
  [`goplan/grouping.go`](../../public/semistructured/leeway/marshall/go/goplan/grouping.go)
  (`TupleSpec.Memberships` / `Channels()`),
  [`goplan/lwtag.go`](../../public/semistructured/leeway/marshall/go/goplan/lwtag.go)
  (`lowCardRef` token),
  [`mappingplan/plan.go`](../../public/semistructured/leeway/mappingplan/plan.go)
  (`TupleMembership`).
- [`marshallgen/emit.go`](../../public/semistructured/leeway/marshall/go/marshallgen/emit.go)
  (`writeTupleSectionDriver` / `writeTupleSectionDecode` / interfaces),
  [`marshallreflect/marshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/marshal.go)
  / [`unmarshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/unmarshal.go)
  / [`validate.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/validate.go).
- Demo: [`anchor/codecdemo/lineagedoc.go`](../../public/semistructured/leeway/anchor/codecdemo/lineagedoc.go);
  gates: `marshallreflect_test/tuplesection_refmulti_test.go`.
