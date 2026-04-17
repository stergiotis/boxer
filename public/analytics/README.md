---
type: router
audience: analytics consumer
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# analytics — Streaming Analytics and Similarity Measurement

## Package tree

```
analytics/
  stats/                                  Streaming statistics (Welford/Kahan) + convergence detection
  processor/                              Generic entity-grouped stream processing framework
  similarity/
    compression/                          Compression-based similarity (NCD, CCC) — compressor-agnostic
      stylometry/                         Authorship analysis with convergence-aware streaming on top of compression/
```

## `stats`

Online (single-pass) streaming statistics using Compensated Welford's Algorithm with Kahan summation.
Mean, Variance, StdDev, Skewness, Kurtosis, Min, Max. Merge via Pébay's parallel formulas.
Convergence detection via sliding-window variance stability.

## `processor`

Generic, allocation-optimized stream processor for entity-grouped batched data.
Partitions rows by entity key, streams to consumers via `iter.Seq`, manages memory via typed `sync.Pool`.
Supports read-ahead prefetching.

## `similarity/compression`

Compression-based similarity measurement: NCD (Normalized Compression Distance) and CCC (Conditional Complexity of Compression).
Compressor-agnostic (gzip, zstd, bzip2, ...) with a zstd raw-dictionary optimization.

## `similarity/compression/stylometry`

Authorship analysis built on `compression`. Streaming NCD/CCC with automatic convergence detection (Instance mode) and equal-length profile comparison (Profile mode).

## References

- Li, Chen, Li, Ma, Vitanyi. *The Similarity Metric*. IEEE Trans. Inf. Theory, 2004.
- Cilibrasi, Vitanyi. *Clustering by Compression*. IEEE Trans. Inf. Theory, 2005.
- Pébay. *Formulas for Robust, One-Pass Parallel Computation of Covariances and Arbitrary-Order Statistical Moments*. Sandia National Laboratories, 2008.
- Halford. [*Text classification using compression*](https://maxhalford.github.io/blog/text-classification-zstd/), 2023.
