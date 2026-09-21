---
type: adr
status: proposed
date: 2026-09-14
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0233: leeway params-codec declaration — the encoding of a section's membership params blobs as a writable schema statement

## Context

The membership params channel (`mvhp`, `mrhp`, and the parametrized `hp` /
`lp` channels) carries the high-cardinality half of an attribute locator: for
the canonical JSON mapping, the array indices elided from the low-cardinality
path, so that `/tags/0` and `/tags/1` share the verbatim path `/tags/_` and
differ only here. Leeway declares the column as opaque bytes and the generated
DML takes a raw `[]byte`, so nothing in the wire format states the encoding.

`membership.{Append,Encode,Decode}Params` (2026-08-06) is the one codec in the
tree — fixed-width lowercase hex, four digits per index, `.`-separated — and
its commit records why it exists: three writers had grown three incompatible
encodings and no reader at all. On 2026-09-14 the first writer outside this
tree, the ontology binding in `hackathon_2026`, was found spelling the index
with `%04d`: identical to the codec for the first ten occurrences, `0010`
against `000a` past that. The codec is universal by convention only, and a
convention without a declaration is what produced four encodings in six
weeks.

A reader, a read-back generator or a SQL extractor that needs the index has
to know the encoding. Today each assumes; none can check.

## Design space (QOC)

**Question.** Where does "the params blobs of section S are encoded with codec
C" live?

**Options.**

- **O1** — a Go type parameter or generic method on the generated DML and
  read-access classes, the codec chosen by the caller per call.
- **O2** — a new field on `common.TaggedValuesSection`.
- **O3** — a section use-aspect per codec, in an exclusive family
  ([ADR-0213](./0213-leeway-single-membership-declaration.md)'s road).
- **O4** — an encoding aspect on the params lane.

**Criteria.** As ADR-0213: **C1** wire and serialization inertness for
undeclared schemas; **C2** round-trips through the names road as proof, never
as inference from absence; **C3** plumbing surface; and **C5** — new here —
*the reader can see the choice*: whatever the writer chose must be knowable
from the schema, because the read side has no other channel.

**Assessment.**

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | −  | ++ | +  |
| C2 | −− | −− | ++ | −− |
| C3 | −− | −  | ++ | −− |
| C5 | −− | +  | ++ | −  |

O1 fails C5 outright: a per-call codec is a choice the reader cannot see, so
it is the anti-pattern this decision exists to close. It also fails on Go's
own terms — a method with type parameters cannot appear in an interface, is
not in the type's method set, and is invisible to reflection, while
`marshallreflect` reaches the DML by `MethodByName`. O2 and O4 fail C2 and C3
for the reasons ADR-0213 gives: the DTO gains a field every serialized table
moves on, and encoding hints on membership lanes are machine-derived output
that the IR rebuild erases. O3 rides machinery that already round-trips.

## Decision

Declare the params codec as **one section use-aspect per codec, in the
exclusive family `params-codec`**. The first and only member is
`useaspects.AspectSectionParamsFixedWidthHex` (index 55), naming
`membership.{Append,Encode,Decode}Params`. Authored via
`TaggedValueSectionMerger.AddSectionParamsCodec(common.ParamsCodecFixedWidthHex)`,
which replaces an earlier declaration (the family is exclusive) and keeps
every other aspect; read back through `common.DeclaredParamsCodec`, which
answers `ParamsCodecUndeclared` when the section states none.

- **Undeclared means unstated, not "some other codec".** Every writer in the
  tree keeps producing the canonical form on undeclared sections; a consumer
  that needs the encoding may refuse an undeclared section rather than guess,
  and the ontology binding assumes the canonical codec on one.
- **The validator ties the aspect to its channel**: a declaration on a
  section without a params-bearing channel (`common.MembershipSpecParamsBearing`)
  is a schema error, as a single-membership declaration without its channel
  is.
- **Declaring renames the section's columns** — the use-aspects segment is in
  every tagged column name — so a declaration is a schema decision like any
  column change, never a side effect of a codec fix. A schema already
  addressed by hand-written SQL declares when its owner regenerates those.
- **The feature is inert**: an undeclared schema regenerates
  byte-identically; the leeway suite's goldens did not move. The sampler
  declares the codec on half the sections that carry a params-bearing
  channel, so the fuzzed generators cover declared schemas.
- **No generator consults the declaration yet.** The DML still takes raw
  bytes, read access still hands back the binary lane, `lwextract` and the
  read-back generator still assume. Making them codec-aware is the
  pluggability step this decision deliberately does not take: it waits for a
  second codec with a named consumer, and its shape is recorded in
  `hackathon_2026/doc/ontology/leeway-binding-notes-for-boxer.md` §5 — a
  `ParamsCodecI` with an SQL half, typed overloads beside the raw DML methods,
  the codec always taken from the schema.

## Surfaces

| Surface | Change |
| --- | --- |
| `useaspects` vocabulary | 1 member appended (55), exclusive family `params-codec` |
| `common` | `ParamsCodecE`, `AllParamsCodecs`, `MembershipSpecParamsBearing`, `DeclaredParamsCodec`, `GetParamsCodecByAspect`; `TaggedValueSectionMerger.AddSectionParamsCodec` |
| `TableValidator` | aspect requires a params-bearing channel |
| `PopulateManipulator` / `generateExampleAspects` | free sample never emits the aspect; the populator declares it coherently |

## Alternatives

- **A type parameter or generic method on the generated DML (O1).** The codec
  becomes a per-call choice the reader cannot see, which is the anti-pattern
  this decision exists to close. It also fails on Go's own terms: a method
  with type parameters cannot appear in an interface, is not in the type's
  method set and is invisible to reflection, while `marshallreflect` reaches
  the DML by `MethodByName`.
- **A field on `common.TaggedValuesSection` (O2).** The DTO gains a field
  every serialized table then has to move on, for a statement the names road
  already round-trips.
- **An encoding aspect on the params lane (O4).** Hints on membership lanes
  are machine-derived output that the IR rebuild erases, so the declaration
  would not survive a round trip.
- **Fix the out-of-tree writer and leave the convention undeclared.** That is
  the state this ADR is a response to: a convention without a declaration
  produced four encodings in six weeks, and the next writer still has nothing
  to read.
- **Make the generators codec-aware in the same step.** Deferred rather than
  rejected — it waits for a second codec with a named consumer, and its shape
  is recorded where the Decision's last bullet points.

## Consequences

A section can now say how its params are spelled, and the first consumer
outside the tree reads it. The cost of the next codec is known and bounded,
and it is paid only when someone needs one.

## Status

Proposed — 2026-09-14. Pre-acceptance: the front-matter `reviewed-by` and
`reviewed-date` are filled when it flips to accepted.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).
