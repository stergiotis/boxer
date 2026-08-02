---
type: adr
status: proposed
date: 2026-08-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0161: a Distribution panel for play — result contract + `descriptive_statistics` macro

## Context

play renders results as table, board, map, and flow — but not as a
distribution; the only distribution machinery in the tree is in-process
(`distsummary` over a caller-owned t-digest). The
[background survey](../adr-background-work/play-distribution-panel-survey.md)
carries the analysis, measurements, and literature; its design dialogue
(2026-08-02) settled the framing: play is one substrate serving three roles —
feature foundry, human-on-the-loop viewer, agentic surface — and
**comparison, not portraiture, is the primary view**.

Three verified facts enable a thin cut:

- The widget stack needs no raw data and no t-digest: `ecdf.RenderGrid`
  takes `(xs, fnAt, n)`; `letterval.Levels` takes any `QuantileOracle`;
  a quantile grid plus a count feeds everything, bands calibrated at true n.
- ClickHouse returns that grid in one aggregate call (`quantiles*` →
  `Array(Float64)`); play's Arrow path already folds `Array` columns.
- grammar1 parses `descriptive_statistics(a, b)` today, and the pass
  pipeline already hosts statement-level rewrites (`CanonicalizeFull` @ 50)
  and registered macro expansion (`LW_ID_*`, ADR-0106 §SD5).

This ADR decides three things and nothing else: the **result-shape
contract**, the **macro** that emits it, and the **panel** that renders it.
The adjacent tracks (time-uniform bands, conformal readout, lineup protocol,
agent surface) are not gated here.

## Design space (QOC)

**Q1 — how a user asks.** Table-function spelling: killed — new FROM grammar
surface, no prior art. Panel-authored follow-up queries: killed — ADR-0141's
single dispatch seam and ADR-0122 §SD1's no-guessing rule. **Chosen:
contract first, macro as sugar over it** — the panel cannot tell macro
output from hand-written SQL, which is what makes the contract the real
interface.

**Q2 — what crosses the wire.** Long form `(series, p, q)`: killed — scalar
stats need a second shape, and the Table fallback is noise. Private-lane
CTEs (the Sankey style): killed — the summary *is* the user's result, not an
auxiliary structure. **Chosen: wide form**, one row per series with
`Array(Float64)` grid columns — what `quantiles*` natively returns.

**Q3 — default estimator.** `exact`: killed as default — O(n) state per
group. `gk`: formally best (rank error composes with F-space bands) but its
`accuracy`→ε mapping is unverified — opt-in until pinned. `dd`: value-space
error, orthogonal to the bands — opt-in. Plain `quantile()`: excluded —
documented nondeterminism, no error model. **Chosen: `tdigest` default**,
estimator always named in the result.

**Q4 — how comparison enters v1.** SQL baseline marker: killed for v1 — a
baseline is a choice the macro cannot know; the column is an additive later
extension. Two-group `ks_*` columns: killed — a pair statistic has no honest
home in per-series rows and changes shape with group count. **Chosen:
panel-side baseline selection**; everything comparison-shaped in v1 derives
from the grids alone.

## Decision

### §SD1 — the result-shape contract

A body-zone tab claims the active main-channel result when the required
columns are present and valid — named columns rather than detection
(ADR-0122). Names are bare; validation makes accidental claims implausible.

| Column | Type | Required | Meaning |
| --- | --- | --- | --- |
| `series` | String | yes | display label; one row per series |
| `n` | UInt64 | yes | non-null count behind the quantiles — the band-calibration n |
| `ps` | Array(Float64) | yes | probability grid, strictly ascending, each in (0, 1) |
| `qs` | Array(Float64) | yes | `Q(p)` per grid point, non-decreasing, `len(qs) == len(ps)` |
| `n_null` | UInt64 | no | NULL-row count |
| `x_min`, `x_max` | Float64 | no | exact extremes |
| `mean`, `sd`, `skew`, `kurt` | Float64 | no | sample moments; shown with n, never alone |
| `hist_lo`, `hist_hi`, `hist_w` | Array(Float64) | no | histogram triplet — all three or none, equal lengths |
| `estimator` | String | no | provenance token (`tdigest`, `exact-hf7`, `gk:1000`, …); absent ⇒ unlabelled-approximate |

Violations (grid not ascending or out of range, length mismatch, non-monotone
`qs`, partial histogram triplet) reject loudly with the reason — never a
silently empty plot. `series_baseline` (UInt8) is reserved, unconsumed in v1.

### §SD2 — the probability grid

The macro emits one fixed grid; the panel decides post-hoc what is
trustworthy (resolving "the macro cannot know n" without a second round
trip): dyadic ladder `2^-k`, `1−2^-k` for k = 1…16 (what `letterval`
consumes) ∪ uniform `j/64` ∪ tails `10^-3`, `10^-4` and mirrors — **87
levels**, exported as a generated constant. The panel clamps letter-value
depth via `letterval.RecommendedDepth(n)`. The contract accepts any valid
grid; this one is the macro's choice, not the panel's demand.

### §SD3 — the macro

```sql
SELECT descriptive_statistics(['exact' | 'gk' | 'dd' | 'tdigest',] col1 [, col2 …])
FROM …  [WHERE …]  [GROUP BY k1 [, k2 …]]
```

- Canonical name `descriptive_statistics`, matched case-insensitively. The
  optional first string literal selects the estimator (default `tdigest`).
- **Sole-select-item rule**: mixing with other select expressions is a loud
  pass error — a merged output shape does not exist. Misuse errors at
  expansion time; a malformed macro never reaches the server (LW_ID
  precedent).
- Expansion: one `UNION ALL` branch per argument column over the original
  FROM/WHERE/GROUP BY, emitting §SD1 columns. `series` = argument text with
  GROUP BY key values folded via `toString` (NULL → placeholder); typed
  keys are deliberately not carried. `n` = `count(col)` (non-null — the
  honest calibration count); `n_null` = `count() − count(col)`; moments via
  `stddevSamp`/`skewSamp`/`kurtSamp`; `qs` from the chosen `quantiles*`
  family over the §SD2 grid. SETTINGS/parameters preserved; output not
  re-canonicalised (existing macro-family consequence).
- No histogram emission in v1 — `hist_*` stays contract-only until M2;
  hand SQL can ship it earlier.

### §SD4 — code home and pass registration

New package `public/analytics/stats/distsql`: grid constant + generator,
contract-name constants, claim validator, `QuantileOracle` adapter over
`(ps, qs, n)`, and the expansion pass. The statistics domain stays beside
`letterval`/`ecdfbands`; play imports it, not the reverse. The pass
registers in the keelson passreg defaults beside `ExpandLwIdMacros`
(`StagePreExecute`, after `CanonicalizeFull` @ 50), declares Idempotent,
is corpus-checked via `AssertProperties`; the play ordering pin
(`TestRegisterPassesOrdering`) grows one entry.

### §SD5 — the panel

`TabSpec{id: "dist", title: "Distribution", body zone, lazy,
shapeContract, writes: [selection]}`. Tabs stay independent — claiming
never affects Table. A series click writes the ordinary row cursor (this is
a main-result panel; the Sankey/ADR-0159 selection lesson in reverse).

Sub-views over implot, one claim, comparison-first:

- **ECDF** — all series via `RenderGrid(qs, ps, n)`, ADR-0156 palette;
  bands on all series when ≤ 3, otherwise focused series only.
- **Shift** (≥ 2 series) — Δ(p) = `Q_s(p) − Q_baseline(p)` per non-baseline
  series; baseline = panel-selected series (default: first row); band by
  conservative α/2 + α/2 combination; Wasserstein-1 readout (trapezoid
  over |Δq| dp) on the status line. Formal test labels are deferred to the
  comparison sibling spelling, where a baseline exists in SQL.
- **Boxen** — one letter-value column per series, depth clamped by n.
- **Histogram** — only when `hist_*` present; density-normalised (height =
  weight/width — the verified fractional-weight server output misleads
  otherwise), labelled a streaming approximation.

The honesty line is always visible: n, null count, estimator, band
method + α, and — under a sketch estimator — that the band excludes sketch
error. Degenerates are decisions: n = 0 renders "empty"; `min == max`
labels the collapse; few distinct `qs` values hint at GROUP BY.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| User-facing SQL vocabulary | new macro `descriptive_statistics` | editor docs/snippets; sqleditor completion corpus (ADR-0147); play help pages |
| keelson passreg defaults | new `StagePreExecute` entry | Passes tab listing; `TestRegisterPassesOrdering` pin; every defaults host (applets included) |
| Result-contract names | `series`, `n`, `ps`, `qs` + optionals (§SD1) | panel claim code; applet/gallery examples; future consumers of the shape |
| Exported Go API | new pkg `public/analytics/stats/distsql` | godoc; downstream modules |
| play tab registry | new `TabSpec` id `dist` | dock layout; `BOXER_PLAY_FOCUS_DIST` env knob (ADR-0009 registry); help corpus test |
| Widgets | none — consumed as-is | — |

## Alternatives

Structural kills live in §Design space. Additionally rejected:

- **Prefixed contract names** (`dist_*`) — hand SQL is first-class and §SD1
  validation already makes accidental claims implausible.
- **A short macro alias** — two names for one expansion; the macro-family
  precedent is one canonical name.
- **Parametric spelling** `descriptive_statistics('gk')(…)` — parses, but a
  second CST shape for zero added expressiveness.
- **Histogram from the macro in v1** — bin-count and normalisation defaults
  deserve their own look (M2); descope over gate.
- **Single-scan expansion** (tuple aggregation instead of `UNION ALL`) —
  deferred, not rejected: surface-invisible optimisation, can land without
  an Update.

## Consequences

### Positive

- The contract works the day the panel lands — before the macro, and for
  producers the macro will never cover.
- Widgets are consumed unmodified; their existing seams are the wire shape.
- The macro is a stable one-line vocabulary: emit-able by an agent,
  verifiable by a human, graduating to scheduled execution unchanged.

### Negative

- `UNION ALL` scans the relation k times for k columns (until the deferred
  single-scan rewrite).
- `series` folding loses group-key types; typed keys require hand-written
  contract SQL.
- Under `tdigest`, sketch error is disclosed but unquantified; the
  quantified path (`gk` + band inflation) waits on the accuracy→ε gap.

### Neutral

- The survey ages into a background snapshot once this is accepted.
- The grid constant is API; changing it moves goldens, not correctness.

## Migration

- **Breaks.** None — every surface is new; a query already naming
  `descriptive_statistics` previously failed with `UNKNOWN_FUNCTION`.
- **Path.** (1) `distsql` package + unit tests. (2) Panel M0 against
  hand-written contract SQL. (3) Expansion pass + defaults registration +
  goldens; ordering pin updated. (4) M2: histogram emission + view; gallery
  applet + docs. Each step leaves the tree green.
- **Regeneration.** None — no IDL, codec, or FFFI2 boundary.
- **Old shape.** n/a; the in-process `distsummary` path is untouched.

## Verification plan

- **Unit lane.** Expansion goldens (spellings, estimator tokens, GROUP BY
  folding, SETTINGS survival, sole-select-item and arity rejects); grid
  constant pinned against its generator; oracle round-trips; claim
  accept/reject fixtures with pinned messages; `AssertProperties`
  (idempotent). Contract drift, pass reorder, or a grid change each redden
  a named test here.
- **Integration lane** (`CLICKHOUSE_ENDPOINT`-gated). Expanded SQL executes
  live; the Hyndman–Fan fixture (`[1,2,3,4]`, p=0.25 → 1.75/1.25
  inclusive/exclusive) pins estimator semantics against server upgrades;
  `Array` columns arrive as Arrow lists through the play client.
- **GUI.** One ADR-0154 tour scene (seeded multi-group dataset: ECDF
  overlay + shift + honesty line) plus an egui-mcp drive for claim, loud
  reject, baseline click, and the 4-series band policy.
- **Gap.** The `gk` accuracy→ε mapping is unverified — blocks flipping the
  default and the band-inflation feature. Server-tdigest ≙ in-process
  tdigest is not verified — acceptable, the token names a family, not an
  implementation. Whether 87 levels look sufficient at every zoom is
  review judgement.

## Status

Proposed 2026-08-02, out of the
[distribution-panel survey](../adr-background-work/play-distribution-panel-survey.md)
and its recorded dialogue. Milestones in §Migration; the four adjacent
tracks are deliberately not gated by this ADR.

## References

- [Background survey](../adr-background-work/play-distribution-panel-survey.md) — measurements, literature, kill detail.
- [ADR-0122](./0122-play-kanban-panel.md) — named-column claim contract.
- [ADR-0159](./0159-imzero2-sankey-flow-widget.md) — the private-lane style not used here.
- [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) §SD5 — macro-family precedent.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — the single dispatch seam.
- [ADR-0149](./0149-implot-core-port-painter-lane.md), [ADR-0156](./0156-qualitative-palette-dark-surface.md) — rendering substrate, palette.
- [ADR-0009](./0009-environment-variable-registry.md) — env-var registry for the focus knob.
