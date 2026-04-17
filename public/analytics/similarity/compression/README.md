---
type: reference
audience: analytics consumer
status: draft
---

> **Status: draft — pre-human-review.** Not yet verified against the current documentation standard. Do not cite as authoritative.

# compression — Compression-Based Similarity Measurement

General-purpose primitives for measuring similarity between texts using data compression as a proxy for Kolmogorov complexity.

## Metrics

- **NCD** (Normalized Compression Distance): `(C(xy) - min(C(x), C(y))) / max(C(x), C(y))`. Range 0 (identical) to ~1 (unrelated).
- **CCC** (Conditional Complexity of Compression): `C(xy) - C(x)`. Measures how much new information y adds given x.

## Features

- Compressor-agnostic: works with any `io.Writer + io.Closer` that supports `Reset` (gzip, zstd, bzip2, ...).
- zstd raw-dictionary optimization: preloads the reference text as initial compressor history, avoiding redundant re-compression on every measurement. Controlled by `useZstdDictOptimization` const.
- Zero-allocation hot path for gzip/zstd (measured via benchmarks).

## References

- Li, Chen, Li, Ma, Vitanyi. *The Similarity Metric*. IEEE Trans. Inf. Theory, 2004.
- Cilibrasi, Vitanyi. *Clustering by Compression*. IEEE Trans. Inf. Theory, 2005.
- Halford. [*Text classification using compression*](https://maxhalford.github.io/blog/text-classification-zstd/), 2023.
