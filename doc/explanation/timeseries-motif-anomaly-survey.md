---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Compiled 2026-07-30 from published
> papers and their artifact repositories. Provenance is uneven and marked
> per claim: findings from the VLDB'25 motif-discovery evaluation were read
> from the paper itself (methodology, Table 2, RQ1–RQ6); the TSB-AD leaderboard
> ordering and the 2026 matrix-profile preprint numbers were read from
> abstracts, leaderboards and a rendered preprint rather than reproduced. No
> algorithm below has been run against boxer data. Treat the rankings as a
> literature summary, not a measurement.

# Non-parametric motif and anomaly discovery in time series — a survey

Two questions recur over any recorded signal: *what repeats* (motifs) and
*what does not fit* (anomalies). This document surveys the algorithms that
answer them **without a trained model** — no labels, no fitted parametric
distribution, no per-domain feature engineering — and records what the
published evaluations actually establish about which ones hold up across
unlike time series. It exists because the field's benchmark history is bad
enough that the naive reading of a leaderboard is misleading, and because the
constraint set that matters here (streaming, multivariate, no ML runtime) cuts
the candidate space very differently from the constraint set most papers
optimize for.

The decision this survey informs is
[ADR-0150](../adr/0150-timeseries-subsequence-anomaly-detection.md). This file
covers the landscape; the ADR covers what we picked and why.

## 1. The shared primitive

Nearly everything below reduces to one operation: the **z-normalized Euclidean
distance between subsequences**.

Given a series `S` of length `n` and a window length `m`, the subsequence
`S[i:i+m]` is z-normalized (mean subtracted, divided by standard deviation)
before comparison. Normalization is what makes the measure amplitude- and
offset-invariant, which is what lets one algorithm work on a heartbeat trace
and a power meter without retuning. It is also the source of a well-known
failure mode: over a flat region the standard deviation approaches zero and
normalization amplifies noise into structure. Methods that skip normalization
(LoCoMotif) misidentify flat areas as motifs; methods that apply it need a
variance floor.

The **matrix profile** is the vector holding, for every subsequence, the
distance to its nearest non-trivial neighbour, plus the index of that
neighbour. From this single structure:

- the **minima** are motifs — the pair (or set) that repeats;
- the **maxima** are *discords* — the subsequence least like anything else,
  which is the standard non-parametric definition of an anomaly.

That two-for-one property is why the matrix profile lineage dominates the
practical literature even where it does not top accuracy tables.

### Why it is affordable

The naive computation is `O(n²m)`. Two observations remove the `m`:

1. **MASS.** One distance profile (one subsequence against all others) is a
   sliding dot product, computable by FFT in `O(n log n)` regardless of `m`.
2. **The STOMP recurrence.** Adjacent dot products differ by two terms:
   `QT[i,j] = QT[i-1,j-1] - t[i-1]·t[j-1] + t[i+m-1]·t[j+m-1]`. So only the
   *first* distance profile needs a transform; every subsequent one is `O(1)`
   per cell.

Total: `O(n²)` time, `O(n)` space, and — load-bearing for a dependency-averse
codebase — **no FFT at all** if the first distance profile is computed
directly in `O(nm)`.

Streaming falls out of the same recurrence. `STOMPI` / `stumpi` extend the
profile by one column and one row per arriving point in `O(n)`, maintaining
motifs and discords *exactly* rather than approximately. Anomaly detection can
do better still by restricting the nearest-neighbour search to the past — a
**left discord** — which is both causally correct for online use and cheaper.

## 2. Motif discovery

### 2.1 The problem is not one problem

There is no agreed definition of "motif", and the disagreements are not
cosmetic. The literature spans at least five formulations — K-Motifs (2002),
k-th Motif Pair (2009), Range Motif (2009), Variable Length Motif (2018),
Top-k Motiflet (2023) — which split into two families of problem:

- **Motif Pair Discovery** — find the two most similar non-overlapping
  subsequences. Well-posed, exactly solvable, and about 35% of published
  methods. Rarely what an application wants.
- **Motif Set Discovery** — find sets of subsequences covering *every*
  occurrence of each distinct repeated pattern. About 65% of methods, better
  aligned with real use, and deliberately less formal.

Pair methods are usually promoted to set methods by post-processing: find the
pair, then collect everything within a radius of it. That promotion is where
the radius parameter `r` enters, and `r` is the single hardest thing to set in
this whole area.

### 2.2 Three families

Guerrini et al. (VLDB'25) classify 55 published methods into three families
and benchmark 11 of them. The taxonomy is the useful part:

| Family | Idea | Representatives |
|---|---|---|
| **Frequency-based** | Count matches falling inside a radius `R`; keep the highest-cardinality sets | SetFinder, LatentMotif |
| **Similarity-based** | Minimize distance within a set, independent of how many occurrences it has | STOMP, VALMOD, PanMP, k-Motiflets, PEPA, A-PEPA |
| **Encoding-based** | Pick the subsequences that best *compress* the series (MDL, grammar induction, autoencoding) | GrammarViz, MDL-Clust, LoCoMotif |

Most recent work is similarity- or encoding-based. Within similarity-based,
most descends from the matrix profile.

### 2.3 What the benchmark establishes

Setup: 8 real labelled datasets (569 series), plus a synthetic generator that
varies **one** challenge at a time; f1-score over range-based
precision/recall with Hungarian matching between predicted and true motif sets
at a 50% overlap threshold; 20,000 s timeout.

The headline is a negative result, stated plainly by the authors: **no single
method addresses all challenges effectively**, and the variation between
datasets exceeds the variation between methods. Beyond that:

**Real data (RQ1).** PEPA, A-PEPA, STOMP and SetFinder are slightly ahead by
critical-difference diagram. Every method except GrammarViz performs well on
at least one dataset. Isolating a single best method is not possible.

**Scale (RQ2).** Only GrammarViz and LatentMotif grow near-linearly.
Everything else is quadratic; SetFinder is cubic. Hard limits observed:
LoCoMotif crashes at 150,000 samples, VALMOD at 200,000, SetFinder times out
at 200,000. STOMP, PanMP, MDL-Clust, PEPA and A-PEPA reach 500,000.

**Many distinct motifs (RQ3).** Fixed-radius methods (SetFinder, LatentMotif)
degrade as motif count rises — the radius that suited two motifs suits ten
badly. Methods that derive the radius from the motif pair's own distance
(STOMP, PanMP, VALMOD) are less affected; VALMOD is stable. All of them still
require `K`, the number of motifs, as input.

**Occurrence counts (RQ4).** Robust to how many occurrences a motif has:
VALMOD, PanMP, STOMP, GrammarViz, LatentMotif, SetFinder. Robust to *imbalance*
between motifs: VALMOD, STOMP, PanMP. k-Motiflets is the interesting exception
— it performs *better* off the balanced diagonal, because its design targets
the top-1 motif for a fixed occurrence count.

**Length variation (RQ5).** Two distinct challenges, two distinct winners:

- *Within* a motif (occurrences stretched by temporal deformation): PEPA is
  most robust, then A-PEPA and SetFinder. STOMP, PanMP, LatentMotif and
  k-Motiflets all degrade even at slight variation, because they enforce a
  uniform occurrence length.
- *Across* motifs (patterns at genuinely different timescales): MDL-Clust,
  STOMP and k-Motiflets handle small variations; PEPA, A-PEPA and VALMOD
  handle larger ones; **only VALMOD** survives extremes.

**Deformation and noise (RQ6).** Under a linear trend (random-walk
background), SetFinder is consistent, and k-Motiflets, PEPA and LoCoMotif stay
correct even at high amplitude — PEPA because its LT-normalized Euclidean
distance is explicitly trend-invariant. Under Gaussian noise (SNR 75 down to
1.5), STOMP, PanMP and k-Motiflets hold up best; VALMOD or PEPA are
serviceable at moderate noise. GrammarViz declines sharply and MDL-Clust
gradually — both because SAX discretization is what noise destroys first.

### 2.4 Reading across the results

Three things generalize:

1. **Parameter burden discriminates more than accuracy does.** Nearly every
   method needs `K`. The radius `R` is worse: unmeasurable in advance,
   dataset-dependent, and the thing fixed-radius methods fail on. k-Motiflets'
   contribution is largely that it swaps `R` for an occurrence count `k`, which
   a domain expert can actually supply; A-PEPA's is that it estimates the
   number of motif sets itself.
2. **Discretization buys scale and costs robustness.** GrammarViz is the only
   near-linear method and the worst under exactly the two deformations
   (noise, trend) that "works over a wide range of time series" implies.
3. **The methods that top real-data f1 are not the ones that scale.** PEPA and
   A-PEPA lead on accuracy and rest on persistent homology over a subsequence
   graph — a computational-topology dependency, not a hundred lines of
   arithmetic.

### 2.5 Multivariate motifs

The evaluation above is entirely univariate; the authors say so explicitly.
The multidimensional literature is much thinner and centres on one insight
from **mSTAMP** (Matrix Profile VI, 2017): in a *d*-dimensional series a motif
usually manifests in a *subset* of dimensions, so the algorithm computes a
`k`-dimensional distance profile for every `k` from 1 to *d* and uses MDL to
choose `k`. Mechanically this is a per-dimension sort of the distance profiles
followed by a cumulative mean — cheap on top of a matrix profile that already
exists.

**LAMA** (Leitmotifs, VLDB'25) is the current refinement: it selects
sub-dimensions and motif set *jointly* rather than sequentially, and auto-tunes
both motif length and set size.

## 3. Anomaly detection

### 3.1 Read the benchmark critique first

Wu and Keogh (2021) examined the datasets the field had standardized on and
found four flaws pervasive across them: **trivial anomalies**, **unrealistic
anomaly density**, **mislabelled ground truth**, and **a high proportion of
anomalies at the end of the series** (a run-to-failure recording artifact,
which rewards any detector with a positive time bias). Their demonstration was
that *one-line* heuristics — the difference between consecutive points, a
moving average — reach state-of-the-art scores on those datasets.

The consequence for anyone choosing an algorithm today: published accuracy
predating the correction is close to uninformative, and any new detector must
be validated against test data designed to avoid these four flaws or the
validation says nothing.

**TSB-AD** (NeurIPS'24) is the response — a curated benchmark of 350
univariate and 180 multivariate sequences with the known flaws removed,
evaluating 40 detectors under VUS-PR, which the authors identify as the most
reliable of the candidate measures. Its finding, consistent with the critique:
**simple statistical methods are competitive with or better than deep
architectures**. On the multivariate track the published ordering runs CNN,
OmniAnomaly, PCA, LSTMAD, USAD, AutoEncoder, KMeansAD, CBLOF — three of the
top eight (PCA, KMeansAD, CBLOF) carry no learned-model runtime at all.

A 2026 preprint (MMPAD) reports a matrix-profile method ranking first by mean
VUS-PR on the multivariate track (0.3548 against CNN's 0.3130), using
pre-sorting aggregation across dimensions in the mSTAMP manner. The same
preprint reports the method's limit honestly: its advantage **inverts with
dimensionality**, from roughly +0.26 VUS-PR on 2–3 dimensional datasets to
−0.17 on 32–248 dimensional ones. Single unreviewed source; the direction of
the dimensional trend is more useful than the absolute numbers.

### 3.2 The families

**Discord-based** — anomaly as "the subsequence with the most distant nearest
neighbour". Non-parametric by construction, and the direct dual of motif
discovery.

- **DAMP** computes *left* discords — nearest neighbour restricted to the past
  — on arriving streams, exactly, with backward scanning and early abandoning
  against a running best-so-far. The reported throughput is up to 300 kHz on
  commodity desktop hardware. This is the strongest streaming univariate
  option in the literature by a wide margin.
- **MERLIN / MERLIN++** remove the remaining parameter: they call DRAG
  repeatedly with adaptively chosen `r` to find discords of *every* length,
  exactly. Batch only.
- **MADRID** (ICDM'23) is the anytime all-lengths successor, motivated
  explicitly by DAMP's sensitivity to the window parameter.
- **Generalized discords** (KDD'25) relax the fixed-subsequence-length
  assumption further.

**Model-of-normality** — build a compact representation of normal behaviour,
score by distance from it.

- **SAND** (VLDB'21) maintains an incremental clustering of subsequences,
  adapting to distribution drift and discarding obsolete data. It beats STAMPI
  and matches its own batch siblings, Series2Graph and NormA. It is the
  reference point for drift handling.

**Isolation and projection ensembles** — score points, not subsequences, but
handle *d* dimensions natively with bounded memory.

- **Half-Space Trees** (2011): a one-class detector over evolving streams
  built from random axis-aligned splits with mass counts — no distance
  computation anywhere, constant work per point.
- **Robust Random Cut Forest** (2016): random cut trees maintained as a
  dynamic sketch of the stream, with a principled displacement-based score.
- **LODA**, **xStream**: ensembles of weak random-projection detectors;
  xStream adds half-space chains for evolving feature spaces.

These are the only family that takes high dimensionality in stride. The cost
is that they see *points*: subsequence semantics have to be supplied by
feeding them a sliding-window embedding, which reintroduces a window
parameter through the back door.

## 4. The constraint intersection

For the three constraints that matter here — **streaming**, **multivariate**,
**no ML runtime** — the literature is genuinely thin, and it is worth being
explicit about why rather than papering over it.

| | Univariate | Multivariate |
|---|---|---|
| **Batch** | Dense. MERLIN, MADRID, every motif method in §2. | Sparse but real: mSTAMP, LAMA, Sub-PCA, KMeansAD. |
| **Streaming** | DAMP (exact, fast), SAND (drift-aware), STOMPI. | **Thin.** Isolation/projection ensembles score points, not subsequences. Subdimensional matrix profiles degrade with `d`. |

The bottom-right cell is the gap. The two working recipes the literature
supports are:

1. **Sliding-window embed → dimensionality reduction → streaming point
   detector.** Recovers subsequence semantics for RRCF or Half-Space Trees at
   the cost of a window parameter and a whitening step. PCA whitening is
   listed as *future work* in the matrix-profile preprint, not a settled step.
2. **Per-channel incremental matrix profile → cross-channel aggregation.**
   Exact, interpretable (you can point at the offending subsequence and the
   dimensions that carried it), and cheap given the profile — but it is
   precisely the approach whose advantage inverts above ~32 dimensions.

They fail in different places, which is the argument for carrying both rather
than choosing.

## 5. What the evidence does not settle

- **No cross-family evaluation exists for motifs *and* anomalies together**,
  despite both deriving from one structure. The motif evaluation and TSB-AD
  share no methods, no metrics and no data.
- **Streaming accuracy is largely unmeasured.** The matrix-profile preprint
  on TSB-AD explicitly evaluates no streaming setting. Claims about online
  behaviour rest on the exactness argument (STOMPI maintains what STOMP would
  have computed) rather than on measurement under drift.
- **Multivariate motif discovery has no equivalent of the VLDB'25
  evaluation.** Method-vs-method claims there rest on individual papers'
  self-reported comparisons.
- **VUS-PR is a young consensus.** It is the best-justified measure currently
  available, not a settled one; competing evaluation proposals continue to
  appear.

## 6. Invariants worth preserving in any implementation

- **Z-normalization needs a variance floor.** Below it, declare the
  subsequence constant rather than dividing. Without this, flat regions become
  the loudest signal in the series.
- **Trivial matches must be excluded.** A subsequence's nearest neighbour is
  otherwise itself shifted by one sample. The standard exclusion zone is
  `m/4` on either side; motif *and* discord results are meaningless without it.
- **Streaming anomaly scores must be causal.** Use left discords. A
  bidirectional nearest neighbour lets the future explain the present, which
  scores well offline and cannot be deployed.
- **A detector validated on flawed data is unvalidated.** Test series must
  avoid Wu and Keogh's four flaws by construction, including the end-of-series
  bias, which a naive synthetic generator reproduces by accident.

## Further reading

- Guerrini, Germain, Truong, Oudre, Boniol. *Time Series Motif Discovery: A
  Comprehensive Evaluation.* PVLDB 18(7), 2025.
  <https://www.vldb.org/pvldb/vol18/p2226-boniol.pdf> — artifacts at
  <https://github.com/grrvlr/TSMD>
- Schäfer, Leser. *Motiflets — Simple and Accurate Detection of Motifs in Time
  Series.* PVLDB 16(4), 2022. <https://www.vldb.org/pvldb/vol16/p725-schafer.pdf>
- Schäfer, Leser. *Discovering Leitmotifs in Multidimensional Time Series.*
  PVLDB 18, 2025. <https://www.vldb.org/pvldb/vol18/p377-schafer.pdf>
- Yeh et al. *Matrix Profile VI: Meaningful Multidimensional Motif Discovery.*
  ICDM 2017. <https://www.cs.ucr.edu/~eamonn/Motif_Discovery_ICDM.pdf>
- Wu, Keogh. *Current Time Series Anomaly Detection Benchmarks are Flawed and
  are Creating the Illusion of Progress.* TKDE 35, 2021.
  <https://arxiv.org/abs/2009.13807>
- Liu, Paparrizos. *The Elephant in the Room: Towards A Reliable Time-Series
  Anomaly Detection Benchmark.* NeurIPS 2024.
  <https://www.paparrizos.org/papers/LiuNeurIPS24.pdf>
- Lu et al. *DAMP: accurate time series anomaly detection on trillions of
  datapoints and ultra-fast arriving data streams.* DMKD, 2022.
  <https://link.springer.com/article/10.1007/s10618-022-00911-7>
- Nakamura et al. *MERLIN++: parameter-free discovery of time series
  anomalies.* DMKD, 2023.
  <https://link.springer.com/article/10.1007/s10618-022-00876-7>
- Boniol, Paparrizos, Palpanas, Franklin. *SAND: Streaming Subsequence Anomaly
  Detection.* PVLDB 14(10), 2021.
  <http://www.vldb.org/pvldb/vol14/p1717-boniol.pdf>
- Guha, Mishra, Roy, Schrijvers. *Robust Random Cut Forest Based Anomaly
  Detection On Streams.* ICML 2016.
  <https://proceedings.mlr.press/v48/guha16.pdf>
- Boniol et al. *Dive into Time-Series Anomaly Detection: A Decade Review.*
  2024. <https://arxiv.org/html/2412.20512v1>
- Decisions: [ADR-0150: streaming subsequence anomaly detection in Go](../adr/0150-timeseries-subsequence-anomaly-detection.md)
- Existing statistics in-tree:
  [`github.com/stergiotis/boxer/public/analytics/stats`](../../public/analytics/stats)
