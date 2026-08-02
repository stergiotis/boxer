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

play can render a result set as a table, a board, a map, a flow diagram — but
not as a *distribution*. The only distribution machinery in the tree is
in-process: `distsummary`/`ecdf`/`boxenplot` fed by a caller-owned t-digest
(ADR-0046 lineage). A ClickHouse result set has no path into any of it.

The [background survey](../adr-background-work/play-distribution-panel-survey.md)
mapped the design space and its design dialogue (2026-08-02) settled the
framing this ADR builds on:

- **play serves three roles on one substrate** — (a) a feature foundry, where
  a statistic developed interactively graduates into automated comparison
  because it already *is* SQL; (b) a human-on-the-loop viewer for routine and
  detector-flagged work; (c) an agentic surface, where a constrained macro
  vocabulary is the tool-call an agent emits and the rendered panel is what
  the human verifies.
- **Comparison, not portraiture, is the primary view.** The single-series
  portrait is the degenerate case.
- Follow-up tracks — time-uniform bands, conformal readouts, the lineup
  protocol, the agent surface itself — are out of scope here and get their
  own dialogues.

Three verified facts shape the cut (survey §3–§4 for the full inventory):

- The widget stack needs **no raw data and no t-digest**:
  `ecdf.Renderer.RenderGrid` takes `(xs, fnAt, n)`; `letterval.Levels` takes
  any `QuantileOracle` — whose doc already anticipates "ClickHouse pushdown
  wrappers"; `boxenplot` takes the resulting ladder. A quantile grid plus a
  count feeds everything, bands calibrated at true n.
- ClickHouse returns that grid in one aggregate call (`quantiles*` →
  `Array(Float64)`), with estimator families whose error models differ in
  kind (exact / rank-bounded / relative-value / unbounded-but-good); play's
  Arrow path already folds `Array` columns.
- grammar1 parses `descriptive_statistics(a, b)` today as an ordinary
  function call, and the pass pipeline already hosts statement-level rewrites
  (`CanonicalizeFull`, order 50) and registered macro expansion
  (`LW_ID_*`, ADR-0106 §SD5).

This ADR decides three things and nothing else: the **result-shape contract**
(what a query must emit for the panel to claim it), the **macro** that emits
that shape for the common case, and the **panel** that renders it.

## Prior art

- **ADR-0122 (Kanban)** — the named-column claim contract on the main result:
  "named columns rather than detection", required/optional split, loud
  rejects. Its §SD2 records why alias tokens must dodge `#` (silent ClickHouse
  comment) and `:` (leeway handle) — hazards this contract avoids outright by
  using fixed names and no tokens.
- **ADR-0159 §SD6 / the Sankey panel** — the *other* contract style:
  convention-named CTEs pulled off the split graph on private lanes, used when
  the panel's input is auxiliary to the user's final SELECT. Also the
  selection lesson: main-result panels write the row cursor; private-lane
  panels must not.
- **ADR-0106 §SD5 (`LW_ID_*`)** — registered macro family expanded by a
  nanopass pass: arity-checked, loud on misuse, fixpoint-iterated, registered
  in the keelson passreg defaults so play and applets both see it.
- **`distsummary`'s provenance ethos** — bands and readouts name what they
  were calibrated on (BandN vs SampleN); this panel extends that to naming
  the server-side estimator.
- **ADR-0156** — the qualitative series palette and its wrap policy.
- **ADR-0141** — queries leave through one dispatch seam; panels observe
  results. (This kills any panel-authored-query design at the root.)

## Design space (QOC)

**Q1 — How does a user ask for a distribution?**

- *O1a: macro in the select list* — `SELECT descriptive_statistics(a, b)
  FROM t WHERE … GROUP BY g`, expanded by a pass into the contract query.
- *O1b: contract only* — users or applets write the expansion by hand.
- *O1c: table-function style* — `FROM descriptive_statistics(…)`. Killed:
  touches the FROM grammar surface with no prior art, and re-poses the
  question O1a already answers (what relation, which clauses carry over).
- *O1d: panel-authored follow-up queries*. Killed by ADR-0141's seam and by
  ADR-0122 §SD1's no-guessing rule.

**Chosen: O1b + O1a.** The contract must exist regardless — the panel cannot
tell macro output from hand-written SQL, which is what makes the contract the
real interface — and the macro is sugar that emits it.

**Q2 — What crosses the wire?**

- *O2a: long form* `(series, p, q)` rows. Killed: scalar stats need a second
  shape anyway, the panel reassembles k×series rows, and the Table fallback
  is noise.
- *O2b: wide form* — one row per series, `Array(Float64)` grid columns.
  Matches what `quantiles*` natively returns; Arrow list handling exists;
  Table fallback is one legible row per series.
- *O2c: convention-named CTEs on private lanes* (Sankey style). Killed for
  this panel: the distribution summary *is* the user's result, not an
  auxiliary structure beside it; a side channel buys plumbing and no
  expressiveness here.

**Chosen: O2b.**

**Q3 — Default estimator?**

- *exact* — killed as default: per-group state is O(n) by design; a default
  that can exhaust memory on the first big table is hostile. Available as an
  opt-in.
- *gk* — the formally attractive option (deterministic rank error that
  composes with the F-space bands), but ClickHouse's `accuracy` parameter →
  ε mapping is not pinned yet (named verification gap). Opt-in now; flipping
  the default is a dated Update once the mapping is verified.
- *dd* — relative *value* error; orthogonal to F-space bands. Opt-in.
- *tdigest* — bounded state, strong practice, matches the in-process choice;
  no worst-case bound, so the panel must label the result approximate.

**Chosen: `tdigest` default, `'exact' | 'gk' | 'dd'` opt-ins, estimator
always named in the result.** Plain `quantile()` (reservoir) is excluded
entirely: documented run-to-run nondeterminism with no error model.

**Q4 — How does comparison enter v1?**

- *SQL baseline marker now*. Killed for v1: a baseline is a *choice*, and the
  macro cannot know it; the column can be added later without breaking the
  contract (additive optional column, name reserved).
- *Two-group test columns* (`ks_d`/`ks_p` emitted when GROUP BY yields two
  groups). Killed: a pair statistic has no honest home in per-series rows
  (which row carries it? against which baseline?), and it silently changes
  shape with group count. Formal test labels belong to the comparison
  sibling spelling (deferred track).
- *Panel-side baseline selection* — any series can be marked baseline in the
  panel; shift function and distance readouts derive from the shared grid.

**Chosen: panel-side selection.** Everything comparison-shaped in v1 is
derivable from the grids alone.

## Decision

### §SD1 — the result-shape contract

A body-zone tab claims the active result on the main channel when the
**required** columns below are present and valid. Names are bare (kanban
precedent: `lane`, `title`) — prefixing was rejected because hand-written
SQL is a first-class producer (Q1) and the validation rules make accidental
claims implausible.

| Column | Type | Required | Meaning |
| --- | --- | --- | --- |
| `series` | String | yes | display label; one row per series |
| `n` | UInt64 | yes | non-null observation count behind the quantiles — the band-calibration n |
| `ps` | Array(Float64) | yes | probability grid, strictly ascending, each in (0, 1) |
| `qs` | Array(Float64) | yes | `Q(p)` per grid point, non-decreasing, `len(qs) == len(ps)` |
| `n_null` | UInt64 | no | rows where the column was NULL |
| `x_min`, `x_max` | Float64 | no | exact extremes |
| `mean`, `sd`, `skew`, `kurt` | Float64 | no | moments (sample forms); shown with n, never alone |
| `hist_lo`, `hist_hi`, `hist_w` | Array(Float64) | no | histogram triplet — all three or none, equal lengths |
| `estimator` | String | no | provenance token, e.g. `tdigest`, `exact-hf7`, `gk:1000`; absent ⇒ treated as unlabelled-approximate |

Claim validation is strict and rejects loudly with the reason (kanban
precedent): grid not ascending / out of (0,1), length mismatch, `n == 0`
with non-empty grid, partial histogram triplet, non-monotone `qs`. A reject
renders as a message in the tab, never as a silently empty plot.

`series_baseline` (UInt8) is a **reserved name**, not consumed in v1 — the
anticipated additive extension for SQL-declared baselines (Q4).

### §SD2 — the probability grid

The macro emits one fixed grid; the panel decides post-hoc what is
trustworthy (this resolves "the macro cannot know n" without a second round
trip):

- the dyadic letter-value ladder `2^-k` and `1 − 2^-k` for k = 1…16 —
  exactly the depths `letterval.Levels` consumes;
- a uniform body grid `j/64`, j = 1…63, for the ECDF curve;
- tail points `10^-3`, `10^-4` and mirrors.

Deduplicated and sorted: **87 levels**, exported as a constant (with its
generator, not a hand-typed literal). The panel clamps rendered letter-value
depth to `letterval.RecommendedDepth(n)` and may clip the ECDF x-window per
the ADR-0093 tail rule. Hand-written SQL may emit any valid grid — the
contract does not require this one; the constant is the macro's choice, not
the panel's demand.

### §SD3 — the macro

Canonical name **`descriptive_statistics`**, matched case-insensitively
(same normalisation the existing call-name handling uses). One spelling:

```sql
SELECT descriptive_statistics(['exact' | 'gk' | 'dd' | 'tdigest',] col1 [, col2 …])
FROM …  [WHERE …]  [GROUP BY k1 [, k2 …]]
```

- The optional **first string literal** selects the estimator; absent ⇒
  `tdigest`. The parametric spelling `descriptive_statistics('gk')(a, b)`
  was rejected: it parses, but it is a second CST shape for zero added
  expressiveness.
- **Sole-select-item rule:** the call must be the only expression in the
  select list. Mixing with other select items is a loud pass error, not a
  merge attempt — combining a distribution summary with arbitrary other
  aggregates in one statement has no coherent output shape.
- ≥1 column arguments; each must be a bare column or expression the pass
  re-emits verbatim into aggregate arguments. Arity/type misuse errors at
  expansion time (LW_ID precedent: an unexpanded or malformed macro must
  never reach the server).

**Expansion** (statement-level): one `UNION ALL` branch per argument column
over the user's original FROM/WHERE/GROUP BY, each branch emitting the §SD1
columns —

- `series` = the argument's source text, with GROUP BY key values folded in
  via `toString` (NULL keys → a placeholder glyph). Typed group keys are
  deliberately *not* carried as columns in v1 (recorded consequence).
- `n` = `count(col)` (non-null — the statistically honest calibration n),
  `n_null` = `count() − count(col)`.
- `x_min`/`x_max`/`mean`/`sd`/`skew`/`kurt` = `min`/`max`/`avg`/
  `stddevSamp`/`skewSamp`/`kurtSamp`.
- `qs` = the chosen `quantiles*` family over the §SD2 grid; `ps` = the grid
  as an array literal; `estimator` = the provenance token literal.
- **No histogram emission in v1** — `hist_*` stays contract-only until the
  M2 milestone decides density-normalised rendering defaults; hand SQL can
  ship it earlier.
- Tail clauses (SETTINGS, parameters) are preserved; expansion output is
  machine-generated and deliberately not re-canonicalised (same recorded
  consequence as the existing macro family).

### §SD4 — where the code lives, and pass registration

New package **`public/analytics/stats/distsql`**: the grid constant + its
generator, the contract column names as constants, the claim validator, the
`letterval.QuantileOracle` adapter over `(ps, qs, n)` (monotone
interpolation, inverse for `CDF`), and the expansion pass. The statistics
domain stays in `analytics/stats` beside `letterval`/`ecdfbands`; play
imports it, not the reverse.

The pass registers in the **keelson passreg defaults** beside
`ExpandLwIdMacros` — `StagePreExecute`, ordered after `CanonicalizeFull`
(50) so it sees canonical shapes — reaching play and sqlapplets alike. It
declares its properties (idempotent: a second application finds no macro
call and is a no-op) and is corpus-checked via `AssertProperties`. The play
pass-catalogue ordering pin (`TestRegisterPassesOrdering`) grows one entry.

### §SD5 — the panel

`TabSpec{id: "dist", title: "Distribution", zone: body, lazy: true,
shapeContract: true, writes: [selection]}` beside Table/World/Kanban. It
observes the main result; claiming does not affect any other tab (tabs are
independent — the World/Kanban behaviour, asserted in the panel tests).
Clicking a series row writes the ordinary row-cursor selection (this is a
main-result panel — the Sankey lesson in reverse), so Detail shows the
series row.

Sub-views over implot, one claim, comparison-first:

- **ECDF** — every series as `RenderGrid(xs=qs, fnAt=ps, n)`, overlaid,
  ADR-0156 palette. Bands: all series when ≤ 3, otherwise
  focused/selected series only (recorded, tunable).
- **Shift** (shown when ≥ 2 series) — Δ(p) = `Q_s(p) − Q_baseline(p)`
  against p for each non-baseline series; baseline is the panel-selected
  series (defaults to the first row). Simultaneous band by conservative
  combination of the two per-series bands at α/2 each; a distance readout
  (Wasserstein-1 as the trapezoid over |Δq| dp) rides the status line.
- **Boxen** — one letter-value column per series, side by side, ladder from
  the oracle adapter, depth clamped by `RecommendedDepth(n)`.
- **Histogram** — rendered only when the optional `hist_*` triplet is
  present: density-normalised (height = weight/width — the verified
  fractional-weight, variable-width server output misleads otherwise),
  labelled as a streaming approximation.

The **honesty line** is always visible: n, null count, estimator token, band
method + α, and — under a sketch estimator — the note that the band does not
include sketch error. Crosshair readouts reuse the existing
`WriteStatusLine`/`Verbose` registers. Degenerate inputs are decisions:
n = 0 renders "empty" (no reject); `min == max` labels the collapse and
skips the plots; a low-cardinality hint suggests GROUP BY when `qs` has few
distinct values.

### §SD6 — comparison semantics in v1

Baseline is panel-local state (a click), not data. Everything shown in the
Shift view derives from the grids; no server round trip and no formal test
labels in v1 — those belong to the comparison sibling spelling (deferred
track, survey §10), where a baseline exists in SQL and pair statistics have
an honest home.

## Surfaces

| Surface | Change | Moves with it |
| --- | --- | --- |
| User-facing SQL vocabulary | new macro `descriptive_statistics` (case-insensitive) | editor docs/snippets; sqleditor completion corpus (ADR-0147); the help book's play pages |
| keelson passreg defaults | new `StagePreExecute` entry after CanonicalizeFull | play Passes tab listing; `TestRegisterPassesOrdering` pin; every host wiring the defaults (applets included) |
| Result-contract names | `series`, `n`, `ps`, `qs` + optionals reserved (§SD1) | panel claim code; applet/gallery examples; any future consumer of the shape |
| Exported Go API | new pkg `public/analytics/stats/distsql` | godoc; downstream modules compiling against it |
| play tab registry | new `TabSpec` id `dist` | initial dock layout; `BOXER_PLAY_FOCUS_DIST` env knob (ADR-0009 registry entry); help corpus test |
| Widgets | none — `ecdf`/`boxenplot`/`implot` consumed as-is | — |

## Alternatives

Killed in §Design space with reasons: table-function spelling (O1c),
panel-authored queries (O1d), long-form wire shape (O2a), private-lane CTE
channel (O2c), `exact`/`gk` as default estimator (Q3), SQL baseline marker
and two-group test columns in v1 (Q4). Additionally:

- **Prefixed contract names** (`dist_series`, …) — rejected: hand-written
  SQL is first-class and the §SD1 validation (a strictly ascending
  probability grid paired with a matching monotone value array) already
  makes accidental claims implausible; prefixes tax every legitimate
  producer to defend against a shape no real query emits by accident.
- **A short macro alias** (`dist_summary` beside the canonical name) —
  rejected: two names for one expansion is a doc and completion burden with
  no expressiveness; the LW_ID family precedent is one canonical name.
- **Emitting the histogram from the macro in v1** — rejected: the
  density-normalisation and bin-count defaults deserve their own look (M2),
  and the contract lets hand SQL ship it meanwhile — descope over gate.
- **A single-scan expansion** (one pass over the relation computing all
  columns via tuple aggregation, instead of UNION ALL per column) —
  deferred, not rejected: it is an optimisation invisible at every surface
  (same contract, same macro), so it can land later without an Update.

## Consequences

### Positive

- The contract is usable the day the panel lands, before the macro exists —
  and by producers the macro will never cover (hand-tuned estimators,
  pre-aggregated tables, non-play tooling writing the same shape).
- The widget stack is consumed unmodified; the seams it already exposes
  (`RenderGrid`, `QuantileOracle`) turn out to be exactly the wire shape.
- The macro is a stable, auditable, one-line vocabulary — the shape an
  agent can emit and a human can verify (role (c)), and a feature that
  graduates to scheduled execution unchanged (role (a)).
- Estimator provenance is in-band, so honesty survives caching, replay, and
  hand-written producers.

### Negative

- `UNION ALL` per column scans the relation k times for k columns. Accepted
  for v1 (the single-scan rewrite is surface-invisible, see Alternatives);
  wide multi-column calls on huge relations pay it meanwhile.
- Folding GROUP BY keys into the `series` string loses their types; a
  consumer wanting typed keys must write the contract by hand. Revisit only
  with evidence.
- Under the default `tdigest`, the band is calibrated as if grid points were
  exact — the sketch's own (unbounded) error is disclosed but not
  quantified. The honest quantified path (`gk` + band inflation) waits on
  the accuracy→ε verification gap.
- An 87-level `quantilesExact*` call on a huge group is memory-heavy when
  the user opts into `exact`; the default avoids it, the opt-in trusts the
  user.

### Neutral

- The background survey ages out as this ADR becomes authoritative
  (adr-background-work convention); it keeps the measurement provenance.
- The §SD2 grid constant becomes API the moment it ships; changing it later
  changes goldens, not correctness (the contract admits any valid grid).
- The panel neither claims exclusively nor alters Table's rendering; a
  result can be a table and a distribution at once.

## Migration

- **Breaks.** None. Every surface is new. A pre-existing query that already
  called `descriptive_statistics` failed at the server with
  `UNKNOWN_FUNCTION` before this change; expansion turning it live is a
  behaviour change only for statements that could not run at all.
- **Path.** (1) `distsql` package: grid + contract constants + validator +
  oracle adapter, unit-tested. (2) Panel M0 against hand-written contract
  SQL (proves wire + widgets; no pass work). (3) The expansion pass +
  defaults registration + goldens; ordering pin updated. (4) M2: histogram
  emission + density-normalised view; gallery applet + docs. Each step
  leaves the tree green.
- **Regeneration.** None. No IDL, no codec, no FFFI2 boundary — neither
  side of an FFI boundary needs rebuilding; `go generate ./...` output is
  unaffected.
- **Old shape.** n/a — nothing is replaced. The in-process
  `distsummary`-over-tdigest path is untouched and remains the right tool
  for live telemetry.

## Verification plan

- **Unit lane (default `go test`).** Expansion goldens over a corpus:
  canonical and quoted spellings, estimator tokens, GROUP BY folding,
  SETTINGS/parameter survival, sole-select-item and arity rejects. Grid
  constant: monotone, in-range, dyadic-ladder membership, the count pinned
  against the generator. Oracle adapter: interpolation round-trips,
  `CDF∘Quantile` consistency on fixtures. Claim validator: accept/reject
  fixtures for every §SD1 rule, reject-message text pinned. Pass
  properties via `AssertProperties` (idempotent).
- **Integration lane** (`//go:build integration`, `CLICKHOUSE_ENDPOINT`-
  gated). Expanded SQL executes against a live server; the Hyndman–Fan
  fixture (`[1,2,3,4]`, p=0.25 → 1.75/1.25 for inclusive/exclusive) pins
  estimator semantics; `Array` columns arrive as Arrow lists through the
  play client path.
- **GUI.** An ADR-0154 tour scene with a seeded multi-group dataset
  (screenshot: ECDF overlay + shift view + honesty line), plus an egui-mcp
  live drive for claim, loud reject, baseline click, and band-policy
  switch at 4 series.
- **What would fail.** Contract drift reddens the claim fixtures; a pass
  reorder reddens the ordering pin; a grid change reddens expansion goldens
  and the letterval depth tests; a server upgrade changing estimator
  semantics reddens the H&F integration fixture.
- **Gap.** The `gk` accuracy→ε mapping is unverified (named above; blocks
  flipping the default and the band-inflation feature). Nothing verifies
  that the server's t-digest matches the in-process one — acceptable, the
  estimator token names the server family, not an implementation promise.
  Whether 87 levels *look* sufficient at every zoom is judgement, left to
  review and the tour screenshots.

## Status

Proposed 2026-08-02, out of the
[distribution-panel survey](../adr-background-work/play-distribution-panel-survey.md)
and its recorded design dialogue. Milestones M0–M2 in §Migration; the four
adjacent tracks (time-uniform bands, conformal readout, lineup protocol,
agent surface) are deliberately **not** gated by this ADR.

## References

- [Background survey + dialogue record](../adr-background-work/play-distribution-panel-survey.md)
  — measurements (ClickHouse 26.7 probes), literature pointers, kill-detail.
- [ADR-0122](./0122-play-kanban-panel.md) — named-column claim contract.
- [ADR-0159](./0159-imzero2-sankey-flow-widget.md) — the private-lane
  contract style this panel deliberately does not use.
- [ADR-0106](./0106-identity-fibonacci-tags-build-tag-retirement.md) §SD5 —
  the macro-family precedent.
- [ADR-0141](./0141-play-endpoint-dispatch-seam.md) — why panels never issue
  queries.
- [ADR-0149](./0149-implot-core-port-painter-lane.md),
  [ADR-0156](./0156-qualitative-palette-dark-surface.md) — rendering
  substrate and series palette.
- [ADR-0009](./0009-environment-variable-registry.md) — the env-var registry
  the new focus knob joins.
- Hofmann, Wickham & Kafadar (2017), letter-value plots — via
  `public/analytics/stats/letterval`; band families via
  `public/analytics/stats/ecdfbands`.
