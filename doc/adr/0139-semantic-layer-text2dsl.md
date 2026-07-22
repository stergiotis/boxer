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

A further requirement (2026-07-22) sets the interaction model: the
model must be able to *introspect every relevant dimension itself* —
tables, semantic-layer entries, nanopass passes, live signals, windows —
through tool calls, not only through whatever context was rendered up
front. The literature backs this: static schema pre-filtering measured
net-neutral-to-negative for frontier models (a wrongly dropped column is
unrecoverable), and the strongest results on realistic benchmarks come
from agentic iterative exploration. boxer's grain fits unusually well
because dimensions here tend to *become tables*: passes are queryable via
`keelson('sql_passes')` ([ADR-0108](./0108-keelson-sql-pass-registry.md)),
appliance topology is data
([ADR-0126](./0126-appliance-topology-as-data.md)), system metrics ride a
pub/sub plane (ADR-0090), windowhost lifecycle lands as audit facts
(ADR-0135), and provider tables self-describe (ADR-0094). SD8 records the
contract and the engine delta.

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

- **SD1 — Engine-side ownership.** `public/db/clickhouse/semlayer`, with
  no dependency on play; consumers reach it through a small
  render/validate API. The SD8 tool registry lives beside the
  orchestrator. *(Settled 2026-07-22.)*
- **SD2 — Artifact form (O3).** One markdown file per scope. Fenced,
  machine-parsed blocks declare: *measures* (name, description, DSL
  expression), *dimensions* (name, description, column or expression,
  value notes), *certified joins* (tables, keys, cardinality note —
  fan-out warnings live here), and *routing hints* (raw table vs
  materialized view). Free prose in between carries disambiguation rules
  and conventions. *(Settled 2026-07-22:)* the block grammar is a
  line-oriented micro-syntax — one declaration per line, e.g.
  `measure revenue = sumIf(amount, status = 'paid') -- paid revenue` —
  diff-friendly, trivially parsed, the expression part feeding the SD3
  lint directly; YAML-in-fences and markdown tables were rejected.
- **SD3 — Loud validation.** A lint pass over a layer: every declared
  expression must grammar1-parse and canonicalize; every referenced
  table/column must resolve against the scope's schema harvest (or its
  leeway handles); failures are errors, not warnings. Schema drift breaks
  the layer visibly — that is a feature, and doubles as a
  rename-detection tripwire. The lint lives beside the engine CLI —
  layers are site content, not repo governance. *(Settled 2026-07-22.)*
- **SD4 — Deterministic seed rendering.** `Render(scope, budget)` produces
  the *seed* context: layer content first, auto-derived schema tier after,
  stable ordering throughout. v0 renders whole scopes — the evidence says
  ~4 KB already moves accuracy. The seed is deliberately compact; depth
  comes from SD8 introspection on demand, non-destructively (nothing the
  seed omits is unrecoverable). Retrieval/selection over large layers
  stays deferred.
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
  - **T2a leeway handles (v0):** friendly handles as generation
    vocabulary where the scope is leeway-mapped — in from day one
    (settled 2026-07-22: the strongest text2dsl cut was chosen over a
    physical-names-first v0).
  - **T2b leeway read-back shapes** as certified queries — first
    follow-up.
  - **T3 mined verified queries** from pinned runs, **T4 ODCS ingestion**
    — deferred.
- **SD7 — text2dsl vocabulary.** Where a scope is leeway-mapped,
  generation may target friendly handles and keelson macros — the pass
  stack lowers them (ADR-0116) — with physical names as the universal
  fallback. *(Settled 2026-07-22 with SD6-T2a:)* handle/macro vocabulary
  is v0. Implications: the CLI path carries the handle-resolution pass
  binding from day one, and the SD8 validate tool resolves handles, not
  only grammar.
- **SD8 — Agentic introspection** *(added 2026-07-22)*. Generation is a
  tool-calling loop, not a single shot: while composing a query the model
  can introspect every relevant dimension. The dimension registry is
  extensible and starts with: tables/columns (`system.*`, `keelson.*`
  providers; ad-hoc scopes later), semantic-layer entries, nanopass
  passes (`keelson('sql_passes')`), signals/topology (ADR-0126 data,
  ADR-0090 plane), and windows (windowhost lifecycle facts, ADR-0135).
  boxer's grain holds — dimensions are, or become, queryable tables — so
  two tool shapes cover the surface: a **read-only, row-capped query
  tool** over the introspection tables (the DSL is its own introspection
  language) and a **validate tool** (grammar1 parse + canonicalize) so
  the model can self-check drafts before answering. *(Settled
  2026-07-22:)* a small bespoke façade rides on top — named
  per-dimension tools (list/describe tables, passes, signals, windows,
  measures) implemented as canned queries compiling into the same single
  guarded executor — with the raw query tool kept as the escape hatch,
  friendlier to small local models. Guardrails: read-only enforcement,
  row/size caps, every tool call emitted through the observer stream.
  SD8 defines the *surface*; the interaction protocol and the client
  delta are SD9's.
- **SD9 — Interactive tool calling, in-conversation** *(added
  2026-07-22)*. The SD8 tools are called *by the model, from within the
  generation conversation* — not pre-fetched by the engine on the
  model's behalf. The protocol is the standard chat tool loop: on any
  turn the model may answer with tool calls instead of SQL; the engine
  executes each call and appends the result as a tool-result turn; the
  conversation continues until the model emits its final SQL or the
  per-question call budget is exhausted. This inner introspection loop
  nests *inside* each attempt of the existing repair loop — a validation
  failure still produces a repair turn, and the accumulated tool-call
  history stays in the conversation, so knowledge the model gathered is
  not lost across attempts. Mechanics: `LLMClientI` is extended so a
  turn can carry tool definitions and return tool calls; the openaichat
  client already models exactly this (tool definitions, `ToolCalls` on
  the response, replayable role=tool turns), the ollama adapter needs
  the equivalent. Interactive introspection is capability-gated per
  client: a model or adapter without tool support degrades to SD4
  seed-only single-shot generation — today's behavior — rather than
  failing.

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
- The agentic loop needs registries, not new data planes: introspection
  reuses the same tables humans query, and the observer stream doubles as
  a complete tool-call audit trail.
- ADR-0120's panel stays thin; grounding quality improves without
  touching play.

### Negative

- T1 overlays are human work that cannot be fully automated; a stale or
  wrong overlay is worse than none. The lint catches drift and syntax,
  not wrong business semantics.
- A new artifact class to specify, lint, document, and version.
- Whole-scope rendering caps practical layer size until
  retrieval/selection lands.
- Agentic loops multiply LLM round-trips and latency (production
  pipelines report up to ~19–21 calls per query); a per-question call
  budget and cancellation are part of the contract, not afterthoughts.
- Handles-in-v0 couples the engine to leeway handle resolution from the
  first release — the CLI inherits the resolution pass binding, not only
  play.

### Neutral

- The dialect preamble (e.g. `uniq` vs `uniqExact`, combinators, join
  strictness) stays engine-side constant text — layers carry *site*
  semantics only.
- ClickHouse upstream is converging on similar ideas (in-client `??`
  generation, cloud-side semantic layers); this artifact is local-first
  and grammar-validated rather than a compatibility target.
- The SD9 capability gate keeps tool-less models usable: they run
  seed-only at reduced grounding depth instead of being excluded.

## Status

Proposed — all formerly open decisions were closed in the 2026-07-22
design dialogue (SD1 home, SD2 line-oriented block grammar, SD3 lint
beside the engine CLI, SD6-T2a handles in v0, SD8 bespoke façade over
one executor) and are folded into the SD texts above. SD9 — the
interactive in-conversation tool protocol — was added the same day after
review feedback that SD8 left it implicit. Sequencing, also
settled: the engine lands first and the `boxer text2sql` CLI proves it;
the ADR-0120 panel consumes the proven engine after. Awaiting review for
acceptance alongside ADR-0120.

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
- [ADR-0108](./0108-keelson-sql-pass-registry.md),
  [ADR-0126](./0126-appliance-topology-as-data.md) — dimensions that are
  already tables (passes, topology), the SD8 pattern.
- Evidence: arXiv:2604.25149 (ClickHouse semantic layer, +17–23 pts);
  arXiv:2509.23338 (PARROT, cross-dialect <38.53%);
  dbt-labs/dbt-llm-sl-bench (vendor-reported).
- Engine: `public/db/clickhouse/text2sql2/`, `public/db/clickhouse/text2sql`
  (v1 harvest query).
