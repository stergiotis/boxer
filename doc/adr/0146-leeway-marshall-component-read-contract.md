---
type: adr
status: proposed
date: 2026-07-27
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0146: leeway marshall — the component read contract

## Context

leeway's wire carries, per entity, a multiset of attributes keyed by
`(section, membership)`. The ECS reading of that wire — the one
[ADR-0075](0075-leeway-typed-component-views.md) adopted — wants
`entity → set of components`, where a component is a named bundle of
attributes. Component identity is **not** on the wire. So wire → components is
a projection, not a function, and it can alias: two components that claim the
same `(section, membership)` slot are indistinguishable on read.

That much is deliberate and follows from the model. What is not deliberate is
that each read surface improvised its own response to it.

### The measured asymmetry

Write-side correctness is decided **statically and locally**: from a DTO's own
tags plus the target DML's method set. `PlanBuilder.Finish` runs seven
whole-DTO checks and the generic `Validate` preflights the method set before
the first row. Read-side correctness additionally depends on what *else* is in the
row, on the caller's membership `lookup`, and on which reader it goes through.

Stated as one invariant: **every non-tuple field writes at most one attribute
per `(section, membership)` per row, and the read path accepts any number.**
The dynamic-membership tuple ([ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md)
/ [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md)) is the
sanctioned N-attribute spelling and correctly owns its section exclusively. The
distinction between the two exists in the model and is unenforced on read.

Measured against a row carrying two attributes under one membership:

| reader shape | `marshallreflect` | marshallgen `ReadRow` | readback SQL |
|---|---|---|---|
| mandatory scalar | error | error | `countEqual(…) = 1` in `Validator`; `Projection` takes the first |
| `option.Option[T]` | **silently absent** | error | `countEqual(…) <= 1`; `Projection` takes the first |
| container `[]T` | **silently concatenated** | concatenated (documented) | `countEqual(…) = 1`; `Projection` takes the first |

Three surfaces, three behaviours, two of them silent. A wrong `lookup` id is
silent for `Option` and container fields on every Go path.

### The decision record already contains the answer, three times over

- [ADR-0066](0066-leeway-dql-clickhouse-readback-generator.md) defines the
  complete contract as three artefacts — **Presence** (cheap, necessary, not
  sufficient), **Validator** (the exact conformance check), **Projection** (the
  decode) — and builds it. For ClickHouse SQL only.
- [ADR-0075](0075-leeway-typed-component-views.md) specifies the ECS vocabulary
  — `Detect` returning `Absent | Approximate | Exact`, `RequiredSections`,
  `SectionSpec` — and the one-sided guarantee that Approximate is necessary for
  Exact and never the reverse. None of it was built; the shipped `RendererI` is
  `Kind() / Title() / Render()`, and the demo hard-codes which components a row
  carries. The ADR's Consequences claim a hand-written leeway-side Presence
  helper exists; it does not.
- [ADR-0073](0073-leeway-membership-role.md) E1 settles the selection rule: "on
  the read side only primary memberships are discriminative". The
  `membershiprole` classifier is consumed by `card`, `table2_emitter` and
  `membership` — never by `marshall/` or `recordstore/`. In the codec every
  membership discriminates, annotations included.

So this ADR adds little new model. It builds the read half that three accepted
ADRs specified and only one delivered, in one back-end.

## Design space (QOC)

**Question.** How should a component's read be made sound when component
identity is absent from the wire?

**Options.**

- **O1 — Put component identity on the wire.** A per-attribute component tag,
  so selection becomes a function.
- **O2 — Derive a read contract from the Plan and enforce it everywhere.**
  Lift ADR-0066's Presence / Validator / Projection to a back-end-neutral
  derivation; every reader consumes it.
- **O3 — Per-field tolerance vocabulary.** A `,shared` tag flag by which a
  field declares that it expects to coexist, plus a fold (first / last /
  concat).
- **O4 — Leave the codec; document that callers must run the SQL validator.**

**Criteria.**

- **C1 — Realizes decisions already accepted** rather than adding model.
- **C2 — Uniformity** across the four read surfaces.
- **C3 — Wire compatibility** — no format change, no migration.
- **C4 — ECS legibility** — does the archetype become inspectable?
- **C5 — Blast radius** on existing DTOs and generated code.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | −− | ++ | −  | +  |
| C2 | +  | ++ | −  | −− |
| C3 | −− | ++ | ++ | ++ |
| C4 | ++ | ++ | +  | −− |
| C5 | −− | −  | −  | ++ |

O2 wins on every criterion except blast radius, and its blast radius is bounded
by the phasing in §Scope. O3 was the first sketch in design dialogue and was
dropped once ADR-0073 E1 surfaced: role already distinguishes identity-bearing
from annotating memberships, so a new tag flag would be a second, parallel
vocabulary for a distinction the model has.

## Decision

**D1 — The read contract is a derivation from the Plan, computed once.** A
`ReadContract` names, for each `(section, membership)` slot a kind claims, the
membership channel and the **arity** the writer guarantees: exactly one
attribute for a mandatory scalar or a container (a container attribute carries
N *values*, not N attributes), zero-or-one for `option.Option[T]`, and
unbounded for a tuple-owned section. Every back-end derives Presence /
Validator / Projection from it instead of re-deriving from Go shape. The
readback generator's `presenceSet` / `countEqual` logic is the reference
implementation and moves up; `ReadContract` lives in `mappingplan`, which is
already the neutral foundation every target imports
([ADR-0074](0074-leeway-marshall-package-layout.md)).

**D2 — `PresenceE` and `Detect` are built, as ADR-0075 specified.**
`Detect[T](readers, i) PresenceE` returns `Absent` when no claimed slot is
populated, `Approximate` when Presence holds, and `Exact` when Presence and
Validator both hold. One refinement to ADR-0075: `Exact` is decided **without
decoding values** — conformance is an arity question, and conflating it with a
successful unmarshal made the check more expensive than it needs to be and hid
decode failures inside a detection result. `ReadComponent[T]` is Detect
followed by Projection; `Unmarshal` becomes the batch form that requires
Presence on every row.

**D3 — Selection is role-filtered, realizing ADR-0073 E1.** Only primary
memberships discriminate; secondary memberships annotate the attribute a
primary one located, and must not pull a value into a field. The codec takes a
`membershiprole.ClassifierI` the way it already takes a `LookupI`.

*A caveat that must not be lost:* the shipped `DefaultClassifier` marks primary
by a path prefix (default `/`), under which ordinary DTO memberships —
`health`, `battery` — classify as **secondary**. It is therefore not a safe
default here. The codec's default is a nil classifier meaning *all
memberships primary*, which is exactly today's behaviour; the section
use-aspects `AspectSectionMembershipsAllPrimary` / `…AllSecondary` short-circuit
it as ADR-0073 F intends. Role filtering is inert until an application supplies
a policy.

**D4 — Arity is enforced uniformly on read.** A surplus attribute on a claimed
primary slot is an error on every read path. This closes the silent
concatenation, the silent `Option` absence, and the SQL projection's
first-match — the three rows of the table above become one behaviour.

**D5 — Slot disjointness is the composition law, checked in a registry.** The
ECS property that matters is that components compose independently, which is
exactly: *two components may share a section, but must not claim the same
`(section, primary membership)` slot.* A component registry holds the
registered Plans and checks that pairwise. An archetype is a set of registered
kinds; `ArchetypePresence` is the conjunction of their `Detect` results — the
leeway twin of the `ecsdemo` stage-1 helper. The registry is a new package
under `leeway/marshall`; it holds cross-kind state that does not belong in
`mappingplan`.

The existing per-DTO guard is also wrong and is corrected here: `AddField` keys
uniqueness on `membership + "\x00" + column`, **not** scoped by section, so it
rejects a DTO declaring `tag,symbol` alongside `tag,u64Array` — unambiguous on
read, because the two sections have separate readers. The key becomes
section-scoped; the cross-DTO case moves to the registry, where it belongs.

**D6 — One section visit per entity; ADR-0070 D3 is retracted.** ADR-0070 D3
states that sections may repeat across DTOs in one entity, producing two
`BeginSectionFoo`…`EndSection` cycles. No generated DML supports this:
`BeginEntity` calls `beginSections()` once, `EndSection` returns the section to
`Initial`, and nothing reopens it, so a second visit fails with a bare
`invalid state transition`. It was only ever exercised against a recording mock
with no state machine.

Rather than make sections re-enterable, the rule becomes one visit per section
per entity, and [ADR-0008](0008-leeway-marshall-extensions.md) D2's per-section
`1,1,…,>1,>1,…` cardinality ordering moves **inside** a single
`GetSection`/`EndSection` frame — `marshalSection` already holds every field it
needs. `RowComposer.AddSingleValueAttributes` / `AddMultiValueAttributes` are
then removable: they exist to produce that ordering across two passes, and any
section holding fields of both cardinality classes makes them visit it twice,
which is the failure above. The API has no consumers outside its own tests.

### Scope and phasing

- **M1 — `ReadContract` derivation + `PresenceE` / `Detect`.** ✓
- **M2 — arity enforcement** in `marshallreflect.Unmarshal` and marshallgen's
  `FillFromArrow` / `ReadRow`. ✓ Landed in two parts, because the codegen half
  regenerates 44 artefacts and its diff had to stay separable from the runtime
  half: **M2a** the reflect decode, **M2b** the two emitters.
- **M3 — the registry + the section-scoped uniqueness key.** ✓
- **M4 — role filtering**, inert by default. ✓ Reflect front-end only:
  generated codecs resolve memberships to package-level `kindXxx` vars at init
  and take no per-read policy, so giving them a classifier means a signature
  change across every generated codec. With the default the two front-ends
  agree exactly, so the asymmetry only exists for a caller that opts in; taking
  it to codegen waits for a consumer.
- **M5 — the single-section-visit rule** and the two-pass removal. ✓ One
  correction to D6 as written: the runtime-cardinality ordering is **dropped**,
  not folded into the section frame. Folding it would reorder attributes at
  runtime in the reflect front-end only, breaking the byte-parity invariant with
  marshallgen's `BuildEntities`; matching it in codegen would change the wire for
  existing data to satisfy no consumer. ADR-0071 C1's static
  scalar-before-container partition — the ordering that IS decided — is
  untouched. [ADR-0101](0101-leeway-marshall-mixed-shape-sections.md) D7, which
  named the two passes, is superseded there.
- **M2c — the ClickHouse artefacts take their arity from `ReadContract`.** ✓
  Pulled forward out of Deferred once M1 showed the divergence was a defect,
  not just an inconsistency.
- **Deferred** — verifying resolved `lookup` ids against the wire; archetype
  APIs beyond `ArchetypePresence`; a generated Presence prefilter, which
  ADR-0075 already deferred to ADR-0066's codegen.

M1 corrected one premise of that first deferral. It was written believing the
ClickHouse back-end already enforced the contract correctly, so moving it would
be consolidation rather than a fix. Measuring the write path showed otherwise
for **non-Option container fields**: a `[]T` or `*roaring.Bitmap` field emits
**zero** attributes when empty — `marshalContainer` splices it — so its arity is
`[0..1]`, but the generator treats every non-Option field as mandatory and emits
a presence literal plus `countEqual(…) = 1`, i.e. `[1..1]`. A row whose
container field is legitimately empty therefore fails the Presence and Validator
its own kind generates. The Go read paths accept it, so the two back-ends
disagree about the same row.

M2c fixed it: `Generate` derives the contract and reads each slot's arity from
it. Landing that surfaced a second problem the ADR had not anticipated —
dropping the container's presence literal left an all-optional kind with **no**
presence terms, so its `Filter` matched every row and the store's
`Scan<Component>` would have returned everything. `ReadContract.Verdict` does
not have that hole: it reports a row populating nothing as `Absent`. So Presence
now falls back to the **disjunction** of the kind's slots when none is required,
which is the SQL reading of `Verdict`'s `populated`. Recorded on
[ADR-0066](0066-leeway-dql-clickhouse-readback-generator.md), whose Presence
this narrows from "necessary for conformance" to "necessary for carrying the
kind" — the two differ only for all-optional kinds.

M2 turned up a second thing worth recording, about the artefacts rather than
the design. The committed `.out.go` codecs had fallen behind marshallgen:
regenerating with an unchanged emitter produced a 24-file diff (`KindVar`
naming, an ADR-0100 SD6 doc comment, the `AddSections` surface). Proving the
generators reproduce their own output *before* changing them is what kept M2b's
diff readable, and is worth doing again for M5. `scripts/dev/generate.sh` drives
only the keelson codecs — the `--target=anchor` codecdemo set and the
test-driven recordstore / ecsdemo regenerations live outside it, which is how
the drift went unnoticed.

## Alternatives

Beyond the QOC matrix:

- **Make sections re-enterable** rather than retracting ADR-0070 D3. Rejected
  for now: it changes the generated DML's state machine and every DML's
  per-section attribute counter, to serve an API with no consumers. If a
  consumer appears, this is the ADR to supersede.
- **Compute role statically into the Plan** instead of taking a classifier.
  Rejected: ADR-0073 F places role at the value level deliberately, because one
  membership kind-slot may hold both roles by name, and a schema-level role
  field cannot express that.
- **Make `Detect` decode** (ADR-0075's literal "exact = a typed unmarshal
  succeeds"). Rejected per D2: it makes detection cost a decode and folds
  decode errors into a presence verdict.

## Consequences

### Positive

- One definition of the read contract, consumed by every back-end, instead of
  four independently drifting ones.
- The three silent-corruption modes measured above become errors.
- The archetype becomes inspectable: a registry of Plans, a disjointness law,
  and a `Detect` per kind.
- The over-broad per-DTO uniqueness key stops rejecting valid DTOs.

### Negative

- D4 is a behaviour break for any reader that relies on today's container
  concatenation. The parity corpus and the round-trip suites are the detector;
  no in-tree consumer is known to depend on it, and a third-party producer that
  legitimately emits several attributes under one membership must move to a
  tuple section.
- Removing the two-pass `RowComposer` methods is an exported-API removal, even
  though nothing outside tests calls them.
- Role filtering adds a second optional policy object alongside `LookupI`, and
  ADR-0073's own consequence applies: two consumers with different classifiers
  key differently.

### Neutral

- No wire format change; `ReadContract` is derived from what the Plan already
  carries.
- ADR-0075's renderer registry is unaffected — it consumes decoded values and
  gains a detection source it currently lacks.

## Status

Proposed on 2026-07-27.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers.

## References

- [ADR-0066](0066-leeway-dql-clickhouse-readback-generator.md) — Presence /
  Validator / Projection, the reference implementation this generalises.
- [ADR-0070](0070-leeway-entity-assembly.md) — entity assembly; D3 retracted by
  D6 here.
- [ADR-0073](0073-leeway-membership-role.md) — membership role; E1's read-side
  rule realized by D3 here.
- [ADR-0074](0074-leeway-marshall-package-layout.md) — where `mappingplan`,
  `goplan` and the two front-ends sit.
- [ADR-0075](0075-leeway-typed-component-views.md) — the ECS component
  vocabulary; its detection half built by D2 here.
- [ADR-0100](0100-recordstore-generated-leeway-clickhouse-store.md) — the
  presence-gated `ReadRow` component read this consolidates with.
- [ADR-0103](0103-leeway-marshall-dynamic-membership-tuples.md) /
  [ADR-0109](0109-leeway-marshall-multi-membership-ref-tuples.md) — the
  sanctioned N-attributes-per-membership spelling.
- [`../../public/semistructured/leeway/marshall/clickhouse/readback/`](../../public/semistructured/leeway/marshall/clickhouse/readback/)
  — the artefact generator.
- [`../../public/semistructured/leeway/membershiprole/`](../../public/semistructured/leeway/membershiprole/)
  — the classifier D3 consumes.
