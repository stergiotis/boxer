---
type: adr
status: proposed
date: 2026-10-11
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0326: Tuple elements carry parameters, and tuple kinds read back through ReadRow

## Context

The journal's vault writes a tool call's arguments and a reply's calls as
shredded attributes (ADR-0325 §SD1): one attribute per JSON leaf, on the
facts data sections, with the JSON Pointer as a verbatim membership name
and the array indices as that membership's parameters. Since the
generated store cannot express that shape, a hand DML in
`agentrunstore/semi.go` writes it through the store's contribution seam
and reads it back by walking the read-access classes. plan002 B6 is
moving it into the generator.

Most of the shape already exists. A dynamic-membership tuple (ADR-0103,
ADR-0109) is a slice-of-struct field whose elements each emit one
attribute with memberships of their own, written byte-identically by
`marshallgen` and `marshallreflect`. Four things are missing:

- **Parameters.** ADR-0109 D1 rejects carrier channels on a tuple
  element, because the mixed channels pair a membership with a per-row
  parameter blob and an element had no field to hold it.
- **One value on an array column.** The array-valued sections
  (`stringArray`, `i64Array`, `f64Array`) take one value per attribute
  through `BeginAttributeSingle`. Tuple elements reject `,unit`.
- **Sections without a value column.** `null`, `emptyObject` and
  `emptyArray` carry only memberships. A tuple element needs at least one
  value field.
- **Reading back.** Tuple kinds are excluded from `<Kind>ReadRow`
  (ADR-0109 open question 2), which is the only decode the generated store
  uses (ADR-0100, Deferred: "carrier channels and exploded fields").

`gov/capmapfacts` hand-writes a second form of the shape. A competence's
sections and lifecycle events are attributes under one fixed membership
id on the mixed reference channel, with the heading or the phase as the
parameter.

## Design space (QOC)

**Question.** Where does a component whose attributes carry per-attribute
names and parameters get written and read?

**Options.**

- **O1 — Extend the tuple grammar and `ReadRow`.** The parameters become an
  element field, and `ReadRow` learns to claim a tuple's attributes on a
  section other kinds share.
- **O2 — A bag component type in `recordstore/gen`.** The store generator
  emits the write and read for a fixed `{Path, Params, Value}` shape on
  its own, beside `marshallgen`.
- **O3 — Keep the hand DML.** One file per consumer knows the layout.

**Criteria.**

- **C1 — One description.** The DTO is the only statement of the layout.
- **C2 — Two front-ends agree.** The reflection marshaller writes the same
  bytes as the generated one, as it does for every other shape.
- **C3 — Cost.** How much new machinery must be held.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | +  | −− |
| C2 | ++ | −− | −  |
| C3 | −  | −  | ++ |

O2 duplicates the tuple write and read in a second generator that the
reflection front-end does not share. O3 is the status quo B6 exists to end.

## Decision

We will **extend the tuple grammar so an element carries the parameters of
its mixed-channel membership, one value on an array column, or no value
at all**, and **read tuple kinds back through `<Kind>ReadRow`**, which
claims a tuple's attributes by channel on sections other kinds share. The
journal's shredded attributes become a generated component.

### SD1: an element's parameters

An element may declare one `@params` field of one of two kinds:

- a `[]uint64` of indices, which the generators encode through
  `membership.EncodeParams` on write and decode through
  `membership.DecodeParams` on read;
- a `string` or `[]byte`, written and read as the blob itself. This is
  what `capmapfacts` writes for a section's heading and a lifecycle
  phase.

Text parameters are not in `membership`'s canonical form, so
`DecodeParams`, `ParamsArity` and the positional SQL decode do not apply
to them; a reader has to know the kind. The field is accepted only
when the attribute's membership is on a mixed channel, which pairs one
membership with one parameter blob:

- a dynamic tuple whose `@membership` field is on `mixedLowCardVerbatim`
  (a `string` or `[]byte` name) or `mixedLowCardRef` (a `uint64` id);
- a static nested section whose tag names a membership on `mixedLowCardRef`
  or `mixedLowCardVerbatim`, which is `capmapfacts`' form.

Only an element with a single membership may carry `@params`, since one
blob belongs to one membership.

### SD2: one value on an array column

A tuple value field, or a static nested section's only sub-column, may
take `,unit`. A scalar Go value then maps to an
array-typed sub-column as one value per attribute, written by
`BeginAttributeSingle` and read as the attribute's single value. This
gives a tuple element on an array column the same spelling a flat section
has.

### SD3: an element without a value

An element whose fields are all membership fields (`@membership`,
`@params`) maps a section with no value column, and every element emits
`BeginAttribute()` with no arguments. Whether the section really has no
column is checked where the DML is known: `marshallreflect.Validate` and
the generated code's compile. Such a section has no attribute class to
count its attributes from, so the read-access generator gives the
section's reader a `GetNumberOfAttributes`, taken from a membership
channel's cardinality column, which holds one entry per attribute.

### SD4: ReadRow claims a tuple's attributes by channel

`<Kind>ReadRow` reads a tuple field from a table whose sections other
kinds share:

- A **dynamic** tuple claims each attribute of its section that carries a
  membership on one of its channels, and decodes each claimed attribute
  to one element as ADR-0109 D3 does. An attribute carrying no
  membership on those channels is another kind's.
- A **static** nested section claims the attributes carrying its
  membership, as a flat field does.
- A row with no claimed attribute in any of the kind's sections reads as
  absent.

The reflection front-end's `Detect` and `ReadComponent` claim by the same
rule, from the read contract: a dynamic tuple's slot names its channels.
`Unmarshal`, the twin of `FillFromArrow`, keeps the section-owning rule.

The store generator refuses two components of one store whose dynamic
tuples claim the same section and channel, since neither could tell its
attributes from the other's.

### SD5: tuple slots in the read-back artefacts

The read-back generator (ADR-0066) renders a tuple slot by SD4's claim
rule, so its Filter admits the rows `ReadRow` reads as present:

- a dynamic tuple is populated when one of its channels' identity columns
  in its section is non-empty (`notEmpty`). It is unbounded, so it adds
  no validator term;
- a static nested section is located by its membership like a flat field,
  with the count bound from its cardinality: exactly one for One with a
  scalar, at most one for One without one and for Optional, none for
  Many.

A tuple kind's Projection is empty, since no single slot holds a tuple.
A store therefore gives a tuple component `Scan<Kind>` and
`ScanLive<Kind>`, but no entry in `<Store>ComponentSQL`, which requires a
Projection, and no `Cas` sets. SQL readers use the read surface (`LW_SEL`
by name, `LW_GET` with `param:`). The Filter does not check a `,unit`
sub-column's value count over every claimed attribute, so a row another
writer gave two values there passes the Filter and fails the Go decode.

### SD6: deferred, recorded

- **Moving `capmapfacts` onto a DTO.**
- **Tuple kinds in `FillFromArrow`'s homogeneous decode**, and in
  `Unmarshal`, keep ADR-0103's rule that a tuple owns its section. SD4's
  claim rule is `ReadRow`'s and `ReadComponent`'s.

## Alternatives

O2 and O3 are weighed in the QOC. One narrower choice:

- **A `@params` field on any channel.** Rejected: only the mixed channels
  pair a membership with a parameter blob in the DML.

## Consequences

### Positive

- The vault's shredded attributes have one description, their DTO, and
  `agentrunstore/semi.go` loses its hand DML.
- The reflection front-end can write and read the same shape, so the
  generated bytes have a second oracle.

### Negative

- `ReadRow` gains a second claiming rule, by channel instead of by
  membership id.
- A tuple kind in a store has no Projection, so no `ComponentSQL` entry
  (SD5).

### Neutral

- ADR-0109 D1's rejection of carrier channels narrows to channels whose
  parameters the element does not carry.

## Verification plan

- **Lane.** Default: both front-ends write the same bytes for a dynamic
  tuple on `mixedLowCardVerbatim` with parameters, for a static nested
  section on `mixedLowCardRef` with numeric and with text parameters
  (`capmapfacts`' shape), for a `,unit` value, and for a value-less
  element. `ReadRow` and `ReadComponent` read each back from a row whose
  sections also carry another kind's attributes, and leave the other
  kind's alone. The journal's vault round trip reads its shredded
  attributes through the generated entity. Against `clickhouse-local`
  (skipped without it), `Scan` of the shredded component selects the
  rows carrying it and no row carrying only another component.
- **What would fail.** A component that claims another kind's attribute;
  parameters that do not round trip; the two front-ends disagreeing; a
  Filter that admits another component's row.
- **Gap.** SD5's value-count check. The Filter is checked against
  `clickhouse-local` for the shredded component only.

## Status

Proposed 2026-10-11.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0103](./0103-leeway-marshall-dynamic-membership-tuples.md), [ADR-0109](./0109-leeway-marshall-multi-membership-ref-tuples.md) — the tuple grammar and read model this extends.
- [ADR-0100](./0100-recordstore-generated-leeway-clickhouse-store.md) — the generated store, whose Deferred list names this shape.
- [ADR-0325](./0325-the-journals-canonical-form-is-the-canonical-wire-over-its-rows.md) §SD1 — the shredded attributes.
- [ADR-0008](./0008-leeway-marshall-extensions.md) — the membership channels.
