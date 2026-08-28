---
name: leeway-components
description: "Use when modelling, writing, or reading leeway components — the ECS layer: component DTOs over (section, membership) slots, registry-resolved membership ids on a shared facts table, cross-kind overlap rules, RowComposer/AddSections write composition, ReadRow presence and arity, archetypes, FatRow.Extract, and the recordstore disjoint-sections gate."
type: reference
audience: agent reading this skill
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Leeway components — the ECS layer

This skill covers the layer *above* the leeway protocol: how components map onto
sections and memberships, what may overlap, how several compose into one entity,
and how one reads back out of a row. Prerequisites:
[leeway-beginner](../leeway-beginner/SKILL.md) (backbone vs payload) and
[leeway-advanced](../leeway-advanced/SKILL.md) (memberships, channels,
co-sections, membership roles) — nothing from those is restated here.

## Two kinds of typing, and why that is the whole model

**Components are structurally typed.** A component is satisfied the way a Go
interface is: by shape, retroactively, with no declaration on either side. A row
satisfies a component when it carries that component's slots — whether or not the
writer had ever heard of it, and whether it was written this morning or years
ago. Satisfaction is decided per row at read time; the contract is static, the
conformance is dynamic. There is no subtyping anywhere, and the 90s class-tag
"is-a" model is the wrong picture to bring.

**Memberships are nominally typed.** A membership is a *name* that a registry
mints into a timeless id. Two domains mean the same slot exactly when they used
the same name from the same vocabulary. Everything else in this layer works
because that one thing does.

That pairing is what makes late formulation possible: a component can be
written *after* the data exists, because the only thing that had to be agreed in
advance is a name.

## The main scenario

Thousands of domain-owned components, written by independent stages and
deployments into one shared `boxer.facts` table, with no coordination point at
write time. A domain formulates a component today over rows another domain wrote
months ago; ids come from the vocabulary registry both went to.

The worked example runs it:
[`runtime/factsschema/meshdemo`](../../../public/keelson/runtime/factsschema/meshdemo/)
— an agent writing through a generated store, and a component formulated
afterwards (a Go struct with no generated artifact at all) reading those rows in
memory and through the table. Read it before the rules below; the rules are what
it obeys.

The **closed world** — one program owning both the writing and the reading, ids
it assigns itself — is the special case, and a legitimate one. It is what
[`anchor/ecsdemo`](../../../public/semistructured/leeway/anchor/ecsdemo/),
[`recordstore/example`](../../../public/storage/recordstore/example/) and
[`recordstore/sharedsection`](../../../public/storage/recordstore/sharedsection/)
demonstrate. Take it deliberately, not by default.

## The id-agreement rule

Correct decode across components requires exactly one thing: **writer and reader
agree on the membership → id mapping**. Sharing a section is safe; disagreeing
about ids is not, and that hazard exists whether or not sections are shared.

- **Registry-resolved ids** are the default. A vocabulary package claims a tag
  value from [`identity/tagmint`](../../../public/identity/tagmint/) and
  registers its names in a
  [`namemint`](../../../public/semistructured/leeway/namemint/registry/)
  registry, which composes tag and declared ordinal into a timeless id
  (ADR-0183 D0). Both id seams take the same snapshot: generated stores through
  `storegen.MembershipIds`, the reflect path through
  `marshallreflect.NewRegistryLookup`. Every vocabulary commits its
  `(ordinal, name, id)` table under `testdata/`, and a repo-wide check asserts
  they stay globally disjoint.
- **Hand-written ids** — a `MapLookup` literal, or `marshallgen.NoOpWrapper`'s
  declaration-order numbering — are the closed-world spelling. Nothing ties a
  literal to a vocabulary, so use it where nothing else writes the table.
- **Drift is a diagnosis, not a check.** A wrong id observes exactly what an
  absent component observes: for `Option`/container slots, zero matches is a
  legal reading. `InspectLookup[T]` / `LookupReport.Suspect()` report what each
  slot resolved to against what the section actually carries. See the corpus
  entries I1 and I2 below.

## A component is a plan; the struct is one way to write it

The definition is a [`mappingplan.Plan`](../../../public/semistructured/leeway/mappingplan/):
slots, arities, channels, sections. A `lw:`-tagged Go struct is one authoring
syntax for it — there are three, and they produce the same plan:

| Authoring syntax | Reads the tags | Used by |
|---|---|---|
| Go struct at run time | `reflect` | `marshallreflect.PlanFor[T]` |
| Go struct at build time | `go/ast` | `marshallgen`, the store generators |
| No struct at all | — | a hand-built `Plan` (the `mappingplanview` playground) |

Two consequences worth holding: a component needs no Go type to exist, which is
what lets one be assembled at run time; and the plan still carries Go residue
(`GoType()`, `KindVar()`, Add-method descriptors), which any future non-Go plan
source inherits as a known constraint (ADR-0183 D6).

A component is **not** a leeway schema concept. The wire carries, per entity, a
multiset of attributes keyed by `(section, membership)`; **component identity is
not on the wire**. Wire → components is a projection, and projections can alias:
two components claiming one slot are indistinguishable on read. The schema
(`TableDesc`) knows sections, channels and membership *specs* — it holds no
membership names and no ids.

**Kind markers are assertions, not identity.** A `kind` attribute is just
another membership; several may ride one row. Gating a read on it is
writer-cooperative and forfeits late formulation — a component formulated later
cannot be named by rows written earlier. Slots decide; assertions only
accelerate. `Filter`, `Detect` and `ArchetypePresence` already ignore them.

## Rules at a glance

| Question | Answer |
|---|---|
| May two kinds bind the same section? | Yes — the model expects it (ADR-0146 D5). One exception: within a single generated recordstore (below). |
| May two kinds claim the same `(section, membership)` slot? | Yes — deliberate under fusion/enrichment. The component registry reports it (`SlotClaims`), never rejects it. |
| May one DTO claim one slot twice? | No — per-DTO uniqueness, keyed per section, is an authoring error caught at plan time. |
| May anything else target a tuple-owned section? | No — a dynamic-membership tuple owns its section exclusively, within that DTO (ADR-0103). |
| One row carrying two attributes on a claimed slot? | Read error on every path — arity is uniform (ADR-0146 D4) — unless the slot is tuple-owned. |
| One attribute carrying two values under a `,unit` field? | Read error on every path (ADR-0183 D5). |
| What isolates kinds sharing a section? | The membership tags (ref ids / verbatim names): decode matches `(section reader, membership value)`, never "kind". |
| What actually breaks sharing? | Membership → id **disagreement**. |
| What does "per-plan" mean? | Per `mappingplan.Plan` — one plan per kind-tagged **root** DTO. Tuple/nested element structs fold into the parent's plan. `NoOpWrapper` numbers a plan's **unique ref-channel memberships** 1..N in declaration order — distinct memberships, not fields; verbatim channels carry none. |
| Inside one generated recordstore? | Under the default per-plan ids, components must bind disjoint sections — a generator precondition (ADR-0100 SD6), not a model rule. Registry-stable ids lift it to id-level disjointness. |

## Read path — presence, arity, archetype

- Decode is presence-gated and membership-matched (`<Kind>ReadRow`,
  `FillFromArrow`, `marshallreflect.Unmarshal`): a row carrying none of the
  kind's memberships is `present=false`, never an error; a row carrying only
  some fields decodes `present` with the missing fields zero-valued. Conformance
  is a separate question with a separate answer — `Detect`'s verdict, or the
  `Filter` a generated `Scan` embeds.
- **Arity is uniform on read** (ADR-0146 D4): a surplus attribute on a claimed
  primary slot errors on both Go paths and the ClickHouse lane refuses it in the
  **Validator**. The `Projection` artefact alone does **not** validate: asked
  without its Filter it takes the first match, so a projection is only as
  trustworthy as the Filter beside it.
- **Value count is uniform too** (ADR-0183 D5): a `,unit` field promises its
  attribute carries exactly one value, and a longer one is refused on the same
  three paths.
- **Widening is safe; narrowing is loud.** A slot's admits-set may only grow —
  required → optional → many — and a widened definition keeps reading rows
  written under the narrower one, so wide readers and narrow writers coexist
  indefinitely. Crossing to a dynamic-membership tuple works too, with a catch:
  a tuple field is scoped to its **section**, not to a membership, so it
  consumes a co-resident component's attributes as well as its own.
- `Detect[T](readers, i)` returns `Absent | Approximate | Exact` without decoding
  values; `ReadComponent[T]` is `Detect` + projection; `ArchetypePresence` folds
  per-kind verdicts — an **archetype** is the set of registered kinds a row
  carries.
- `FatRow.Extract[T]` (ecsdemo) is the same mechanism ad hoc: reflect-read one
  component out of a wide row; the rest of the row is ignored.
- Membership-role filtering (ADR-0146 D3) is inert by default (nil classifier =
  every membership primary). The shipped `PathPrefixClassifier` marks primary by
  path prefix, under which ordinary DTO memberships classify as secondary — not
  a safe default for codec reads.

## Write path — composing components into one entity

- A generated DML opens each section frame **once per entity**. A second visit
  to one section inside an entity frame fails with an invalid-state-transition
  error at commit.
- The reflect composer
  ([`marshallreflect.RowComposer`](../../../public/semistructured/leeway/marshall/go/marshallreflect/))
  is the sanctioned way to express overlap on write: `BeginRow(dtoA)` owns the
  plains, `AddSections(dtoB…)` buffers each DTO's per-section attributes, and
  `CommitRow` writes **one frame per section carrying every contribution in call
  order** (ADR-0146 D6). Cost: an attribute-emit failure surfaces at `CommitRow`
  (naming the section), not at the `AddSections` call that supplied it.
- The generated twin `<Kind>AddSections(dml, row)` opens and closes its own
  frames, so several *generated* kinds compose under one entity frame only when
  their sections are disjoint. Same-section composition in one entity frame needs
  the reflect `RowComposer` today.
- Every non-tuple field writes **at most one attribute per slot per row**; the
  tuple is the sanctioned N-attribute spelling.

## The recordstore exception (store-local, not model)

Under its default id regime, `recordstore/gen` requires the components of **one
generated store** to bind disjoint sections and rejects violations at generation
time. This is a precondition of that regime — per-plan `NoOpWrapper` ids baked
into decode switches and `Scan` filter SQL, where a shared section under
colliding ids would silently cross-read — not a rule of the component model
(ADR-0100 SD6, as corrected 2026-08-10). Notes:

- The gate is cross-kind per section and counts only sections a kind reaches
  through a ref channel; one kind may hold several memberships in one section.
  A literal-name (`lowCardVerbatim`) membership is matched by its bytes on its
  own lane and cannot alias by id, so all-verbatim kinds may share sections
  under any id source (2026-08-28; before that the gate was channel-blind and
  such a store passed only via an empty `FixedIdsWrapper`).
- Two kinds naming one membership is a generation error under any id regime —
  for ref names because the `kind<Name>` symbol is declared once per generated
  package, for verbatim names per `(section, name)` because a component is
  present on any matched slot; cross-kind slot sharing inside one store needs
  the reflect path. On a shared table this is why two domains get two stores
  rather than one.
- The lift landed 2026-08-10 (ADR-0105 D2): `gen.Input.Wrapper` selects the id
  source and the gate relaxes to id-level disjointness. One source feeds the
  codec consts, the baked `Scan` filters and the exported `<Store>MembershipIds`
  map, so they cannot disagree.

## What is silent, and where it is pinned

Prose about behaviour drifts from behaviour. These are the readings a consumer
meets that no error announces — each has a test that is the authority, and the
test is where to look first when a field is unexpectedly empty.

| You see | It means | Pinned by |
|---|---|---|
| A field reads empty, no error | A wrong lookup id and honest absence are one observation (I1) | `marshallreflect_test/failure_modes_test.go` |
| A whole batch reads as carrying nothing | The reader is on a different assignment than the writer (I2) | same |
| `present` with zero-valued fields | Present is not conforming (R1); ask `Detect` or the Filter | same |
| An empty slice where you wrote one | Empty, nil and never-written are one wire observation (R2) — emptiness cannot be asserted, only observed | same |
| An all-container kind reads absent | A container writes zero attributes when empty (splice semantics, ADR-0146 M1), so a kind whose memberships are all containers has no presence signal on a row where every container is empty; give it a scalar membership if "present with nothing" must be observable | `recordstore/example/empty_container_test.go` |
| A string that changed by itself | Decoded strings alias the Arrow record; retain it or copy (R6) | same |
| A widened field still reads old rows | Widening is admits-superset by construction | `marshallreflect_test/arity_evolution_test.go` |
| A tuple field carrying a neighbour's attribute | Tuple fields are section-scoped | same |
| Two front-ends disagreeing | They must not; each deliberate asymmetry is recorded | `marshallreflect_test/parity_corpus_test.go` |

## Pointers

- **The load-bearing decisions** — thirteen ADRs touch this layer; these are the
  ones a consumer needs. [ADR-0146](../../adr/0146-leeway-marshall-component-read-contract.md)
  (read contract: D4 arity, D5 overlap, D6 shared frame),
  [ADR-0066](../../adr/0066-leeway-dql-clickhouse-readback-generator.md)
  (Scan/Filter, presence), [ADR-0100](../../adr/0100-recordstore-generated-leeway-clickhouse-store.md)
  SD6 and [ADR-0105](../../adr/0105-keelson-adopts-generated-record-stores.md) D2
  (the store gate and its lift), [ADR-0073](../../adr/0073-leeway-membership-role.md)
  (roles), [ADR-0183](../../adr/0183-leeway-component-consumer-simplification.md)
  (id regime, value-count arity, this skill's shape),
  [ADR-0182](../../adr/0182-leeway-aspects-v2-codec-and-vocabulary.md) (timeless
  vocabulary). [ADR-0075](../../adr/0075-leeway-typed-component-views.md),
  [ADR-0070](../../adr/0070-leeway-entity-assembly.md) (D3 retracted),
  [ADR-0103](../../adr/0103-leeway-marshall-dynamic-membership-tuples.md) and
  [ADR-0109](../../adr/0109-leeway-marshall-multi-membership-ref-tuples.md) are
  the shape record for tuples and views. The rest are historical for consumers.
- **Running code**:
  [`meshdemo`](../../../public/keelson/runtime/factsschema/meshdemo/) (the main
  scenario), [`sharedsection`](../../../public/storage/recordstore/sharedsection/)
  (two kinds, one section, registry-stable ids),
  [`anchor/ecsdemo`](../../../public/semistructured/leeway/anchor/ecsdemo/) (the
  closed-world overlap example).
- **Code**: [`mappingplan`](../../../public/semistructured/leeway/mappingplan/)
  (`ReadContract`, `Slot`),
  [`marshall/component`](../../../public/semistructured/leeway/marshall/component/)
  (the registry),
  [`marshallreflect`](../../../public/semistructured/leeway/marshall/go/marshallreflect/)
  (`RowComposer`, `Unmarshal`, `InspectLookup`, `NewRegistryLookup`),
  [`marshallgen`](../../../public/semistructured/leeway/marshall/go/marshallgen/)
  (`AddSections`/`ReadRow` emitters),
  [`namemint`](../../../public/semistructured/leeway/namemint/registry/) and
  [`tagmint`](../../../public/identity/tagmint/) (where ids come from).
- **How-to**: [leeway-marshalling](../../howto/leeway-marshalling.md) — the
  single-DTO tag grammar this layer builds on.
- **From SQL** rather than through the Go read path:
  [the SQL read surface](../../explanation/leeway-sql-read-surface.md). The
  per-kind Presence / Projection / Validator artefacts are this contract's SQL
  rendering — the same arity rule, so a slot that is optional here is optional
  there.
