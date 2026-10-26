---
type: adr
status: proposed
date: 2026-10-26
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0337: competences are anchored to the tree and assessed by evidenced claims — three roots, a committed catalog

## Context

[ADR-0168](./0168-capmap-business-capability-corpus.md) gave the toolkit a
competence corpus: a vault of markdown read into two grains, served as
three keelson tables, loaded into `boxer.facts`, browsed as a treemap and
linted as a filter over relations. It also drew a scope line on 2026-08-15:
nothing in the design produces an assessment, every competence in the
boxer catalog carried the not-assessed sentinel for both scores, and so
capmap is a *structure-and-prose* tool and the prototype's scoring lenses
were not ported. Three things have changed since.

- **The catalog is gone from this tree.** §SD7 kept it in-tree but
  git-ignored, to avoid deciding by accident whether two other catalogs
  could be published. The boxer catalog had neither of their problems, and
  the working tree this record was written on holds only the vault's
  README. §SD11's recovery path — dump from a store it was loaded into —
  needs a store that was written, which no record says happened.
- **The tree has outgrown it.** Half the decision record postdates the
  catalog, including an area with no branch at all — the single-person
  cell for agents ([ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md)
  and the records around it).
- **The reason the scores stayed empty is a property of their shape.** A
  bare 0..5 holds no rubric, no basis, no assessor and no date, and the
  competence it rates is bound to nothing in the tree — no package, app,
  decision or measurement — so a rating could be neither defended nor
  falsified. Every scale that gets filled in practice attaches evidence to
  the rating, and this repository already enforces that rule in two
  places: a positioning clause with no rests-on row is cut, and a trial
  figure travels only as its §0 claim.

The survey behind this decision is
[competence-modelling-design-space-survey](../adr-background-work/competence-modelling-design-space-survey.md)
(2026-10-26): the enterprise-architecture literature on capability maps
(TOGAF, BIZBOK, ArchiMate's strategy layer, a vendor's assessment
dimensions, Wardley's evolution stages), the maturity and readiness scales
(CMMI, ISO/IEC 33020, TRL), the appraisal rules on evidence, and a
derivation of the catalog's structure from the tree's own self-descriptions.
What it established, and this decision rests on:

- **A rating is justified by evidence or it is an affirmation.** CMMI
  appraisals distinguish artefacts from affirmations and refuse the latter
  alone; TRL anchors every rung to a named document; ISO 33020 rates
  attributes and *derives* a level by rule. The ADR-0168 scores were the
  one scale in the survey's comparison with no justification column.
- **The capability is stable; what is assessed is its realisation.**
  TOGAF's capability-versus-instance split and ArchiMate's realisation
  relation both put a named edge between the capability and the things
  that implement it. ADR-0168 argued for a join between competences and
  what the runtime knows about itself and never gave the join an edge.
- **Assessment dimensions are separate attributes with small
  vocabularies**, not one score: importance, maturity, lifecycle,
  definition quality in the vendor practice; evolution stage in Wardley.
- **The catalog is derivable.** Level 2 of the toolbelt is the positioning
  set; level 3 is each page's cited decisions grouped by what a customer
  could want alone; the tree's other self-descriptions (app topics,
  package properties, coverage, trials, the ADR tables) are the evidence a
  claim can cite and a lint can check.

## Design space (QOC)

**Question.** Where does a competence's assessment get its truth?

**Options.**

- **O1** — Human scalars, as today: `maturity` and `pain` typed into
  frontmatter, no basis required.
- **O2** — Dimensioned human claims, each carrying a basis of anchors that
  resolve against the tree, with the tree's evidence derived beside them
  for comparison.
- **O3** — A derived score only: readiness computed from anchors by rule,
  nothing typed by hand.
- **O4** — An external capability-management tool, with the vault as its
  export.

**Criteria.**

- **C1** — Fillable by one maintainer with AI assistance, without an
  appraisal team (why-boxer P6).
- **C2** — Falsifiable against the tree: a wrong claim is visible as a
  disagreement with something the tree carries.
- **C3** — Round-trips through the vault and is reviewable as a diff
  (ADR-0168 §SD3).
- **C4** — Survives refactors: anchors are symbols and ids, not paths and
  line numbers.
- **C5** — Cost: parser, vocabulary and one provider, not a new subsystem.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | +  | +  | ++ | −− |
| C2 | −− | ++ | +  | −  |
| C3 | ++ | ++ | −  | −− |
| C4 | +  | +  | +  | −  |
| C5 | ++ | +  | +  | −− |

O3 loses on C3: a score nobody wrote is not a claim anybody defends, and
the point of the corpus is a defended description, not an oracle. O2 keeps
O3's derivation as a *signal beside the claim* rather than as the claim.

## Decision

We will bind each competence to the things in the tree that realise it, by
a typed anchor relation the parser resolves against the tree's own corpora;
replace the two bare scores with a list of dimensioned claims, each carrying
a basis of anchors, an assessor and a date; derive the tree's evidence per
competence as a fourth keelson table shown beside the claims; define what
the four levels mean here and give the catalog three roots; and commit the
boxer catalog, narrowing ADR-0168 §SD7's ignore rule to the catalogs it was
written for. ADR-0168's shape — vault authoritative, two grains, prose kept
verbatim, relations as rows with a resolution, the `competence` word, the
CLI as the only mutation surface — is unchanged.

### SD1 — A `realised_by` anchor is a relation of its own kind, resolved against the tree

A competence's frontmatter may carry `realised_by:`, a list of `kind:key`
strings. Each becomes a relation of kind `realised-by` whose target is the
anchor text and whose resolution is checked by a resolver for its kind:

| Kind | Key | Resolved against |
| --- | --- | --- |
| `adr` | four-digit number | `adrcorpus` over `doc/adr`; the relation also records the decision's status |
| `pkg` | repository-relative import path, or a directory holding packages — a package *tree*, which a subsystem anchors to | the Go packages of the checkout, by a walk that skips what `go list` skips |
| `app` | the app's directory name under `apps/` | the app registry when one is populated, else a manifest declared in that directory |
| `table` | `boxer.<name>` or `keelson/<name>` | the facts-schema table names and the provider roster; the roster is unchecked in a process with no providers |
| `howto`, `explanation`, `positioning`, `trial` | the page's slug | the doc tree under `doc/`; a trial by its directory's README |
| `env` | variable name | the generated env-vars page, which is the ADR-0009 registry as the tree records it; the in-process registry when the page is absent |
| `applet`, `scene`, `skill` | the minted slug, the scene file's stem without `.scene`, the skill's directory | the applet books, `apps/*/scenes`, `doc/skills` |

Resolution gains one state beyond ADR-0168 §SD2's four: **unchecked**, for
a resolver whose corpus is not reachable from where the parse runs (no
checkout, no process), on the providers' empty-rather-than-erroring
convention. An anchor that resolves to a *retired* decision — withdrawn,
superseded, deferred — resolves, and the lint lens lists it, because the
competence may describe something the tree no longer does.

Rejected: wikilinks with a prefix. An anchor is not an Obsidian link and
must not dangle as one; a typed string is read by the resolver and nothing
else. Rejected: paths and line numbers as keys. The detail-budget rule
applies to the corpus as to a document — a key is a symbol, an id or a
registry name, so a refactor that keeps the decision keeps the anchor.

### SD2 — Assessment is a list of claims, each with a dimension, a rating, a basis, an assessor and a date

```yaml
assessment:
  - dimension: readiness
    rating: 7
    basis: [adr:0223, adr:0234, howto:watchbill-jobs]
    by: "<handle>"
    at: 2026-10-26
```

The dimension vocabulary is registered, small, and open on the ADR-0158
precedent — adding a dimension is a registry entry, changing what one
means is a decision. The first four:

- **`readiness`**, nine rungs adapted from TRL's software column to this
  tree's artefacts: 1 named in a Context or a survey; 2 decided, milestones
  unticked; 3 built in part, default-lane tests pass; 4 built end to end
  behind a surface; 5 exercised against the real engine — integration lane
  or a scene; 6 documented for a consumer; 7 positioned, every clause
  resting on a record; 8 measured by a trial's §0; 9 operated as the
  toolkit's own workload with its facts to show it (P4). A rung is claimed
  by a person and its basis names the artefact; the lens computes the rung
  the anchors *support* beside it (SD3), and the two differing is a row,
  not an error.
- **`evolution`** — `genesis`, `custom`, `product`, `commodity`: what kind
  of thing the competence is in the world, which drives the P1 posture
  toward it. Not a maturity.
- **`importance`** — `foundational`, `differentiating`, `innovating`: why
  the toolkit carries it, read against the premises.
- **`pain`** — 0..5 with named rungs: 0 none; 1 a known rough edge; 2
  costs a maintainer time monthly; 3 weekly; 4 blocks a consumer's task
  without a workaround; 5 blocks the toolkit's own workload.

Rules: a claim whose `basis` is empty is a lint finding — it is an
affirmation; every basis anchor is an SD1 anchor and must resolve; a
dimension absent from the list is *unassessed*, which retires the `255`
sentinel for new notes. A claim on a dimension the build does not register,
or with a rating off its scale, fails the parse like malformed frontmatter —
there is one right spelling — while what is a judgement (an empty basis, a
missing assessor or date, a basis the tree lacks) is a `problems` entry on
the claim, listed and never refused. `maturity:` and `pain:` as bare keys
are read for compatibility; the renderer writes them only when a judgement
was recorded, never the sentinel, so a vault that carries them round-trips
and a new note never acquires them.

Rejected: a target rung per dimension (the vendor practice's *target
maturity*). A target is a course of action, and this tree records those as
decisions and tags, not as scores. Rejected: a quality characteristic per
competence. The whole-product assessment already grades those, and a
per-part grade would be a second matrix nobody maintains.

### SD3 — Evidence is derived from the tree, as a fourth table, and never a verdict

A `competenceevidence` provider joins each competence's anchors against
what the tree already serves: the decision's status and milestone ratio
(`adr`, `subtask`), the presence of a howto, explanation, positioning page
or trial (the doc tree), the package's existence and which test lanes it
carries (a file walk for `_test.go` with and without the integration tag),
coverage when a snapshot is loaded (`coverage_pkgs`), the app's
registration and operation count (`apps`, `app_operations`), and the
commit digest's recency for the anchored paths. One row per competence per
signal. From the signals the lens computes the readiness rung the anchors
support, by ISO 33020's rule — the highest rung all of whose lower rungs
hold — and shows it beside the claimed rung.

The table states what the tree carries. It does not write a claim, does
not average anything, and is empty rather than erroring when a corpus is
out of reach, like every other provider.

Rejected: writing derived ratings into the vault. A ranking the ranker
writes back (`similar:`) is a measurement of the prose; a readiness the
tree computes is a measurement of the tree, and it would be stale the
moment either moved. The vault holds what people claimed; the table holds
what the tree shows.

### SD4 — The levels are defined for this tree, and the catalog has three roots

- **Level 1 — a root**: a customer type of its own. Three: **toolbelt**,
  for a consumer of the toolkit; **cell**, for a person operating agents
  inside the single-person cell; **practice**, for the maintainer of the
  repository.
- **Level 2 — a subsystem** in the positioning README's sense: a customer
  distinct from the product's, a nameable external alternative, enough
  decisions to fill a rests-on table. The toolbelt's level 2 is the
  positioning set plus the two pages it defers; the other roots' level 2
  is read off the cell's records and the engineering-practices sections.
- **Level 3 — a competence** a customer could want without the rest of its
  subsystem; usually one to a few decisions.
- **Level 4 — a building block** shared by several level-3 competences;
  the one level where several parents are ordinary.

Naming follows the design-principle literature: a what, not a how; never
a package, a decision number, a role or an initiative; siblings disjoint.
The survey's §11 carries the derived skeleton — three roots, about thirty
level-2 nodes, on the order of a hundred and sixty level-3 candidates —
which review merges and splits before prose is written.

Rejected: one root with the cell and the practice as branches of the
toolbelt. They have different customers, which is the level-1 definition,
and the cell cuts across five toolbelt subsystems.

### SD5 — The boxer catalog is committed; the ignore rule narrows to the catalogs §SD7 was written for

The `boxer` catalog directory under `doc/competences` is versioned. The ignore rule keeps ignoring every
other directory under `doc/competences/`, so the two catalogs ADR-0168
§SD7 excluded — one derived from a private checkout, one whose licence has
not been gated — stay out by the same mechanism. The README's §SD7
paragraph changes to say so.

Three consequences are the point. The applet book's corpus gate reads the
real catalog rather than a fixture, so a buffer that names a column the
catalog lacks fails where it should. CI runs `capmap parse` over it, so an
unresolved anchor or a basis-less claim is a red check. And the catalog
cannot be lost from a working tree again.

doclint walks what git does not ignore and applies its front-matter rule,
which competence notes do not carry. The vault is excluded by doclint's
path filter: it has a lint of its own, and that is the gate that should
run over it. Rejected: a `type: competence` front-matter kind doclint
learns — two linters over one tree, with the second knowing nothing about
the notes it passes.

### SD6 — Lifecycle is derived, not authored; sections get a recommended shape

The sixteen `lifecycle_*` keys stay readable and are not written for the
boxer catalog: a phase follows from the readiness claim and the anchored
decision's status, which the evidence table carries. The renderer keeps
them for a vault that has them, so ADR-0168's round-trip test is
unaffected.

New notes take five h1 sections — *Scope*, *Mechanisms*, *Evidence*,
*Boundaries*, *Standards* — as a recommendation the README states and the
parser does not enforce; ADR-0168 §SD5's free headings stand.

### SD7 — Encoding: memberships appended, and the assessment carried as attributes grouped by the dimension

The vocabulary appends, after the twenty-fourth ordinal: a relation kind
value for `realised-by` and `basis` needs no membership (kind is a symbol
attribute already) but the anchor's resolved *status* does; and four for
the assessment — rating, by, at, basis — each a mixed low-card-ref carrying
the *dimension* as its high-card parameter. The parameter is what groups a
claim's fields, so no claim ordinal is stored: one claim per dimension is
the rule, and a second is marked a duplicate and not written. The basis is
one attribute per anchor under its membership, the shape
`capmapCompetenceTag` already uses for a list. A dynamic-membership tuple
owns its section within one DTO (ADR-0103), so the rating rides the symbol
section, the assessor the string section, the date the time section, and
the basis — a second tuple wanting the symbol section — is a component of
its own on the same row, as the tags are. The generated component codecs
ADR-0168's 2026-10-13 Update adopted take the new kinds; `load` and `dump`
round-trip them; the SQL read path's handles gain the string section's
lanes.

Rejected for now: the assessment as a tuple kind on the facts row under
[ADR-0326](./0326-tuple-elements-carry-parameters-and-tuple-kinds-read-in-readrow.md).
It is the better shape — one tuple per claim, its fields as elements — and
it is a second decision with a data break of its own; §Deferrals carries
the trigger.

### Milestones

- **M0 — the skeleton reviewed.** The survey's §11 merged and split in a
  design dialogue; the result is the catalog's level 1–2 and the level-3
  names. No code.
- **M1 — parser and renderer.** `realised_by`, `assessment`, the
  `realised-by` kind, the resolvers behind one interface with an
  *unchecked* result, `maturity`/`pain` read-only. The parse-render test
  gains the new fields.
- **M2 — vocabulary and encoding.** The appended memberships, the codecs,
  the golden name-to-id table, `load`/`dump`, the read-surface handles.
- **M3 — evidence.** The `competenceevidence` provider and the supported
  rung; the roster test moves.
- **M4 — lenses.** `comp-lint` gains unresolved anchors, anchors to retired
  decisions and basis-less claims; a `comp-evidence` lens shows claims
  beside signals; `comp-overview`'s coverage row counts claims per
  dimension instead of non-sentinel scores. Until then `boxer capmap parse`
  prints the same findings.
- **M5 — the catalog committed.** The ignore rule narrowed, the doclint
  filter, the README, the corpus gate re-pointed at the real catalog.
- **M6 — the catalog written.** Level 2 in full for all three roots;
  level 3 for every branch with anchors and at least a readiness claim;
  prose from the lost vault where a copy is found, from the tree where
  not.

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| Vault front-matter contract (`doc/competences/README.md`) | Added — `realised_by`, `assessment`; `maturity`, `pain`, `lifecycle_*` become read-only | the parser, the renderer, the parse-render test, the README |
| `capmapcorpus` relation kinds and resolutions | Added — `realised-by`; a fifth resolution, `unchecked` | `competencerelation`'s column values; the lint lens; `ParseResolution` |
| `capmapvocab` memberships | Added — appended after the last ordinal (ADR-0168 §SD6's rule) | the golden name-to-id test; the component codecs in `capmapfacts`; the SQL read handles |
| keelson table-name namespace ([ADR-0094](./0094-keelson-introspection-tables.md)) | Added — `competenceclaim`, `competenceevidence` | `RegisterStatic` and the roster it is pinned by |
| keelson `competence` table columns | Added — `claim_count`, `dimensions`; `maturity` and `pain` stay, read-only, until the lenses move to the claims table (M4) | the applet book's buffers; the corpus gate |
| keelson `competencerelation` table columns | Added — `target_status`; `resolution` gains `unchecked` | the lint lens |
| Applet id namespace ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) | Added — `comp-evidence` | the cross-book slug-collision test |
| `.gitignore` and doclint's path filter | Reshaped — the vault's ignore narrows to non-boxer catalogs; doclint excludes the vault | the README's §SD7 text; the applet corpus gate's fixture |
| `boxer capmap parse` | Reshaped — exits non-zero on unresolved anchors and basis-less claims when asked to gate | the CI step that runs it |

## Alternatives

- **Keep the scope line and port nothing.** Rejected: the catalog is lost
  and half the tree is undescribed; the structure-and-prose tool has no
  structure to show.
- **Fill `maturity` and `pain` as they are.** Rejected in the QOC: a bare
  scalar cannot be defended or falsified, which is why none was ever
  written.
- **Generate the catalog from the tree and keep no vault.** Rejected: the
  derivation gives a skeleton, not a description; a customer-facing scope
  and boundary are authored, and ADR-0168 §SD3's reviewable diff is the
  reason the vault exists.
- **Anchors as wikilinks to reference notes kept beside the vault.**
  Rejected: the reference trees are what ADR-0168 keeps out of the read
  because their names collide with competence names, and a decision is
  not a note.
- **A per-package declaration (`packageprops`) naming its competence,
  instead of the vault naming packages.** Rejected: a package realises
  several competences and a competence spans several packages; the edge
  belongs to the corpus, and `packageprops` stays the per-package registry
  an anchor resolves against.

## Consequences

### Positive

- A competence can be wrong in a way the tree shows: an anchor that does
  not resolve, a claim without a basis, a claimed rung above the supported
  one, a decision that was withdrawn.
- The join ADR-0168 chose `boxer.facts` for has an edge: which app, package
  or table realises which competence is a query.
- The catalog is versioned, linted in CI and reviewable as a diff; the
  applets and the corpus gate see real data on every machine.
- Breadth follows the tree: two customers the toolkit now describes get a
  root each, and new decisions have a place to land.

### Negative

- Authoring cost rises: a note needs anchors and at least one evidenced
  claim to be more than a name. The skeleton in the survey sizes that at
  roughly twice the August catalog.
- Resolvers are one more thing that reads the tree's corpora; each is
  small, and each is a place a corpus rename breaks a lint rather than a
  build.
- The `competence` table's scalar score columns change shape, which breaks
  the three existing lens buffers until M4 rewrites them.
- The vault is now in the public tree. The privacy rule the README states
  — no sibling repositories, no personal paths, no individuals — becomes
  enforceable by review of the diff rather than hoped for.

### Neutral

- `maturity` and `pain` as bare keys remain parseable indefinitely; a
  vault that has them loses nothing and gains nothing.
- The evidence table is a convenience over joins a reader could write by
  hand; its value is that the lens writes them once.

## Migration — Tier 1

- **Breaks.** None before M4: the `competence` provider keeps `maturity`
  and `pain`, and the new tables and columns are additive. At M4 the two
  columns go, and buffers naming them fail the corpus gate. Nothing in the
  facts encoding is re-keyed — the natural key stays the slug — so a store
  holding a load of the August catalog reads back unchanged.
- **Path.** M1 lands additive; M2 appends memberships and regenerates the
  codecs; M4 rewrites the three buffers in the same commit as the column
  change; M5 flips the ignore rule and the filter together with the README.
- **Regeneration.** The capmap component codecs and the SQL read-surface
  handles; no FFI boundary is involved.
- **Old shape.** Read indefinitely for `maturity`, `pain` and
  `lifecycle_*`; never written for the boxer catalog.

## Verification plan — Tier 1

- **Lane.** Default `go test`: anchor parsing and resolution against a
  fixture tree, each resolver's *unchecked* path, the claim rules (empty
  basis, unresolvable basis, unknown dimension), the parse-render round
  trip with the new fields, the golden membership table. The integration
  lane: load and dump with claims and anchors against a scratch database.
  The applet corpus gate over the committed catalog. CI: `capmap parse`
  gating on the committed catalog.
- **What would fail.** An anchor kind whose resolver silently reports
  *direct* for everything — pinned by a fixture with one anchor of each
  kind that must *not* resolve. A claim written without a basis reaching
  the store — pinned by the parse rule. A buffer naming a column the
  catalog lacks — the corpus gate.
- **Gap.** Nothing verifies that a claim is *right*; a readiness 7 with a
  resolving basis can still overstate. The supported-rung column makes the
  overstatement visible and does not prevent it, which is the chosen
  posture (QOC, O2 over O3).

## Deferrals

Each with a trigger.

- **The assessment as an ADR-0326 tuple kind.** Trigger: a second
  consumer that reads claims as rows rather than through the lens, or a
  claim field that is not a scalar.
- **Anchor-overlap similarity** — Jaccard over `realised_by` sets as a
  second `similar` signal beside compression distance. Trigger: a merge
  review that asks why two notes are alike and gets no answer from the
  prose.
- **A supported-rung lint that fails rather than lists.** Trigger: the
  catalog's claims having been reviewed once, so that a regression is a
  regression and not the initial state.
- **Porting the lost catalog's prose.** Trigger: a copy of the August vault
  turning up in another checkout or a store.
- **Resolvers for a downstream repository's own corpora** (ADR-0179's
  consumers describing their toolbelt). Trigger: a consumer adopting the
  vault format.

## Status

Proposed — awaiting review by the code owner. Written against the tree on
2026-10-26; the survey's §2 states what was measured that day. The same day
M1, M2, M3, M5 and M6 were implemented in the working tree ahead of
acceptance, so the review has the catalog and the lint to look at rather
than a skeleton: 197 notes, every anchor resolving, each with a readiness
claim beside the rung its anchors support. M4 waits on the applet book,
which another change was editing at the time. The claims in that first
catalog were written by an agent and are tagged `needs-review`; the
supported-rung column is where to start.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [competence-modelling-design-space-survey](../adr-background-work/competence-modelling-design-space-survey.md) — the literature, the scales, the diagnosis, the derived skeleton.
- [ADR-0168](./0168-capmap-business-capability-corpus.md) — the corpus this builds on; §SD7 is narrowed here, the 2026-08-15 scope line lifted.
- [ADR-0158](./0158-app-classification-topics-keywords-kind.md) — the registered-vocabulary precedent for dimensions.
- [ADR-0080](./0080-packageprops-per-package-declarations.md), [ADR-0169](./0169-continuous-coverage-keelson.md), [ADR-0092](./0092-adr-overview-tool.md) — the per-package, coverage and decision tables the evidence joins.
- [ADR-0307](./0307-threat-model-for-an-interactive-single-person-cell.md) — the customer the `cell` root describes.
- [positioning README](../explanation/positioning/README.md) — the level-2 definition and the rests-on rule.
- [trials README](../trials/README.md) — the §0 citation rule.
