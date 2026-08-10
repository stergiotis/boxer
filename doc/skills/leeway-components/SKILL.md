---
name: leeway-components
description: "Use when modelling, writing, or reading leeway components — the ECS layer: component DTOs over (section, membership) slots, cross-kind overlap rules, membership-id agreement, RowComposer/AddSections write composition, ReadRow presence and arity, archetypes, FatRow.Extract, and the recordstore disjoint-sections gate."
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leeway components — the ECS layer

This skill covers the layer *above* the leeway protocol: how flat Go DTOs
("components", "kinds") map onto sections and memberships, what may overlap,
how several components compose into one entity, and how a component reads
back out of a row. Prerequisites: [leeway-beginner](../leeway-beginner/SKILL.md)
(backbone vs payload) and [leeway-advanced](../leeway-advanced/SKILL.md)
(memberships, channels, co-sections, membership roles) — nothing from those is
restated here.

The governing decisions are
[ADR-0146](../../adr/0146-leeway-marshall-component-read-contract.md) (the
component read contract), [ADR-0075](../../adr/0075-leeway-typed-component-views.md)
(typed component views) and [ADR-0070](../../adr/0070-leeway-entity-assembly.md)
(entity assembly). The worked example is
[`anchor/ecsdemo`](../../../public/semistructured/leeway/anchor/ecsdemo/)
(see its `EXPLANATION.md`).

## What a component is

- A **component** (a *kind*) is a flat `lw:`-tagged Go struct with a
  `kind:"…"` tag. Its fields claim **slots** — `(section, membership)` pairs —
  each with an **arity** the writer guarantees (exactly-one for a mandatory
  scalar or container attribute, zero-or-one for `option.Option[T]`,
  unbounded for a tuple-owned section).
- A component is **not** a leeway schema concept. The wire carries, per
  entity, a multiset of attributes keyed by `(section, membership)`;
  **component identity is not on the wire**. Wire → components is a
  projection, not a function — and projections can alias: two components
  claiming the same slot are indistinguishable on read (ADR-0146 Context).
- The schema (`TableDesc`) knows sections, channels and membership *specs* —
  it holds no membership names and no membership ids. Names and ids live in
  the DTO layer and its lookup/registry.

## Rules at a glance

| Question | Answer |
|---|---|
| May two kinds bind the same section? | Yes — the model expects it (ADR-0146 D5). One exception: within a single generated recordstore (below). |
| May two kinds claim the same `(section, membership)` slot? | Yes — deliberate under fusion/enrichment. The component registry reports it (`SlotClaims`), never rejects it. |
| May one DTO claim one slot twice? | No — per-DTO uniqueness, keyed per section, is an authoring error caught at plan time. |
| May anything else target a tuple-owned section? | No — a dynamic-membership tuple owns its section exclusively, within that DTO (ADR-0103). |
| One row carrying two attributes on a claimed slot? | Read error on every path — arity is uniform (ADR-0146 D4) — unless the slot is tuple-owned. |
| What isolates kinds sharing a section? | The membership tags (ref ids / verbatim names): decode matches `(section reader, membership value)`, never "kind". |
| What actually breaks sharing? | Membership → id **disagreement** — e.g. per-plan positional numbering without a shared lookup. |
| Inside one generated recordstore? | Components must bind disjoint sections — a generator precondition (ADR-0100 SD6), not a model rule; lift recorded as ADR-0105 D2. |

## Overlap is expected; tagging is the isolation

ADR-0146 D5: facts are fused, enriched and aggregated by stages that do not
know every component in the system; no process holds the global component
vocabulary, so slot disjointness cannot be a law, and **detecting overwriting
components is a non-goal**. The component registry
([`marshall/component`](../../../public/semistructured/leeway/marshall/component/))
catalogues kinds, their sections and slot claims — a key with more than one
claimant is worth knowing and deliberately not prevented.

What keeps co-resident kinds apart in a shared section is the membership
tagging: on a ref channel the membership rides as a `uint64` id, on a
verbatim channel as the literal name; readers loop a section's attributes and
`switch` on the membership value. A kind only matches its own memberships —
attributes no bound kind claims are ignored (ADR-0075: detection is
one-sided under fusion).

## The id-agreement rule (the real hazard)

Correct decode across kinds requires exactly one thing: **writer and reader
agree on the membership → id mapping**. Sharing sections is safe; disagreeing
about ids is not — and that hazard exists whether or not sections are shared.
There are two assignment regimes:

- **Registry-backed targets** (the keelson facts pattern): ids resolve from a
  vocabulary registry (`vdd.Memb<Name>.GetId()`), globally unique — sharing
  is trivially safe.
- **Schema-agnostic targets** (`marshallgen.NoOpWrapper`, used by anchor
  examples and the recordstore): each plan numbers its memberships
  `1..N` in declaration order, emitted as `const kind<Membership> uint64`.
  Per-plan, so two kinds' *distinct* memberships can carry the same id.

Corollaries of the per-plan regime:

- Several generated kinds in one package coexist because the const *symbol*
  is keyed on the membership name (schema-global names ⇒ unique symbols) —
  but the *values* still collide across kinds (`kindDeviceStatus = 1`,
  `kindDeviceCharge = 1`, …).
- Two kinds reusing one membership **name** in one package would emit
  duplicate consts — a build break. `anchor/ecsdemo` avoids it by generating
  a codec only for the fat kind and reading the component kinds reflectively
  through one shared `marshallreflect.MapLookup` (`droneLookup`) — the
  shared-lookup pattern that makes full slot overlap work.
- Id drift is a **diagnosis, not a check**: a wrong lookup id observes
  exactly what an absent component observes (for `Option`/container slots
  zero matches is legal). `InspectLookup[T]` / `LookupReport.Suspect()`
  report per-slot resolution against what the section actually carries
  (ADR-0146, 2026-07-27 update).

## Write path — composing components into one entity

- A generated DML opens each section frame **once per entity**
  (`BeginEntity` → `beginSections()`; `EndSection` returns the section to its
  initial state and nothing reopens it). A second visit to one section inside
  an entity frame fails with an invalid-state-transition error at commit.
- The reflect composer
  ([`marshallreflect.RowComposer`](../../../public/semistructured/leeway/marshall/go/marshallreflect/))
  is the sanctioned way to express overlap on write: `BeginRow(dtoA)` owns
  the plains, `AddSections(dtoB…)` buffers each DTO's per-section
  attributes, and `CommitRow` writes **one frame per section carrying every
  contribution in call order** (ADR-0146 D6, retracting ADR-0070 D3's
  two-visit shape). Cost: an attribute-emit failure surfaces at `CommitRow`
  (naming the section), not at the `AddSections` call that supplied it.
- The generated twin `<Kind>AddSections(dml, row)` opens and closes its own
  frames, so several *generated* kinds compose under one entity frame only
  when their sections are disjoint (the recordstore `Begin`/`Add<Kind>`
  pattern). Same-section composition in one entity frame needs the reflect
  `RowComposer` today.
- Every non-tuple field writes **at most one attribute per slot per row**;
  the tuple is the sanctioned N-attribute spelling. An **empty container**
  writes no membership at all and reads back as absent — "present with an
  empty list" is unrepresentable.

## Read path — presence, arity, archetype

- Decode is presence-gated and membership-matched (`<Kind>ReadRow`,
  `FillFromArrow`, `marshallreflect.Unmarshal`): a row carrying none of the
  kind's memberships is `present=false`, never an error; a row carrying only
  some fields decodes `present` with the missing fields zero-valued. Exact
  conformance is a separate question (the Validator artefact / `Detect`
  returning `Exact`).
- **Arity is uniform on read** (ADR-0146 D4): a surplus attribute on a
  claimed primary slot errors on every path — reflect, generated, and the
  ClickHouse Presence/Validator/Projection artefacts, which derive from the
  same `ReadContract`. (Before D4, scalars errored while containers silently
  concatenated; that split is gone.)
- `Detect[T](readers, i)` returns `Absent | Approximate | Exact` without
  decoding values; `ReadComponent[T]` is `Detect` + projection;
  `ArchetypePresence` folds per-kind verdicts — an **archetype** is the set
  of registered kinds a row carries.
- `FatRow.Extract[T]` (ecsdemo) is the same mechanism ad hoc: reflect-read
  one component out of a wide row; the DTO's `lw:` tags select the sections,
  membership matching selects the values, the rest of the row is ignored.
- Membership-role filtering (ADR-0146 D3) is inert by default (nil
  classifier = every membership primary). The shipped `DefaultClassifier`
  marks primary by path prefix, under which ordinary DTO memberships
  classify as secondary — not a safe default for codec reads.

## The recordstore exception (store-local, not model)

`recordstore/gen` requires the components of **one generated store** to bind
disjoint sections and rejects violations at generation time. This is a
precondition of that generator — it drives the per-plan `NoOpWrapper` ids
into decode switches and baked `Scan` filter SQL, where a shared section
under colliding ids would silently cross-read — not a rule of the component
model (ADR-0100 SD6, as corrected 2026-08-10). Notes:

- The gate is cross-kind per section; one kind may hold several memberships
  in one section.
- Two kinds reusing a membership name in *different* sections pass the gate
  but break the build (duplicate `kind<Name>` consts).
- The lift is recorded, not built: ADR-0105 D2 — a caller-supplied
  membership-id override on `gen.Input`, with the gate relaxed to id-level
  disjointness under the override.

## Pointers

- Decisions: [ADR-0146](../../adr/0146-leeway-marshall-component-read-contract.md)
  (read contract: D4 arity, D5 overlap, D6 shared frame),
  [ADR-0075](../../adr/0075-leeway-typed-component-views.md) (component
  views, archetypes), [ADR-0070](../../adr/0070-leeway-entity-assembly.md)
  (entity assembly; D3 retracted),
  [ADR-0103](../../adr/0103-leeway-marshall-dynamic-membership-tuples.md) /
  [ADR-0109](../../adr/0109-leeway-marshall-multi-membership-ref-tuples.md)
  (tuples), [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md)
  SD6 and [ADR-0105](../../adr/0105-keelson-adopts-generated-record-stores.md)
  D2 (the store gate and its lift).
- Code: [`mappingplan`](../../../public/semistructured/leeway/mappingplan/)
  (`ReadContract`, `Slot`),
  [`marshall/component`](../../../public/semistructured/leeway/marshall/component/)
  (the registry),
  [`marshallreflect`](../../../public/semistructured/leeway/marshall/go/marshallreflect/)
  (`RowComposer`, `Unmarshal`, `InspectLookup`),
  [`marshallgen`](../../../public/semistructured/leeway/marshall/go/marshallgen/)
  (`AddSections`/`ReadRow` emitters),
  [`anchor/ecsdemo`](../../../public/semistructured/leeway/anchor/ecsdemo/)
  (the overlap worked example),
  [`recordstore`](../../../public/storage/recordstore/) (the gated consumer).
- How-to: [leeway-marshalling](../../howto/leeway-marshalling.md) — the
  single-DTO tag grammar this layer builds on.
