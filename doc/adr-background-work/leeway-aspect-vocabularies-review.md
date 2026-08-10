---
type: explanation
audience: contributor
status: draft
reviewed-by:
reviewed-date:
---

> **Status: draft — pre-human-review.** Working material as of 2026-08-10. A
> review of the three leeway aspect vocabularies for orthogonality,
> completeness and consistency. The lens is deliberately *not* expressivity:
> each observation is graded by what it costs to check and over what
> neighbourhood, to keep the vocabulary out of the ontology tarpit. Findings
> are facts about the code as reviewed; §5 records what was subsequently
> changed, including the same-day outright removal of
> `AspectScaleOfMeasurementCategorial` (M6).

# The leeway aspect vocabularies — a review

## 1. The system as built

Three closed vocabularies annotate leeway schemas
(`public/semistructured/leeway/{valueaspects,useaspects,encodingaspects}`):

| vocabulary | attaches to | slots used / 64 | encoded where |
| --- | --- | --- | --- |
| `valueaspects` ("value semantics") | every column (plain, tagged, entity, transaction, opaque) | **62** | column name segment |
| `useaspects` | tagged-value **sections** only (structural: plain has no field) | **58** | column name segment |
| `encodingaspects` ("encoding hints") | every column | **24** | column name segment |

An `AspectSet` is a base62 string of a `uint64` bitmask. All three sets are
embedded as segments of every physical column name by
`HumanReadableNamingConvention` and recovered by
`DiscoverTableFromColumnNames` — self-description is the design's point, and it
has a hard consequence:

**The numbering is a durable wire format.** `TestCanonicalAspectEnum` pins
dense contiguity (`AllAspects[i] == i`, no gaps), so removing an aspect forces
a renumber, and a renumber silently re-interprets every aspect segment already
frozen into live tables' column names. The enums are therefore **append-only,
forever**. This invariant is load-bearing and was stated nowhere before this
document.

A second consequence is capacity arithmetic: `valueaspects` had **2 free
slots** as reviewed (3 since the M6 removal), `useaspects` 6,
`encodingaspects` 40. Families drafted in past working notes (temporal
roles, media type, confidentiality ×4, quality dimensions ×6) do not fit in
`valueaspects`; admitting any of them is a wire-format decision (second
segment or wider mask), not an enum edit.

## 2. Reader census

The decisive measurement. An aspect earns its slot when some consumer changes
behaviour on reading it. Census over the repository (excluding tests,
generated files, and schema *writers* that merely stamp aspects):

| vocabulary | read by name | reader |
| --- | --- | --- |
| `encodingaspects` | 14 codec aspects | ClickHouse DDL codec derivation (`generateTypeAndCodec`) + `GetEncodingHintImplementationStatus` filtering |
| `useaspects` | `SectionMembershipsAllPrimary`, `…AllSecondary` | `membershiprole.DefaultClassifier` (advisory short-circuit; piped through `marshallreflect` and streamreadaccess) |
| `valueaspects` | `HumanReadable`, `MachineReadable` | table widget hide rule (`table2_emitter.go`: machine ∧ ¬human ⇒ hidden) |

**18 of 144 aspects are ever read.** Roughly seven more appear only as writes
in schema definitions (`CanonicalizedValue`, `Linking`, `Documentation`, …);
the remaining ~119 are referenced nowhere outside their own declaration.
Generic consumers (schemaview chips, catalog pass-through, naming round-trip)
carry all aspects uniformly without interpreting any.

The three vocabularies sit at the cheap end of the checking-cost ladder:
name-admissibility is the only universal check (`IsValid`, a closed set), and
the 18 consumed aspects act as per-column / per-section local rules. There is
no hierarchy, no implication, no cross-aspect axiom, no check that ranges over
the store. **The tarpit failure mode — checks with unbounded neighbourhoods —
is absent.** The failure mode that is present is the opposite one: a
vocabulary populated far ahead of its consumers, in enums where a slot, once
granted, can never be reclaimed.

## 3. Findings

### Orthogonality

- **O1 — cross-vocabulary name duplication (resolved: deliberate).** The
  `Json*`/`Cbor*` names exist in both `valueaspects` and `encodingaspects` by
  design and mean different things: the encoding aspect permits the ddl
  module to use a native JSON/CBOR database type, the value aspect states
  that the value is (or may be dealt with as) a JSON/CBOR string
  serialization. Recorded as doc comments on both enums (2026-08-10). The
  name collision still requires ADR-0181's `sem:`/`enc:` prefixes.
  `ApplicationLevelCompression`/`…Encryption` (value) remain adjacent in
  intent to the encoding compression family; no consumer forces the issue.
- **O2 — exclusive families flattened into independent bits.** At least ten
  families are 1-of-n choices packed as unrelated bits with no exclusivity
  check anywhere: scale-of-measurement ×5 (where `Categorial` conceptually
  duplicates `Nominal`), lifespan ×5, SCD types ×8 (+`MiniDimension`),
  medallion tiers ×3, source-of-truth ×3, general-compression levels ×4,
  slowly-changing-float ×4, bias-small-integer ×2, emulated-membership ×4,
  feature-scaling ×3, `Mandatory`/`Optional`, the section-uniformity pair.
  Contradictory combinations encode fine and embed into column names; each
  reader resolves privately — the classifier by `if`-order (AllPrimary wins,
  documented as decision order), the codec generator by emitting *both* codecs
  in bit order (chain order is an accident of enum numbering).
- **O3 — overlap with the core model (largely resolved).**
  `Mandatory`/`Optional` declare requiredness at the *semantic* level,
  deliberately distinct from structural presence (a plain non-scalar value is
  always structurally present yet may be empty). The `EmulatedMembership` ×4
  family supports transitioning from other EAV systems and stays. Both
  clarifications are recorded as enum doc comments (2026-08-10). What
  remains: `GraphVertex`/`GraphEdge`/`HyperGraphEdge` (value) vs `Linking`
  (use) vs the membership machinery is an unforced three-way adjacency.

### Completeness

- **C1 — checking is asymmetric.** The validator checks set validity for
  section use-aspects and tagged encoding hints, but never for value
  semantics (tagged or plain) nor plain encoding hints. The authoring path
  (`TableManipulator` mergers) routes every aspect through `…IgnoreInvalid`,
  silently dropping unknown values; the `receivedInvalidAspects` field meant
  to record such drops is initialized, reset, and never set or read. An
  unchecked rule is a suggestion; here even the *detector* for broken rules is
  a suggestion.
- **C2 — dual encoding of "no aspects".** Bit 0 (`AspectNone` /
  `AspectIndefinite`) and the empty set (`"0"`) are distinct valid encodings
  of the same meaning and produce *different physical column names* for
  identical schemas. Nothing normalizes one to the other.
- **C3 — the zero value is invalid.** `AspectSet("")` fails `IsValid`; empty
  is `"0"` (`EmptyAspectSet`). Call sites defensively initialize
  (`lwsql`, validator tests) — a footgun the API places in every DTO.
- **C4 — no per-aspect semantics record.** Meaning lives in identifier names
  and scattered comments (Kimball links, PROV, WHATWG). For values frozen
  into physical names forever there is no document answering, per aspect:
  what does it assert, what checks it, which consumer fails without it. The
  section-uniformity pair shows the template — ADR-0007/0073 plus a consumer
  plus a documented resolution rule — and is the only family that has it.
- **C5 — no reverse parser.** Nothing maps kebab names back to `AspectE`.
  ADR-0181 SD6 requires one; the enum tests already pin `String()` totality,
  injectivity and stylable-name validity per vocabulary, so the base is
  sound. Collisions are cross-vocabulary only (`json*`, `cbor*`, `none`),
  which is exactly why 0181's prefixes are load-bearing.

### Consistency

- **K1 — identifier/string drift, about to be frozen twice.** Go identifiers
  are frozen already (an external adopter writes them); DQL will freeze the
  *strings*. Current drift: `AspectMachineGenerate` ↔ `"machine-generated"`
  (vs `AspectHumanGenerated`); `AspectEvolution` ↔ `"change-evolution"`;
  `"…categorial"` (non-standard spelling, and conceptually a duplicate);
  `AspectTextUnicodeCaseFolded`'s comment is a copy-paste of NFKC's.
- **K2 — one API, three error philosophies, three copies.** Encode has
  error/ignore/panic variants; `Contains` returns silent-false on any invalid
  set; `IterateAspects` returns a **nil iterator** on invalid input, and
  ranging over a nil `iter.Seq2` panics — reachable from any zero-value or
  future-versioned set, including on the render thread (schemaview iterates
  sets straight off schemas). All of it exists in triplicate: the three
  encoder files are marked "Code generated by copy paste; DO NOT EDIT" with
  no generator.
- **K3 — version skew is silent and total.** `decode` rejects a set whose
  highest bit exceeds the reader's `MaxAspectExcl`, so one too-new aspect
  poisons the *entire* set for an older reader: every `Contains` false, every
  codec dropped, the classifier falls back to heuristics — with no error
  surfaced anywhere on the read path. Appending an aspect is therefore safe
  to *declare* but degrades old readers the moment it is *used*. This is the
  compatibility relation of the aspect system, and it was undocumented.

## 4. What keeps this out of the tarpit

The review's stop-rule, stated so it can be enforced rather than felt:

1. **Locality stays law.** Every aspect check is decidable from one set plus
   the enum declaration. Exclusivity checking, if adopted, is a per-set
   predicate in `TableValidator` — never a relation the system reasons over.
   Disjointness-as-axiom is the door to the far side of the discontinuity;
   it stays shut.
2. **Admission over retirement.** Removal is structurally impossible (wire
   format), so the ratchet control must sit at admission: a new aspect names
   its checker and the consumer that fails without it *before* it takes a
   slot. With 2 slots left in `valueaspects` this is arithmetic, not hygiene.
3. **Retirement is deprecation-in-place.** Doc marker plus lint against new
   writes; never renumber, never re-use a slot.
4. **Population follows consumers.** The census (18/144) is the number to
   move — by adding readers or stopping writes, not by adding vocabulary.

## 5. Minor changes

| # | change | status |
| --- | --- | --- |
| M1 | `IterateAspects`: empty iterator instead of nil on invalid input (×3 encoders + tests) | **applied 2026-08-10** |
| M2 | Validator symmetry: `IsValid` for value semantics (tagged + plain) and plain encoding hints; error when both section-uniformity aspects are set | **applied 2026-08-10** |
| M3 | Delete the dead `receivedInvalidAspects` field | **applied 2026-08-10** |
| M4 | Comment/typo fixes (`CaseFolded` NFKC copy-paste, "copy pase" headers) plus semantics doc comments for the Json*/Cbor* pair, `EmulatedMembership`, `Mandatory`/`Optional` | **applied 2026-08-10** |
| M5 | Promote this document's §1–§4 into a maintained explanation page | dropped 2026-08-10 — this background document suffices; no explanation page |
| M6 | Retire `Categorial` (`EmulatedMembership` and `Mandatory`/`Optional` were withdrawn as candidates after review dialogue, see O3) | **superseded 2026-08-10 — removed outright.** Stevens' scales are nominal/ordinal/interval/ratio; every known write marked unordered categories, i.e. nominal. Removed pre-DQL while the name is not yet user-facing: values above 4 renumbered, all artifacts embedding value-semantics segments regenerated, and the sole external adopter's writes migrated to `Nominal` in lockstep |

None of the applied changes touches the wire format. The M2 check caught a
real defect on its first run: the sample table generator drew use-aspects
**per column** and unioned them into the section, so two columns of one
section could contribute the contradictory uniformity pair — exactly the
union-formed contradiction O2 describes. Fixed by sampling section-level
aspects once per section (`lw_sample.go`).

Removal/renumbering was initially not proposed here (wire break); the
project then chose exactly that for `Categorial` — accepted knowingly,
pre-DQL, with the external adopter migrated in the same change and every
generated artifact regenerated. Physical tables created under the old
numbering decode differently under the new one and must be re-created from
regenerated DDL; none carrying the shifted segments were known outside
regenerable artifacts. Still **not** proposed: renaming Go identifiers
(external adopter); restructuring exclusive families into typed enums (a
redesign of the wire format the bit-set *is*); building the reverse parser
(it belongs to ADR-0181's implementation, and its naming decisions —
including whether DQL spells `machine-generated` against the misnamed Go
identifier — should be made there while strings are still cheap to choose).
