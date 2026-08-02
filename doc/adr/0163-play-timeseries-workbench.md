---
type: adr
status: proposed
date: 2026-08-02
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to accepted
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to accepted
---

> **Status: proposed — pre-human-review.** Decision under consideration; do not implement as if accepted.

# ADR-0163: play timeseries workbench — Series panel and the `ts*` client vocabulary

## Context

The tree carries a timeseries substrate — matrix profile, streaming left
discords, an honest scorer with flaw-avoiding fixtures, modified-sinc
smoothing ([ADR-0150](./0150-timeseries-subsequence-anomaly-detection.md),
[ADR-0152](./0152-modified-sinc-smoothing.md)) — and none of it is reachable
from play. play also has no numeric series-over-time chart at all: Timeline
renders events and spans, Projection a scatter, and the shape panels their
own contracts.

The design space, the comparator analysis (Grafana's four smoothing layers,
its implicit stance on non-equidistant data), the scientific commitments
(S1–S8) and nine settled dialogue questions (Q1–Q9) live in the
[background survey](../adr-background-work/play-timeseries-analysis-survey.md);
its §9 pipeline diagram is this ADR's context sketch. Framing settled there:
the exposure is a workbench for algorithm evaluation, a data-quality
assessment surface and an education tool — comparator parity is the floor.
This ADR records only the resulting decisions; arguments stay in the survey.

## Design space — settled by dialogue

| Q | Outcome (survey §8) |
|---|---|
| Q1 | Invocation = buffer vocabulary (O4), client nodes are terminal-leaf CTEs; `ts*` family reserved (Q7) |
| Q2 | Series panel: typed claim + row-cursor selection; brush-to-signals deferred |
| Q3 | Causal backtest in v1 as vocabulary (`tsAnomalyScores` = DAMP exact) |
| Q4 | Motif UI held for the motif-set follow-up ADR; names stay reserved |
| Q5 | Grid refusals plus scaffold affordances that write the SQL idiom |
| Q6 | Adjudication v1: minimal mark UI into a keelson-style labels table |
| Q8 | Fixtures publish as per-session ad-hoc datasets; build is M4 |
| Q9 | Render decimation = per-pixel min/max envelope; SQL `lttb` documented |

## Decision

### §SD1 — the Series carrier tab

A new body tab `series` (Title "Series", DockID **23**, lazy). Claim is
**typed**: x = the first temporal column, one lane per numeric column,
anything else ignored; no temporal or no numeric column rejects with a
reason. This is detection where *types* disambiguate — the named-columns
doctrine ([ADR-0122](./0122-play-kanban-panel.md) §SD1) was motivated by
same-typed ambiguity, which a time axis plus numeric lanes does not have.
The panel is multi-channel in the Timeline mould: `chMain` (required, the
charted result), `chScores` and `chSpans` (optional), auto-suggested from
split nodes whose static shape matches and overridable via the Graph-view
binding. Point click publishes the ordinary selection row cursor; box-zoom
stays implot-native and re-renders from the client-held series. Irregular
series render time-true. Display smoothing is `trendsmooth` (raw underlay,
degree fixed at 4, half-width the only knob) with the extrapolation-backed
live edge rendered distinctly (S1). Rendering decimates through a
per-pixel min/max envelope — render-only; hover, selection and analysis
always read the full series (Q9).

### §SD2 — grid validation and scaffolds

The claim validator computes the Δt distribution and sorts every series
into three classes: *regular with jitter* (grid declared at the median Δt;
a deviation beyond ±20% of the median — an initial, property-tested
constant — opens a gap), *regular with gaps* (segment at gaps, analyse per
segment), *genuinely irregular* (charted time-true; analysis refuses and
points at aggregation). Fill is never applied client-side; the two
refusal hints carry one-click scaffolds written into the buffer via the
delivery ops — `GROUP BY toStartOfInterval(…)` and
`ORDER BY t WITH FILL STEP …` (explicit NULL gaps) — with the measured Δt
substituted. Timestamps are read back forced-UTC. Series longer than the
package ceiling (~500k) refuse with the limit named.

### §SD3 — the `ts*` vocabulary, v1 roster

Client-executed functions spelled in the buffer, recognised only as the
**sole select item** of a CTE body; arguments are column identifiers,
integer literals, or `{name:Type}` param slots (which thereby become live
signals). Exact-case names; the family is reserved, including unshipped
motif names (Q4/Q7). One algorithm per name today; variants arrive as new
names, additive-only (the [ADR-0162](./0162-leeway-co-ragged-function-pack.md)
discipline). v1 ships four:

| Function | Output columns | Notes |
|---|---|---|
| `tsSmooth(t, v, halfWidth)` | `t, smooth` | MS kernel, degree 4; conditioning use (S8), not display |
| `tsProfile(t, v, window)` | `t, profile` | z-normalised matrix profile; centre-attributed; two-sided |
| `tsAnomalyScores(t, v, window)` | `t, score, warm_up` | DAMP `Config.Exact` left discords; **causal**; centre-attributed |
| `tsAnomalySpans(t, v, window, k)` | `_tl_band_from, _tl_band_to, _tl_band_label, _tl_band_color, score` | top-k plateau extents; palette colours ([ADR-0156](./0156-qualitative-palette-dark-surface.md)) |

Outputs speak the consuming contract *directly* — under the terminal-leaf
rule no downstream SQL can rename them, so `tsAnomalySpans` emits the
Timeline band columns itself and its result feeds Timeline, chart overlays
and Table unchanged. One value column per call in v1; multivariate waits
for ADR-0150 M4. The registry (name → transform, output schema, causality
flag, param spec) is play-local; a second consumer is the lift trigger.

### §SD4 — client nodes on the graph

Classification happens at split time on the parsed body. A client node
must read exactly one CTE (`FROM <cte>`); it is a **terminal leaf** — a
client name in any node's `DependsOn`, or a client call in sink position,
is a loud error naming the fix ("bind a pane to the CTE"). Execution: a
`tsExecutor` implementing `nodeExecutorI` wraps the wire executor — its
`compiledNode.SQL` is the fused input CTE, so it executes the input on
ClickHouse, transforms in Go, and returns Arrow; the lane's (SQL, params)
memo key stays the identity unchanged, and supersession, staleness and
last-good come with the lane. Computation is asynchronous with the lane's
loading state. At classify time a cached `system.functions` probe warns
when a `ts*` name shadows a real server UDF; the buffer vocabulary wins
inside play.

### §SD5 — honesty chrome

Graph view badges client nodes with their engine; the Preview/as-sent pane
captions them ("computed client-side — not sent to ClickHouse") and shows
only the CH part as sent. Score lanes render exact-mode values only, with
the `warm_up` region shaded (S1/S2); span extents are plateaus, not
argmaxes. When a score channel is filled, the Series panel renders the
moving-average-residual baseline beside it **by default**, computed
in-panel via `adscore.BaselineScores` on the same input and window and
labelled as the baseline (S3 — the baseline is mandated chrome, not an
optional feature, which is the kill for spelling it in SQL). Threshold
affordances are labelled as reference-window quantiles, not guarantees
(S4); calibrated flagging (conformal, anytime-valid) stays a directed
track.

### §SD6 — labels and the backtest readout

A minimal adjudication affordance (mark a flagged span
confirmed/false-alarm) writes an append-only keelson table `tslabels`
(the [ADR-0148](./0148-app-workingsets.md) pattern): `created_at`,
`span_from`, `span_to` (all UTC), `verdict`, `input_hash` (hash of the
input node's compiled SQL and params), `detector`, `window`, `note`;
latest verdict per (`input_hash`, span) read via `argMax`. When labels
exist for the current input, the panel shows VUS-PR (and VUS-ROC with its
usable-band annotation) for detector *and* baseline via `adscore` —
"beats the one-liner on adjudicated spans" becomes a displayed,
falsifiable claim. Modelling adjudications as facts is deferred to its
own dialogue (identity via pinned QueryRuns).

### §SD7 — fixtures as ad-hoc datasets

The M2 generator publishes per-session ad-hoc datasets
([ADR-0134](./0134-adhoc-datasets.md) route): `fixture_series(t, v)` plus
`fixture_truth(span_from, span_to, kind)`, queryable like any data — the
workbench runs on fixtures with no demo mode. A minimal affordance (kind,
seed) lands in M4.

### §SD8 — deferrals, each with a trigger

- **Client-sink sugar** (auto-observe when the sink is a client call) —
  trigger: recurring hits on the §SD4 sink error.
- **Brush → `{sel_from}`/`{sel_to}` signals** — first cross-filter-by-time
  request.
- **Downstream SQL over client output** — ADR-0097 SD13 materialization;
  first buffer that needs it.
- **Motif vocabulary and UI** — the motif-set follow-up ADR (Q4).
- **Labels as facts** — the identity dialogue (pinned QueryRuns).
- **Server-side execution of `ts*` (O5)** — first non-play or native-TCP
  consumer.
- **Multivariate calls** — ADR-0150 M4 landing.
- **Conformal / anytime-valid flagging** — the directed track's own
  dialogue.
- **ACF-suggested `trendsmooth` half-width** — once the §SD2 period probe
  exists (the ASAP idea on the better kernel, survey §3.1).

## Milestones

| Milestone | Content |
|---|---|
| **M0** | Series tab: typed claim, channels, envelope decimation, trendsmooth + live edge, row cursor; Δt validator, refusals, scaffold affordances |
| **M1** | Vocabulary: registry, split classification, terminal-leaf errors, `tsExecutor`, the four functions, engine chrome, collision warn, pass-through pin |
| **M2** | Overlays and honesty: score/span channels rendered, baselines default-on, warm-up shading, spans on Timeline via the existing contract |
| **M3** | `tslabels` + adjudication UI + the VUS readout with usable-band annotation |
| **M4** | Fixture lab: generator → ad-hoc dataset affordance |

M0 and M1 are independent (M1's outputs render as plain tables until M2).

## Surfaces

- Tab slug `series`, DockID 23 (frozen; built-ins stay < 64), and the
  derived `BOXER_PLAY_FOCUS_SERIES` knob ([ADR-0009](./0009-environment-variable-registry.md)
  registry, `env-vars.md` regen).
- The `ts*` names — user-visible, recorded in buffers, snippets and
  history; reserved as a family including unshipped motif names.
- Output contract columns (`t, smooth / profile / score, warm_up`; the
  reused `_tl_band_*` set).
- The keelson table `tslabels`.
- Help book sections and snippets-corpus entries (vocabulary, scaffolds,
  the SQL `lttb` idiom).

## Alternatives

- **Panel-side analysis controls (O1/O3-as-primary).** Rejected: violates
  all three settled roles — invocations unrecorded, unreplayable,
  agent-invisible (the Projection counter-precedent); the shared executor
  work makes its cost advantage illusory (survey §5.2).
- **CH-executed contract + macro only (O2).** Rejected as the primary
  route: ClickHouse cannot express the matrix profile or DAMP at all.
- **Server executable UDFs now (O5).** Deferred: the only option touching
  server configuration and binary lifecycle; wanted only by consumers
  that do not yet exist.
- **Named `_ts_*` claim contract.** Rejected: ceremony on `SELECT t, v`,
  the most ordinary result shape SQL produces, with no ambiguity for
  names to resolve.
- **LTTB as the automatic decimator.** Rejected: selection-based
  decimation can drop a narrow extreme; the envelope cannot. LTTB remains
  the documented SQL option for huge-range viewing.
- **Silent client-side resampling / interpolation.** Rejected: fabricated
  samples are data the detector then scores (the loadstudy invariant).
- **Shipping the motif pair+radius promotion.** Held (Q4): the radius
  promotion's known weakness stays out of recorded artifacts; the
  set-discovery ADR is the real answer.
- **Session-only or facts-first adjudication.** Rejected v1: the former
  loses the labels bootstrap, the latter gates v1 on the facts-identity
  dialogue.

## Consequences

### Positive

- One recorded SQL artifact carries query, gridding and analysis —
  replayable, pinnable, agent-callable from day one; parameters are live
  signals with no new machinery.
- Detector output renders through existing contracts (Timeline bands,
  Table) and the new carrier; the honesty apparatus (baselines, warm-up,
  usable-band scoring) ships as product, not documentation.
- The labels loop makes "beats the one-liners on this host" a measurable,
  displayed claim — closing the gap ADR-0150's loadstudy named.

### Negative

- A play buffer using `ts*` no longer runs on bare ClickHouse; the engine
  split must be visible chrome forever.
- Two engines and a vocabulary to document and complete against
  (ADR-0147's completion seam eventually wants the registry).
- O(n²) profile latency on long inputs is real; the lane hides it behind
  last-good, not away.
- The Series panel is the largest single build item and the least
  pre-specified surface here; M0 carries most schedule risk.

### Neutral

- The registry stays play-local until a second consumer.
- Motif capability remains dark until the follow-up ADR, by choice (Q4).
- `dockTab` numbering grows to 23; the frozen-ID discipline is unchanged.

## Migration

Additive throughout: no existing panel changes contract (Timeline merely
gains eligible span sources), the dock layout is not persisted across runs
(ADR-0097 slice-6 record), and `env-vars.md` regenerates. A user UDF named
like a `ts*` function keeps working outside play; inside play the
vocabulary wins and the collision warning names the shadowing.

## Verification plan

- **Unit.** Split classification and terminal-leaf errors (sink position,
  downstream reference, multi-input); transform outputs checked against
  the `matrixprofile`/`damp`/`mssmooth`/`adscore` packages through the
  executor (never oracling a function with itself); output-schema goldens;
  Δt classifier property tests including the jitter tolerance.
- **Pass-through pin.** A corpus case proving CanonicalizeFull and the
  default pass registry leave `ts*` calls intact.
- **Envelope property.** Per pixel bucket, rendered min/max equals source
  min/max — the cannot-drop-extremes claim as a test.
- **Benchmarks.** Dense-series render (10⁶ points) with envelope on/off;
  documented `tsProfile` latency at 10⁴–10⁵ samples.
- **Live.** Tour scenes for the Series tab via `BOXER_PLAY_FOCUS_SERIES`
  ([ADR-0154](./0154-headless-carrier-tree-and-driver.md) lane); an
  integration-lane probe for the `system.functions` collision warning.

## Status

Proposed 2026-08-02. Consumes the survey's settled dialogue (Q1–Q9);
supersedes nothing.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers. Subsequent refinements land as dated `## Updates`
entries, not as silent rewrites.

## References

- [Background survey — timeseries analysis in play](../adr-background-work/play-timeseries-analysis-survey.md)
  (design space, comparator analysis, S1–S8, Q1–Q9, the §9 pipeline)
- [ADR-0150](./0150-timeseries-subsequence-anomaly-detection.md) and the
  [motif and anomaly survey](../explanation/timeseries-motif-anomaly-survey.md)
- [ADR-0152 — modified-sinc smoothing](./0152-modified-sinc-smoothing.md)
- [ADR-0097 — play reactive query graph](./0097-play-reactive-query-graph.md)
- [ADR-0161 — play distribution panel](./0161-play-distribution-panel.md)
  (contract and macro precedent)
- [ADR-0162 — leeway function pack](./0162-leeway-co-ragged-function-pack.md)
  (vocabulary discipline)
- [ADR-0122](./0122-play-kanban-panel.md) ·
  [ADR-0134](./0134-adhoc-datasets.md) ·
  [ADR-0148](./0148-app-workingsets.md) ·
  [ADR-0149](./0149-implot-core-port-painter-lane.md) ·
  [ADR-0156](./0156-qualitative-palette-dark-surface.md) ·
  [ADR-0009](./0009-environment-variable-registry.md)
