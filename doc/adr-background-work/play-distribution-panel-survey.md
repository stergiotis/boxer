---
type: explanation
audience: package maintainer
status: stable
reviewed-by: "p@stergiotis"
reviewed-date: 2026-08-02
---

# Distribution data as a play panel — a design-space survey

> **Provenance.** Compiled 2026-08-02, ahead of any decision: nothing in here
> is settled, and an ADR — not this page — is where any of it would become
> one. Provenance is three-tiered and marked throughout: (a) claims about this
> repository were verified against the working tree on the compile date;
> (b) claims about ClickHouse behaviour were verified empirically against
> `clickhouse-local` 26.7.1.1315 (official build) — the probe queries and
> observed outputs are inlined; (c) the literature in §2 comes from general
> knowledge — titles and venues are given so a reader can verify, but the
> works were not re-read for this survey. Treat tier (c) as pointers, not
> citations. Outcomes of the design dialogue of 2026-08-02 are recorded
> inline as settled direction — decisions, not measurements.

## 1. The question

play should be able to show *distributions* — of one or more numeric result
columns, possibly per group — as a regular dock panel, with the same
statistical footing the in-process `distsummary` inspector already has for
live telemetry. Four things need deciding, and they are separable:

1. **The SQL surface** — how a user asks for a distribution (§5). The prompt
   for this survey sketched `SELECT descriptiveStatistics(mycol1, mycol2,
   mycol3)` expanded by a nanopass pass.
2. **The result-shape contract** — what crosses the wire from ClickHouse to
   the panel (§6).
3. **The estimator policy** — what the numbers mean, and how honestly the
   panel can label them (§4).
4. **The chart roster** — what is drawn, and when each form is the right
   one (§2, §7).

The existing widget and pass infrastructure constrains all four in a helpful
way: most of the hard parts already exist (§3).

### 1.1 The three roles play serves (settled 2026-08-02)

The design dialogue settled a framing that reshapes the priorities below.
play serves three roles on one substrate:

- **(a) Feature foundry** — a human develops statistical "features" (a
  quantile contrast, a distribution distance, a drift score) that later
  graduate into automated comparison and monitoring. Graduation is cheap
  because the feature already *is* SQL: the same query re-executes on a
  schedule and its output persists as facts.
- **(b) Human-on-the-loop viewer** — the explorative substrate for routine
  work (sql applets, drill-down, adjudicating what detectors flag).
- **(c) Agentic surface** — a human–machine team steers; the macro
  vocabulary is the agent's constrained, auditable tool-call surface and the
  rendered panel is the human's verification artifact. Marked a strategic
  goal in the dialogue.

The roles are one pipeline read at three maturity stages — explore,
routinize, delegate — and artifacts must cross the stage boundaries without
translation. That is why everything below is data: the queries, the result
contract, and (for the lineup protocol, §2.4) the adjudications themselves.
The composition consequence: **comparison, not portraiture, is the primary
view** (§7).

## 2. What "scientifically correct" means here (literature)

Tier (c) throughout this section.

### 2.1 Estimation is a choice, before anything is drawn

- **Quantile definitions differ.** Hyndman & Fan, "Sample quantiles in
  statistical packages" (The American Statistician, 1996) catalogues nine
  conventions; they disagree noticeably at small n and in tails. A panel that
  shows quantiles should know — and be able to say — which convention
  produced them. ClickHouse exposes several (§4.1).
- **Histogram binning is a free parameter.** Sturges' rule underbins at
  large n; Scott ("On optimal and data-based histograms", Biometrika, 1979)
  and Freedman & Diaconis ("On the histogram as a density estimator",
  Z. Wahrscheinlichkeitstheorie, 1981) derive data-driven widths (FD:
  2·IQR/n^⅓). Two honest options exist: fix the rule and say so, or show the
  sensitivity. A histogram drawn with an unstated, arbitrary bin count is the
  classic way to manufacture or hide modality.
- **KDE bandwidth is a stronger free parameter.** Violin plots (Hintze &
  Nelson, The American Statistician, 1998) and their descendants inherit
  kernel bandwidth selection (Silverman's rule vs. Sheather–Jones); the
  smoothing silently shapes conclusions and behaves worst exactly where data
  are interesting (bounded support, heavy tails, discreteness). This survey
  treats KDE-based forms as opt-in extras, not defaults.
- **Sketches have different error models.** Greenwald & Khanna (SIGMOD 2001)
  bound *rank* error (ε·n); DDSketch (Masson, Rim & Lee, VLDB 2019) bounds
  *relative value* error; t-digest (Dunning & Ertl, "Computing extremely
  accurate quantiles using t-digests", arXiv 2019) has excellent tail
  behaviour in practice but no worst-case guarantee. Rank-error bounds are
  the interesting ones for ECDF display, because they compose with
  ECDF confidence bands, which also live in rank/probability space (§6.3).

### 2.2 Uncertainty must be visible

- The ECDF admits *simultaneous* finite-sample confidence bands: the
  closed-form DKW inequality with Massart's tight constant ("The tight
  constant in the Dvoretzky–Kiefer–Wolfowitz inequality", Annals of
  Probability, 1990), and tighter exact bands (Berk & Jones 1979;
  Moscovich & Nadler, "Fast calculation of boundary crossing probabilities
  for Poisson processes", 2017). This repository already implements all of
  these (§3.2) — the panel should inherit, not re-derive.
- Showing distributions without n, or point summaries without spread, misleads
  even experts: Correll & Gleicher, "Error bars considered harmful" (IEEE
  TVCG, 2014); Hullman, Resnick & Adar on hypothetical outcome plots
  (PLOS ONE, 2015); Kay, Kola, Hullman & Munson on quantile dotplots
  (CHI 2016) and Fernandes et al. (CHI 2018) on their decision-making
  advantage. Consequence for the panel: n, null-count, and estimator
  provenance are first-class display elements, not tooltips.
- Summary statistics alone famously underdetermine shape: Anscombe's quartet
  (1973); Matejka & Fitzmaurice, "Same stats, different graphs" (CHI 2017).
  This is the argument for the panel existing at all, rather than a stats
  row in the Table tab.

### 2.3 Which display answers which question

A rough task mapping, synthesised from Cleveland (*Visualizing Data*, 1993),
Wilke (*Fundamentals of Data Visualization*, 2019, distribution chapters),
and the papers above:

| Question | Best form | Notes |
| --- | --- | --- |
| "What fraction is below x?" / "what is Q(p)?" | **ECDF + band** | Every quantile readable; no binning or bandwidth parameter; bands quantify finite-sample doubt. |
| "Did the distribution shift between groups?" | **Overlaid ECDFs** | Scales to ~5–8 series before overplotting; stochastic dominance is visible directly. |
| "What happens in the tails?" | **Letter-value (boxen) plot** | Hofmann, Wickham & Kafadar, "Letter-value plots: boxplots for large data" (JCGS, 2017): at large n the 1.5×IQR fence flags thousands of "outliers"; LV depths keep tail resolution honest. Already the repo's position (`letterval` rejects the fence). |
| "Is it multimodal / discrete / rounded?" | **Histogram** (rule-stated bins) | ECDF shows modality as slope changes but reads harder; histograms are the familiar form — with binning stated. |
| "How sure are we?" | Bands, n, estimator label | §2.2. Quantile dotplots are a candidate later addition (§8). |

The classical boxplot is deliberately absent: at the data sizes ClickHouse
serves, its outlier rule degenerates (HWK 2017), and the 5-number summary it
draws is a strict subset of both the ECDF and the LV plot. The repo already
took this position in-process (`distsummary` offers ECDF + boxenplot, no
boxplot).

Two-sample formality: ClickHouse can compute Kolmogorov–Smirnov,
Mann–Whitney U, and Welch/Student t directly (§4.4). These are affordances
for *labelling* an observed contrast, with the usual multiple-comparison
caveat when a panel invites eyeballing many pairs.

### 2.4 Where the workflow is heading (and what a look licenses)

Tier (c). Descriptive-statistics visualization has not been superseded as a
substrate; the workflow around it changed in two directions — automation
below it (what deserves a look) and guarantees above it (what a look
entitles you to conclude). The chart's modern role is the surface where an
automated distributional claim gets audited by a person.

- **Continuous profiling and drift detection.** Distribution summaries
  computed on a schedule, diffed against baselines, with humans shown a
  ranked list of changes (the ML-monitoring tool families around whylogs /
  Evidently; data contracts). The distribution view is entered from a flag.
  For boxer the flag source is ADR-0150's detectors.
- **Visual inference / lineups.** Buja, Cook, Hofmann, Lawrence, Lee,
  Swayne & Wickham, "Statistical inference for exploratory data analysis
  and model diagnostics" (Phil. Trans. R. Soc. A, 2009); Wickham, Cook,
  Hofmann & Buja, "Graphical inference for infovis" (IEEE TVCG, 2010);
  validation in Majumder, Hofmann & Cook (JASA, 2013). The observed plot is
  hidden among K−1 null plots (group labels permuted, or data simulated
  under H0); an adjudicator who picks the real one provides evidence
  against H0 at p = 1/K, and m independent adjudicators combine binomially.
  This makes "it looks different" defensible, supports four-eyes review,
  and its decision base — the K underlying summaries, the permutation
  seeds, the picks — archives as plain data. (Same Hofmann as the
  letter-value plots.) The dialogue marked the approach as wanted.
- **Anytime-valid inference.** Confidence sequences and e-values (Howard,
  Ramdas, McAuliffe & Sekhon, "Time-uniform, nonparametric, nonasymptotic
  confidence sequences", Annals of Statistics, 2021; Howard & Ramdas on
  sequential quantile estimation, Bernoulli, 2022). A fixed-n band that is
  watched continuously and acted on at crossing time does not hold its
  stated error rate; time-uniform bands do, at an iterated-logarithm
  widening — the honest price of watching. The dialogue directed adding
  time as a dimension to `ecdfbands` (§10).
- **Distribution-free prediction.** For a raw next observation the interval
  is order statistics at adjusted ranks — classically Wilks (1941)
  nonparametric tolerance intervals; conformal prediction generalises the
  idea via score functions. Finite-sample coverage holds under
  exchangeability, which is exactly what a drift detector monitors — so
  the two compose: the interval is honest *while the detector stays
  quiet*. Directed as wanted in the dialogue.
- **Effect sizes over p-values.** Shift functions — quantile-by-quantile
  differences with uncertainty (Doksum 1974; Rousselet, Pernet & Wilcox,
  "Beyond differences in means", Eur. J. Neuroscience, 2017) — readable
  directly from the same quantile grids this survey already proposes
  (§6.4).

## 3. What the repository already has (tier a)

### 3.1 Widgets

- **`widgets/ecdf`** — ECDF + simultaneous band renderer over implot.
  Critically, `Renderer.RenderGrid(p, xs, fnAt, n)` takes an explicit grid:
  x-positions, F-values, and the *true* count the band is calibrated at. A
  t-digest is **not** required; `widgets/ecdfdigest` is a thin bridge that
  builds such a grid from one, and says so in its package doc ("callers that
  build the grid from a different sketch … can compose the equivalent
  themselves"). Crosshair + verbose/terse status-line readouts exist.
- **`widgets/boxenplot`** — letter-value plot renderer, fed a ladder of
  `letterval.LVLevel`s.
- **`widgets/distsummary`** — the two-level anchor + inspector combining the
  above for in-process digests; its EXPLANATION records the design ethos the
  panel should inherit (band provenance made visible; conservative preview
  band immediately, exact band as a cancellable background job).
- **`widgets/implot`** — full plotting substrate post-ADR-0149 (bars, shaded,
  custom-item lane for variable-width rectangles, legends, crosshair
  registers). Qualitative series colours per ADR-0156.

### 3.2 Statistics library

- **`public/analytics/stats/ecdfbands`** — Berk–Jones (default), DKW–Massart,
  equal-precision, higher-criticism bands; exact Moscovich–Nadler inversion
  with a cache keyed by (n, α, method).
- **`public/analytics/stats/letterval`** — LV ladders from any
  `QuantileOracle` (`Quantile(q)`, `CDF(x)`, `Count()`). The package doc
  already names the missing piece: "Implementations include
  stats/tdigest.TDigest, exact sort-backed tables, and **ClickHouse pushdown
  wrappers**" — the wrapper is what this survey's panel would provide.
  `RecommendedDepth(n)` (Hofmann's ≈⌊log₂(n/8)⌋) bounds trustworthy depth.
- **`public/analytics/stats`** — `StreamStats` moments, convergence detector,
  t-digest.

### 3.3 play integration seams

- **Dock tabs with shape contracts.** Body-zone tabs observe the active Arrow
  result; `world` and `kanban` declare `shapeContract: true` and claim a
  result when their *named columns* are present. ADR-0122 §SD1 is explicit:
  "named columns rather than detection" — the pane asks for an `AS` per
  query instead of guessing. A distribution tab follows the same pattern.
- **Arrow list columns are already handled** — `*array.List` cases exist in
  `play_table_attr.go`, `play_format.go`, `play_detail_timeline.go`,
  `play_schema_infer.go`. A contract using `Array(Float64)` columns needs no
  new plumbing at the transport layer.
- **Pass pipeline.** `passreg` `StagePreExecute` hosts `CanonicalizeFull` at
  order 50 (statement-level rewriting exists and runs first); the `LW_ID_*`
  macro family (`public/identity/identsql`) is expression-level macro
  expansion prior art: registered names, arity checking with loud errors,
  fixpoint iteration. grammar1 parses parametric aggregates
  (`nanopass/testdata/corpus/034_parametric_aggregate.sql`), so
  ClickHouse-style `f(params)(args)` spellings are inside the existing
  grammar.

The load-bearing observation: **nothing in the widget stack requires raw data
or a t-digest.** ECDF wants (xs, F-values, n); boxenplot wants quantiles at
dyadic depths plus n. Both are exactly what a ClickHouse aggregate query can
return.

## 4. What ClickHouse offers (tier b, verified on 26.7.1.1315)

### 4.1 Quantile families

One parameterised call returns an array of quantiles per group:
`quantilesExactInclusive(0.25, 0.5, 0.75)(x)` → `Array(Float64)`.

| Family | Error model | Notes |
| --- | --- | --- |
| `quantilesExact` / `ExactLow` / `ExactHigh` | exact order statistics | O(n) memory per aggregation state — fine to serious n on one node, but unbounded by design. |
| `quantilesExactInclusive` / `ExactExclusive` | exact, interpolated | Verified on x=[1,2,3,4], p=0.25: Inclusive → 1.75 (= Hyndman–Fan **type 7**, R default / Excel `PERCENTILE.INC`); Exclusive → 1.25 (= **type 6**, Excel `.EXC`); plain `quantileExact`, `ExactLow`, `ExactHigh` → 2 (nearest-rank conventions). The panel can therefore name its convention precisely. |
| `quantiles` (default) | reservoir sample, no error model | Upstream documents result nondeterminism under parallel merge. A local 3-run probe on `clickhouse-local` was stable (single pipeline; the probe cannot exercise multi-part merges), so treat the upstream statement as governing. Unquantified error + run-to-run instability makes this the *wrong* default for a panel that re-executes. |
| `quantilesTDigest` | no worst-case bound; strong practice | Mirrors the in-process choice; state per group is bounded. |
| `quantilesGK(accuracy, p…)` | deterministic **rank** error | Verified spelling `quantilesGK(1000, 0.25, 0.5, 0.75)(x)`. Rank error composes with ECDF bands (§6.3). |
| `quantilesDD(rel_acc, p…)` | **relative value** error | Verified `quantilesDD(0.01, …)`; observed error ≪ bound. Value-axis honesty; positive-skewed value domains. |
| `quantilesBFloat16`, `quantilesTiming`, `quantilesDeterministic`, `quantilesPrometheusHistogram` | domain-specific | Noted for completeness; not candidates for the default. |

### 4.2 Histograms

- `histogram(M)(x)` → `Array(Tuple(lo Float64, hi Float64, weight Float64))`.
  Verified: bins are **variable-width** and weights are **fractional**
  (e.g. 212.5) — it is a streaming, adaptive, mass-redistributing summary
  (in the spirit of Ben-Haim & Tom-Tov's streaming histogram, JMLR 2010;
  attribution is an inference, not verified against the source). Two display
  consequences: bars must be drawn **density-normalised** (height =
  weight/width) or variable widths mislead; and the panel should label the
  histogram as a streaming approximation.
- Fixed-width alternative: `widthBucket(x, lo, hi, M)` exists (verified).
  A Freedman–Diaconis width needs IQR and n first — a two-stage query
  (scalar subquery for the ungrouped case; heavier per group). Costed as a
  follow-up, not v1 (§8).

### 4.3 Moments and support

`count()` vs `count(x)` (nulls skipped by aggregates → null count is the
difference), `min`/`max`, `avg`, `stddevSamp` (+`Stable` variants),
`skewSamp`, `kurtPop`/`kurtSamp`. Skewness/kurtosis are non-robust — show
them with n, never alone. No medcouple (robust skew) server-side. `entropy`
and `uniq*` exist; a low `uniq` is a useful "this column is discrete —
an ECDF/histogram of 12 distinct values wants a bar chart" hint.

### 4.4 Two-sample and k-group tests

`kolmogorovSmirnovTest('two-sided')(value, group)` → `(D, p)` (verified:
`(0.02, 0.27)` on a null case); `mannWhitneyUTest`, `welchTTest`,
`studentTTest`(+`OneSample`), `meanZTest`. All take the value column plus a
two-valued group column — usable for a "compare these two series" affordance
without new math client-side.

For k groups, `analysisOfVariance(value, group_index)` (alias `anova`)
exists — verified: `(F, p)` ≈ `(62.2, 0)` on three shifted synthetic
groups. It is one-way and fixed-effects, with the usual normality and
homoscedasticity sensitivity, and there is no Welch variant. There is no
Kruskal–Wallis (verified absent); the rank-transform route — window-function
ranks, then ANOVA on the ranks (Conover & Iman, 1981) — is the in-SQL
approximation if a robust omnibus test is wanted.

Separately useful for §2.4's lineups: `arrayShuffle(arr, seed)` is
deterministic for a fixed seed (verified) — an in-SQL permutation-null
generator whose seed can be archived, so a lineup's null panels re-derive
exactly.

### 4.5 Determinism summary

Safe for a re-executing panel: `Exact*` (order-independent), `GK`
(deterministic bound), `DD`. Practically stable but unbounded: `TDigest`
(merge-order sensitivity exists in principle). Documented-unstable:
plain `quantile`/`median`. The estimator name should ride along in the
result (§6.2) so the panel can label what it shows.

## 5. Decision 1 — the SQL surface

**O1. Macro in the select list** (the prompt's sketch):

```sql
SELECT descriptiveStatistics(duration_ms, payload_bytes)
FROM runs WHERE day >= yesterday() GROUP BY service
```

A `StagePreExecute` nanopass pass detects the call and rewrites the
*statement* into the contract query of §6 (one output row per column ×
group). This parses today as a plain function call — no grammar change. It
is a statement-level rewrite, which is more than `LW_ID_*` does
(expression→expression) but less than `CanonicalizeFull` (whole-statement,
already at order 50). An estimator knob fits either as a leading string
argument (`descriptiveStatistics('gk', a, b)`) or the parametric spelling
(`descriptiveStatistics('gk')(a, b)`); both parse (§3.3), the plain-argument
form reuses the `LW_ID` CST handling as-is.

**O2. Contract only, no macro.** Publish §6's named-column contract; users
(or snippets, or an ADR-0132 applet) write the expansion by hand. The kanban
precedent says the contract is the real interface regardless — anything the
macro can emit, a hand query can too, and the panel cannot tell the
difference.

**O3. Table-function style** — `FROM descriptiveStatistics(…)`. Killed:
touches the FROM grammar surface, no prior art in the pass stack, and the
subquery plumbing (what relation does it scan?) reintroduces everything O1
already solves inside a familiar shape.

**O4. Panel-authored queries** (panel inspects the user's FROM/WHERE and
issues its own follow-ups). Killed: play panels observe results on the main
channel; queries leave through one dispatch seam (ADR-0141). Guessing at
user intent is the thing ADR-0122 §SD1 rejected.

**Lean: O2 + O1.** The contract must exist first and is independently
useful; the macro is sugar that emits it. Registering the pass beside
`ExpandLwIdMacros` (keelson passreg defaults) makes it reach play and
sqlapplets alike, ordered after canonicalize (canonical shapes in;
machine-generated output need not re-canonicalise — same recorded
consequence as the existing macro family).

Grouping and multi-column compose: each GROUP BY key row × each argument
column yields one output row; `series` is the argument column's name,
suffixed with the group key values when a GROUP BY is present.

The comparison-first redirection (§1.1) suggests a sibling spelling — a
baseline-marking argument, or a second macro for window-vs-baseline — whose
shape is an open question (§9).

## 6. Decision 2 — the result-shape contract

### 6.1 Shape options

- **O-A. Long form** — `(series, p, q)` rows. No arrays, but scalar stats
  (n, mean, …) then need a second shape or NULL-padded rows; k×series rows
  ask the panel to reassemble; the Table-tab fallback view is noise.
- **O-B. Wide form** — one row per series, `Array(Float64)` columns for the
  grid. Matches what `quantiles*` natively returns; play's Arrow path
  already folds list columns (§3.3); Table fallback is one legible row per
  series.
- **O-C. Split-graph side channel** — a named CTE node like kanban's
  `lanes`. More machinery than v1 needs; nothing here requires a second
  channel.

**Lean: O-B.** Sketch (names are open, §9):

| column | type | required to claim |
| --- | --- | --- |
| `series` | String | yes |
| `n` | UInt64 | yes |
| `ps` | Array(Float64) — the fixed probability grid | yes |
| `qs` | Array(Float64) — `Q(p)` per grid point | yes |
| `n_null` | UInt64 | no |
| `x_min`, `x_max`, `mean`, `sd`, `skew`, `kurt` | Float64 | no |
| `hist_lo`, `hist_hi`, `hist_w` | Array(Float64) | no (all three or none) |
| `estimator` | String — e.g. `'tdigest'`, `'gk:1000'`, `'exact-hf7'` | no (default assumed approximate) |

Claiming mirrors kanban's required/optional split, with loud reject messages
for near-misses (e.g. `ps`/`qs` length mismatch). No `@`-style tokens are
needed — the contract is fixed names, no aliases, so the `#`-comment and
leeway `section:column` collision lessons (ADR-0122 §SD2, ADR-0116) are
avoided by construction.

### 6.2 The fixed probability grid

The macro cannot know n at expansion time, so it emits a generous fixed
grid and the *panel* decides what is trustworthy:

- dyadic letter-value ladder: 2⁻ᵏ and 1−2⁻ᵏ for k = 1…16 (31 points —
  exactly what `letterval.Levels` wants);
- a uniform body grid (1/64 steps, 63 points) for the ECDF curve;
- extreme tail points (10⁻³, 10⁻⁴ and mirrors).

Deduplicated ≈ 90 levels — one `quantiles*` call. The panel clamps rendered
LV depth to `letterval.RecommendedDepth(n)` after seeing n, resolving the
"macro can't know n" tension without a second round trip.

### 6.3 Why this feeds every widget honestly

- **ECDF**: `(xs=qs, fnAt=ps, n)` is *precisely* `ecdf.RenderGrid`'s
  signature. With an `Exact*` estimator the points lie on the true empirical
  CDF (a subsample of its vertices; a simultaneous band evaluated on a
  subset of x-positions retains its coverage guarantee — it only gets
  conservative). With GK, the ε rank error is an additive band inflation in
  F-space — a principled widening, since both live in probability space.
  With t-digest, the curve is labelled approximate via `estimator`.
- **Boxenplot**: a small `QuantileOracle` over (ps, qs, n) — monotone
  interpolation for `Quantile`, inverse for `CDF` — is the "ClickHouse
  pushdown wrapper" `letterval`'s doc anticipated.
- **Histogram**: `hist_*` triplets drawn density-normalised via implot
  (Boxes or the custom-item lane both draw variable-width rectangles).
- The band is calibrated at true `n` from `count()`, matching the
  band-provenance ethos `distsummary` already established (BandN vs SampleN).

### 6.4 Comparison feeds from the same grids

Two series sharing the fixed p-grid give the comparison layer without any
new transport:

- **Shift function**: Δ(p) = Q_B(p) − Q_A(p) against p — the quantile-wise
  effect size (§2.4). A conservative simultaneous band comes from combining
  the two per-series bands at α/2 each; bootstrap tightening is a later
  refinement.
- **Wasserstein-1 distance**: W₁ = ∫₀¹ |Q_A(p) − Q_B(p)| dp — a trapezoid
  over the shared grid. A natural scalar drift feature for role (a).
- **PSI-style stability features**: bins at the *baseline's* quantiles make
  expected mass uniform by construction, so population-stability indices
  reduce to arithmetic on the grid.
- Omnibus labels from §4.4 (KS, MWU, ANOVA), with false-discovery framing
  once many series are scanned at once.

How a baseline is marked in the contract (a `series_baseline` flag column,
a naming convention, or panel-side selection) is open (§9).

## 7. Decision 3 — panel composition

The dialogue inverted the original composition: **comparison is the primary
view** — a window-vs-baseline or A/B entry with overlaid ECDFs, the shift
function, and paired boxen — and the single-series portrait is the
degenerate case, not the default. The roster reads with that inversion:

- A body-zone tab `{id: "dist", lazy, shapeContract}` beside Table / World /
  Kanban, claiming on §6's required columns, rejecting loudly on
  near-misses.
- Sub-views over implot, one claim: **ECDF** (overlaid series + bands),
  **Shift** (Δ(p) with its conservative band, shown when ≥2 series),
  **Boxen** (one LV column per series, side by side), **Histogram**
  (density-normalised). Crosshair readouts reuse the existing
  `WriteStatusLine`/`Verbose` registers.
- The honesty line is always visible: n, null count, estimator, band method
  and α — the panel's analogue of the `distsummary` provenance chip.
- Series colours from the ADR-0156 qualitative roster, with its wrap
  policy; band fill only where it stays legible (many overlaid bands
  occlude — an open question, §9).
- Degenerate inputs are decisions, not accidents: n=0 (render "empty", don't
  reject); support collapse min=max (label, skip plots); low-`uniq`
  discreteness (render, hint that a GROUP BY bar chart may serve better).

## 8. Descoped (recorded, not gating)

- **Violin/KDE views** — bandwidth honesty (§2.1); revisit only as opt-in.
- **Quantile dotplots** — cheap later addition (derivable from ps/qs alone),
  strong uncertainty-communication literature (§2.2).
- **Freedman–Diaconis fixed-width histograms** — needs the two-stage query
  (§4.2); the adaptive `histogram(M)` with honest labelling ships first.
- **All-pairs test matrices** — comparison ships as baseline-vs-window /
  selected-pair first (§7); the full matrix and its multiple-comparison UI
  stay out until false-discovery framing is designed.
- **Time-faceted / ridgeline distributions** — adjacent to ADR-0150's
  windowing work; separate dialogue.
- **Raw-sample small-n mode** (ship rows, panel sorts, exact ECDF via
  `Render(sorted)`) — a second contract; the grid path is already exact
  when the estimator is exact.
- **`distsummary` anchors inside Table cells** (per-cell drill-in) — wants
  per-column digests on a different channel; separate design.
- **Streaming/progressive results** — waits on ADR-0143 (E8).

## 9. Open questions for the design dialogue

Resolved 2026-08-02: the roles framing (§1.1); comparison-first composition
(§7); tests surface in v1 as comparison labels; time-uniform bands,
conformal readout, and the lineup protocol are wanted (tracks in §10); the
agent surface is a strategic goal. Still open:

1. Macro name and spelling — `descriptiveStatistics` is long;
   `distSummary`? Leading-string-arg vs parametric estimator knob? And the
   comparison sibling's shape (§5).
2. Estimator default — `tdigest` (practice, bounded state) vs `gk`
   (formal rank bound that composes with the bands). This page leans
   tdigest-by-default with `'exact'`/`'gk'`/`'dd'` opt-ins, but the formal
   composability argument for GK is real.
3. Contract column names — bare (`series`, `n`, `ps`) vs prefixed
   (`dist_series`, …); and how a baseline series is marked (§6.4).
4. Multi-series band policy — all bands, focused-series-only, or ≤k series.
5. Histogram default M, and whether `histogram()` ships in v1 at all.
6. Where the expansion pass registers — keelson passreg defaults (reaches
   applets) vs play-only to start.
7. Claim exclusivity — does Dist claiming a result affect Table's rendering
   of the same frame? (World/kanban precedent says tabs are independent;
   verify at implementation time.)
8. Lineup protocol specifics — null-generator query shape (seeded
   `arrayShuffle` label permutation, §4.4), K, blinding when an *agent*
   prepares the lineup, and how picks persist as facts.
9. Time-uniform band family — which boundary (the Howard–Ramdas line has
   several) lands in `ecdfbands` first, and how the panel decides
   static-snapshot vs monitored-stream mode.
10. Whether rank-transform ANOVA (§4.4) is worth wrapping as a macro, given
    Kruskal–Wallis is absent server-side.

## 10. Suggested path

> **Pointer.** The panel + contract + macro scope was decided by
> [ADR-0161](../adr/0161-play-distribution-panel.md) (accepted 2026-08-02);
> that record is authoritative and this page is its background snapshot.
> The four directed tracks below remain un-gated by it.

Dialogue round 1 (2026-08-02) settled the roles and the comparison-first
redirection; the remaining §9 questions gate the ADR (a new public SQL
surface + a new pass + a new panel is comfortably ADR-tier). Suggested cut:

- **M0** — contract + panel with the comparison core (overlaid ECDF, shift
  function, paired boxen) fed by hand-written SQL (O2). Proves the wire and
  the widgets; no grammar or pass work.
- **M1** — the macro pass (O1), including the comparison spelling.
- **M2** — histogram + degenerate-case affordances.

Parallel directed tracks, each meriting its own dialogue before code:

- **Time as a dimension in `ecdfbands`** — a time-uniform band family for
  monitored streams, labelled via the existing band-provenance machinery.
- **Conformal / tolerance readout** — interval overlay + readout line
  derived from the same grid, stated with its exchangeability caveat and
  composed with drift detection (honest while the detector is quiet).
- **Lineup protocol** — seeded in-SQL nulls, blind adjudication UI,
  picks-as-facts (four-eyes capable, archivable; §2.4, §9.8).
- **Agent surface** — the macro vocabulary as tool-calls (strategic; rides
  ADR-0120 Ask / ADR-0139 text2dsl rather than this panel).
