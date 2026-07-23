---
type: adr
status: proposed
date: 2026-07-21
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0120: play Ask panel — natural-language query authoring

> **Numbering note.** 0120 previously held the package-capability survey,
> withdrawn 2026-07-15 (`e42e3b97`) as redundant with
> [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD10. The slot is
> deliberately reused. References to ADR-0120 dated before
> 2026-07-21 — the [2026-07 changelog](../changelog/2026-07-02--2026-07-16.md)
> and the status note in
> [query-system-requirements](../explanation/query-system-requirements.md) —
> mean the withdrawn survey, not this document.

## Context

play authors SQL by hand: the editor, the Snippets tab, param widgets
([ADR-0124](./0124-play-param-editing-widgets.md)). There is no on-ramp for
a user who knows the question but not the schema or the ClickHouse dialect.

The repo already carries a natural-language→SQL engine, currently with zero
in-tree consumers beyond its own test:

- `public/db/clickhouse/text2sql2/orchestrator` — a compile pipeline
  (template directives → LLM → grammar1 parse → canonicalize → grammar2 →
  AST) with a bounded error-feedback repair loop, policy-pass seam, compile
  cache interface, and a structured observer event stream. Its
  ClickHouse-execution retry path is an acknowledged stub.
- `public/db/clickhouse/text2sql2/llmclient` — Ollama and OpenAI-compatible
  backends over `public/llm/openaichat`.
- `public/db/clickhouse/text2sql` (v1) — the `boxer text2sql` CLI, including a
  `system.columns` schema harvest (names, types, comments, key markers).

play's extension seams anticipated a generator: the editor-delivery ops
([ADR-0097](./0097-play-reactive-query-graph.md) slice-6 D5) name "a query
generator" as an intended consumer, the tab registry is public, `bgjob` runs
cancellable work behind the render loop, and `RequestRun` triggers execution.

Forces:

1. **Embedder footprint.** play is embedded by sqlapplets
   ([ADR-0132](./0132-sqlapplet-sql-defined-applets.md)) and other hosts. A
   dependency added to package `play` rides into every applet binary and its
   capability baseline (ADR-0026 §SD10).
2. **Egress.** A prompt necessarily carries schema names, types, comments,
   and the user's question. Ad-hoc datasets
   ([ADR-0134](./0134-adhoc-datasets.md)) are deliberately walled; the
   local-first premise makes a loopback LLM the default posture, and any
   remote endpoint an explicit, visible choice.
3. **Grammar coverage.** The nanopass grammar has known gaps against full
   ClickHouse SQL (e.g. infix `DIV`/`MOD`), so generation must be validated
   with a repair loop rather than trusted; the orchestrator already works
   this way.
4. **Three table universes.** Useful grounding spans physical tables
   (`system.columns`), `keelson.*` introspection macros
   ([ADR-0094](./0094-keelson-introspection-tables.md)), and bound ad-hoc
   dataset aliases (ADR-0134) — plus play's pane-driving column conventions
   (`lane`/`title`, `label@mime`, `{name: Type}` params).

Evidence (2026-07 literature review; primary sources under References):

- Realism collapses accuracy: the same model scores 91.2% on Spider 1.0,
  73.0% on BIRD, and 21.3% on Spider 2.0 (enterprise workflows). SQL
  *syntax* generation is largely solved; grounding in real schemas and
  business semantics is not.
- On ClickHouse specifically, frontier models are reported at 45.5–50.5%
  from schema text alone, rising to 67.7–68.7% — +17 to +23 points — once
  a ~4 KB hand-authored semantic layer (measure definitions,
  disambiguation rules) is added to the prompt (arXiv:2604.25149).
- The enterprise floor is lower still, and public benchmarks flatter it.
  On private warehouses no model saw in training (BEAVER — 812 tables
  across 19 domains, arXiv:2409.02038) the best agentic systems score in
  the low teens — ~11% against ~63% for the same method on a public suite
  — and even hand-supplied join hints lift a real 1,400-table warehouse
  only to ~30%. Because public schemas sit in the training data, their
  numbers (the ClickHouse figures above included) read as upper bounds;
  evaluation on a deployment's own unseen data is the only measure to
  trust. This is why the golden question→SQL corpus below is deferred
  work, not a nicety (Stonebraker & Chen, CACM, 2026-07).
- Production deployments converge on one recipe regardless of vendor:
  curated semantics over raw DDL, verified example queries, show-the-SQL,
  human-in-the-loop (a user cannot vouch for a query they cannot read);
  documentation quality outweighs model choice.
- The failure modes differ in kind: raw generation fails *silently*
  (plausible wrong numbers via join fan-out, NULL handling, ambiguous
  business terms); a semantic layer fails *loudly* ("unsupported").

The consequence: schema-text grounding alone — SD4 as first closed — is
below the evidence bar. Grounding becomes the job of a semantic layer,
designed in [ADR-0139](./0139-semantic-layer-text2dsl.md).

## Design space (QOC)

**Question.** Where does the natural-language affordance live relative to
play?

**Options.**

- **O1** — Panel inside package `play`, like the built-in tabs.
- **O2** — Sibling package registering a panel through the public tab seam;
  only the standalone play app target links it.
- **O3** — Separate app that sends SQL to play via launch requests
  ([ADR-0135](./0135-app-launch-requests.md)).

**Criteria.**

- **C1** — Embedder footprint: applet binaries stay free of LLM/network
  dependencies.
- **C2** — Capability auditability: egress surface appears only where the
  feature is actually linked.
- **C3** — Interaction loop: ask → preview → refine → run without leaving
  the editor's context.
- **C4** — Implementation cost against existing seams.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 |
|----|----|----|----|
| C1 | −− | ++ | ++ |
| C2 | −  | ++ | ++ |
| C3 | ++ | ++ | −− |
| C4 | ++ | +  | −  |

O2 is proposed: it keeps the tight loop of O1 without O1's footprint, at the
cost of a small registration seam.

## Decision

We will add an **Ask panel** to the standalone play app: a natural-language
question box that compiles to ClickHouse SQL through the existing
`text2sql2` orchestrator and hands the result to the SQL editor. Proposed
settled decisions:

- **SD1 — Compile-only.** The panel calls the orchestrator's compile path
  only. Generated (canonical) SQL is delivered via the editor-delivery ops;
  play's normal run path — query graph, params, history, pins,
  observability — executes it. The orchestrator's own execute/cache-execute
  path stays unwired inside play.
- **SD2 — Placement.** A sibling package (working name `apps/play/ask`)
  holds the panel and the LLM dependency; package `play` does not import it.
  The standalone app target links it via a registration seam (init-hook
  registry or wiring-time tab add — settled at implementation); applet and
  embedder targets do not.
- **SD3 — Egress gate.** The panel registers only when
  `BOXER_PLAY_ASK_ENDPOINT` is set (ADR-0009 registry; no probing, no dead
  tab). The prompt contains schema text, a play-conventions preamble, and
  the question — never row data; the orchestrator's LLM result-verification
  stays off. The panel displays the endpoint host, keeping egress visible.
  Setting the variable is the acknowledgement: a non-loopback endpoint
  needs no second opt-in (a separate acknowledgement variable and a
  loopback-only v0 were both considered and rejected in the 2026-07-21
  dialogue — one explicit knob with a visible host, and remote endpoints
  such as a LAN inference box stay first-class).
- **SD4 — Grounding via the semantic layer** *(revised 2026-07-22,
  superseding the 2026-07-21 "physical only" closure after the evidence
  review above)*. Prompt grounding is owned by the semantic layer of
  [ADR-0139](./0139-semantic-layer-text2dsl.md); the panel consumes the
  layer's rendered context and never assembles raw schema text itself.
  Nothing already decided is lost: the layer's automatic base tier is
  exactly the earlier choice (the `system.columns` harvest), measures,
  certified joins, and disambiguation rules are its authored overlay, and
  ad-hoc-dataset (ADR-0134) plus `keelson.*` (ADR-0094) grounding are
  deferred tiers there. The pane-conventions preamble stays play-side —
  consumer vocabulary, not data semantics.
- **SD5 — Config.** `BOXER_PLAY_ASK_ENDPOINT`, `BOXER_PLAY_ASK_MODEL`
  (default `qwen3-coder-next`), `BOXER_PLAY_ASK_ATTEMPTS` (default 3),
  registered per ADR-0009.
- **SD6 — UI shape.** One `bgjob`-backed flow: question field, Ask with
  progress and cancel, highlighted read-only SQL preview, then
  Insert / Replace / Replace-and-run actions over the delivery ops. Every
  gesture stops at the preview: generated SQL never executes unseen, and
  Replace-and-run is a single click made after the SQL is on screen (an
  ask-and-run gesture was rejected in the 2026-07-21 dialogue). Progress
  surfaces the engine's activity — including ADR-0139 SD8 introspection
  tool calls — through the observer events.
- **SD7 — The target language is a DSL, not open SQL** *(added
  2026-07-22)*. What generation must emit is the nanopass-validated
  canonical dialect — grammar1/grammar2 plus the pass-stack vocabulary
  (keelson macros, leeway column handles, selection conditions) —
  text2dsl rather than text2sql. The orchestrator's validation loop
  already enforces the grammar half; ADR-0139 owns the vocabulary half,
  including how much handle/macro vocabulary v0 generation targets.

Deferred, recorded rather than gating (descope over gate):

- Execution-error repair — completing the orchestrator's stubbed
  ClickHouse-error retry and a "fix this error" affordance from play
  diagnostics.
- Grounding beyond the layer's base tier — ad-hoc dataset aliases
  (ADR-0134), `keelson.*` one-liners (ADR-0094), handle/macro
  vocabulary — owned as content tiers by ADR-0139 (SD4/SD7).
- A golden question→SQL evaluation corpus against the demo dataset.
- Multi-turn refinement (conversation state across asks).
- Compile-cache persistence (`text2sql2/cache`) and sample-value grounding.

## Alternatives

- **Wire the orchestrator's execute path.** Rejected: a second execution
  path bypassing the query graph, run history, and endpoint routing would
  duplicate exactly what play already audits.
- **Build on v1 `text2sql`.** Rejected as engine: superseded by the
  componentized pipeline; v1's CLI remains for terminal use, its schema
  harvest is reused.
- **O1 / O3** — see QOC; O3 is worth revisiting for remote or shared-session
  scenarios once launch requests carry arguments routinely.

## Consequences

### Positive

- First real consumer of `text2sql2` and a live dogfood of the tab-registry
  and delivery-op seams built for exactly this class of panel.
- A schema-grounded, grammar-validated NL on-ramp that stays local by
  default.
- Package `play` and applet binaries keep their current dependency and
  capability surface.

### Negative

- The standalone play app gains a network-egress surface toward the
  configured LLM endpoint; the ADR-0026 §SD10 baseline must record it.
- Answer quality is hostage to the configured model and to schema-context
  quality; grammar gaps burn repair attempts on some valid ClickHouse
  constructs.
- A second SQL-producing surface must stay coherent with editor conventions
  (params, friendly names, pane-driving aliases).

### Neutral

- Unconfigured installations see no change; the panel does not exist.
- The CLI and the panel share the engine but not UX; divergence between
  them is possible and acceptable.

## Status

Proposed — the formerly open decisions (SD3 remote acknowledgement, SD4
v0 scope, SD6 run gesture) were closed in the 2026-07-21 design dialogue;
on 2026-07-22 an evidence review revised SD4 (grounding moved to the
ADR-0139 semantic layer) and added SD7 (DSL target). Awaiting review for
acceptance alongside ADR-0139.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

<!--
## Updates

Tier-2 dated entries land here when implementation reveals a refinement, an aspirational
claim turns out false, or a milestone records what shipped. Single H2; add H3s dated
YYYY-MM-DD. Remove this HTML comment when the section first gains a real entry.
-->

## References

- [ADR-0097](./0097-play-reactive-query-graph.md) — query graph and the
  editor-delivery seam (slice-6 D5).
- [ADR-0132](./0132-sqlapplet-sql-defined-applets.md),
  [ADR-0134](./0134-adhoc-datasets.md) — embedding and privacy walls.
- [ADR-0026](./0026-app-runtime-and-capability-subjects.md) §SD10 — the
  capability question this panel must answer per app.
- [ADR-0009](./0009-environment-variable-registry.md) — env registration.
- [ADR-0094](./0094-keelson-introspection-tables.md),
  [ADR-0124](./0124-play-param-editing-widgets.md) — a deferred grounding
  tier, and the conventions the play-side preamble teaches.
- [ADR-0139](./0139-semantic-layer-text2dsl.md) — the semantic layer that
  owns grounding (SD4) and the DSL vocabulary (SD7).
- Evidence: arXiv:2604.25149 (ClickHouse semantic layer, +17–23 pts);
  arXiv:2411.07763 (Spider 2.0) and arXiv:2305.03111 (BIRD) for the
  realism collapse; arXiv:2409.02038 (BEAVER — the enterprise-warehouse
  floor and the benchmark-vs-production gap), framed by Stonebraker &
  Chen, "If You Think You Can Do Real-World Text-to-SQL" (CACM, 2026-07);
  dbt-labs/dbt-llm-sl-bench (semantic layer vs raw generation —
  vendor-reported).
- Engine: `public/db/clickhouse/text2sql2/{orchestrator,llmclient,cache,observers}`,
  `public/llm/openaichat`, `public/db/clickhouse/text2sql` (v1 + CLI).
