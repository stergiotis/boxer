---
name: vizeval
description: "Use when searching for, scoring or comparing renderings of leeway data with the vizeval harness (ADR-0266) — writing a scenario (*.vizeval.md), enumerating sinks and their option spaces, scoring candidates headlessly for geometry metrics, task-question accuracy and pairwise rankings, and reading scorecards back from files or boxer.facts. Covers what each metric does and does not mean, and the pitfalls that make two scorecards incomparable."
type: how-to
audience: agent or engineer searching the encoding space of a leeway batch
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Scoring renderings of leeway data with vizeval

vizeval renders one leeway batch through play's Experiments pane under the
headless host, once per **candidate** — a sink and its options — and records a
**scorecard** per candidate: geometry metrics from the capture's SVG, and
optionally a vision model's answers to the scenario's questions and its
pairwise preferences. The decision record is
[ADR-0266](../../adr/0266-vizeval-scored-renderings-of-leeway-batches.md)
(proposed); the step-by-step recipe is
[vizeval-score-renderings](../../howto/vizeval-score-renderings.md). This page
is what you need to run a search with it.

The harness scores; it does not search. Choosing the next candidates is yours.

## Before you start

- **ClickHouse** at the configured endpoint (`CLICKHOUSE_ENDPOINT` /
  `CLICKHOUSE_URL`; default `http://localhost:8123/`). Datasets are generated
  there from `numbers()` / `values()`; no table needs to exist. Every candidate
  whose scene cannot reach it is `failed` with a `skip:` reason.
- **The headless client**: `rust/imzero2/build_rust_headless_soft.sh`. Rebuild it
  after any change under `rust/imzero2`, or the capture runs old code.
- **A model** only for `--judge` and `rank`: `BOXER_LLM_ENDPOINT` and
  `BOXER_LLM_MODEL` (an OpenAI-compatible endpoint serving a vision model).
  Without one, task questions can be answered by readers from judge sheets
  (below).
- Run from the checkout: `scripts/dev/vizeval.sh` builds the `imzero2` binary
  once per checkout and runs `imzero2 vizeval`.

## The loop

```bash
V=scripts/dev/vizeval.sh
$V space apps/play/vizeval/10_host_metrics.vizeval.md      # what may vary: sinks, row caps, option spaces (JSON lines)
$V score --facts --out tmp/vz --candidates c.jsonl apps/play/vizeval/10_host_metrics.vizeval.md
$V score --facts --judge --out tmp/vz --candidates c.jsonl …   # + task accuracy for gate-passing candidates
$V rank  --facts --out tmp/vz apps/play/vizeval/10_host_metrics.vizeval.md
$V table --out tmp/vz --metrics text.elided_share,rows.labelled_share --sort -rows.labelled_share
$V facts --scenario 10_host_metrics                         # everything filed, as JSON lines
```

- A candidates file holds one `{"sink": …, "options": {…}}` per line. Options
  left out take their defaults; an option the sink does not declare, a value
  out of range or of the wrong type is refused, never clamped. Without
  `--candidates`, each admitted sink is scored at its defaults.
- **Identity.** A candidate's id is a hash of its canonical form, so an option
  left out and one set to its default are the same candidate.
- **Output.** `<out>/<scenario>/contact.png` is the round in one image:
  every candidate's artifact, numbered and labelled with status, sink and
  options, six to a sheet (`contact-2.png` and on past that), one scale for
  all sheets. Look at it first; open a candidate's `artifact.png` for text,
  which is not legible at the sheet's scale. `<out>/<scenario>/index.md` has
  the answers, then each candidate under the same number with its cropped
  artifact, gates and metrics;
  `<out>/<scenario>/<candidateId>/` holds the capture, `capture.svg`,
  `capture.tree.jsonl`, `artifact.png` and the scorecard;
  `<out>/scorecards.jsonl` accumulates every scorecard written to that
  directory. `rank` writes `ranking.md` / `ranking.json` beside the index.
- **Comparing a round.** `table` prints one aligned line per scorecard — id
  prefix, sink, options, status, the `--metrics` you name (`-` when a card
  lacks one) — grouped by scenario and batch digest, each candidate's latest
  card unless `--all`. It reads `--out`'s `scorecards.jsonl`, or `boxer.facts`
  with `--facts`. There is no need to script over `scorecards.jsonl` for this.
- **Cost.** One candidate is one launch of play: seconds, not milliseconds.
  `--judge` asks one model call per question per candidate; `rank` asks
  `n(n-1)` calls for `n` candidates. `--judgeCalls` bounds a run; replies are
  cached under `<out>/judge-cache` by what the picture shows, so re-running is
  free.

## Judging without a model: reader sheets

`score --judgeSheet` writes `<out>/<scenario>/judge/<sheetId>.md` and
`.png` per drawing of a candidate that passed its gates — the picture, the
intent, the questions, nothing else — plus one **control**: a scored artifact
blurred until nothing is legible. `key.json` beside them maps sheets to
candidates; never hand it to a reader.

**You are not a blind reader.** You wrote or read the scenario, and
`index.md` prints every expected answer. Do not answer sheets yourself.
Instead, per sheet:

1. Copy `<id>.md` and `<id>.png` to a directory of their own, away from the
   output directory.
2. Start one **fresh** sub-agent for that sheet alone, told to read only
   those two files, with a reader label (`agent:r1`, …) and an answers file
   of its own. Never give one reader two sheets of a scenario.
3. Concatenate the answers files and score them:

```bash
$V score --judgeSheet --out tmp/vz apps/play/vizeval/10_host_metrics.vizeval.md
# … one fresh reader per sheet, as above …
cat readers/*/answers.jsonl > answers.jsonl
$V score --answers answers.jsonl --out tmp/vz apps/play/vizeval/10_host_metrics.vizeval.md
```

The replies go through the rule the model's do. A reader who answered two
sheets of the scenario, or any control question correctly, is **informed**:
its replies land in `task.informed.*`, which no gate or ranking reads. A
sheet answered by two readers is refused. Sheet ids follow the drawing, so
answers survive a re-render; a candidate that now draws something else gets a
`stale` verdict. `--answers` and `--judge` do not combine. The scorecard's
`taskJudge` says who answered (`model:<id>` or `reader:<label>`): compare
accuracies within one judge only.

## Statuses

| status | meaning |
| --- | --- |
| `scored` | rendered, measured, passed every gate the scenario names |
| `gated` | rendered and measured, failed a gate; metrics are there, judges skip it |
| `inadmissible` | not rendered: the scenario does not admit the sink, or the batch has more rows than the sink's row cap |
| `failed` | attempted, nothing measured: the scene could not launch, or the pane drew no artifact; the reason says which. A judge's failed or unbudgeted call does not fail the candidate — it shows as `task.errors` |

## Writing a scenario

A scenario is `*.vizeval.md` beside the others in
[apps/play/vizeval](../../../apps/play/vizeval); the format's reference is
`vizeval.ScenarioSpec` in
[scenario.go](../../../public/thestack/imzero2/vizeval/scenario.go), and
[10_host_metrics](../../../apps/play/vizeval/10_host_metrics.vizeval.md) is the
shortest complete example.

- **`sql base` fence** — the data with plain column names, self-contained:
  `numbers()` or `values(…)`, sizes from `cityHash64(x, seed)`, never `rand()`
  and never an ingested table. Data that can change makes scorecards
  incomparable.
- **First plain `sql` fence** — the projection into a leeway table with the
  `LW_PLAIN` / `LW_TV` / `LW_TV_MEMB` / `LW_TV_SUPPORT` constructors
  ([leeway-sql-reading-and-authoring §9](../../howto/leeway-sql-reading-and-authoring.md)),
  reading `base`. Name the entity key `natural-key`: every sink labels by it.
- **`questions`** — `answer` is SQL over `base`, so answers come from the same
  rows; `compare` is `eq` (ordered), `set` or `approx` with `tol`. Ask what the
  intent promises a reader can see; a question no rendering could answer
  measures nothing.
- **`artifact: WxH`** — the box the sink is given, whatever play's chrome
  takes (about 95 by 440 points of the viewport). Without it, `size` is the
  viewport and the sink gets the remainder: a 1600x1000 scenario gives it
  roughly 1500x560, and row-heavy sinks run out of height. With it, `size`
  may be left out; a viewport too small for box and chrome fails the
  candidate. Cards of two boxes are grouped apart by `table` and never
  reused for each other.
- **`gates`** — bounds on metric names (`text.clipped: {max: 0}`). A metric a
  candidate does not have fails a gate that names it — except `task.*` gates,
  which are only evaluated when a judge ran.
- **`sinks`** — admit only sinks the data suits: a scenario about magnitudes
  should not admit a sink that discards values.

The sinks project a batch in fixed ways; the options vary how the projection
is drawn. Exact option ranges come from `space` (the catalogue is
[sinks.go](../../../public/thestack/imzero2/vizeval/sinks.go)):

| sink | projection | options |
| --- | --- | --- |
| `card` | one row per attribute, grouped by entity and section | palette |
| `unicode` | box-drawn tables per section | table width, per-column cap |
| `json` | canonical card-JSON | — |
| `chart` | first tagged section with a numeric value: category per entity, series per membership | mark, seriesBy, sort, legend, colormap |
| `graph` | node per entity; edges from the section whose values most often name another entity | layout, orientation, spacing, labels, arrows, group tones |
| `hierarchy` | entity label split into a path, leaves weighed by values or counted | form (treemap/icicle/sankey), sizeBy, separator, maxDepth, colorBy |
| `topology`, `topo`, `braille`, `treemap` | the batch's shape, values discarded | — |

A batch whose shape a projection does not fit is a scenario that should not
admit that sink — the chart of a batch with no numeric section, the graph of
one where no value names an entity.

## Reading the metrics

Names and definitions are the `Metric*` constants in
[geometry](../../../public/thestack/imzero2/vizeval/geometry) and the harness.
All are measured inside the `experiments.artifact` node's visible rect only.

- `text.overlap_pairs`, `text.clipped`, `text.elided` — the gate-worthy
  faults. In practice overlap and clipping rarely fire on the table-like
  sinks; `text.min_contrast` and `text.elided` are the ones that have caught
  faults there. `clipped` is text cut by its own cell or widget; `text.cut_at_edge`
  is text running past the artifact's edge, which a scrolling table does by
  design — do not gate it without meaning to.
- `text.elided` counts egui's ellipses and ellipses a sink wrote into the
  text itself (the box-drawn tables cut wide cells that way).
- `text.elided_share` — `text.elided` over `text.runs`. Only what was
  written and shortened; what a sink left out entirely is not in it.
- `rows.labelled`, `rows.labelled_share` — the batch's distinct natural keys
  written, as a delimited token, in a visible unelided run of the artifact
  (count, and share of all keys). A floor on the rows a reader can name, not
  a count of rows drawn: an unlabelled mark, a label that is only part of the
  key (the hierarchy's path segments), or a row summarised away ("… 6 more
  rows") counts as missing. Compare within one sink family; across families
  it mostly measures whether the sink labels by key at all.
- `text.min_contrast`, `text.low_contrast` — WCAG ratio of each label against
  what is painted under its centre, composited in paint order.
- `color.distinct`, `color.min_delta_e` — chromatic colours in use and how far
  apart the closest two are (CIEDE2000; under ~10 is hard to tell apart).
- `table.numeric_columns`, `table.numeric_right_aligned` — columns of numbers,
  and how many line up on the right.
- `graph.*` — only for the graph sink: `edge_crossings` (edges meeting at a
  node do not count) and `label_node_overlaps` (a label over a node that is
  not its nearest).
- `task.informed.accuracy` (and `.unreadable`, `.errors`) — the same from
  a reader shown to know the answers; not a measurement of the picture.
- `task.accuracy` — share of questions the judge answered correctly from the
  artifact alone; absent when any question got no answer (`task.errors`).
  `task.unreadable` counts "the picture does not show this".
- `rank` strengths are Bradley–Terry log-strengths: a difference of 1 is odds
  of e to 1. A preference counts only when both presentation orders agree;
  disagreements are splits, so a position-biased model ranks nothing.

## Comparing scorecards

- **Same `batchDigest`** — else different data. **Same `build`** — else
  different code. A build ending `+dirty` is never reused from `boxer.facts`,
  nor is `unknown` (a binary with no VCS stamp); commit before a long search
  if you want reuse. Built by `scripts/dev/vizeval.sh`, only tracked changes
  make a build dirty; built any other way, Go's own flag applies, which also
  counts untracked files.
- **`drawingDigest`** identifies what was drawn. Two candidates with one
  digest drew the same thing — an option the sink ignored for this data — so
  keep one of them and stop varying that option.
- **Pixels are not reproducible, drawings are.** The software rasterizer
  differs from run to run by a level or two on a few hundred pixels; the
  metrics and the digest come from the SVG and do not.
- A single run's rank is relative to the candidates in it. Rank again when
  the set changes; comparisons already made are cached.

## Searching well

- **Gate first, judge later.** Score a broad set without `--judge`, keep what
  passes the geometry gates, then judge and rank only those.
- **Vary one option at a time from a good candidate**, and use
  `drawingDigest` to drop options that change nothing.
- **Look at the contact sheet after every round**, then at the artifacts
  of the candidates it makes you doubt.
- **Look at the pictures of the extremes.** A metric that says a candidate is
  best is a claim about the metric until the artifact agrees: every flaw
  found in building this harness showed first as a number that the picture
  contradicted.
- **Suspect the measurement when a number is implausible** — hundreds of
  overlaps, clipping on text that reads whole. The known traps are fixed
  (glyph ink bounds, haloed labels counted once); a new widget can bring a
  new one.

## When a candidate fails

- `failed` with `skip: no ClickHouse at …` — the endpoint does not answer.
- `failed`, "the artifact node is not in the tree" — the pane drew nothing.
  Usually the dataset is not leeway-shaped: open
  `<candidate>/capture.png`; the pane's notice says why.
- A sink's picture shows old behaviour after a code change — the headless
  client or the `imzero2` binary was not rebuilt.
- `rank` says fewer than two scored candidates — score more, or relax the
  gates; only `scored` candidates of the newest batch take part.
