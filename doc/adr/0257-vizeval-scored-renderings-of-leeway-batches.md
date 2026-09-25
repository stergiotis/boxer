---
type: adr
status: proposed
date: 2026-09-25
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0257: vizeval — scored renderings of leeway batches, for searching the encoding space

## Context

Whether a rendering of leeway data is clear is judged today by a person
looking at a capture. That does not scale to the question the harness is
for: given a dataset and what a reader wants from it, which of many
possible encodings — which sink, which options — is uncluttered, legible
and correct? Answering it systematically needs a rendering to be a
function of declared inputs, and its quality a record that can be
compared across candidates and builds.

Most of the parts exist and are not connected:

- **A venue that varies the encoding and holds the data fixed.** play's
  Experiments pane ([play_experiments_tab.go](../../apps/play/play_experiments_tab.go))
  drives one leeway batch through a chosen `streamreadaccess.SinkI` —
  card table, topology treemap, card-JSON, box-drawn tables, sparks — from
  either a built-in fixture or the active query's result. Its sinks'
  settings are constants, its selection is in-app state only, and it
  drives at most `experimentsMaxRows` rows, a cap sized for reading shape.
- **Scripted headless capture.** Scenes
  ([ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md))
  launch an app against the headless host
  ([ADR-0154](./0154-headless-carrier-tree-and-driver.md)), drive it and
  write PNGs. The `capture` step writes nothing else, although the host can
  produce the two things numerical analysis needs: an SVG of egui's shapes
  before tessellation (`svgexport.rs`, glyph-positioned text) and the
  accessibility tree with bounds.
- **Perceptual review.** The Tier 2 rubric catalogue
  ([tier2-llm-review](../design-system/policy/tier2-llm-review.md),
  ADR-0029 §SD9) defines clutter, colour-encoding, density and legend
  rubrics, a cache keyed on the screenshot hash and a cost cap; its driver
  does not exist beyond the SSIM pre-filter. The chat client
  (`public/llm/openaichat`) carries text only, so no judge can be shown a
  capture.
- **Model access as a capability.**
  [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) puts model
  calls behind one configured client and one sensitivity policy point.

Chart, graph and hierarchy renderings exist as play panels
(ADR-0097's channel negotiation over column names) rather than as sinks, so
they sit outside the venue that varies encodings over one batch.

## Decision

We will build **vizeval**: a harness that renders a *candidate* (a sink and
its options) over a *scenario* (a ClickHouse query that yields a leeway
batch, an intent, a viewport, and task questions with known answers) in
play's Experiments pane under the headless host, and records a
*scorecard* — geometry metrics, task-question accuracy and pairwise
aesthetic judgements — to `boxer.facts`. The search over candidates is not
part of this decision; the harness exposes what a search needs (SD2, SD8).

### SD1 — The Experiments pane is the render venue

Every candidate is rendered by the Experiments pane, so one mechanism —
a leeway batch driven through a `SinkI` — covers every representation
evaluated. Charts, graphs and hierarchies enter as **new sinks** that
project a batch onto the existing widgets (`implot`, `graphview`, `icicle`,
`sankey`, and a value-bearing `treemap` beside the shape-only topology
sink). What a sink claims from a batch — which section becomes an axis,
which membership an edge — is part of the candidate, which makes the
leeway-to-encoding mapping itself searchable, not only its styling.

The row cap becomes a per-sink property: shape-reading sinks keep a small
one, sinks whose picture depends on every row (charts, graphs) declare a
larger one, and a batch over a sink's cap is refused with its reason, as
panels refuse schemas, never silently truncated.

The pane wraps a sink's output in one named accessibility node
(`experiments.artifact`) so the harness can crop captures and scope metrics
to the artifact, not play's chrome.

### SD2 — A sink declares its option space

Each sink declares its settings as data: name, kind (enum, bounded int or
float, bool, colour palette by registry key), default, and for numerics the
range a search may explore. The declaration is the single source for the
pane's controls, for validation, and for the harness — a search enumerates
it rather than knowing sink internals. A value outside the declaration is
refused with a status line and the default stays in charge, the posture of
`graph_opts` (ADR-0231 §SD5).

A **candidate** is `(sink id, options)`; its identity is a content hash of
that pair in canonical form, which is the cache key everywhere below.

### SD3 — Seeding the pane

One registered variable (ADR-0009), `BOXER_PLAY_EXPERIMENTS`, carries a
candidate as JSON — `{"source": "result", "sink": …, "options": {…}}` — and
puts the pane in that state at launch; with `BOXER_PLAY_FOCUS_EXPERIMENTS`,
`BOXER_PLAY_SQL` and `BOXER_PLAY_AUTORUN` a capture needs no clicks. A seed
that does not parse against SD2 is refused at launch, not rendered with
defaults, because a harness scoring the wrong candidate is worse than one
that stops.

### SD4 — A scenario is a markdown document

`*.vizeval.md`, following the scene document (ADR-0248 §SD3): frontmatter,
prose, role-marked fences.

- **Frontmatter:** `size`, an `intent` sentence, the sinks the scenario
  admits (a scenario about hubs in a network does not admit a box-drawn
  table), and `questions` — each a prompt plus an `answer` SQL over the same
  data and a comparison (`eq`, `set`, `approx`+`tol`).
- **The first `sql` fence** is the dataset. It must be self-contained:
  data generated from `numbers()` with explicit seeds (`cityHash64`, not
  `rand()`), no reads of ingested tables. A scenario over live data cannot
  be compared across runs.
- **The prose** states the scenario for a human reviewer and is also the
  context handed to the judges.

Answers are computed by the harness from the answer SQL at run time, never
written into the document, so an answer cannot disagree with its data.
Every run records a digest of the batch; two scorecards with different
digests are not comparable, and the harness says so when asked to compare
them.

Scenario families in the first cut: **tables** (card, box-drawn, and any
table sink added later), **charts** (lines, bars, heatmap), **graphs**
(graphview), **hierarchies** (treemap, icicle, sankey). Tables carry the
most weight: they are the default reading of any batch.

### SD5 — Capture writes sidecars

The scene `capture` step gains an option to write, beside the PNG, the SVG
export of the same frame and the tree as JSONL (the `--treeFormat jsonl`
shape). The harness always requests them. This change is to the scene
runner and the headless host; scenes that do not ask are unaffected.

A candidate is rendered by a scene generated in memory from the scenario
and the candidate, run through the scene library (ADR-0248 §SD5), never
written to disk as a document.

### SD6 — The scorecard: three layers, gates before rankings

**Geometry metrics** are computed from the SVG and tree, cropped to
`experiments.artifact`, with no model involved:

- text/text and text/mark overlap; labels clipped at the artifact bounds;
  text ellipsised by its widget;
- smallest rendered glyph height; text contrast against the fill beneath it
  (WCAG ratio);
- ink ratio and whitespace; mark count against row count;
- pairwise distance (CIEDE2000) of categorical colours in use;
- for graphs, edge crossings and node–label overlap;
- for tables, numeric-column alignment, truncated cells, and row rhythm.

Each metric is a number, not a verdict. A scenario may name thresholds that
**gate** a candidate — "no overlapping labels", "no clipped text" — and a
gated-out candidate is scored no further.

**Task-question accuracy** asks a vision model each question with the
cropped PNG as the only evidence and compares its answer with the SQL
answer. Accuracy over a scenario's questions is the measure of *clear and
correct*: an encoding that a reader cannot answer the scenario's questions
from has failed, however it looks.

**Pairwise aesthetic judgements** show a model two candidates for the same
scenario and digest, with the scenario's intent and the Tier 2 rubric
criteria (V1 clutter, V2 colour encoding, V4 density, V5 legends, V7
typographic rhythm), and record which it prefers per criterion, with a
rationale. Rankings are fitted from the pairs (Bradley–Terry); each pair is
asked in both orders to cancel position bias. Pairwise, not absolute scores,
because a model's 1–10 grades drift between calls and a comparison does
not need a scale.

Judgement calls reuse the Tier 2 cache discipline — a key over the input
hashes, the rubric version and the model id — and a per-run cost cap. The
judge layers are built as the Tier 2 driver's first rubric consumers rather
than beside it.

### SD7 — Model calls go through ADR-0254

The judges call the model through `runtime.llm`'s client, so they are
configured by `BOXER_LLM_*`, recorded as `llmCall` rows, and pass the
sensitivity point. Scenario data is synthetic by SD4; a scenario whose SQL
reads a sealed dataset is refused by that point unless the provider is
local, which is the intended outcome. `openaichat` gains image content
parts in messages; that is the one change to the client.

### SD8 — Scorecards are facts

Results land in `boxer.facts` through a generated record store (the lane
[facts-bound-record-stores](../explanation/facts-bound-record-stores.md)
recommends), as kinds for a render (scenario, candidate hash, build commit,
batch digest, capture paths), a metric value, a task answer, and a pairwise
judgement. The PNG, SVG and tree stay on disk under the run directory;
facts carry their paths and hashes. A search session reads what has been
scored with SQL, and a candidate already scored at the same build and digest
is not rendered again.

The run directory also gets a contact-sheet gallery per scenario — the
candidates side by side with their gates, accuracy and rank — written the
way the scene runner writes its index.

### SD9 — Entry point

`imzero2 vizeval` with verbs to list a scenario's admissible candidates
(the option spaces of SD2), score a set of candidates given as JSONL, and
rank a scenario's scored candidates. It is a library first, as the scene
runner is, so a search written later calls it in-process.

### SD10 — Deferred

- **The search strategy.** Grid, random, Bayesian or LLM-proposed
  candidates are the next session's decision; nothing here assumes one.
- **A mark ledger.** A sink could emit, beside its drawing, which row each
  mark stands for, making correctness checkable without a model (every row
  has a mark, positions are monotone in value). Worth it once geometry and
  task accuracy show where they miss.
- **Human judgements** in the same pairwise kind, to calibrate the model
  judge. The kind admits a human judge from the start; the collection UI is
  deferred.
- **Maps and vector fields.** Tiles and paint callbacks do not appear in the
  SVG export, so geometry metrics would be partial there.
- **CI.** Scoring needs ClickHouse and model calls; no lane runs it.

### Milestones

- **M1 — Capture sidecars** ✓ (SD5) in the headless host and scene runner.
- **M2 — Option spaces and the seed variable** (SD1–SD3): declared option
  spaces, per-sink row caps, `BOXER_PLAY_EXPERIMENTS` and the artifact node,
  for the existing sinks, card table first.
- **M3 — Scenario documents, the runner and geometry metrics** (SD4, SD6
  first layer, SD9), with table scenarios, results as files.
- **M4 — The facts record store** (SD8).
- **M5 — Image content in `openaichat` and task-question accuracy** (SD6
  second layer, SD7).
- **M6 — Pairwise judgements and ranking** (SD6 third layer).
- **M7 — Chart sinks.**
- **M8 — A graphview sink.**
- **M9 — Hierarchy sinks** (value-bearing treemap, icicle, sankey).

## Surfaces — Tier 1

| Surface | Change | Moves with it |
| --- | --- | --- |
| scene `capture` step | adds SVG and tree sidecar outputs | the imzero2-drive skill page; the headless host's capture handling |
| Experiments sinks | declare an option space and a row cap | the pane's controls, which are generated from the declaration |
| `BOXER_PLAY_EXPERIMENTS` | added (ADR-0009 registry) | `doc/env-vars.md` |
| `openaichat` messages | image content parts | `runtime.llm`'s request path and the ADR-0254 sensitivity point |
| `boxer.facts` | new vizeval kinds | the generated record store and its regeneration lane |

## Alternatives

- **Render through play's panels, candidates as SQL plus opts.** The knob
  space is already data there, but it spans one vocabulary per panel
  (column names, `graph_opts`, mark chips) and none of them starts from a
  leeway batch; comparing a table against a treemap of the same data would
  mean two unrelated candidate models. Rejected in favour of one venue.
- **Render the widgets directly from Go fixtures.** Deterministic and
  independent of ClickHouse, but every knob needs Go plumbing and the result
  drifts from what play draws. Rejected.
- **Datasets from seeded Go generators.** Deterministic without a server;
  rejected because a scenario written as SQL is how play's users meet data,
  the task answers come from the same engine as the data, and SD4's
  self-containment rule gives the determinism back.
- **Absolute 1–10 aesthetic scores.** One call per candidate instead of per
  pair, but model grades are not stable across calls or comparable across
  scenarios. Rejected for pairwise preference.
- **SSIM against a golden rendering.** Measures change, not quality; it has
  no notion of a better rendering. It stays the Tier 2 pre-filter.
- **Results as JSONL under the run directory only.** Simplest, but a search
  that asks "what has been scored at this build" would reimplement a query
  engine over files. Rejected; files remain for captures.

## Consequences

### Positive

- A rendering's quality becomes a query: across candidates, across builds,
  and across judges.
- The Experiments pane gains charts, graphs and hierarchies, and its sinks'
  settings become visible controls rather than constants.
- The Tier 2 driver gets its first consumers and the image-capable client it
  was missing.

### Negative

- Chart, graph and hierarchy renderings exist twice — as panels over column
  names and as sinks over leeway batches — with separate mappings to
  maintain.
- The judge layers cost money per run and depend on a configured model; a
  harness without one produces geometry metrics only.
- Model judgements are not reproducible bit for bit; the cache makes a
  re-run agree with its first answer, not with a second opinion.
- Geometry metrics read the SVG export, which flattens gradients, drops
  textures and styles multi-section text by its first section; a widget
  that relies on those is measured partially.

### Neutral

- Scenario datasets are ClickHouse queries, so scoring needs a server, as
  scenes that run SQL already do.
- Correctness is measured through a reader (task accuracy), not by
  inspecting the drawing, until a mark ledger exists (SD10).

## Migration — Tier 1

Nothing to migrate: every surface change is additive. Existing scenes do not
request sidecars; existing sinks keep their current settings as the defaults
of their declared spaces.

## Verification plan — Tier 1

- **Lane.** Default `go test` for the metrics (fixture SVGs with known
  overlaps, clipping and contrast), the option-space validation and the
  scenario parser; the integration lane for one table scenario scored end to
  end with geometry metrics.
- **What would fail.** A metric that stops detecting a planted overlap; a
  sink whose controls and declared space diverge; a scenario whose answer
  SQL no longer runs.
- **Gap.** The judge layers are not tested against a live model; their
  parsing and caching are tested with recorded responses.

## Status

Proposed — awaiting review by the code owner.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way) for the edit-policy tiers (Tier 1 in-place / Tier 2 dated `## Updates` entry / Tier 3 new superseding ADR).

## References

- [ADR-0029](./0029-imzero2-design-system-and-policy-as-code.md) §SD9 — Tier 2 LLM review.
- [ADR-0154](./0154-headless-carrier-tree-and-driver.md) — headless host, tree and capture.
- [ADR-0248](./0248-imzero2-scenes-one-runner-and-assertions-in-the-trace.md) — scenes.
- [ADR-0254](./0254-model-inference-as-a-keelson-capability.md) — model inference as a capability.
- [tier2-llm-review](../design-system/policy/tier2-llm-review.md) — the rubric catalogue.
- Bradley, R. A.; Terry, M. E. (1952). Rank analysis of incomplete block designs: I. The method of paired comparisons. *Biometrika* 39 (3/4).
