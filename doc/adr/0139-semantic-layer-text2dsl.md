---
type: adr
status: proposed
date: 2026-07-22
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0139: Semantic layer for text2dsl grounding

## Context

[ADR-0120](./0120-play-natural-language-ask-panel.md) wires the existing
`text2sql2` engine into play. Its evidence section records why raw-schema
grounding is not enough; the numbers that matter here:

- On ClickHouse, frontier models are reported at 45.5–50.5% accuracy from
  schema text alone, and 67.7–68.7% (+17 to +23 points) once a ~4 KB
  hand-authored semantic layer — measure definitions and disambiguation
  rules — is added (arXiv:2604.25149). Comparable deltas are reported for
  semantic-layer-bound generation elsewhere (dbt's open benchmark,
  vendor-reported).
- Cross-dialect generation averages under 38.53% (PARROT,
  arXiv:2509.23338): targeting one canonical dialect is materially easier
  than targeting many.
- Raw generation fails *silently* (plausible wrong numbers via join
  fan-out, NULL handling, ambiguous business terms); semantic layers fail
  *loudly* ("unsupported"). Production post-mortems consistently rank
  documentation quality above model choice.

The task is therefore not "text to ClickHouse SQL" but **text2dsl**: the
generation target is the nanopass-validated canonical dialect —
grammar1/grammar2 plus the pass-stack vocabulary (keelson macros, leeway
column handles, selection conditions) — and the missing piece is the
artifact that maps a deployment's *business vocabulary* onto that DSL.

boxer's position is unusual in four ways, and the design should exploit
all of them:

1. **The grammar is in-process.** Every expression a layer declares can be
   grammar1-parsed and canonicalized at authoring time by the same
   nanopass machinery that validates generation — a semantic layer that
   *cannot* drift silently into invalid syntax.
2. **leeway already models semantics.** Friendly column handles
   ([ADR-0116](./0116-play-leeway-column-handle-resolution.md)), mapping
   plans with a SQL read-back generator
   ([ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md)), and
   ODCS data contracts
   ([ADR-0060](./0060-leeway-data-contracts-odcs.md)) are existing
   sources a layer can derive entries from instead of hand-writing them.
3. **A verified-query corpus already accrues.** Pinned query runs
   ([ADR-0115](./0115-query-observability-data-plane-strategy.md)) are
   human-blessed question/SQL material — the "verified example queries"
   lever, natively.
4. **Self-describing virtual tables exist.** `keelson.*` providers
   ([ADR-0094](./0094-keelson-introspection-tables.md)) can enumerate
   themselves into a layer tier.

One constraint dominates the format choice: a layer describes a
*deployment's data*, not boxer itself — it is site content. It must be
authorable and editable without recompiling boxer, live as local files
(sovereignty, versionability), and be lintable in CI.

## Design space (QOC)

**Question.** What form does the semantic-layer artifact take?

**Options.**

- **O1** — Free prose: one markdown file per scope (the ~4 KB style from
  the evidence), injected verbatim.
- **O2** — Pure structured data: YAML/JSON (or compiled-in Go)
  declarations of measures, dimensions, and joins.
- **O3** — Markdown carrier with machine-parsed structured blocks:
  fenced declarations for measures/dimensions/joins/routing, free prose
  for disambiguation rules; the whole file renders to prompt text, the
  fenced parts are validated.

**Criteria.**

- **C1** — Authoring friction: a data owner can write and amend it in
  minutes.
- **C2** — Loud validation: expressions parse, references resolve, drift
  breaks CI.
- **C3** — Prompt quality: compact, deterministic rendering.
- **C4** — Reuse: the same artifact serves humans (docs/help) and other
  tools (lint, future retrieval).
- **C5** — Site-content lifecycle: no recompile, plain files,
  versionable.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | ++ | −  | +  |
| C2 | −− | ++ | ++ |
| C3 | +  | +  | ++ |
| C4 | −  | +  | ++ |
| C5 | ++ | +  | ++ |

O3 is proposed: it keeps O1's authoring ergonomics and human readability
while giving the structured parts real validation.

## Decision

We will build a **semantic layer**: a per-scope, file-based artifact that
grounds text2dsl generation, validated by the nanopass grammar, rendered
deterministically into prompts, and consumed by every generation surface
(the `boxer text2sql` CLI today, the ADR-0120 Ask panel next). Proposed
settled decisions:

- **SD1 — Engine-side ownership.** A package beside the engine (working
  name `public/db/clickhouse/semlayer`), with no dependency on play;
  consumers reach it through a small render/validate API. **OPEN:** final
  package home and name.
- **SD2 — Artifact form (O3).** One markdown file per scope. Fenced,
  machine-parsed blocks declare: *measures* (name, description, DSL
  expression), *dimensions* (name, description, column or expression,
  value notes), *certified joins* (tables, keys, cardinality note —
  fan-out warnings live here), and *routing hints* (raw table vs
  materialized view). Free prose in between carries disambiguation rules
  and conventions. **OPEN:** the concrete block grammar (leaning: a small
  line-oriented syntax inside fences, not YAML-in-fences).
- **SD3 — Loud validation.** A lint pass over a layer: every declared
  expression must grammar1-parse and canonicalize; every referenced
  table/column must resolve against the scope's schema harvest (or its
  leeway handles); failures are errors, not warnings. Schema drift breaks
  the layer visibly — that is a feature, and doubles as a
  rename-detection tripwire. Lint home (a `gov` subcommand vs beside the
  engine CLI) is a minor open point.
- **SD4 — Deterministic rendering.** `Render(scope, budget)` produces the
  prompt context: layer content first, auto-derived schema tier after,
  stable ordering throughout. v0 renders whole scopes — the evidence says
  ~4 KB already moves accuracy; retrieval/selection over large layers is
  deferred.
- **SD5 — Scoping.** A layer binds to (endpoint, database, optional table
  subset). play passes its active scope; the CLI takes a flag. Ad-hoc
  datasets ([ADR-0134](./0134-adhoc-datasets.md)) may carry layer
  fragments as a later tier.
- **SD6 — Content tiers.**
  - **T0 auto-derived (always on, zero authoring):** the `system.columns`
    harvest — tables, columns, types, comments, key markers (v1
    text2sql's query, reused).
  - **T1 authored overlay (the v0 deliverable):** measures, dimensions,
    certified joins, disambiguation rules, routing hints — dogfooded with
    a real layer for the demo dataset.
  - **T2 leeway-derived:** friendly handles as generation vocabulary and
    read-back shapes as certified queries. **OPEN:** v0 or first
    follow-up.
  - **T3 mined verified queries** from pinned runs, **T4 ODCS ingestion**
    — deferred.
- **SD7 — text2dsl vocabulary.** Where a scope is leeway-mapped,
  generation may target friendly handles and keelson macros — the pass
  stack lowers them (ADR-0116) — with physical names as the universal
  fallback. **OPEN:** tied to SD6-T2's timing.

## Alternatives

- **Adopt an external semantic layer** (dbt Semantic Layer, Cube, LookML,
  warehouse-vendor equivalents). Rejected: server and cloud dependencies
  against the sovereignty premises, and none can express — let alone
  validate — the nanopass dialect or leeway vocabulary. The *evidence*
  transfers; the tooling does not.
- **Raw-DDL grounding only.** Rejected by the evidence this ADR exists to
  answer (the 45–50% band on ClickHouse).
- **Pure prose (O1).** Rejected as the artifact — silent semantic failure
  is the production killer, and an unvalidated layer rots into one more
  silent-failure source; prose survives *inside* O3 where it belongs.
- **Play-internal grounding assembler.** Rejected: multiple consumers
  exist today (CLI) and next (panel); this is an engine concern.

## Consequences

### Positive

- The single most reproducible accuracy lever the literature identifies,
  made boxer-native: layer expressions are checked by the same grammar
  that validates generation, at authoring time.
- The artifact is dual-use by construction: prompt grounding for models,
  data documentation for humans, drift tripwire for CI.
- ADR-0120's panel stays thin; grounding quality improves without
  touching play.

### Negative

- T1 overlays are human work that cannot be fully automated; a stale or
  wrong overlay is worse than none. The lint catches drift and syntax,
  not wrong business semantics.
- A new artifact class to specify, lint, document, and version.
- Whole-scope rendering caps practical layer size until
  retrieval/selection lands.

### Neutral

- The dialect preamble (e.g. `uniq` vs `uniqExact`, combinators, join
  strictness) stays engine-side constant text — layers carry *site*
  semantics only.
- ClickHouse upstream is converging on similar ideas (in-client `??`
  generation, cloud-side semantic layers); this artifact is local-first
  and grammar-validated rather than a compatibility target.

## Status

Proposed — open decisions: SD1 package home, SD2 block grammar, SD6-T2 /
SD7 timing (leeway tier in v0 or first follow-up). Being closed in the
same design dialogue as ADR-0120.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0120](./0120-play-natural-language-ask-panel.md) — the consumer
  whose evidence section motivates this ADR.
- [ADR-0116](./0116-play-leeway-column-handle-resolution.md),
  [ADR-0066](./0066-leeway-dql-clickhouse-readback-generator.md),
  [ADR-0060](./0060-leeway-data-contracts-odcs.md) — leeway sources for
  derived tiers.
- [ADR-0115](./0115-query-observability-data-plane-strategy.md) — pinned
  runs, the future verified-query tier.
- [ADR-0094](./0094-keelson-introspection-tables.md) — self-describing
  provider tables.
- Evidence: arXiv:2604.25149 (ClickHouse semantic layer, +17–23 pts);
  arXiv:2509.23338 (PARROT, cross-dialect <38.53%);
  dbt-labs/dbt-llm-sl-bench (vendor-reported).
- Engine: `public/db/clickhouse/text2sql2/`, `public/db/clickhouse/text2sql`
  (v1 harvest query).
