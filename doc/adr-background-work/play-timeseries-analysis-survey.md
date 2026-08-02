---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# Timeseries analysis in play — a design-space survey

> **Provenance.** Compiled 2026-08-02, ahead of any decision: nothing in here
> is settled, and an ADR — not this page — is where any of it would become
> one. Provenance is three-tiered and marked throughout: (a) claims about
> this repository were verified against the working tree on the compile date;
> (b) claims about comparator products (Grafana, Prometheus) come from
> general product knowledge and were **not** re-verified against current
> releases — treat versions and feature boundaries as approximate; (c) the
> algorithm literature is covered by the existing
> [motif and anomaly survey](../explanation/timeseries-motif-anomaly-survey.md)
> and is only pointed at here. No new algorithm was run for this survey.
> Outcomes of the design dialogue of 2026-08-02 are recorded inline as
> settled direction — decisions, not measurements.

## 1. The question

ADR-0150 and ADR-0152 built a timeseries substrate —
smoothing, subsequence anomaly detection, the matrix profile that also
carries motifs — and none of it is reachable from `play`. The ask is to
expose **smoothing, anomaly detection and motif mining** as play
capabilities, with three qualities named up front: scientifically honest,
expressive enough to compose, and designed as one surface rather than three
bolt-ons. The comparator is the core of Grafana: query → time-series chart →
transformations → anomaly flags.

**Framing (dialogue outcome, 2026-08-02 — settled direction).** Comparator
parity is the floor, not the frame. The exposure is equally three things the
comparator is not:

- **an algorithm-evaluation workbench** — the M2 scorer, the baselines and
  the backtest are product surface, not CI plumbing; play is where a
  detector's claim gets examined on this host's data, against the
  one-liners. The loadstudy discipline, made interactive.
- **a data-quality assessment surface** — the S5 validators are findings,
  not error handling: a series failing a detector's preconditions (gaps,
  jitter, silent fill) is itself a result about the data, and the
  anomaly/motif views double as data-auditing lenses.
- **an education tool** — the profile-under-series view, the fixture
  generator's known-ground-truth series and the honesty chrome teach what
  these methods can and cannot claim, which is a capability in its own
  right given the field's benchmark history.

This framing promotes the §4 honesty layer from chrome to product, and it
raises the backtest readout and a fixture "lab" (§5.6) in priority relative
to pure charting.

Four sub-questions are separable, and the first is prior to the rest:

1. **The carrier.** play has no numeric series-over-time chart panel at all
   (§2.2). Timeline renders discrete events and spans; Projection is a UMAP
   scatter; the distribution, sankey and icicle panels each render their own
   shape. Nothing plots `SELECT t, v ORDER BY t` as a line.
2. **Substrate and spelling.** Where each computation runs (ClickHouse SQL,
   client-side Go, server-side executable UDF) and how it is invoked (panel
   controls, contract columns, a macro vocabulary, graph nodes). This is the
   malleability axis, and the decision with the longest consequences (§5.2).
3. **Output shapes.** What a detector or motif miner *returns*, such that the
   rest of play can consume it as ordinary data — spans, score series,
   tables — rather than as pixels inside one panel (§5.3).
4. **The honesty layer.** What the ADR-0150 evaluation record obliges the UI
   to say, mark, and refuse (§4). This is where "scientifically right"
   becomes concrete, and it is the part no comparator ships.

## 2. What exists — tier (a), verified against the working tree

### 2.1 The substrate and its sharp edges

Five packages under `public/analytics/timeseries/`, all
WASM-freestanding, no module dependencies beyond the existing tree:

| Package | State | Facts a UI must respect |
| --- | --- | --- |
| `mssmooth` (ADR-0152) | shipped; two app consumers | Zero-phase, so non-causal: the last `m` samples of a live series rest on boundary extrapolation. Degree is fixed at 4 by evidence; only half-width is a user knob. |
| `matrixprofile` (M1) | shipped | Batch, univariate. `Profile.Motif()` / `Profile.Discord()` are **top-1 readers only** — no top-k, no occurrence collection. Soundness unconditional; optimality can fail on scale-mixing series. ~500k-sample practical ceiling. |
| `adscore` (M2) | shipped | VUS-ROC's usable band is ≈[0.55, 0.92], not [0,1]; VUS-PR is the less distorted measure. Ships the one-liner baselines and the triviality check. |
| `damp` (M3) | shipped | Left discords = the causal variant. Default mode produces **no usable score vector** (upper bounds, quietly); `Config.Exact` does, at ~3× cost. Scores attribute at the window **centre**; extent is the plateau, not the argmax. Needs a longer warm-up on real data than fixtures suggest. |
| `loadstudy` | shipped (integration lane) | On real host load scored against event proximity, the detector does **not** reliably beat the one-liners. Preferred window ≈ 5–16 min wall-clock. Events are not anomaly labels — that gap is the finding. |

Milestones M4 (subdimensional/multivariate) and M5 (Half-Space Trees) are
unbuilt. Motif **set** discovery (k-Motiflets and kin) was deliberately
deferred by ADR-0150 to a follow-up ADR; the substrate carries it, nothing
reads it.

Two numerical disciplines from the ADR-0150 record recur below because they
are UI-visible: window scores belong at the window centre (measured: half the
achievable accuracy), and the window must track the signal's period, not the
expected anomaly length.

### 2.2 The play seams

**The graph runtime is executor-agnostic.** `nodeExecutorI` is a single
method (`apps/play/play_graph.go`), `newNodeLane(exec, alloc, timeout)`
takes the executor per lane, and nine call sites already inject one — the
map raster, kanban, sankey, network, flow, docs and diagnostics panels all
run panel-authored lanes today. A lane brings async execution, supersession,
memoisation on a `(SQL, params)` key, and last-good-while-recomputing for
free. `nodeResult` already carries a content fingerprint and a revision, so
"recompute when the input record actually changed" is the machinery's normal
case, not an extension.

**Panel-authored nodes are precedented.** The Timeline bands node and the
Map raster template node are graph nodes whose SQL is owned by a panel, with
parameters riding SD8 signals (`tl_min`/`tl_max`, `vp_*`). A node whose
*compute* is owned by a panel is one step further on the same path.

**Contracts are named columns.** ADR-0122 §SD1 set the doctrine; ADR-0161
(distribution panel) is the freshest instance: required columns
`series/n/ps/qs`, schema-only acceptance, loud rejection at fold time, a
macro (`descriptiveStatistics`) as sugar over the contract, and a shared
package (`distsql`) holding names, grid and validator. The Timeline span
contract — `_tl_band_from/to/color/label` — is already exactly the shape an
anomaly span or motif occurrence has. **Span-shaped detector output renders
in play today with zero new widget work.**

**The plotting substrate is ready.** The implot port (ADR-0149) shipped its
custom-item lane, and its adoption survey's lane-chart demo — time x-axis,
pinned lanes, band-under, flag markers, callout-over — is the rendering
vocabulary a score-under-series view needs. That survey names the ADR-0150
loadstudy UI as an anticipated consumer. The pending P3 (timeline-on-frame)
is adjacent, not a prerequisite.

**The display-smoothing consumer pattern exists.** `trendsmooth`
(`public/thestack/imzero2/egui2/widgets/trendsmooth`) wraps `mssmooth` for
the monitoring apps with three commitments worth importing verbatim: the raw
series stays visible under the smoothed curve, the filter-design knob is not
exposed, and errors fall back to raw.

**The counter-precedent.** The Projection panel computes UMAP client-side
with parameters that live only in panel state — not recorded, not
replayable, not addressable by an agent. It predates the three-roles framing
(§2.3) and is the shape this design should *not* multiply.

**One absence worth naming:** play has no timer-driven refresh. The Live
toggle re-runs on *signal* change only. Whatever "streaming" means for this
exposure, it does not currently mean play re-querying on an interval (§5.7).

### 2.3 The strategic frame is already settled

The distribution-panel dialogue fixed play's three roles, and this exposure
should be designed against them rather than re-deriving them:

- **Feature foundry** — analyses developed interactively graduate to
  scheduled re-execution; the recorded artifact is SQL; outputs persist as
  facts.
- **Human-on-the-loop viewer** — and *detector adjudication* is literally
  the named example of this role.
- **Agentic surface** — macro vocabulary doubles as agent tool-calls; the
  panel is the human verification artifact.

The same dialogue directed tracks that border this design: anytime-valid
bands for continuous watching (Howard–Ramdas confidence sequences), a
conformal/tolerance readout, and the lineup protocol. Anomaly flagging under
continuous observation is exactly the problem those tracks exist for (§4,
S4).

## 3. The comparator — tier (b), product knowledge, not re-verified

What "the core of Grafana" concretely is: the time-series panel; a per-panel
**transformations** pipeline (client-side dataframe operations — reduce,
join, window); **alerting** as reduce + threshold + for-duration over a
query, with notification policies; and, behind the cloud subscription,
**Grafana ML** — forecasting (Holt-Winters/Prophet-class), outlier detection
(k-sigma, MAD, DBSCAN-flavoured), adaptive alerting — plus Sift for guided
investigation. Prometheus, the other half of that ecosystem, takes the
opposite stance: analysis functions live *in the query language* (`deriv`,
`predict_linear`, `double_exponential_smoothing` — the renamed
`holt_winters`).

Three comparator lessons this design should take seriously, none of them
flattering to copy:

1. **Per-panel transformation pipelines do not compose or record.** Grafana's
   transformations are a flat list private to one panel — invisible to other
   panels, not versioned as a query, awkward past three steps. The concession
   is that Grafana later added SQL-over-dataframes ("SQL expressions") on
   top. play starts where the comparator arrived: the query *is* the
   artifact, and the reactive graph already gives what their pipeline
   cannot — shared intermediate results, signals, per-node observation.
2. **Uncalibrated thresholds are the alerting model.** A user types a number
   against an unnormalised score and nothing in the product says what
   false-alarm behaviour that buys. The ADR-0150 record (VUS bands, baseline
   one-liners, the loadstudy verdict) is precisely the discipline that
   product class lacks; carrying it into the UI is a differentiator that
   costs mostly honesty, not code.
3. **The anomaly features are a cloud service with unpublished evaluation.**
   Given the field's benchmark pathology (Wu & Keogh), an unevaluated
   detector behind an API is the illusion-of-progress failure mode,
   productised. A local, inspectable, baseline-accompanied detector is the
   opposite posture and fits the sovereign premise.

What play already has that the comparator does not: one recorded SQL
artifact per analysis, a reactive signal system, facts persistence for
outputs, an in-repo scorer with flaw-avoiding fixtures, and — in
`adscore.BaselineScores` — the institutional habit of showing the one-liner
beside the detector.

### 3.1 Smoothing, specifically — the comparator's four layers vs ADR-0152

Added at the user's direction (dialogue 2026-08-02). Exception to this
section's tier-(b) disclaimer: the mechanism names and options below were
checked against the current Grafana documentation on the compile date; the
filter-theoretic characterisations are standard signal-processing facts.

Three different jobs hide under the word "smoothing" — **data reduction**
(fit millions of samples through a wire and a few thousand pixels),
**noise filtering** (attenuate what obscures the signal), and **line
aesthetics** — and Grafana's four mechanisms split across them at four
layers:

| Layer | Mechanism | Filter character |
| --- | --- | --- |
| query time | interval bucketing (`$__interval` avg/max/min); datasource functions (`avg_over_time`, `moving_average`, `exponential_moving_average`) | decimation plus trailing boxcar or single-pole EWMA; solves data *volume*, only incidentally noise |
| transformation | *Window functions*: moving mean/stddev/variance, **trailing or centered** | uniform boxcar FIR; centered is zero-phase, trailing lags by half the window |
| transformation | *Smoothing*: the ASAP algorithm, one *Resolution* knob (1–1000) | still a boxcar — ASAP automates the **window length**, not the kernel |
| panel render | *Smooth* line interpolation | a spline through the unmodified samples; removes no noise, can invent extrema between points |

Filter-theoretically, everything upstream of the renderer is a boxcar or a
single-pole EWMA. The boxcar is the weakest low-pass in common use: its
frequency response is a sinc — first sidelobe about −13 dB, 1/f rolloff,
and spectral nulls with phase inversion at multiples of the window rate —
so noise above the cutoff substantially survives and narrow peaks distort.
This is the same defect class the ADR-0152 paper quantifies for
Savitzky–Golay (−11 to −13 dB sidelobes), and the boxcar is the worse
member of it (Savitzky–Golay at least keeps a flat passband). The EWMA is
causal but has frequency-dependent lag — different components of a
waveform are delayed by different amounts — and no zero-phase form. The
spline mode is not a filter at all: every sample passes through unchanged,
curvature is invented between them, and noise is beautified rather than
removed.

The causality trade underneath is an exclusion, not an engineering choice:
a causal filter with exactly zero phase would need an impulse response
symmetric about zero yet zero for all negative time — a pure gain, no
smoothing. Every smoother picks a side. Causal ones pay in **delay**: the
trailing boxcar displaces every feature by (N−1)/2 samples; the EWMA
delays different frequency components by different amounts, distorting
shape as well as position. Zero-phase ones pay in **lookahead**: MS's last
`m` samples rest on the weighted-linear tail extrapolation and are revised
as real samples arrive, so the newest stretch of a live chart — the part a
viewer watches — is exactly where the smooth is provisional, which is what
S1's live-edge marking is for. This is the same axiom as left discords:
looking back may use both directions; anything live must be causal.

ASAP (Rong & Bailis, PVLDB 2017) is the one genuinely interesting entry,
and it is worth being precise about what it contributes: not a better
kernel but **automated bandwidth selection** — an autocorrelation-guided
search over window lengths that minimises roughness subject to preserving
kurtosis, the latter serving as a don't-hide-the-outliers guard.
"Parameter-free" is a UX fact, not a fidelity guarantee: accepting the
automation means accepting its objective — perceptual roughness for
at-a-glance reading — which promises nothing about peak heights or
shapes. Two of
its ideas independently reappear in this survey: window-from-ACF is S7,
and don't-let-smoothing-hide-anomalies is `trendsmooth`'s raw-underlay
commitment — solved there by display honesty rather than by capping the
filter. What ASAP leaves untouched is the kernel: the smoother it tunes
remains a boxcar with everything above.

Against this, the MS kernel (ADR-0152) is the complementary contribution —
a *designed* kernel: flat passband (peak heights preserved), first
sidelobe below −70 dB with 1/f⁴ decay, zero-phase, explicit boundary
treatment, analytic derivatives — with bandwidth left to the caller plus
mapping helpers. The honest trades run the other way too: the trailing
boxcar and the EWMA are causal and therefore live-safe, where MS's last
`m` samples rest on extrapolation (S1); ASAP needs no parameter at all
where `trendsmooth` still exposes a half-width; and interval bucketing
addresses data volume, which MS does not and this design places in the
SQL (S5). Two consequences for this design: the comparator's smoother
appears in it twice, both times as a reference point rather than the
product. First as an O2 window-function one-liner —

```sql
SELECT t, v,
       avg(v) OVER (ORDER BY t ROWS BETWEEN 5 PRECEDING AND CURRENT ROW) AS v_smooth
FROM series
```

— the comparator's Window-functions transformation as ordinary, recorded,
replayable SQL (`5 PRECEDING AND 5 FOLLOWING` for the centered, zero-phase
variant). Second as the moving-average residual among the `adscore`
baselines — one of the Wu-and-Keogh one-liners, the null hypothesis a
detector must beat (S3). And ASAP's bandwidth-selection idea is worth
adopting *on top of* the better kernel — an ACF-driven suggested
half-width for `trendsmooth`, sharing S7's machinery — rather than
alongside it.

### 3.2 Non-equidistant series, and where LTTB sits

Added at the user's direction (dialogue 2026-08-02). The comparator's
panel documentation was checked on the compile date; the ClickHouse probe
below ran against the live 26.7 server.

**The comparator's stance is implicit.** Rendering is time-true — samples
plot at their real positions — and gap handling is a display toggle: the
panel offers *Connect null values* (Never / Always / Threshold) and
*Disconnect values* (above a spacing threshold). The documentation expects
"unique timestamps" and says nothing else about spacing. The *analysis*
machinery, however, is index-based: the Window-functions moving mean
counts **rows**, so on irregular data an N-sample window has no fixed
duration, and ASAP likewise assumes regular sampling. Where regularity
exists it was manufactured upstream, per datasource — PromQL evaluates
range queries on a fixed `step` with a staleness lookback (resampling by
heuristic, applied silently), and SQL sources are offered
`$__timeGroup(col, interval, fill)` with null/previous/zero fill. The
equidistance question is thus dissolved into per-datasource conventions;
no layer surfaces it as a property with a policy.

**This design's treatment (S5 made concrete).** Three classes of series,
told apart by the Δt distribution the claim validator computes:

1. *Regular with jitter* — a sampler wobbling by a small fraction of its
   period. The validator declares the grid at the median Δt within a
   tolerance and the series is treated as equidistant; the approximation
   is explicit rather than assumed. The tolerance policy is an open detail
   for the ADR.
2. *Regular with gaps* — the loadstudy case. Segment at gaps, analyse per
   segment; fill only where the user spells it in SQL (`WITH FILL`,
   `INTERPOLATE`), visible and recorded.
3. *Genuinely irregular* — events, request logs. Aggregation onto a grid
   (a rate, a per-bucket quantile) is a modelling decision and is spelled
   in SQL; unaggregated, it is event data and belongs to the Timeline, not
   the analysis tier.

The chart renders all three time-true (the plot takes explicit x
positions); only the analysis tier gates on the grid, and the display
smoother declines off-grid and falls back to raw (the trendsmooth
commitment).

**LTTB is the data-reduction job done honestly.**
Largest-Triangle-Three-Buckets (Steinarsson, 2013) decimates for
*display*: one real sample kept per output bucket — the one maximising the
triangle area with the previously kept point and the next bucket's mean —
so spikes survive where an `avg` bucket erases them, and every plotted
point is a datum, not a fabrication. It is not in the comparator's core
product (it lives in storage layers such as TimescaleDB's `lttb()`
instead), but **ClickHouse ships it**: verified against the live 26.7
server on the compile date,

```sql
SELECT lttb(4)(t, v) FROM series  -- alias of largestTriangleThreeBuckets
```

returns an `Array(Tuple(t, v))` of selected real samples. Two properties
bound its role. Its output is non-equidistant *by construction* (the probe
returned bucket picks at 00:00, 04:26, 07:56 and 11:33 from a 7-second
grid), and its selection objective is visual-shape preservation, not
distributional or spectral fidelity — so **LTTB output must never feed
the analysis tier**. It is renderer-adjacent: the analysis path reads the
gridded series, the display path may decimate. Two legitimate seams
exist — SQL-side `lttb` for long-range exploration (volume stays off the
wire, S5's concern) and client-side per-viewport decimation before the
plot (the full series is client-side anyway for the Go analysis tier, and
zooming re-decimates without a re-query) — with a per-pixel min/max
envelope as the alternative decimation family (exact extremes, twice the
points). The seam is settled (Q9): the carrier decimates automatically via
the per-pixel min/max envelope, render-only; SQL-side `lttb` is the
documented option for huge-range viewing.

## 4. Scientific commitments — what "right" must mean in the UI

Each of these descends from a measured finding already in the repository;
the UI consequence is stated with it. These are survey-level candidates for
an ADR's Verification section, not decisions.

- **S1 — The causality split is a display rule.** Two-sided analyses (batch
  matrix profile; zero-phase smoothing) are legitimate for looking *back*;
  only causal analyses (left discords; causal filters) may be presented as
  "this would have fired". A backtest view must use `damp`, render its
  warm-up region as such, and never present batch-profile maxima as alert
  claims. Similarly the last `m` samples of a zero-phase smooth rest on
  boundary extrapolation and should be visually distinct at the live edge.
- **S2 — Plotted scores are exact-mode scores.** DAMP's default output looks
  like a score vector and is not one. Any score lane, threshold affordance
  or scorer hand-off uses `Config.Exact` (play is a batch context; the 3×
  cost is irrelevant next to honesty). Scores attribute at window centres;
  reported extents read the plateau, not the argmax.
- **S3 — Baselines render beside the detector, by default.** The one-liners
  are cheap everywhere (SQL window functions server-side, or
  `adscore.BaselineScores` client-side). loadstudy's `event_rate` channel —
  where a one-liner "wins" by reading the answer key — was caught only
  because baselines are always reported alongside. The UI inherits that
  rule, on by default.
- **S4 — A distance is not a probability.** Raw z-normalised distances
  support *ranking*, not calibrated flagging. Turning scores into flags
  honestly needs either labels (S6) or distribution-free calibration
  against a reference window — and under continuous watching, anytime-valid
  forms (confidence sequences, e-values) rather than fixed-n ones. That is
  the same mathematics the distribution-panel dialogue already directed for
  ecdfbands-over-time; it should be **one** track, not two. Deferrable, but
  the design should leave the seam: a score lane whose threshold line is
  labelled for what it is (a quantile of a reference window, not a
  guarantee).
- **S5 — The grid is a precondition, and interpolation is fabrication.**
  `matrixprofile`, `damp` and `mssmooth` assume equidistant samples; SQL
  results carry gaps and jitter. loadstudy's invariant — a span is usable
  only with zero forward-filled bins — exists because filled samples are
  plausible data the detector then scores. The honest behaviours are:
  validate Δt on claim; **segment at gaps and analyse per segment** (what
  loadstudy did); and when the user wants a grid, put the gridding *in the
  SQL* (`WITH FILL` / `INTERPOLATE`) where it is visible and recorded —
  never resample silently client-side. Display decimation is the one
  legitimate exception to grid discipline, and it is barred from the
  analysis tier for the same reason (§3.2).
- **S6 — Adjudication closes the labels gap.** loadstudy's verdict was not
  "detector bad" but "events are not labels, so this evaluation cannot
  demonstrate value". A minimal adjudication affordance — mark a flagged
  span confirmed/false, persist as facts — is simultaneously the
  human-on-the-loop role and the only path to scoring detectors on this
  host's own data. It also gives the lineup-protocol track its decision
  base.
- **S7 — Window defaults follow the signal, not the anomaly.** The single
  most accuracy-relevant parameter should default from the series' own
  structure — a cheap autocorrelation scan suggesting the dominant period
  (O(n·maxlag), dependency-free) — with the loadstudy prior (5–16 min
  wall-clock on 1 Hz host metrics) as the sanity band, and a sweep
  affordance rather than false confidence in one value.
- **S8 — Conditioning is a first-class composition step.** z-normalisation
  makes the profile trend-sensitive (survey RQ6), so detrend-then-profile
  (smooth → residual → profile) must be spellable as ordinary composition,
  not a hidden checkbox. Timestamps read back from ClickHouse are forced
  UTC (`toDateTime(x,'UTC')`) — the server-TZ shift has bitten twice.

## 5. Design dimensions

### 5.1 D1 — the carrier: a Series panel

Everything else renders through a numeric series-over-time chart, and play
does not have one. Options for how results claim it:

- **Type-directed**: a result with one temporal column plus ≥1 numeric
  column charts, first temporal column as x. This is the comparator reflex
  (`SELECT t, v` just works) and the panel that makes play usable as a plain
  metrics viewer at all.
- **Named contract** (`_ts_*` slots, ADR-0122 doctrine): unambiguous, but
  imposes ceremony on the most ordinary result shape SQL produces.
- **Macro-gated** (chart only what a `ts*` vocabulary emitted): couples the
  basic chart to the analysis vocabulary; kills the plain-viewer case.

The named-columns doctrine was motivated by genuinely ambiguous claims
(kanban's string columns). A temporal axis plus numeric lanes is *typed*
evidence, closer to the World panel's shape claim than to kanban's
name-guessing — but this is a doctrine question and belongs to the
dialogue (Q2). Display smoothing lands here as presentation, via the
`trendsmooth` commitments (raw underlay, fixed degree, live-edge marking) —
it is a view concern and should not appear in any data contract. The
carrier also owns display decimation (settled, Q9: per-pixel min/max
envelope, client-side, render-only — §3.2); irregular series render
time-true, and both smoothing and analysis gate on the grid rather than
assuming it.

### 5.2 D2 — substrate × spelling: where computation runs, how it is invoked

The core decision. Five options, assessed against the three roles:

**O1 — panel controls, panel compute** (the Projection shape). Cheapest;
immediately demo-able. Fails all three roles: nothing is recorded, nothing
re-executes, nothing is agent-callable; composition is capped at whatever
the panel hard-codes. Legitimate *only* for pure presentation (D1
smoothing). As the primary exposure it is the comparator's mistake with
fewer features.

**O2 — ClickHouse-executed SQL, contract + macro** (the ADR-0161 shape).
The honest capability boundary, assessed:

- *Expressible in CH today*: the one-liner baselines and residuals (window
  functions), threshold flags, boxcar/EWMA smoothing (built-ins), gridding
  (`WITH FILL`), period probes via array ops. These are cheap, prunable,
  agent-callable, and some belong in the pack vocabulary or the snippets
  corpus regardless of everything else.
- *Plausible but unverified*: FIR smoothing with the MS kernel inlined as a
  literal array by a macro pass (weights computed in Go at expansion time);
  O(n·(2m+1)) lambda evaluations. Boundary extrapolation in SQL would be
  genuinely messy.
- *Out of the question*: the matrix profile (O(n²) lambda evaluations),
  DAMP, motif discovery. **O2 alone cannot carry the headline
  capabilities.**

**O3 — panel-authored analysis nodes, Go executor** (the bands/map-raster
path, one step further). A `tsExecutor` behind `nodeExecutorI` computing
profile/discords/motifs from a *parent node's* served record; a lane per
analysis; memo key = input fingerprint + parameters; parameters as SD8
signals. Verified against the code: the seam is one interface method, nine
lanes already inject executors, and fingerprint/revision plumbing exists.
v1-cheap. The cost is strategic, not mechanical: the invocation lives in
panel/node config, so the recorded-artifact story (graduation, agents,
history) needs a serialization answer *later*, or it quietly becomes
Projection-shaped.

**O4 — client-executed vocabulary in the buffer.** The analysis is spelled
*in the SQL text* — e.g. a CTE `SELECT tsProfile(t, v, 64) FROM grid` —
and the graph routes that node to the Go executor instead of ClickHouse.
Two facts make this smaller than it sounds, both tier (a): the
function-call form parses today (grammar1's canonical form *is* function
calls), and the splitter already lifts every CTE into its own node — so
recognition and routing are a vocabulary check plus an executor choice at
lane construction, exactly the seam O3 uses. What O4 genuinely adds:

- *Fusion boundary*: a client node must never be fused into pushed-down CH
  SQL. The v1-viable restriction is **client nodes are terminal leaves** —
  observed by panels, never read by downstream SQL. Downstream consumption
  is precisely the deferred ADR-0097 SD13 materialization problem, and
  becomes its trigger.
- *Honesty chrome*: Preview/as-sent must say which engine runs which node;
  the canonicalize/passes lane must pass the vocabulary through untouched
  (and per the ADR-0162 SD7 lesson, it must never be registered in the
  literal-only MacroExpander).
- *A vocabulary registry*: agents and docs need to know these functions
  exist and that CH alone cannot execute them.

The payoff is the whole point: **the SQL text stays the only recorded
artifact.** History, snippets, QueryRun pinning, scheduled re-execution and
agent tool-calls work unchanged, from day one. This is the Prometheus
stance (analysis in the query language) with an explicit engine split
instead of a pretence that the database did it.

**O5 — server-side executable UDF** (ClickHouse `executable` /
`executable_pool`; someday the wazero host of ADR-0162 SD8). The only
option where the SQL is *genuinely* executed by the server and the result
is composable downstream in CH without SD13. Costs: server configuration
surface and binary lifecycle (the one option that touches the server
install), `groupArray`-to-one-row plumbing, optimizer-opaque. Defensible on
a sovereign localhost deployment; disproportionate as the first cut. Defer
with a trigger: the first non-play consumer (native TCP tools, other HTTP
clients) that needs the vocabulary server-side.

**Relationship between O3 and O4:** O3 is the execution mechanism O4 needs
anyway. The real fork is whether the *spelling* starts in panel config (O3,
with names and parameters chosen to *be* the future vocabulary) or in the
buffer (O4, accepting the terminal-leaf restriction and the preview chrome
now). That fork — migration debt versus upfront compiler honesty — is Q1,
the main dialogue point.

**Settled (dialogue, 2026-08-02): O4, CTE-only v1 — with the `ts*`
camelCase family reserved (Q7).** Two code findings collapsed O4's marginal
cost and carried the decision:

- The terminal-leaf rule is a walk over `splitNode.DependsOn` edges that
  already exist, and `{name:Type}` slots inside a client body already land
  in `splitNode.Reads` — so analysis parameters are live, sweepable,
  history-recorded signals with no new machinery.
- The client executor *composes* rather than coordinates: the client
  node's `compiledNode.SQL` is the fused input CTE (the CH-executable
  part), and the executor wraps the wire executor — execute the input on
  ClickHouse, transform in Go, return Arrow — so the lane's (SQL, params)
  memo key remains the identity unchanged, and the swap point is the
  existing per-node lane construction. An observed input recomputes on its
  own lane, which is the recorded SD13 status quo, not a new concession.

O3-as-primary is rejected with three kill-reasons: it violates all three
§2.3 roles (the Projection counter-precedent the roles framing exists to
stop); its cost advantage is illusory (transform, validation and lane work
are shared — the delta is chrome versus controls); and its discoverability
virtue is achievable SQL-first by affordances that *write the spelling
into the buffer* via the existing delivery ops. v1 scope: client
vocabulary is legal only as a terminal-leaf CTE, rendered through
bindings/channels; a client call in the sink position is a loud error with
the CTE hint (auto-observe sugar is a follow-on slice); downstream SQL
reading a client node remains the SD13 trigger. Residual risks with their
mitigations: buffer no longer runs on bare ClickHouse (engine badge on the
node plus a Preview caption; the spelling names the computation, not the
engine — it becomes genuinely server-executable if O5 ever lands);
vocabulary calcifying into recorded artifacts (tiny v1 roster, strict
sole-select-item arguments, additive-only evolution per the ADR-0162
discipline); passes must pass unknown function names through (pinned by a
test); collision with a real server UDF (classify-time check, the chpack
precedent).

### 5.3 D3 — outputs are ordinary data

Detector and miner outputs should be records with contracts, not pixels:

- **Score series** `(t, score, exact)` — renders as a lane under the series
  in the carrier panel; feedable to `adscore`; exact-mode only (S2).
- **Span sets** `(from, to, label, color, score)` — this is the existing
  `_tl_band_from/to/color/label` contract. Anomaly extents (plateaus) and
  motif occurrences (one colour per motif) render on the Timeline **today**,
  and as chart bands via the implot custom lane. A ranked span table feeds
  the Table panel and the adjudication affordance (S6).
- **Motif output** — *(Q4, settled: none of this ships in v1.)* The
  surveyed cut was a small reader on `matrixprofile.Profile` (top-k
  non-overlapping pairs; collect-within-r, radius defaulted from the
  pair's own distance — left open by ADR-0150's sub-decision 1); the
  dialogue held all motif UI for the set-discovery follow-up ADR instead.
  The span output contract below remains the ready seam, and motif names
  stay reserved in `ts*`.
- **The profile itself** — a second line under the series. Cheap, and the
  single most trust-building view the field has: minima visibly repeat,
  maxima visibly do not fit.

### 5.4 D4 — parameters

Window `m` as a signal with an S7-derived suggested default and a sweep
affordance; exclusion divisor fixed at the package default; DAMP exact mode
on in play; motif radius/k per the v1 reader. Parameters-as-signals is what
makes "drag the window, watch the profile" a property of the runtime rather
than a feature of one panel.

### 5.5 D5 — multivariate

v1 is per-channel: `SELECT t, c1..cn` yields n profiles side by side (the
carrier must handle multi-numeric results anyway). M4's subdimensional
aggregation slots in behind the same score/span contracts (plus a "which
channels carried it" field — the explainability M4 exists for). The known
inversion above ~32 dimensions and the M5 complement are chrome-level
guidance when they land, not v1 scope.

### 5.6 D6 — the honesty chrome and the adjudication loop

Baselines on by default (S3); loud refusals with actionable hints (gappy
grid → "add WITH FILL or analyse per segment"; DateTime vs DateTime64;
non-monotone t); warm-up and live-edge regions rendered as such (S1);
threshold lines labelled for what they are (S4). Adjudication-to-facts (S6)
is strategically load-bearing but schema-shaped — it wants the
facts-modelling dialogue before any schema is committed; the v1 cut can be
deliberately minimal (a mark, a span, a verdict) with the schema question
flagged. (Settled, Q6: the v1 home is a dedicated keelson-style labels
table; the facts dialogue is deferred with a trigger.)

Under the §1 framing this section is product, not chrome, and the M2
fixture generator joins it: flaw-avoiding synthetic series with known
ground truth, generated on demand, are the education role's laboratory —
run a detector against a fixture whose answer is known, see VUS-PR with
its honest band, let the triviality check catch a too-easy setup. (Shape
settled, Q8: fixtures publish as per-session ad-hoc datasets, ADR-0134
route; the generator affordance is an early post-v1 slice.)

### 5.7 D7 — the app split: batch here, causal there

play is a batch/investigation surface: profile, motifs, and **backtest** —
"when would this have fired" as a causal DAMP replay with warm-up shown,
scored against adjudicated spans where they exist. Live causal monitoring
belongs to imzrt/imztop, wiring `damp` the way `trendsmooth` wired
`mssmooth` (the second-consumer lift precedent). play gains no timer in
this design; if interval refresh ever arrives it is a play-wide feature,
not a detector feature.

### 5.8 D8 — non-goals, recorded

- **Alerting as a product** (notification, routing, for-duration): belongs
  to the QueryRun-as-facts / workingsets line, not this exposure.
- **Forecasting**: nothing in-tree; its own ADR if ever (dependency-free
  candidates exist: seasonal-naive, theta).
- **Changepoint/drift**: the third standing question after "what repeats /
  what does not fit". ADR-0150 declined the model-of-normality family
  (SAND) on machinery-versus-demonstrated-need; the same posture holds
  here. PELT/BOCPD are the literature anchors when a real series shows
  drift the current pair mis-handles.
- **Motif-set algorithms**: the standing deferred follow-up ADR.

## 6. A worked composition, to test the shape

The loadstudy scenario as UX, exercising every dimension: query
`system.asynchronous_metric_log` onto an explicit grid (`WITH FILL` spelled
in the buffer — S5); the Series panel charts it, smoothing toggle for
reading (D1); a profile/discord analysis at the suggested window (S7) adds
a score lane and span set (D3); spans land as Timeline bands and a ranked
table; the moving-average baseline renders beside (S3) and agrees on one
span, is silent on another; the user adjudicates both (S6 → facts); the
backtest readout replays DAMP causally with the warm-up shaded (S1) and
scores against the adjudicated spans with VUS-PR and its honest band (S4).
Everything that happened is recorded: the SQL text (plus node config or
vocabulary call, per Q1), the signal values, the adjudications. This
narrative is also the natural dogfood applet (a sqlapplet book over the
host's own metrics) once the pieces exist.

## 7. Leans — pre-dialogue, held loosely

- **L1** *(claim rule settled by Q2)*: build the Series panel first. It is
  the largest single gap against the comparator, independently valuable,
  and every other piece renders through it.
- **L2** *(resolved by Q1)*: execution is client-side Go behind the
  graph's executor seam; the spelling is the buffer vocabulary (O4,
  §5.2); O2 covers only what CH genuinely expresses; O5 deferred with
  trigger.
- **L3**: outputs on existing contracts — spans are `_tl_band_*`-shaped,
  scores are series. No new widget beyond the carrier for v1; the motif
  gallery waits.
- **L4**: exact-mode scores only; baselines default-on; segment at gaps,
  never silently fill; centre attribution and plateau extents throughout.
- **L5** *(not taken — Q4 settled the other way)*: the survey leaned
  toward shipping the small pair+occurrences reader; the dialogue held all
  motif UI for the set-discovery ADR instead.
- **L6** *(resolved by Q6)*: adjudication in v1 in its most minimal form;
  the v1 home is a keelson-style labels table, and the facts-schema
  dialogue is deferred with a trigger.

## 8. The dialogue agenda — all settled

All nine questions were settled in the dialogues of 2026-08-02; the entries
below record the outcomes. The ADR consumes them as decisions.

1. **The spelling fork (D2). SETTLED 2026-08-02: O4, CTE-only v1.** The
   recorded artifact is the SQL text from day one; findings, kill-reasons
   and v1 scope in §5.2.
2. **The carrier's claim rule (D1). SETTLED: typed claim + row cursor.**
   x = the first temporal column, lanes = all numeric columns, reject with
   reason otherwise — detection where *types* disambiguate, the doctrinal
   carve-out from ADR-0122's same-typed-ambiguity motivation. Point click
   publishes the ordinary selection row cursor (Detail follows); box-zoom
   stays implot-native; brush-emitting `{sel_from}`/`{sel_to}` signals is
   deferred with a trigger (first cross-filter-by-time request).
3. **Backtest scope (D7). SETTLED: v1, as vocabulary.** `tsAnomalyScores`
   is DAMP in exact mode — causal by construction, so replaying it over a
   queried range *is* the backtest. Warm-up is marked in the output
   contract; the vocabulary registry records per-function causality so the
   chart labels lanes per S1. The VUS-PR readout activates when Q6 labels
   exist.
4. **Motif timing (D3). SETTLED: hold for the set-discovery ADR.** The
   pair-plus-radius promotion does not ship; v1 vocabulary is anomalies
   and smoothing only. The span output contract remains the ready seam,
   and motif names stay reserved inside the `ts*` family.
5. **Grid policy (S5). SETTLED: hints plus scaffold affordance.** Refusals
   stay loud; one-click scaffolds are written into the buffer via the
   delivery ops — `GROUP BY toStartOfInterval(…)` for irregular data,
   `WITH FILL STEP` with explicit NULL gaps for gappy grids — with the
   validator's measured Δt filled in.
6. **Adjudication v1 (S6). SETTLED: minimal affordance, keelson-table
   home.** Mark-span-confirmed/false ships in v1, writing to a dedicated
   keelson-style labels table (the workingsets precedent) — persistent and
   SQL-joinable by the scorer. Modeling adjudications as facts (series
   identity via pinned QueryRun) is deferred to its own dialogue with a
   trigger.
7. **Vocabulary naming (D2/O4). SETTLED 2026-08-02: the `ts*` camelCase
   family is reserved** — ADR-0162 SD2 conventions, strict arguments,
   collision-checked against server UDFs at classify time. Final names
   land in the ADR.
8. **Fixture lab (§1 framing). SETTLED: shape now, build as an early
   follow-on.** Fixtures publish as per-session ad-hoc datasets (the
   ADR-0134 route, the pprof-suite precedent): series plus ground-truth
   labels as ordinary queryable tables, so the workbench operates on them
   identically to real data — no special demo mode. The generator
   affordance lands as an early post-v1 slice.
9. **Decimation seam (§3.2). SETTLED: automatic per-pixel min/max
   envelope.** Client-side and render-only — the full series always backs
   hover, selection and the analysis tier; an envelope cannot drop an
   extreme, which LTTB can. SQL-side `lttb` is documented in the snippets
   corpus for huge-range viewing (available today, zero build).

## References

In-repo:

- [ADR-0150 — streaming subsequence anomaly detection](../adr/0150-timeseries-subsequence-anomaly-detection.md)
  and the [motif and anomaly survey](../explanation/timeseries-motif-anomaly-survey.md)
- [ADR-0152 — modified-sinc smoothing](../adr/0152-modified-sinc-smoothing.md)
- [ADR-0161 — play distribution panel](../adr/0161-play-distribution-panel.md)
  and the [distribution-panel survey](./play-distribution-panel-survey.md)
  (the three-roles framing and the directed tracks)
- [ADR-0097 — play reactive query graph](../adr/0097-play-reactive-query-graph.md)
  (nodes, lanes, signals, SD13 materialization deferral)
- [ADR-0122 — play kanban panel](../adr/0122-play-kanban-panel.md)
  (§SD1, the named-columns doctrine)
- [ADR-0149 — ImPlot core port](../adr/0149-implot-core-port-painter-lane.md)
  and the [implot adoption survey](../explanation/implot-adoption-survey.md)
  (the custom-item lane; the anticipated loadstudy UI)
- [ADR-0162 — leeway co/ragged function pack](../adr/0162-leeway-co-ragged-function-pack.md)
  (the substrate-portfolio frame; SD7; SD8's deferred executable-UDF host)

External (tier c — pointers, covered in the in-repo surveys):

- Wu, Keogh. *Current Time Series Anomaly Detection Benchmarks are Flawed…* TKDE 2021.
- Liu, Paparrizos. *TSB-AD.* NeurIPS 2024.
- Lu et al. *DAMP.* DMKD 2022.
- Rong, Bailis. *ASAP: Prioritizing Attention via Time Series Smoothing.* PVLDB 10(11), 2017 — the comparator's Smoothing transformation (§3.1).
- Steinarsson. *Downsampling Time Series for Visual Representation.* MSc thesis, University of Iceland, 2013 — LTTB (§3.2).
- Schäfer, Leser. *Motiflets.* PVLDB 2022.
- Howard, Ramdas et al. *Time-uniform, nonparametric, nonasymptotic confidence sequences.* Ann. Statist. 2021 — the anytime-valid track's anchor.
