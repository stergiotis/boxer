---
type: adr
status: accepted
date: 2026-07-30
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-30
---

# ADR-0150: streaming subsequence anomaly detection in Go

## Context

The repository records and plots a lot of time series — `sysmetrics` samplers,
the imztop/imzrt dashboards, play's query results, the metrics overlay — and
has no way to ask either of the two standing questions about them: *what
repeats* and *what does not fit*. A grep for matrix-profile, discord, motif or
anomaly machinery across the Go tree returns nothing; the closest existing
work is descriptive statistics
([`github.com/stergiotis/boxer/public/analytics/stats`](../../public/analytics/stats)
— streaming Welford moments, t-digest, letter values, ECDF bands), which
summarizes a distribution but says nothing about *sequence* structure.

The literature landscape, the benchmark history, and what the published
evaluations do and do not establish are surveyed separately in
[the motif and anomaly survey](../explanation/timeseries-motif-anomaly-survey.md).
This ADR records only the choice. The findings that drive it:

- **Motifs and discords come from one structure.** The matrix profile — for
  every subsequence, the distance to its nearest non-trivial neighbour — has
  motifs at its minima and anomalies at its maxima. Building one substrate
  answers both questions, even though this ADR scopes only the second.
- **The substrate needs no dependencies.** STOMP's `O(1)` dot-product
  recurrence means only the *first* distance profile needs a transform, and
  that one can be computed directly in `O(nm)`. Total `O(n²)` time, `O(n)`
  space, no FFT, no BLAS. This matters because
  [ADR-0080](./0080-packageprops-per-package-declarations.md) has every package
  under `public/analytics/` declaring `WASMFreestanding: WASMCompiles`, and
  because `gonum` — the one library that would otherwise be reached for — is
  currently a direct dependency used in exactly **one** file
  (`leeway/card/feature_projection.go`). Not widening that is worth something,
  and here it is free.
- **Streaming is exact, not approximate.** The same recurrence extends the
  profile by one row and column per arriving point. For anomaly scoring the
  causally correct variant is the *left discord* — nearest neighbour
  restricted to the past — which is both cheaper and the only version that can
  honestly be deployed online. DAMP computes these exactly at reported
  throughputs up to 300 kHz on commodity desktop hardware.
- **The benchmark history is bad enough to be load-bearing.** Wu and Keogh
  (2021) showed that the datasets the field standardized on carry four
  pervasive flaws — trivial anomalies, unrealistic anomaly density,
  mislabelled ground truth, and anomalies concentrated at the end of the
  series — and that one-line heuristics reach state-of-the-art scores on them.
  Any detector we write is unvalidated until it is scored on data designed to
  avoid those flaws, under a defensible measure. The curated replacement
  benchmark, TSB-AD (NeurIPS'24), settles on VUS-PR.
- **Simple methods win on the corrected benchmark.** On TSB-AD's multivariate
  track, three of the top eight detectors (PCA, KMeansAD, CBLOF) carry no
  learned-model runtime at all. The "no heavy ML dependency" constraint costs
  far less accuracy than it would have appeared to cost pre-2024.
- **The multivariate story has a known ceiling.** Aggregating per-channel
  distance profiles in the mSTAMP manner is nearly free once the profile
  exists, and a 2026 preprint reports it ranking first by mean VUS-PR on
  TSB-AD's multivariate track. The same preprint reports the advantage
  *inverting* with dimensionality — roughly +0.26 VUS-PR at 2–3 dimensions,
  −0.17 at 32–248. One unreviewed source, but the direction is consistent with
  the geometry: nearest-neighbour distances concentrate as `d` grows.

## Design space (QOC)

**Question.** Which family of non-parametric anomaly detector do we implement
in Go, given that it must work on arriving streams, handle multivariate
series, and add no learned-model runtime?

**Options.**

- **O1 — Discord / matrix profile.** STOMP substrate, DAMP for streaming left
  discords, mSTAMP-style pre-sorting aggregation across channels.
- **O2 — Model-of-normality.** SAND / NormA / Series2Graph lineage:
  incrementally clustered subsequences representing normal behaviour, scored
  by distance from the model.
- **O3 — Isolation and projection ensembles.** Half-Space Trees, Robust Random
  Cut Forest, LODA, xStream: score points from random structure, no distance
  computation.
- **O4 — Learned reconstruction models.** AutoEncoder, USAD, OmniAnomaly, CNN
  — the top of the TSB-AD multivariate leaderboard.

**Criteria.**

- **C1 — Streaming exactness and throughput.** Does the online result equal the
  batch result, and at what per-sample cost?
- **C2 — Behaviour as dimensionality grows.** Measured on TSB-AD's multivariate
  track where published.
- **C3 — Dependency and runtime footprint.** Must satisfy ADR-0080's
  WASM-freestanding declaration for `public/analytics/`: pure Go, no cgo, no
  BLAS, no model artifacts.
- **C4 — Explainability of a flag.** Can the detector point at *which*
  subsequence and *which* channels carried the anomaly?
- **C5 — Implementation size in Go.** Rough LOC to a correct, tested version.

**Assessment.** `++` strong positive, `+` positive, `−` negative, `−−` strong negative.

|    | O1 | O2 | O3 | O4 |
|----|----|----|----|----|
| C1 | ++ | +  | ++ | −− |
| C2 | −  | +  | ++ | +  |
| C3 | ++ | +  | ++ | −− |
| C4 | ++ | +  | −  | −− |
| C5 | +  | −  | ++ | −− |

O1 and O3 are complements, not competitors: O1 is exact, interpretable and
subsequence-native but concentrates as `d` grows; O3 is dimension-tolerant and
tiny but scores *points*, so subsequence semantics must be supplied by a
sliding-window embedding. Their failure modes are disjoint.

## Decision

We will implement **O1 as the substrate and O3 as its high-dimensional
complement**, in four leaf packages under `public/analytics/timeseries/`, and
we will build the evaluation harness *before* the second detector:

| Milestone | Package | Content |
|---|---|---|
| **M1** | `matrixprofile` | MASS (direct, no FFT), the STOMP recurrence, exclusion zone, variance floor. Batch, univariate. Exactness tested against a brute-force oracle. |
| **M2** | `adscore` | VUS-PR plus range-based precision/recall, and a synthetic series generator that avoids Wu and Keogh's four flaws by construction. |
| **M3** | `damp` | Streaming left discords on M1's MASS: backward scan, early abandoning against a running best-so-far. |
| **M4** | `matrixprofile` | Subdimensional extension: per-channel profiles, pre-sorting aggregation, MDL selection of the `k`-of-`d` subset. Multivariate. |
| **M5** | `hstrees` | Half-Space Trees over a sliding-window embedding, for the dimensionality range where M4 degrades. |

M2 lands second, before any detector beyond the substrate, deliberately: the
survey's central finding is that a detector measured on flawed data is
indistinguishable from a one-liner, and we would rather discover that about
our own code early.

Scope is **anomaly detection only**. Motif-set discovery — k-Motiflets and the
families around it — is deliberately deferred to a follow-up ADR even though
M1 hands it the substrate for free.

## Alternatives

- **O2 (SAND / model-of-normality).** Rejected for now, not on merit — it is
  the reference point for *distribution drift*, which O1 and O3 both handle
  poorly. Rejected because it is materially more machinery (incremental
  subsequence clustering plus obsolescence policy) for a capability we cannot
  yet demonstrate we need. Revisit when a real series shows drift that DAMP
  mis-scores.
- **O4 (learned reconstruction).** Rejected on a hard constraint: model
  artifacts and an inference runtime break ADR-0080's WASM-freestanding
  declaration for `public/analytics/`, and TSB-AD shows the accuracy premium
  over PCA-class methods to be small.
- **RRCF instead of Half-Space Trees at M5.** RRCF's displacement-based score
  is better justified than half-space mass counts, but it requires tree surgery
  on deletion and is several times the code. Half-Space Trees first; RRCF if
  M5 underperforms on the same harness.
- **PEPA / A-PEPA.** Lead the VLDB'25 motif evaluation on real-data f1 and are
  the most robust methods there to within-motif length variation. Rejected:
  they rest on persistent homology over a subsequence graph — a
  computational-topology dependency, which is exactly the kind of weight C3
  exists to refuse.
- **VALMOD.** The only method in that evaluation that survives extreme
  timescale variation between motifs. Rejected: it crashes at 200,000 samples
  in the published benchmark, and its lower-bound pruning structure is
  substantial. PanMP is the cheaper partial substitute if variable window
  length becomes a requirement.
- **GrammarViz.** The only near-linear method in the field. Rejected: SAX
  discretization makes it the worst performer under both noise and linear
  trend — precisely the two deformations that "works across unlike series"
  means.
- **MERLIN / MADRID.** Remove the window-length parameter entirely, which is
  the one parameter this design still carries. Rejected as premature: both are
  batch or anytime rather than streaming, and a small sweep over `m` recovers
  most of the benefit. Reconsider if window sensitivity becomes the dominant
  source of false positives.
- **FFT-accelerated MASS via `gonum/dsp/fourier`.** Deferred rather than
  rejected. The direct `O(nm)` first distance profile is paid once per profile;
  if profiling shows it dominating, the transform is a contained change behind
  the same function.

## Consequences

### Positive

- One `O(n²)`-time, `O(n)`-space structure answers both standing questions.
  Motif discovery becomes a follow-up ADR with the hard part already built.
- No new module dependency. `gonum` stays confined to its single existing call
  site, and the packages can keep ADR-0080's `WASMFreestanding: WASMCompiles`
  declaration.
- A flagged anomaly is explainable by construction: the detector names the
  offending subsequence, its nearest neighbour, and — after M4 — the channels
  that carried it. That is a materially better fit for a UI-heavy repository
  than a scalar score from a learned model.
- Candidate consumers already exist, though none is wired: the `sysmetrics`
  samplers, the imztop/imzrt dashboards, and discord/motif span overlays on
  the implot widget from
  [ADR-0149](./0149-implot-core-port-painter-lane.md). Wiring is out of scope
  here.
- M2 leaves the repository with a reusable scorer for any future detector,
  including ones this ADR rejects.

### Negative

- **Quadratic batch cost.** `O(n²)` is fine for the series lengths in this
  repository and is not fine for arbitrary archives. The published benchmark
  puts STOMP's practical ceiling around 500,000 samples.
- **The window length `m` stays a parameter.** Deferring MERLIN and MADRID
  means callers must choose it, and the survey records that this is the single
  parameter most published work is trying to eliminate.
- **The multivariate path has a documented ceiling.** M4's advantage is
  expected to invert somewhere above roughly 32 dimensions. M5 exists to cover
  that range, which means two detectors and two calibration surfaces rather
  than one.
- **No drift adaptation.** Neither DAMP nor Half-Space Trees re-baselines when
  normal behaviour shifts. That is O2's contribution and we are declining it
  for now.
- **Anomalies only.** Motif discovery is left on the table despite the
  substrate supporting it, so the repository will briefly hold a structure
  whose minima nothing reads.

### Neutral

- Package sprawl: four leaf packages rather than one. This follows the
  existing shape of `public/analytics/stats`, whose sub-packages (`tdigest`,
  `letterval`, `ecdfbands`) are similarly independent leaves.
- [`github.com/stergiotis/boxer/public/observability/slidingwindow`](../../public/observability/slidingwindow)
  is the obvious ring buffer to reuse, but it is documented as `O(cap)` per
  push — it memmoves rather than tracking a head index. That is fine at a 1 Hz
  sampler cadence and wrong for a detector aiming at kHz rates. Either it gains
  a head-index mode or these packages carry their own buffer; the choice is
  deferred to M3, where the requirement first becomes concrete.
- Naming: `discord` is the field's standard term for the maximally isolated
  subsequence and is unrelated to the chat product. The `damp` package name
  follows the algorithm.

## Status

Accepted 2026-07-30. Implementation started at M1.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers. Subsequent refinements land as dated `## Updates`
entries, not as silent rewrites.

Three sub-decisions were deliberately left open at acceptance, each deferred
to the milestone where it first becomes concrete rather than guessed at now:

1. **Motif readers on M1.** The substrate carries motif information whether or
   not anything reads it. Whether M1 exposes a motif accessor or leaves it for
   the follow-up ADR is a surface question, not a structural one, and does not
   change what M1 computes.
2. **M4's aggregation.** MDL-based subset selection (as mSTAMP specifies)
   removes a caller-facing parameter and adds a failure mode of its own; a
   fixed `k`-of-`d` is predictable and pushes the choice outward. Decide at M4
   against M2's harness rather than in the abstract.
3. **Buffer ownership.** Whether
   [`github.com/stergiotis/boxer/public/observability/slidingwindow`](../../public/observability/slidingwindow)
   gains a head-index mode or these packages carry their own ring is decided at
   M3, where a streaming detector first makes the `O(cap)`-per-push cost real.

## Updates

### 2026-07-30 — M1 implemented; four numerical findings

`public/analytics/timeseries/matrixprofile` carries MASS, the STOMP recurrence,
the exclusion zone and the constant-window rule, tested against a brute-force
oracle that shares no code path with it. The dependency claim held: no new
module, and the package declares `WASMFreestanding: WASMCompiles`.

Four things the decision above assumed, or did not consider, turned out
differently. Property-based testing found all of them; none was visible in the
deterministic fixtures.

1. **Rolling statistics cannot use prefix sums.** Computing window variance as
   E[x²]−E[x]² in one pass is the standard O(n) trick and is unusable here: near
   zero variance it cancels catastrophically, so an exactly constant window
   reports a standard deviation many orders above its true value, reads as real
   structure, and yields a garbage correlation. The implementation computes each
   window's statistics two-pass at O(n·Window) — the same order as the single
   direct distance profile the design already pays, and negligible against the
   O(n²) body.
2. **The constant-window threshold must be relative.** An absolute
   standard-deviation floor silently reclassifies windows when the same signal
   arrives in millivolts rather than volts, which contradicts the scale
   invariance z-normalization exists to provide. The floor is a fraction of the
   series' own standard deviation.
3. **The series is kept in two conditionings.** Centering on the global mean is
   what keeps the dot-product search away from cancellation on a large constant
   offset — but it *hurts* whenever a window's internal variation is tiny
   relative to the whole series' range, because the centered values land where
   the ULP swamps the variation being measured. Neither conditioning dominates,
   so the search runs on centered values while the per-window statistics and the
   refinement pass run on the originals.
4. **Reported distances need a refinement pass.** Through the d = sqrt(2m(1−ρ))
   identity alone, an exactly matching pair reports roughly 1e-6 rather than 0,
   because a correlation error δ surfaces as sqrt(2mδ) of distance. Recomputing
   each reported pair from materialized z-normalized values costs O(n·Window)
   and removes the effect from everything a caller sees.

One limit is documented rather than fixed, because it is inherent to STOMP: on
a series mixing local scales across many orders of magnitude, the centered
representation cannot resolve the small-scale region's shape and the search can
return a genuinely worse neighbour than exists. **Soundness holds
unconditionally** — the reported distance is the true distance to the reported
neighbour, and never understates the true minimum, so the failure mode is
missing a match rather than inventing one. Optimality holds on well-conditioned
input. The test suite asserts these separately.

This also answers open sub-decision 1: M1 does expose motif and discord
readers, since both are single scans over a profile that already exists.
Motif *set* discovery remains deferred.

### 2026-07-30 — M2 implemented; VUS is not calibrated the way an AUC is

`public/analytics/timeseries/adscore` carries VUS-PR and VUS-ROC, the
range-based curve they integrate, the flaw-avoiding fixture generator, and the
triviality check. No new module dependency.

**The sweep is exact and cheaper than the reference.** The published
implementation re-derives the curve per threshold; this one admits positions in
decreasing score order and accumulates true- and false-positive mass
incrementally, including the existence reward, so a full curve costs O(n) once
the sort is paid. Total O(n log n + n·maxBuffer). Tied scores enter together, or
the result would depend on sort stability.

**VUS does not run 0 to 1, and a caller who assumes it does will overstate a
mediocre detector.** Two effects, both measured on our own fixtures:

- *Chance is not 0.5 under VUS-ROC.* Positives are counted as the mean of the
  binary and buffered label mass while true positives are credited against the
  full buffered label, so a random scorer earns recall faster than
  false-positive rate. Measured on 50-sample anomalies: 0.49 at buffer 0, 0.53
  at buffer 25, **0.66 at buffer 100**. The existence reward damps it, not
  removes it.
- *A perfect detector caps near 0.92.* Firing on exactly the labelled extent
  scores 1.0 point-wise, but a wider buffer adds positive mass at positions the
  detector scored 0.

So the usable VUS-ROC band is roughly [0.55, 0.92], not [0.5, 1.0]. VUS-PR is
far less distorted — a random scorer lands about 1.4× the prevalence — which
independently corroborates TSB-AD's choice to lead with VUS-PR. Both are
reported; the package documents the bands.

**The triviality check justified itself immediately, against our own code.**
The first generator — anomalies injected into a pure sine — was solved by a
moving-average residual at VUS-PR 0.45 to 0.76 on every anomaly kind. A pure
periodic background makes any shape change locally obvious, which is Wu and
Keogh's first flaw reproduced by accident. Three changes fixed it: a
quasi-periodic background (incommensurate components, so the waveform never
repeats and a transplanted segment is genuinely wrong), cross-faded segment
edges (an abrupt join is itself locally detectable), and choosing each
transplant donor and phase offset to *match the displaced segment's level and
spread* subject to a correlation ceiling. Best one-liner now scores 0.035 to
0.116 across the four kinds, against 0.57 to 0.63 for the matrix profile.

**Two operational facts about M1 as a detector, both worth more than half the
achievable accuracy.** They surfaced only because M2 existed to measure them,
and both bear on M3:

1. **A window's score belongs at the window's centre.** Leaving it at the start
   displaces every peak by half a window: VUS-PR 0.255 start-aligned against
   **0.587** centre-aligned, same data and window.
2. **The window must track the signal's period, not the expected anomaly
   length.** At window 20 against a period of 50 the profile scores about 0.15
   regardless of alignment — barely above the one-liners — because a window
   shorter than the pattern being violated cannot see the violation.

M3 inherits both: DAMP must decide where a left-discord score lands relative to
its window, and the window-length sensitivity is the parameter MERLIN and MADRID
exist to remove, deferred in the decision above.

## References

- Survey and literature landscape:
  [Non-parametric motif and anomaly discovery in time series](../explanation/timeseries-motif-anomaly-survey.md)
- [ADR-0080: Per-package property declarations (`packageprops`)](./0080-packageprops-per-package-declarations.md)
  — the WASM-freestanding constraint on `public/analytics/`.
- [ADR-0149: porting the ImPlot core to Go on the painter lane](./0149-implot-core-port-painter-lane.md)
  — the plotting surface a future overlay would use.
- Wu, Keogh. *Current Time Series Anomaly Detection Benchmarks are Flawed and
  are Creating the Illusion of Progress.* TKDE 35, 2021.
  <https://arxiv.org/abs/2009.13807>
- Liu, Paparrizos. *The Elephant in the Room: Towards A Reliable Time-Series
  Anomaly Detection Benchmark.* NeurIPS 2024.
  <https://www.paparrizos.org/papers/LiuNeurIPS24.pdf>
- Lu et al. *DAMP: accurate time series anomaly detection on trillions of
  datapoints and ultra-fast arriving data streams.* DMKD, 2022.
  <https://link.springer.com/article/10.1007/s10618-022-00911-7>
- Yeh et al. *Matrix Profile VI: Meaningful Multidimensional Motif Discovery.*
  ICDM 2017. <https://www.cs.ucr.edu/~eamonn/Motif_Discovery_ICDM.pdf>
- Tan, Ting, Liu. *Fast Anomaly Detection for Streaming Data* (Half-Space
  Trees). IJCAI 2011.
- Guerrini et al. *Time Series Motif Discovery: A Comprehensive Evaluation.*
  PVLDB 18(7), 2025. <https://www.vldb.org/pvldb/vol18/p2226-boniol.pdf>
