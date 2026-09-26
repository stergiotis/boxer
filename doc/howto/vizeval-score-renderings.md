---
type: how-to
audience: engineer comparing renderings of leeway data
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# How to score renderings of a leeway batch

This recipe renders one dataset through several of play's Experiments sinks
and records geometry metrics for each — overlap, clipping, elision, contrast,
colour distance, numeric alignment — as files you can read or compare. It
covers the harness as built through
[ADR-0257](../adr/0257-vizeval-scored-renderings-of-leeway-batches.md) (proposed)
M6: geometry metrics, model-answered task questions and a pairwise ranking,
filed as files and optionally in `boxer.facts`. Model-judged task accuracy and pairwise ranking are later
milestones, and searching the candidate space is left to the caller.

## When to use this recipe

You want to know which way of drawing a batch is legible, or whether a change
to a sink made its output better or worse, with numbers rather than by eye.
Each candidate is one headless launch of play, so this is minutes, not a unit
test.

## Prerequisites

- A ClickHouse the configured endpoint reaches (`CLICKHOUSE_URL`, see
  [env-vars](../env-vars.md)); scenario datasets are generated there, so no
  table needs to exist.
- The headless Rust client: `rust/imzero2/build_rust_headless_soft.sh`. The
  launcher names it if it is missing or older than the generated sources.

## Steps

1. **Write a scenario** as `<name>.vizeval.md` — the maintained ones are in
   [apps/play/vizeval](../../apps/play/vizeval). The frontmatter's `vizeval:`
   key holds `size` or `artifact` (below), a one-sentence `intent`, the `sinks` the scenario admits,
   `questions` with answer SQL, and `gates`. A `sql base` fence holds the data
   with plain column names, generated from `numbers()` with seeded hashes; the
   first plain `sql` fence projects it into a leeway table with the `LW_*`
   constructors ([leeway-sql-reading-and-authoring §9](./leeway-sql-reading-and-authoring.md)).
   Answers read `base` too, so they come from the same rows as the picture.
   The format's reference is `vizeval.ScenarioSpec`
   ([scenario.go](../../public/thestack/imzero2/vizeval/scenario.go)).

   `size` is the capture's viewport, of which play's own chrome takes about
   95 by 440 points; the sink gets what is left, and that changes when play's
   chrome does. `artifact: WxH` gives the sink exactly that box instead, with
   the viewport grown to fit (or set it with `size` too). A candidate whose
   artifact is not the box is `failed` with a reason saying which way it
   missed. Prefer `artifact` for a sink whose picture depends on its height,
   such as one that summarises rows it has no room for.

   The sinks a scenario can admit are the Experiments pane's: the card
   table, the box-drawn tables, card-JSON, the topology treemap and sparks,
   and `chart`, which draws the first tagged section with a numeric value
   as bars, lines, points or a heatmap, one category per entity and one
   series per membership, and `graph`, which draws entities as nodes and
   the section whose values name other entities as edges
   ([40_service_graph](../../apps/play/vizeval/40_service_graph.vizeval.md)
   is the worked example). A graph candidate also gets `graph.*` metrics:
   edge crossings and labels drawn over another node. `hierarchy` splits
   each entity's label into a path and draws the weighted tree as a
   treemap, an icicle or a sankey
   ([50_storage_hierarchy](../../apps/play/vizeval/50_storage_hierarchy.vizeval.md)).

2. **List what can vary.** Each admitted sink's row cap and option space, one
   JSON line per sink — the input a search enumerates:

   ```bash
   scripts/dev/vizeval.sh space apps/play/vizeval
   ```

3. **Score.** Without `--candidates`, every admitted sink at its defaults;
   with it, one `{"sink":…,"options":{…}}` per line:

   ```bash
   scripts/dev/vizeval.sh score --out tmp/vizeval apps/play/vizeval
   printf '%s\n' '{"sink":"unicode","options":{"width":240}}' '{"sink":"card","options":{"palette":"magma"}}' > c.jsonl
   scripts/dev/vizeval.sh score --out tmp/vizeval --candidates c.jsonl apps/play/vizeval/20_long_labels.vizeval.md
   ```

4. **File them, optionally.** `--facts` also writes each scorecard to
   `boxer.facts` (creating the table if needed) and answers a candidate
   already measured there — same scenario, candidate, clean build and batch
   digest — without rendering it again; `--rescore` renders anyway. A build
   from a dirty tree is never reused. Read everything filed back as JSON
   lines:

   ```bash
   scripts/dev/vizeval.sh score --facts apps/play/vizeval
   scripts/dev/vizeval.sh facts --scenario 20_long_labels
   ```

5. **Ask a model, optionally.** `--judge` asks each scenario question of the
   vision model `BOXER_LLM_ENDPOINT` / `BOXER_LLM_MODEL` name, about every
   candidate that passed its geometry gates, and records `task.accuracy`;
   the gallery shows each answer beside the expected one. Replies are cached
   under `<out>/judge-cache` by what the picture shows, so a re-run of the
   same drawing costs nothing; `--judgeCalls` bounds the calls a run makes.
   Scenario data is synthetic, so nothing sealed leaves the machine.

   ```bash
   BOXER_LLM_ENDPOINT=http://localhost:1234/v1/ BOXER_LLM_MODEL=<vision model> \
     scripts/dev/vizeval.sh score --judge --facts apps/play/vizeval
   ```

6. **Rank.** `rank` reads a score run's output, keeps the candidates that
   passed their gates, and asks the model to compare every pair in both
   orders on five criteria; it writes `<out>/<scenario>/ranking.md` with
   Bradley–Terry strengths overall and per criterion beside task accuracy.
   Candidates that drew the same thing are tied without a call; comparisons
   are cached like answers. `n` candidates cost `n(n-1)` calls the first time.

   ```bash
   scripts/dev/vizeval.sh rank --facts --out tmp/vizeval apps/play/vizeval/10_host_metrics.vizeval.md
   ```

7. **Read the results.** `tmp/vizeval/<scenario>/contact.png` is every
   candidate of the run in one image, numbered in scoring order and labelled
   with its status, sink, id prefix and options, and — for a candidate that
   was not scored — the failed gates or the reason. It is 1600 pixels wide,
   two candidates to a row and six to a sheet; further candidates go to
   `contact-2.png` and on. All sheets of a run share one scale, so sizes
   compare across them. The thumbnails show layout and density; small text in
   them is not legible, so read a candidate's own `artifact.png` for that.

   `tmp/vizeval/<scenario>/index.md` has the computed answers, the sheets,
   and each candidate's cropped artifact with its gates and metrics under the
   same number. `tmp/vizeval/scorecards.jsonl` has every scorecard ever
   written there, one per line, with the build it was scored at and a digest
   of the batch; each candidate's directory holds the full capture, its SVG
   and tree sidecars, and `artifact.png`.

8. **Compare a round as a table.** `table` prints one aligned line per
   scorecard — id prefix, sink, options, status and the metrics you name —
   grouped by scenario and batch digest, with a build column when a group
   spans builds. A candidate scored more than once over one batch shows its
   latest card unless `--all`; `--sort` orders each group by a metric
   (`-` descends); `--facts` reads `boxer.facts` instead of `--out`.

   ```bash
   scripts/dev/vizeval.sh table --out tmp/vizeval --scenario 60_mixed_kinds \
     --metrics text.elided_share,rows.labelled_share,text.min_contrast --sort -rows.labelled_share
   ```

## Verification

Every candidate line printed by `score` reads `scored`, `gated` or
`inadmissible`; `failed` means the launch or the measurement did not complete,
and its reason says which. The integration test scores one scenario end to
end:

```bash
go test -tags="$(cat ./tags),integration" ./public/thestack/imzero2/vizeval/harness/ -run TestScoreHostMetrics
```

## Reading the numbers

- Metric names and definitions are the `Metric*` constants in
  [geometry/metrics.go](../../public/thestack/imzero2/vizeval/geometry/metrics.go).
  They are measured inside the `experiments.artifact` node's visible rect only.
- Compare scorecards only when their `batchDigest` and `build` match; a
  different digest is different data.
- `text.cut_at_edge` counts text running past the visible artifact, which in
  a scrolling pane is expected; `text.clipped` counts text cut by its own cell.
- `text.elided_share` is `text.elided` over `text.runs`: the share of what is
  written that was shortened. It says nothing of what was not written at all.
- `rows.labelled_share` is the share of the batch's distinct natural keys
  written somewhere in the visible artifact. It is a floor on how many rows a
  reader can name, not a count of rows drawn: a bar with no tick label, a
  hierarchy cell labelled by the last part of its path, or a row the sink
  summarised ("… 6 more rows") counts as missing. Compare it between
  candidates of one sink family rather than across families.
- A sink whose output is one text shape (the JSON code view) is one text run:
  its overlap and clipping counts say little.

## Troubleshooting

- **Symptom:** a candidate is `inadmissible` with "over the sink's cap".
  **Cause:** the batch has more rows than that sink draws.
  **Fix:** none needed — the harness does not score a picture of part of the
  data. Use fewer rows, or a sink with a larger cap.
- **Symptom:** every candidate `failed` with a `skip:` reason.
  **Cause:** the scene precondition `clickhouse` does not hold.
  **Fix:** check the endpoint answers `SELECT 1`.
- **Symptom:** `failed`, "the artifact node is not in the tree".
  **Cause:** the Experiments pane did not draw — usually the dataset is not
  leeway-shaped, and the pane showed a notice instead.
  **Fix:** open `<candidate>/capture.png`; run the dataset in play by hand.
