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

### 2026-07-30 — M3 implemented; DAMP's published speed rests on two things we do not have

`public/analytics/timeseries/damp` carries a streaming `Detector` over left
discords — nearest neighbour restricted to the past — with DAMP's backward
expanding-block scan and early abandoning, plus an exact mode. No new module
dependency. 15 tests, checked against a brute-force left-matrix-profile oracle.

Four findings, two of which qualify the decision above.

1. **Forward pruning is not implementable in a stream.** The published
   algorithm's second optimization looks ahead from the current subsequence and
   prunes future positions already shown to have a close neighbour. Those
   positions have not arrived yet. It is available only when replaying a stored
   series, which is how the paper's throughput figures were produced. This
   package implements the streaming-admissible half and says so.
2. **DAMP does not produce a score vector, and the failure is quiet.** Early
   abandoning stops as soon as a subsequence is shown not to be the discord, so
   most readings carry an upper bound rather than a distance. DAMP answers
   "where is the anomaly" exactly and "how anomalous is each position" not at
   all. Measured on M2's fixtures: VUS-PR 0.458 for a DAMP score vector against
   0.558 for the same detector in exact mode. Both look like score vectors.
   The package therefore ships an exact mode and marks every reading with
   whether its score is real.
3. **The FFT deferral's trigger is met.** The decision above deferred
   FFT-accelerated MASS on the grounds that the batch path pays it once. That
   reasoning does not carry to M3, where a block scan is the hot loop at
   O(block·Window) rather than O(block·log block). Benchmarked at Window 50 over
   an 8000-sample horizon: **57k samples/s under DAMP, 19k exact** — early
   abandoning is worth about 3×, and the whole figure sits roughly 5× below the
   published one, consistent with the missing transform. Adopting it would widen
   `gonum` from one call site to two without adding a module; it remains
   deferred pending a decision rather than taken silently.
4. **The argmax is a bracket, not a hit, when the window outruns the anomaly.**
   A 50-sample window over a 20-sample anomaly peaks at the window starting just
   before the novel content enters, so its centre lands past the anomaly's
   trailing edge and neither endpoint falls inside the event. This does not
   contradict M2's centre-attribution finding — a per-position scorer integrates
   the whole plateau of overlapping windows, which is centred — but code reading
   a single reading should read the plateau.

Two smaller notes. `Series` in M1 is immutable, so M3 realizes the same
primitive incrementally rather than calling it; "on M1's MASS" in the decision
above should be read as sharing the conventions, not the code. And **open
sub-decision 3 is resolved**: these packages own their buffer.
[`github.com/stergiotis/boxer/public/observability/slidingwindow`](../../public/observability/slidingwindow)
was not adopted and does not need a head-index mode on this account — the dot
product needs its history *contiguous*, which a ring would not give, so the
right structure is an append-and-compact slice at amortized O(1).

### 2026-07-30 — the FFT was implemented and measured; the M3 entry above was wrong about it

The M3 entry treats FFT-accelerated MASS as the explanation for the gap to the
published throughput, and as a change worth making. Implemented and benchmarked,
neither holds at the window lengths that matter. Correcting it here rather than
editing that entry, per the edit policy.

Both scan methods now ship — `ScanMethodDirect`, `ScanMethodTransform`, and
`ScanMethodAuto` — and are tested to agree on every reading and on the discord.
Measured over the same benchmark (8000-sample horizon), samples/s:

| Window | direct | transform | |
| ---: | ---: | ---: | --- |
| 16 | 365,671 | 88,393 | direct 4.1× faster |
| 50 | 57,405 | 19,744 | direct 2.9× faster |
| 128 | 12,981 | 9,446 | direct 1.4× faster |
| 256 | 3,474 | **5,256** | transform 1.5× faster |
| 512 | 1,097 | **2,948** | transform 2.7× faster |

The crossover sits between 128 and 256, so `TransformMinWindow` is 256 and
`ScanMethodAuto` picks per block. Below that the transform is a substantial
loss: three length-N passes of complex butterflies over a power-of-two padded
buffer do not compete with a few hundred fused multiply-adds over contiguous
memory, and `O(N log N)` does not care. The earlier estimate of a ~6× *gain* at
Window 50 came from counting operations and ignoring constants; the measured
result there is a 2.9× loss.

Three things follow:

1. **The deferral in the decision above was right for the wrong reason, and is
   now closed on evidence.** The transform is adopted, but only above a measured
   threshold, and it is inert at the window lengths this repository's fixtures
   use.
2. **The gap to the published figures is not the missing transform.** At Window
   50 an FFT-based MASS would widen it. Forward pruning — unavailable to a
   stream, per the M3 entry — remains the explanation we can point at, along
   with the usual hardware and data differences that make the comparison loose
   in the first place.
3. **`gonum` now has a second call site**, `dsp/fourier`, still with no new
   module and no loss of the WASM-freestanding declaration. Given the measured
   benefit is confined to Window ≥ 256, removing it again and capping the
   package at direct scanning would be a defensible simplification if that
   regime never materializes.

### 2026-07-30 — re-measured on an unthrottled CPU; figures corrected, conclusions unchanged

The two entries above were benchmarked while the machine sat in a power-saving
frequency governor. Re-run with the governor at `performance` (boost enabled,
`amd-pstate-epp`), two repetitions, agreeing to within 2%:

| Window | direct | transform | |
| ---: | ---: | ---: | --- |
| 16 | 566,000 | 96,000 | direct 5.9× faster |
| 50 | 60,700 | 21,600 | direct 2.8× faster |
| 128 | 14,300 | 10,300 | direct 1.4× faster |
| 256 | 3,800 | **5,740** | transform 1.5× faster |
| 512 | 1,190 | **3,240** | transform 2.7× faster |

DAMP against exact mode at Window 50 is 64k samples/s against 21k, so early
abandoning is worth 3.0×.

Everything moved up by about 10% except the Window 16 direct case, which gained
55% — it is by far the shortest-running benchmark, so it was the one the
frequency ramp had least time to catch up with. **Every conclusion drawn above
survives**: the crossover still sits between 128 and 256, `TransformMinWindow`
stays 256, and the transform is still a 2.8× loss at Window 50. The ratios were
the load-bearing part and they barely moved, which is what one would expect from
a frequency effect applied to both arms.

Recorded because the earlier figures are quoted in the package documentation and
in the entries above, and because "benchmarked under an unnoticed governor" is a
failure mode worth naming rather than quietly patching.

### 2026-07-31 — first evaluation on data nobody synthesised; the detector does not clear the one-liners

Every accuracy figure above comes from fixtures this repository generated, which
is a real gap: a generator and a detector written by the same hand can agree
about a signal that does not exist. `public/analytics/timeseries/loadstudy`
closes it, and the result is unflattering.

**What it reads.** Host load from ClickHouse's own `system.asynchronous_metric_log`
at 1 Hz — CPU split three ways, run-queue depth, resident memory, block IO both
directions, inbound network — correlated against application lifecycle and run
events from `boxer.facts`. Not from
[`github.com/stergiotis/boxer/public/observability/sysmetrics`](../../public/observability/sysmetrics),
which would have been the natural source: **that scraper publishes to NATS and
persists nothing**, so its data does not exist to analyse. Nor from `boxer.facts`
itself, which carries no numeric payload at all.

**The result.** Across five gap-free spans on two grids, the left-discord
detector beats Wu and Keogh's one-liner baselines on **some** channels at
**some** windows, and loses on most. On the longest span it is behind the
baseline on six of eight channels at every window tried. On the one span where
it leads clearly it does so on seven of eight channels, at a single window.
There is no configuration at which it is reliably ahead.

The honest reading is **not** "the detector is bad". It is that *this evaluation
cannot demonstrate its value*, for reasons that are properties of the data:

- Events are not anomaly labels. A detector can be right and score badly here,
  because a detection with no event near it counts against precision.
- Label prevalence ran from about 2% to about 38% depending on the span. At the
  top of that range VUS-PR near 0.5 is close to chance, so the span says nothing.
- The spans are short — hours, not days.

**The `event_rate` channel proved the baseline comparison earns its place.**
Binning the events into a rate series and scoring it produced a *baseline*
VUS-PR above 0.8 on every span, far beyond anything the detector reached. That
is not a finding about detection; the labels are derived from the events, so a
one-liner on the event rate is reading the answer key. Had we reported the
detector's number on that channel without the baseline beside it, we would have
reported a leak as a success.

**Two answers to open questions.**

1. **Preferred window, in wall-clock, is roughly five to sixteen minutes** — the
   ten-second grid favoured windows of 32–64 bins, the sixty-second grid 16. So
   the window is not arbitrary on real data, and the M2 finding that window
   choice dominates accuracy survives contact with it.
2. **The transform's regime is unreachable here.** No gap-free span was long
   enough to test a window of 256 at all, let alone need one. That is evidence
   about this host's recording rather than about signal structure, but it is the
   only evidence we have, and it weakens the case for keeping the FFT path.

**One detector behaviour worth recording.** On the sparsest span, scores went
flat above window 16 — every window reporting nearly the same value. A left
discord is dominated by the first genuinely novel stretch after training, and
once that sets the running maximum the remaining variation is small enough that
ranking carries little information. Streaming discords appear to need a longer
warm-up on real data than a synthetic fixture suggests.

**The harness found two bugs in itself before it found anything about the data**,
both of the kind that yield confident wrong answers rather than errors:

- ClickHouse renders `DateTime` in the *server's* timezone. Reading a timestamp
  back and re-parsing it as UTC shifted every window by the server's UTC
  offset — far enough to select a neighbouring stretch of real data and look
  entirely plausible.
- `rowNumberInAllBlocks()` does not respect a subquery's `ORDER BY` once the scan
  is split across blocks, so the gap-free-run detector over-reported run lengths
  roughly three-fold. Runs are now delimited with `lagInFrame` over an explicit
  window frame.

Both are guarded by an invariant in the study: a span reported gap-free must
extract with zero forward-filled bins on the channel the span was detected from.

**What would make a better study.** Continuous recording. The limiting factor
throughout was that the host records intermittently — the longest usable span was
a few hours. Giving `sysmetrics` a persistence path (declined at acceptance, and
still not obviously worth it on its own) would produce dense, continuous,
per-process data and turn this from a suggestive exercise into a measurement.

### 2026-08-02 — M4 implemented; the aggregation direction, and a cheap alternative that matches it

`public/analytics/timeseries/matrixprofile` gains `MultiSeries` / `MultiProfile`:
mSTAMP over the existing STOMP recurrence, computing the k-dimensional profile
for every k from 1 to d at once, with the selected channel subset recorded per
position and MDL selection of k for motifs. No new module dependency. The
univariate recurrence was extracted into a shared row scanner so both paths
drive one transcription of it; with d = 1 the multivariate path now reproduces
the univariate profile bit for bit, which is asserted rather than assumed.

The channel subset is a `uint64` bitmask, so d is capped at 64. That is well past
where the method stops being the right choice — the survey records its advantage
inverting somewhere between a handful of channels and a few dozen.

**The aggregation direction was got wrong first, and measurement corrected it.**
mSTAMP averages the k *smallest* per-channel distances. For an anomaly confined
to some channels that reads backwards — the unaffected channels still match well,
so the k smallest look like the channels where nothing happened — and an
aggregation over the k *largest* was implemented on that argument. Measured, it
lost at every k on every fixture kind, by three to seven times, and it was
removed rather than shipped beside the better option. The intuition ignores that
the neighbour is chosen *jointly*: an anomaly does not have to make the selected
channels look bad, it only has to remove them from the pool, after which the
remaining channels must find a single position that suits all of them. The
published aggregation is right, for a reason the paper does not spell out
because the paper is about motifs.

**Swept over k, accuracy peaks at the number of affected channels.** On composed
multivariate fixtures — five channels, an anomaly in two — VUS-PR by k runs
0.057, **0.303–0.527**, 0.16–0.20, 0.12–0.21, 0.09–0.15 across the four anomaly
kinds, peaking at k = 2 in all four. So the affected-channel count can be read
off the sweep rather than supplied, which is the anomaly-side counterpart of MDL
recovering a motif's dimensionality. Both ends of the sweep are bad and for
opposite reasons: k = 1 finds some channel matching somewhere and sits at chance,
k = d dilutes the affected channels among all the rest. **A factor of ten across
k on one series makes k the dominant parameter here**, as window length was at M2.

**The result that does not flatter this code**: the obvious cheap alternative —
run d univariate profiles, keep the largest score at each position, same
O(d·n²) — scores 0.53–0.59 against the joint profile's 0.30–0.53, matching or
beating it on every kind. On these fixtures the subdimensional machinery buys
the channel subset and the count, **not accuracy**. The fixtures' channels are
mutually independent, which is the worst case for a joint nearest neighbour and
the best case for treating channels separately, so this is a bound rather than a
verdict. The missing experiment is a correlated-channel fixture; it is deferred
rather than done, because building one means an anomaly injector for
multivariate series, which is its own design question and would have gated M4 on
its hardest peripheral piece.

**The MDL formula does not match the widely used reference implementation.** The
paper's worked example is unambiguous — a pair of ten-sample series at 4 bits
costs 80 bits stored directly and 50 stored as reference-plus-difference — and 50
is one raw side plus one difference. The reference implementation omits the raw
side, which makes a matched channel cost only its difference and biases the
argmin toward larger k. The paper's arithmetic is implemented here. Two further
choices are ours: the difference width is taken per channel rather than pooled
across the selected ones, and it comes from the difference's *range* rather than
a count of its distinct values. The count degenerates — at most m distinct values
occur among m samples, so on any window shorter than 2^bits every channel
compresses and the argmin is always d.

Two smaller notes. `Profile.PositionScores` and `MultiProfile.PositionScores`
now do the centre attribution that M2 measured as worth more than half the
achievable accuracy; it had been hand-rolled at each call site, and the copy in
adscore's tests is gone. And the monotonicity of the profile in k holds exactly
on the values the search ranks, but each k selects and refines its neighbour
independently, so on a series mixing local scales across many orders of
magnitude two adjacent k can cross by far more than rounding — measured at 0.34
against a window of 3. That is the identity's documented behaviour, not the
aggregation's.

**M5 `hstrees` is next**, unchanged.

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
