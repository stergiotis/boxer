---
type: adr
status: proposed
date: 2026-07-04
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0103: leeway marshall — dynamic-membership tuples (multi-membership multi-sub-column sections)

## Context

[ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) taught the marshall
pair the mixed-shape multi-sub-column model — a scalar tuple plus zipped
co-containers — but kept the pre-existing rule that such a section carries
**one membership and one attribute per row**, deferring its open question
"Multi-membership mixed sections" *"until a consumer needs a concrete
section."*

That consumer now exists: a downstream schema (external repository) maps
entities whose many same-typed properties (a name, an alias, a first
name, …) all land in ONE mixed-shape string-like section — three scalar
metadata sub-columns plus two zipped co-containers (S = 3, C = 2) — one
attribute per property, the property name as the attribute's verbatim
membership. The consumer's hand-written DML probe proves the **DML**
carries this today — a loop of `BeginAttributeSingle(<scalars…>, <one
element per container…>).AddMembershipLowCardVerbatim(property)` writes N
attributes with N memberships — while the **`lw:`-DTO front-ends cannot
express it**:

- `goplan.PlanBuilder.Finish` rejects *"multi-sub-column section with
  multiple memberships not supported"* — the only static spelling of "many
  memberships" is many fields, and with several sub-columns per attribute
  the field↔attribute grouping is genuinely ambiguous (which membership
  pairs with which sub-column tuple?);
- ADR-0101 D2 rejects carrier channels in multi-sub-column sections, so the
  per-row-membership escape hatch (`mixedLowCardVerbatim`) is unavailable —
  and it would write different physical columns anyway (`lmv`+`mvhp`, not
  the plain `lv` the schema and probe use).

So the DML is strictly more expressive than the DTO for exactly this shape.
The RA read side already exposes everything needed (per-sub-column
accessors + `GetMembValueLowCardVerbatim` per attribute).

## Design space (QOC)

**Question.** How should an `lw:`-tagged DTO express N attributes in one
(multi-sub-column) section, each attribute carrying its own membership?

**Options.**

- **O1** — Dynamic-membership tuple: a slice-of-struct field
  (`Texts []LabeledText` tagged `lw:"<section>"`); the element struct
  declares an `@membership` field plus one field per sub-column; each
  element emits one attribute.
- **O2** — Per-element carrier: lift ADR-0101 D2's carrier-channel
  rejection and pair the section's fields with a
  `[]marshalltypes.MixedLowCardVerbatim` sibling.
- **O3** — Static field groups: N repetitions of the section's field
  group, each group under its own static membership
  (``TitleText string `lw:"title,text:text"`` …).
- **O4** — No DTO change; consumers keep hand-driving the DML loop.

**Criteria.**

- **C1** — expressiveness: dynamic N (per row) with per-attribute
  memberships as *data*, not schema.
- **C2** — wire fidelity: byte-identical to the DML loop the consumer
  already runs (plain `lv` membership columns).
- **C3** — front-end parity: implementable identically in marshallgen
  (static codegen, no runtime lookups) and marshallreflect.
- **C4** — grammar coherence: consistent with the existing `lw:`
  vocabulary and the ADR-0101 authoring contract.
- **C5** — failure-mode quality: unrepresentable DTOs fail at
  `PlanFor`/`Validate`, never a reflect panic.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | +  | −− | −− |
| C2 | ++ | −− | +  | ++ |
| C3 | ++ | +  | ++ | ++ |
| C4 | +  | −  | −  | −  |
| C5 | ++ | +  | +  | −− |

O3 fixes N at compile time — useless for per-entity property sets — and
explodes the DTO (N × (S+C) fields). O2 changes the wire (carrier channels
write `lmv`+`mvhp`, breaking C2's byte-identity with the consumer's
committed schema and probe) and contradicts ADR-0101 D2. O4 leaves the
front-ends permanently weaker than the DML. O1 wins: the element struct is
the attribute, its slice is the section's attribute list, and the
membership is ordinary per-element data.

## Decision

Extend both `lw:` front-ends with the **dynamic-membership tuple** shape.
This resolves ADR-0101's open question "Multi-membership mixed sections".

### D1 — Grammar

A tuple field is a slice of a named element struct, tagged with the bare
section name. The element struct declares exactly one `@membership` field —
a `string` or `[]byte` scalar with a **mandatory verbatim channel flag** —
plus one value field per sub-column, spelled `<section>:<column>` (column
defaults to `value`; `,ct=` composes, e.g. `,ct=u8h`):

```go
type LabeledText struct {
    Label      string   `lw:"@membership,verbatim"` // per-attribute membership
    Text       string   `lw:"text:text"`
    WordLength []uint32 `lw:"text:wordLength"`
    WordBag    []string `lw:"text:wordBag"`
}

Texts []LabeledText `lw:"text"` // N elements → N attributes
```

Element tags have no static-membership slot (`goplan.SplitTupleElemLW`);
the value fields repeat the section name so each tag is self-describing and
validated against the outer section. Membership names starting with `@` are
reserved in top-level tags (`goplan.SplitLW` callers reject them), so the
marker cannot silently become a literal verbatim label. The outer tag takes
no flags — the channel is the membership field's concern.

### D2 — Semantics

N slice elements emit N attributes, in element order, each the ADR-0101 D4
call sequence with a per-element membership:

```
for each element e:
    // per-element zip-length guard over the container class (error, not panic)
    attr := sec.BeginAttribute(e.<scalars…>)          // scalar class, declaration order
    for k := 0 .. N_e-1:
        attr.AddToCoContainersP(e.<c₁>[k], …)          // AddToContainerP when C == 1
    attr.AddMembership<Verbatim>P(e.<membership field>)
    attr.EndAttributeP()
```

An element **always emits** — its presence in the slice is the presence
signal, so there is no per-element splice (an S = 0 element with empty
containers still writes a membership-only attribute); zero elements emit
zero attributes. The shape works at any sub-column count (S + C ≥ 1) —
restricting it to multi-sub-column sections would be an arbitrary boundary
(ADR-0101's own C4 argument), and the single-sub-column case falls out of
the same driver.

### D3 — Plan model and validation (shared)

The element struct's value fields become ordinary `mappingplan.TaggedField`s
(one per sub-column, empty `LWMembership`) carrying tuple metadata —
`TupleField` / `TupleStructType` / `TupleMembField` / `TupleMembGoType` —
so ADR-0101's grouping and class views (`ScalarSubColumns` /
`ContainerSubColumns`, zip rule, positional contract) apply unchanged;
`goplan.SectionGroup.TupleSpec()` is the dispatch key every emit / drive /
validate / read site checks **before** the sub-column-count split. All
validation lives in the shared `goplan.PlanBuilder.AddTupleSliceField` +
`Finish`, so the front-ends accept identical DTOs:

- exactly one `@membership` field, `string`/`[]byte` scalar, explicit
  verbatim channel (`,verbatim` / `,lowCardVerbatim` / `,highCardVerbatim`);
- **ref and carrier channels rejected**: a ref membership resolves through
  a compile-time `kindXxx` symbol that the generated `BuildEntities` cannot
  parameterise per element (and the reflect side honouring a runtime lookup
  would break front-end byte-parity); carriers cannot reach sub-columns
  (ADR-0101 D2, unchanged);
- element value fields follow the ADR-0101 multi-sub-column rejections
  (`Option[T]`, roaring, `,unit`, `,explode`, const) plus: one field per
  sub-column, section must match the outer tag;
- **section exclusivity**: a tuple owns its section — a static field, const
  or second tuple field on it is rejected (attribute count and memberships
  are per-element data; anything else could not be disambiguated on read) —
  checked before channel uniformity so sharing is reported as sharing;
- `marshallreflect.Validate[T]` inherits the multi-sub-column DML contract
  checks at any sub-column count: `BeginAttribute` arity = S,
  `AddTo(Co)Container(s)P` arity = C, the verbatim `AddMembership…P`.

### D4 — Write drivers and cardinality passes

`marshallgen.writeTupleSectionDriver` and
`marshallreflect.marshalTupleSection` emit the D2 sequence with identical
call order (byte-identity); `<Kind>AddSections` inherits via the section
driver. For `RowComposer`, each **element** classifies independently by its
shared container length (N_e ≤ 1 single-value pass, N_e > 1 multi-value —
ADR-0101 D7 at element grain). The DML protocol's one-section-frame-per-
entity rule is unchanged: as with static sections, one row's section cannot
open in both passes, so a row whose elements straddle the two classes is a
caller error surfaced by the DML's state machine.

### D5 — Read paths

Every attribute of a tuple section belongs to the tuple (D3 exclusivity),
so there is no membership matching: each attribute yields **one element per
membership value it carries** — codec-written wire carries exactly one — in
wire order, the membership value decoded (copied) into the element's
membership field; zero attributes decode to a nil slice; an N_e = 0
container reads back nil. Tuple sections read every sub-column through its
**named** accessor (`GetAttrValue<Col>`) at any sub-column count — a lone
sub-column may not be named `value`. `[]byte`-typed scalar sub-columns and
membership values are copied out of the Arrow buffer (elements are retained
in the slice). The SoA column is the outer `[][]Elem`; `<Kind>ReadRow` does
not cover tuple kinds (`ReadRowSupported` reports the reason, like carriers
and explode).

### D6 — File / package rules (front-end specific)

marshallgen resolves the element struct from the DTO's own file: a file may
declare the DTO plus its tuple element structs — with several structs the
DTO is the (single) one carrying the `_` kind field, and any struct that is
neither the DTO nor a referenced element type keeps the one-DTO-per-file
error. marshallreflect requires the element struct in the DTO's package and
rejects foreign-package elements with the parity rationale. Unexported and
untagged element fields are rejected by both front-ends.

## Verification

1. **Tri-identity** (anchor `text`, S = 1 / C = 2; N = 0, 1, 3 attributes,
   distinct memberships, per-element containers incl. empty): explicit DML
   loop ≡ `marshallreflect.Marshal` ≡ generated
   `LabeledTextDocBuildEntities` — `array.RecordEqual` + IPC-byte equality +
   both cross-decodes (`marshallreflect_test/tuplesection_roundtrip_test.go`,
   `anchor/codecdemo/labeledtextdoc*`).
2. **Consumer byte-identity**: the originating consumer reproduced, in its
   own repository's test suite, that the DTO form emits byte-for-byte what
   its hand-driven DML probe emits against its production mixed section
   (S = 3, C = 2, verbatim memberships; N = 3 / 1 / 0) and round-trips.
3. Single-sub-column tuple (anchor `symbol`) + `[]byte` membership field;
   per-element zip-mismatch error; RowComposer per-element pass routing.
4. Negative matrix at `PlanFor`/`Validate` (both front-ends through the
   shared builder): second/missing `@membership`, missing/ref channel
   flag, wrong membership type, wrong section, duplicate sub-column,
   Option/roaring/explode/channel-flag on element fields, no value fields,
   shared/duplicated tuple sections, reserved `@` at top level,
   foreign-package element, stray structs, DML contract arity mismatches.
   The former blanket rejection test now pins both directions: static
   multi-membership stays rejected, the tuple form is accepted.
5. Byte-stability: every in-tree marshallgen `.out.go` regenerates
   identically under the extended generator (the keelson codecs' regen
   delta — missing ADR-0100 `AddSections`/`ReadRow` — reproduces with the
   pre-change generator, i.e. pre-existing staleness, not this change).

## Alternatives

- **O2 — per-element carriers.** Different physical membership columns
  (`lmv`+`mvhp` vs `lv`) — not what the consumer's committed schema
  declares; contradicts ADR-0101 D2; the carrier struct duplicates what a
  plain element field states directly.
- **O3 — static field groups.** N is compile-time, but the property set
  varies per entity; N × (S+C) fields per section is unwritable at
  DTO-fleet scale.
- **Dynamic ref memberships via `LookupI`.** Works only in the reflect
  front-end (generated `BuildEntities` has no lookup surface) — breaks the
  byte-parity invariant; deferred until a consumer needs ref-channel
  tuples (see Open questions).
- **Defaulting the membership channel to `lowCardVerbatim`.** One flag
  saved, but the wire channel would be invisible at the declaration site
  and inconsistent with the grammar's LowCardRef default elsewhere.
- **A membership-field reference in the outer tag** (`lw:"@Property,string"`).
  Rename-fragile string coupling to a field name; the field-attached marker
  matches how carriers and roaring already select behaviour by field.

## Consequences

### Positive

- The `lw:` front-ends reach DML parity for the multi-membership shape:
  several same-typed properties per entity marshal natively into one mixed
  section, with plan-time safety and both front-ends byte-identical.
- The membership becomes ordinary per-element data; the element struct is
  a reusable, self-describing unit (one Go type per section shape).

### Negative

- A second tag grammar (element tags have no membership slot) — small, but
  a reader must know the context; mitigated by the `@membership` marker
  being greppable and reserved at top level.
- Tuple metadata rides duplicated on each sub-column's `TaggedField`
  (builder-enforced consistency) — a modelling shortcut that keeps the
  grouping machinery untouched.
- The tuple's SoA column is `[][]Elem` — jagged like every container
  column (`[][]T`), but with a struct leaf: within one row's attribute
  list the sub-column values are interleaved per element (AoS at
  attribute grain). The columnar layout that matters is untouched —
  `BuildEntities` re-columnarises immediately into one Arrow array per
  physical sub-column, byte-identical to the DML — so columnar scans
  belong on the Arrow record (RA accessors), not on the staging
  `<Kind>Columns`. Exploding the tuple into per-sub-field parallel
  columns instead would break the one-column-per-DTO-field contract and
  push the per-element zip invariant into every `Columns` consumer;
  rejected while no consumer scans `Columns` on a hot path.

### Neutral

- Static multi-sub-column sections are unchanged (still single-membership,
  one attribute per row, splice rules intact); all existing generated
  codecs are byte-stable.
- The one-section-frame-per-entity DML protocol is untouched (D4).

## Open questions

1. **Dynamic ref memberships.** *(Resolved 2026-07-05 by
   [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md).)* The
   consumer materialised; the lookup-surface concern is dissolved by carrying
   the ref id directly on the `@membership` field as a `uint64` (per-element
   data — no `kindXxx` symbol, no `LookupI`, both front-ends byte-identical),
   which also lifts the single-membership rule (D1/D5) to multi-membership.
2. **`ReadRow` / recordstore coverage.** Tuple kinds are excluded from
   `<Kind>ReadRow` (and thus ADR-0100 store reads) like carriers and
   explode; lift when a store consumer needs a tuple component.
3. **Plan-derived DDL.** ADR-0100's plan→DDL path has no tuple mapping
   (tuples target existing schemas today); inherits ADR-0101 OQ 1's
   set-canonical caveat as well.

## Status

Proposed (2026-07-04). Implemented behind the shared plan layer in
goplan + mappingplan + marshallgen + marshallreflect with the verification
suite above green; resolves the "Multi-membership mixed sections" open
question of [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) (see
its Updates section).

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers.

## References

- [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) — mixed-shape
  multi-sub-column sections; this ADR resolves its "Multi-membership mixed
  sections" open question and reuses its D1–D5 machinery.
- [ADR-0008](0008-leeway-marshall-extensions.md) — marshall extensions
  (channel flags, carrier pairing precedent).
- [ADR-0074](0074-leeway-marshall-package-layout.md) — marshall/<target>
  layout (goplan / marshallgen / marshallreflect homes).
- [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) —
  `<Kind>AddSections` / `ReadRow` surfaces (Open question 2).
- [`goplan/lwtag.go`](../../public/semistructured/leeway/marshall/go/goplan/lwtag.go)
  (`SplitTupleOuterLW` / `SplitTupleElemLW`),
  [`goplan/build.go`](../../public/semistructured/leeway/marshall/go/goplan/build.go)
  (`AddTupleSliceField`, Finish exclusivity),
  [`goplan/grouping.go`](../../public/semistructured/leeway/marshall/go/goplan/grouping.go)
  (`TupleSpec`).
- [`marshallgen/emit.go`](../../public/semistructured/leeway/marshall/go/marshallgen/emit.go)
  (`writeTupleSectionDriver` / `writeTupleSectionDecode`),
  [`marshallreflect/marshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/marshal.go)
  / [`unmarshal.go`](../../public/semistructured/leeway/marshall/go/marshallreflect/unmarshal.go).
- Demo: [`anchor/codecdemo/labeledtextdoc.go`](../../public/semistructured/leeway/anchor/codecdemo/labeledtextdoc.go).
- Consumer evidence (DML probe, byte-identity + round-trip against the
  production schema) lives in the originating consumer's own repository;
  the in-tree equivalents are verification gate 1's codecdemo and
  marshallreflect_test files.
