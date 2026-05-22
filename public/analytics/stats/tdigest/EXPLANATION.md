---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# tdigest — Explanation

`tdigest` is a streaming quantile sketch that maintains a compact summary
of a stream of `float64` observations and answers `Quantile(q)` and
`CDF(x)` queries with rank error that *decreases* near the tails. It
sits next to [`StreamStats`](../stats.go) in `public/analytics/stats/`:
where `StreamStats` tracks moments (mean, variance, skewness, kurtosis),
`tdigest` tracks the shape of the distribution itself.

## Background

A naive way to compute the median or 99th-percentile of a stream is to
keep every observation and sort. For a million samples that is 8 MiB of
float64 data per group; for telemetry pipelines with millions of groups
or unbounded retention this is unworkable.

Three families of streaming quantile sketches dominate the literature:

- **GK** (Greenwald & Khanna, 2001) — rank-error sketches with
  deterministic bounds. Not cleanly mergeable across shards.
- **KLL** (Karnin, Lang & Liberty, 2016) — randomized,
  optimally-sized for *uniform* rank error across the distribution.
- **t-digest** (Dunning, 2019; arXiv:1902.04023) — clusters
  observations into centroids whose size is governed by an asymmetric
  scaling function, giving rank error proportional to `q·(1-q)`. That
  is: tighter at the tails (q near 0 or 1), looser in the body.

For dashboards focused on the body of the distribution KLL is arguably
the better fit; for SLO-style work and **letter-value plots** — where
tail quantiles drive the rendering — t-digest is the right primitive.
ClickHouse uses the same algorithm for its `quantileTDigest` aggregate,
which lets server-side state be transferred and merged client-side
without re-collecting the raw observations.

## How it works

The digest holds an ordered array of *centroids* `(m_i, w_i)` — each
centroid is the weighted mean of a contiguous range of sorted
observations. Together with the exact `min` and `max` of the stream,
the centroids form a piecewise-linear approximation of the empirical
CDF.

### k-scale and the merge constraint

The asymmetric scaling function

    k1(q) = δ / (2π) · arcsin(2q − 1)

maps a quantile `q ∈ [0, 1]` to a k-position `k ∈ [−δ/4, +δ/4]`.
Its derivative `dk/dq = δ / (2π · √(q(1−q)))` diverges at 0 and 1,
which forces centroids to be narrow there. A centroid that spans
quantile interval `[q_left, q_right]` is admissible iff

    k1(q_right) − k1(q_left) ≤ 1

Adjacent admissible centroids tile the k-scale, so the total centroid
count is bounded above by `δ/2 + 1`. For the default `δ = 100` that is
about 50 centroids — independent of `n`.

### Push / compress

New observations land in an unsorted buffer of capacity `5·δ`. When the
buffer fills (or a query forces a flush), `compress` runs:

1. Sort the buffer by mean.
2. 2-way-merge the buffer into the existing centroid array, walking
   left-to-right and *greedily* merging consecutive centroids whose
   combined `k1` span stays ≤ 1.
3. Swap the result into place.

`Push` is therefore amortised `O(log δ)` per observation — the buffer
sort dominates and runs once every `5·δ` pushes.

### Quantile and CDF interpolation

`Quantile(q)` walks centroids until it finds the one straddling the
target rank `q·W`, then linearly interpolates between centroid
*centers* — anchor points at rank `Σ_{j<i} w_j + w_i/2`. The two end
anchors are `(rank=0, value=min)` and `(rank=W, value=max)`, which
preserves the exact extrema in the tails.

`CDF(x)` is the dual: binary-walk the centroid array by mean, then
linearly interpolate the rank, giving `q = rank / W`.

The two operations are inverses up to FP precision:
`Quantile(CDF(x)) ≈ x`.

### Merge across digests

Two digests over disjoint streams can be combined without revisiting
the raw observations: the centroids of `other` are staged through
`inst`'s buffer, then a single `compress` re-applies the k1 constraint
across the union. The exact `min`, `max`, and observation count are
propagated explicitly so the merge result remains accurate at the
tails and reports a true `n`.

## Invariants

- `means` is sorted ascending after every `compress`.
- `len(means) == len(weights)` at all times.
- `min ≤ means[0]` and `means[len-1] ≤ max`, both exact (not estimated).
- `Σ weights[i] + Σ bufWeights[j] == totalWeight` (no rounding drift in
  this invariant — the running sum is updated by addition only).
- `Quantile(q)` is monotone non-decreasing in `q`, up to float64
  precision at extreme magnitudes (see the `FuzzTDigest` relative-slack
  bound).
- `Quantile(0) == min`, `Quantile(1) == max` exactly.
- The compressed centroid count is bounded by `⌈δ/2⌉ + 1`.

## Trade-offs

- **Tail bias is a feature, not a bug.** Anyone who wants uniform rank
  error should reach for a KLL sketch instead. Letter-value plots,
  P99/P999 SLOs, and distributional anomaly detection benefit; smooth
  body-of-distribution viewers may not.
- **No bounded rank error in the body.** The k1 scaling function only
  bounds errors by `q·(1-q)`. For median-style queries, expect ~1–2%
  rank error at `δ = 100`.
- **NaN and ±Inf are silently dropped.** Quantile sketches over
  non-finite data have no meaningful definition; the alternative is
  pushing the responsibility (and a `(value, ok)` return) onto every
  caller. The drop matches the convention used by `quantileTDigest` in
  ClickHouse and by the upstream Java reference implementation.
- **Single-threaded.** Wrap in a mutex or shard-then-merge for
  multi-producer scenarios. The merge path is designed to be cheap
  enough that shard-then-merge is the usual answer.

## Further reading

- Dunning, T. (2019). *Computing Extremely Accurate Quantiles Using
  t-Digests*. arXiv:1902.04023.
- Karnin, Z., Lang, K., & Liberty, E. (2016). *Optimal Quantile
  Approximation in Streams*. FOCS.
- Greenwald, M., & Khanna, S. (2001). *Space-efficient Online
  Computation of Quantile Summaries*. SIGMOD.
- ClickHouse `quantileTDigest`:
  <https://clickhouse.com/docs/en/sql-reference/aggregate-functions/reference/quantiletdigest>
- Reference: <https://pkg.go.dev/github.com/stergiotis/boxer/public/analytics/stats/tdigest>
- Sibling: [`StreamStats`](../stats.go) for streaming moments.
