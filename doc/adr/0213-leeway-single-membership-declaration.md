---
type: adr
status: accepted
date: 2026-08-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-31
---

# ADR-0213: leeway single-membership declaration — exactly one membership per attribute, per channel, as a writable schema statement

## Context

An attribute channel that provably carries exactly one membership per
attribute reads as `value[indexOf(ident, lit)]` — no cumulative sum, no
position→attribute map, and none of the silent co-index hazard the ragged
form carries (the jsonbench trial's finding: the hand-written co-index is
correct on `len ≡ 1` data and wrong on any multi-element array, with nothing
saying so). Leeway has long had this property as a **reading** rule with no
**writing** rule:

- [`github.com/stergiotis/boxer/public/semistructured/leeway/lwextract`](../../public/semistructured/leeway/lwextract) licenses the
  fast form when the channel's cardinality lane *does not exist* — "its
  absence is the schema stating that every attribute carries exactly one
  membership" — closing ADR-0066's open fast-path-detection item
  structurally (ADR-0181 §SD3).
- The read-back generator's invariants I4/I5
  ([readback EXPLANATION](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md))
  define the fast path and, until this decision, recorded its detection as
  open.
- [`github.com/stergiotis/boxer/public/semistructured/leeway/constructsql`](../../public/semistructured/leeway/constructsql)'s
  `LwExtractExpand` **refused** an absent cardinality lane — absence from a
  column listing is not proof — and the read-back generator refused the
  same way.
- No writer could produce the licensed shape: the DDL generator emitted a
  `<role>card` lane per channel unconditionally
  (`IntermediateTaggedValuesDesc.loadSectionMembership` →
  `ResolveMembership`), so `TableRowConfigMultiAttributesPerRow` — the only
  row config — always carried the lane, and ADR-0181 called the fast path
  "implemented, proven, and currently unreachable".

Two readers with two policies, and no writer. The first consumer that needs
the writing rule is a downstream facts schema whose "emulated memberships"
(value columns tagged `AspectEmulatedMembership*`) exist precisely because
mandatory/single-instance could not be declared on a real channel; its
target design puts exactly-one on the `lv` and `lmv`/`mvhp` channels and
deliberately not on `hv`. The declaration must therefore be **per section
and per membership channel**, not per section alone.

## Design space (QOC)

**Question.** Where does "exactly one membership per attribute on channel C
of section S" live in the schema?

**Options.**

- **O1** — new `MembershipSpecE` modifier bits (a "single" twin per channel
  bit).
- **O2** — a new field on `common.TaggedValuesSection` (a second
  `MembershipSpecE`-typed bitmask, subset of `MembershipSpec`).
- **O3** — section use-aspects, one per channel
  (`useaspects.AspectSectionSingleMembership*`), following the
  `AspectSectionMembershipsAllPrimary`/`AllSecondary` precedent.
- **O4** — encoding aspects on the membership lane (an
  `encodingaspects.AspectE` per channel).

**Criteria.**

- **C1 — wire and serialization inertness**: undeclared schemas must keep
  byte-identical CBOR table descriptions, physical column names, canonform
  digests and canonwire bytes.
- **C2 — round-trips through the names road**: `lwsql`, the datacatalog and
  the play card driver reconstruct schemas from physical column names
  (`DiscoverTableFromPhysicalColumns`); the declaration must survive that
  reconstruction *as proof*, not as an inference from absence — a view
  projecting a subset of columns must not read as a declaration.
- **C3 — plumbing surface**: how many serialization, merge, copy and
  compare paths must move in the same commit.
- **C4 — per-channel expressiveness**: `lv` single beside `hv` ragged in
  one section.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−`
strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | −  | ++ | +  |
| C2 | −  | −− | ++ | −− |
| C3 | −− | −  | ++ | −− |
| C4 | ++ | ++ | ++ | +  |

O1 is killed by the type itself: `MembershipSpecE` is a full `uint8` — a
per-channel twin bit doubles the space and widening the type breaks the
CBOR table description for every existing schema. O2 is killed twice: the
DTO gains a map key (moving the encoded bytes of every serialized table
unless `omitempty` is retrofitted), and schema discovery ignores
cardinality lanes entirely, so the field cannot be reconstructed from
column names except by reading *absence* as proof — exactly the inference
`constructsql` refuses for good reason. O4 is killed three ways: encoding
aspects have no authored home on a membership lane —
`TaggedValuesSection` carries them for value columns only, and the lanes'
hints are derived output (`ResolveMembership` machine-chooses them from
the channel bit on every IR rebuild), so the declaration would be erased
by the schema's own round-trip unless the section grows a per-channel
hints field (O2's plumbing) and `TechnologySpecificMembershipSetGenI`
changes shape; the column the declaration most naturally describes is the
one it removes, putting the names road back to inferring from absence;
and the vocabulary's contract is *droppable hints* (technology filters
silently discard unimplemented aspects, correctness never depends on
them), which a load-bearing declaration — it moves the column set, the
write contract and the licensed read form — inverts. O3 rides machinery
that already round-trips: use-aspects serialize in the existing DTO
field, encode into every tagged column name (the naming convention's
use-aspects segment) and are rebuilt by discovery, so the names road
carries the declaration as a positive statement.

O3 carries a semantic tension worth stating (owner review, 2026-08-31):
the use-aspects vocabulary is framed as intended *use cases* of a
section's data, and exactly-one-per-attribute is a structural contract.
The vocabulary has hosted a second genre since
`AspectSectionMembershipsAllPrimary`/`AllSecondary` — engine-anchored
section contracts, admissible under ADR-0182's "a format the engine
itself commits to" — and the new aspects read as writer-side usage
contracts in that family. The constraint is the wire, not the words: the
aspect segment is the only per-section, authored, serialized,
discovery-round-tripped slot in the 13/21-component physical naming
convention, and a dedicated "layout contract" segment would rename every
column of every existing table. The Go identifiers are free to move (the
wire is the numeric index); the segment placement is not.

## Decision

Declare single-instance membership as **section use-aspects, one per
channel** (`useaspects.AspectSectionSingleMembership<Channel>`, indices
47–54), authored via
`TaggedValueSectionMerger.AddSectionSingleMembership(spec)`. For a declared
channel:

- **DDL omits the `<role>card` column**: `loadSectionMembership` skips the
  support lane, so the omission reaches ClickHouse DDL, the Arrow schema,
  the generated DML/RA classes, streamreadaccess, canonwire and the record
  stores from one seam.
- **The DML enforces the arity at write time**: the generated
  `completeAttribute` path appends
  `dml/runtime.ErrSingleMembershipViolated` when an attribute closes with a
  membership count other than one on the channel (after the ambient
  ADR-0112 stamp replay, which runs first). Elements are counted —
  placeholder identities included — because physical co-indexing is what
  the fast form relies on.
- **Absence plus declaration is the licence, absence alone stays a
  refusal**: the read-back generator (`Generator.locate`) and
  `LwExtractExpand` (`lwsql.Channel.SingleMembership`, recovered from the
  use-aspects the column names encode) emit `lwextract`'s fast form exactly
  for declared channels; a card column missing from an undeclared schema
  remains a conformance error.
- **Read access loads an identity accel**: the generated membership packs
  load the declared channel's position lookup from the membership column's
  own list structure (`readaccess/runtime.LoadAccelIdentityFromRecord`);
  the pack class key becomes the (channel set, declaration) pair, so
  same-spec sections with different declarations get distinct classes.
- **The validator ties the aspect to its channel**: a single-membership
  aspect without the matching bit in `MembershipSpec` is a schema error.
- **The feature is inert**: an undeclared schema regenerates
  byte-identically everywhere (pinned by the tree's golden artifacts, which
  moved only in generator source-location comments), and the declaration is
  representation, never content (below).

Two checkpoint decisions the requirements left open are taken here:

- **canonform** (ADR-0201): the declaration does not move a digest. The
  cardinality channel is already on SD2's erasure list; the new aspects are
  consulted by no classifier, so the SD5 carve-out does not reach them; and
  the physically card-less layout emits the identical membership multiset
  through streamreadaccess. Pinned by
  `canonform.TestSingleMembershipDeclarationIsRepresentation`.
- **canonwire** (ADR-0210): "declared single" is a **storage-layout fact**
  and does not join the wire, the slot key, or the decode accept mask. A
  multi→single decode — an entity carrying several memberships on a channel
  the target declares single — is refused, but by the target's own
  write-time arity enforcement (the generated decoder drives the same DML
  builders, so the violation surfaces as `ErrSingleMembershipViolated` at
  commit), not by a mask that would have to grow an arity dimension and
  move every generated accept-mask golden. Single→multi decodes are
  unaffected, mirroring the ADR-0210 stance that a wider target is not a
  narrowing.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| `useaspects` vocabulary (ADR-0182 wire format, segment in physical names) | 8 members appended (47–54), non-exclusive family `single-membership` | aspect round-trip goldens; declaring a section renames **all** its columns (the use-aspects segment), so the declaration is a schema decision like any column change |
| `common.TaggedValuesSection` semantics | `UseAspects` gains load-bearing members; `SingleMembershipSpecs` / `GetSingleMembershipAspectByMembershipSpec` and role↔spec mappings in `common` | `TableValidator` (aspect requires channel); `PopulateManipulator` samples declarations coherently, so every generator fuzz covers them |
| DDL / IR (`loadSectionMembership`) | card lane omitted for declared channels | ClickHouse/Arrow/Go DDL, generated DML and RA classes, canonwire generated tables, record stores |
| generated DML write contract | `completeAttribute` enforces exactly-one; new `dml/runtime.ErrSingleMembershipViolated` | `marshallreflect` writes through the same builders; canonwire decode inherits the refusal |
| `readaccess` generated packs + `ComposeMembershipPackInfo` (exported) | pack key is (spec, declaration); new return value; `LoadAccelIdentityFromRecord` in both runtime build-tag variants | callers of `ComposeMembershipPackInfo` (in-tree: the generator and its test) |
| read-back generator | absent card + declaration ⇒ fast form (I5); refusal otherwise unchanged | readback EXPLANATION I5/trade-offs text updated |
| `lwsql.Channel` | `SingleMembership` field, recovered from column names | `constructsql.LwExtractExpand` licenses the fast form on it |
| `streamreadaccess` | `sectionTagCount` counts a card-less channel's one tag beside carded ones | canonform (the tag frame is what the encoder consumes) |

## Alternatives

Beyond the QOC: a **namemint registry restriction**
(`registry.CardinalitySpecE`, declared but consumed nowhere) was rejected
because a table's physical layout must be decidable from the `TableDesc`
alone — the registry is a vocabulary-level, optional companion, and a
layout that changes with registry availability would make the same schema
mean two different tables.

## Consequences

### Positive

- The exactly-one property is finally writable, so the two readers'
  policies stop disagreeing: `lwextract`'s licence, `constructsql`'s
  refusal and the read-back generator's refusal now describe the same
  schema statements.
- Declared channels read as `value[indexOf(ident, lit)]` on every road —
  generated read-back, `LW_GET`, and hand-written SQL against the visible
  column shape — and the write side cannot produce data the fast form
  misreads.
- One fewer physical column per declared channel.

### Negative

- Eight enum values of the use-aspects wire vocabulary are spent on one
  concern (the vocabulary is append-only; nothing is reclaimed if this
  is superseded).
- The arity check runs per attribute on the write hot path of declared
  channels (an integer compare; undeclared channels pay nothing).
- The identity accel allocates a per-record all-ones cardinality slice on
  load where the carded path borrows the Arrow buffer; acceptable until
  measured otherwise.

### Neutral

- A declared channel's arity error surfaces at `CommitEntity`/
  `CheckErrors` like every DML error — a decode targeting a declared table
  reports a multi→single violation there rather than as a canonwire
  channel refusal.
- `AspectEmulatedMembership*` (the transitional value-column emulation) is
  untouched; retiring emulations is the consumer's move, now unblocked.

## Migration — Tier 1

- **Breaks.** Nothing on the default path: undeclared schemas regenerate
  byte-identically; `readaccess.ComposeMembershipPackInfo` gained a return
  value (in-tree callers updated).
- **Path.** Declaring a channel on an *existing* table is a schema change
  like any other: every column of the section is renamed (the use-aspects
  segment) and the card column disappears — provision a new table and
  migrate, exactly as for a column-type change.
- **Regeneration.** `go generate ./...` plus the leeway generator tests;
  golden artifacts move only where a schema actually declares.
- **Old shape.** Kept indefinitely — undeclared channels are the general
  case, not a deprecated one.

## Verification plan — Tier 1

- **Lane.** Default `go test`:
  [`github.com/stergiotis/boxer/public/semistructured/leeway/test/singledecl`](../../public/semistructured/leeway/test/singledecl)
  (write-time enforcement, identity accel, fast ≡ general over
  clickhouse-local server truth, `LW_GET` licence, discovery round-trip),
  `canonform.TestSingleMembershipDeclarationIsRepresentation` and
  `TestSingleMembershipTagFramesConsistent`, and the generator fuzz lanes
  (`PopulateManipulator` now samples declarations, so
  `TestGoClassBuilderSample` and the readaccess/canonwire samples cover
  declared schemas every run).
- **What would fail.** A digest move on declaration (canonform pin); a
  fast-form/general-form divergence on server-written data (the
  clickhouse-local oracle); a write path accepting a ragged attribute on a
  declared channel (enforcement test); regenerated goldens for undeclared
  schemas (byte-stability of the tree's `.out.*` artifacts).
- **Gap.** No canonwire fixture decodes into a declared table yet — the
  multi→single refusal is pinned at the DML layer the decoder drives, not
  end-to-end through a generated decoder; acceptable because the decoder
  has no arity path of its own, and worth closing when the first declared
  canonwire table exists.

## Status

Accepted 2026-08-31.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0066: leeway DQL ClickHouse read-back generator](0066-leeway-dql-clickhouse-readback-generator.md)
  — the fast-path item this closes on the writing side (its 2026-08-14
  update built the licence; this ADR makes it reachable).
- [ADR-0181: leeway DQL authoring surface](0181-leeway-dql-authoring-surface.md)
  — §SD3's structural fast path, previously unreachable.
- [ADR-0182: aspects v2 codec and vocabulary](0182-leeway-aspects-v2-codec-and-vocabulary.md) —
  the admission criterion the new members enter under (a format the engine
  itself commits to; independent booleans).
- [ADR-0201: leeway canonical record form](0201-leeway-canonical-record-form.md)
  — §SD2 erasure list (membership spec and cardinality channels), §SD5's
  classifier carve-out the new aspects deliberately stay out of.
- [ADR-0210: leeway canonical wire generator](0210-leeway-canonical-wire-generator.md)
  — the accept-mask/narrowing frame the checkpoint decision is taken
  against.
- [readback EXPLANATION](../../public/semistructured/leeway/marshall/clickhouse/readback/EXPLANATION.md)
  — invariants I4/I5.
