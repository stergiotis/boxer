---
type: explanation
audience: package maintainer
status: draft
# reviewed-by: "@<handle>"     # fill in and uncomment when flipping to stable
# reviewed-date: YYYY-MM-DD    # fill in and uncomment when flipping to stable
---

> **Status: draft — pre-human-review.** Not verified; do not cite as authoritative.

# ecdfbands — Explanation

`ecdfbands` produces finite-sample exact simultaneous confidence
bands on the empirical CDF of an iid univariate sample. "Finite-sample
exact" is the load-bearing phrase — every band the library returns is
calibrated to the nominal (1-α)·100% coverage at the actual sample
size, not at the n → ∞ asymptotic limit. This file documents why the
package is built around two independent O(n²) algorithms, what each
band-shape family is good for, and where the numerical envelope
lives.

## Background

A simultaneous (1-α) confidence band on F is a random region
R(X_1, …, X_n) ⊆ [0,1] × ℝ such that

```
P( {(t, F(t)) : t ∈ ℝ} ⊆ R )  =  1 - α
```

i.e. the entire graph of F sits inside R with probability 1-α. Three
classical pivot statistics generate competing band shapes by
inverting their acceptance region: the Kolmogorov-Smirnov sup-norm
(Smirnov 1948), the binomial-likelihood-ratio pointwise statistic
(Berk & Jones 1979), and the weighted KS that equalises tail
precision (Stepanova & Wang 2008). A fourth, higher criticism
(Donoho & Jin 2004), trades worst-case width for sensitivity in
sparse-heterogeneous-mixture regimes. Each defines a per-rank
acceptance region `[a_i(c), b_i(c)]` on the i-th uniform order
statistic; the simultaneous coverage is the boundary-crossing
probability

```
P( a_i(c) ≤ U_{(i)} ≤ b_i(c) for all i ) = 1 - α.
```

The library's job is to invert this implicit equation in `c` —
which means it must evaluate the crossing probability *fast* and
*accurately*, for any monotone (a, b) pair, then bisect to hit the
target α.

## How it works

### Layer cake

```
                        +----------------------+
  public/.go            |   BandsForSample     |
                        |   BandsForGrid       |
                        |   QuantileBoundaries |
                        |   CriticalValue      |
                        +----------+-----------+
                                   |
                        +----------v-----------+
  invert.go             |  Cached bisection    |
                        |  on c ↦ P(c) - target |
                        +----+-----------+-----+
                             |           |
                +------------+           +------------+
                v                                     v
         +-------------+                     +-----------------+
  band_*.go| family.boundaries(n, c) |   | family.bracket(n, α) |
         +-------------+                     +-----------------+
                |
                v
         +---------------+
  crossprob*.go| CrossingProbability |
         +---------------+
            |          |
            v          v
       Steck     Moscovich (default)
```

### Layer L1 — log-space arithmetic (`logmath.go`)

`logSumExp`, `logFactorial`, `logPoissonPMF`, and the binomial-KL
divergence are the four primitives downstream layers compose. We
keep everything in the natural log so the Moscovich DP can carry
Poisson PMFs as low as e⁻¹⁰⁰⁰ without underflow, and the band
families can compute KL contours without exp/log round-trips inside
their bisection loops.

### Layer L2 — boundary-crossing probability (`crossprob_*.go`)

Two independent algorithms compute the same quantity:

- **Steck-Noé** rectangle-probability determinant (Steck 1971,
  Noé 1972). For valid (a, b) with both sequences monotone and
  `a_i ≤ b_i`, the probability equals `n! · det(L)` where L is an
  n×n upper-Hessenberg matrix with entries
  `(b_i - a_j)_+^{j-i+1} / (j-i+1)!` on and above the sub-diagonal.
  Implemented as plain double-precision Hessenberg LU. **Numerical
  envelope: reliable up to n ≈ 24.** Above that, catastrophic
  cancellation between the diagonal and the propagated sub-diagonal
  entries degrades the determinant to fewer than 5 digits. The
  package keeps Steck for small-n cross-validation only.

- **Moscovich-Nadler Poissonized DP** (Moscovich, Nadler &
  Spiegelman, *Annals of Statistics* 2020, Algorithm 2). Replaces
  the n iid uniforms with the event times of a unit-rate Poisson
  process N on [0, 1], computes the band-crossing probability for
  N via a forward DP on the state distribution at each boundary
  jump time, then conditions on N(1) = n to recover the
  order-statistic probability. Because all DP entries are
  unnormalised Poisson PMFs (strictly positive), the algorithm
  never subtracts; cancellation is impossible. **Numerical
  envelope: stable to at least n = 10⁴ in double precision.**
  This is the production engine.

The `CrossingAlgorithmAuto` dispatcher routes n ≤ steckN to Steck
and otherwise to Moscovich. Steck remains exposed as an explicit
choice so the test suite can cross-validate Moscovich at small n
where both algorithms agree to ~10⁻⁶.

### Layer L3 — band-shape families (`band_*.go`)

Each family is a `bandFamilyI` value whose `boundaries(n, c, …)`
method fills (lower, upper) with the per-rank band edges, and
whose `criticalValueBracket(n, α)` returns an initial bisection
range on c. The four built-in families:

- **`berkJonesFamily`** — `T_n = max_i n · D(p_i ‖ U_{(i)})` with
  p_i = i/n and D the binomial KL. Band edges via 1-D bisection of
  `D(p_i ‖ q) = c/n` on each side of p_i. The KL curve is convex in
  q with a unique minimum at q = p_i, so the bisection converges
  globally in ~60 iterations to ~10⁻¹⁵.

- **`dkwFamily`** — `T_n^DKW = √n · sup_x |F_n(x) - F(x)|`.
  Symmetric ε-strips around i/n. The closed-form Massart (1990)
  bound `ε = √(ln(2/α)/(2n))` seeds the bisection bracket; the
  exact finite-sample ε is slightly smaller and arrives from the
  inversion in a handful of iterations.

- **`equalPrecisionFamily`** — Stepanova-Wang weighted KS. Width
  proportional to `√(p_i(1-p_i)/n) · c`. Trims `⌈log(n)⌉` ranks
  from each tail (η = log(n)/n) — the standard Stepanova-Wang
  asymptotic trim rate. Within the trimmed range the band reverts
  to the trivial `[0, 1]`. Visibly tighter than HC in the
  centre; unbounded at the tails.

- **`higherCriticismFamily`** — Donoho-Jin HC. Same algebraic
  width formula but no tail trim — the only excluded rank is the
  variance-collapse point at i=n. Tight bands all the way out to
  the tails; correspondingly larger critical value than EP at the
  same α (typically c_HC ≈ c_EP + 1 across n ∈ [20, 500]).

The continuous-p extension `bandEdgeAtP` evaluates each family's
boundary at an arbitrary p ∈ [0, 1] — needed by `BandsForGrid`
when F_n at a grid point is not on the integer-rank lattice.

### Layer L4 — critical-value inversion (`invert.go`)

The dispatcher takes (n, α, method), gets the family's
`criticalValueBracket`, and bisects on c via at most 60 evaluations
of `CrossingProbability(boundaries(n, c, …))`. Results are cached
by (n, method, quantised α), so the practical cost is paid once per
distinct configuration — at n=100, α=0.05, the first call takes ~3
ms; the cache hits are O(n) memcpys.

### Layer L5 — public API (`ecdfbands.go`)

- `BandsForSample(sorted, α, method)` — the canonical "I have a
  sample, give me a band" entry. Returns the (Xs, LowerCDF,
  UpperCDF) trio packaged in a `SampleBand`.

- `BandsForGrid(xs, F_n, n, α, method)` — streaming-friendly entry
  for callers holding a sketch (t-digest, Greenwald-Khanna) at
  sample size n. Reads F_n(xs[i]) and expands to per-grid bands.

- `QuantileBoundaries(n, α, method)` — raw per-rank (lower, upper)
  on the order statistics themselves; the level the inversion
  produces before any CDF-axis reinterpretation.

- `CriticalValue(n, α, method)` — diagnostic accessor for the
  critical value the inversion converged to.

## Invariants

- Every (lower, upper) pair returned by a family satisfies
  `0 ≤ lower[i] ≤ upper[i] ≤ 1` and `lower[i] ≤ lower[i+1]`,
  `upper[i] ≤ upper[i+1]` (monotone non-decreasing). The
  crossing-probability engines require this; `clampMonotone`
  enforces it against ulp-level drift from bisection.

- For valid monotone bands, `CrossingProbability` returns a value
  in `[0, 1]`. Out-of-range inputs (NaN, lo > hi, non-monotone)
  return an error rather than a meaningless number.

- The inversion bisection is monotone: `P(c)` is increasing in c
  for every family. Wider bands accept more outcomes; the bisection
  converges globally in `O(log₂(precision))` iterations.

- Coverage is calibrated: the Monte Carlo `slow`-tag test verifies
  that the empirical (1-α)·100% coverage matches the nominal value
  within ±4σ at K = 10⁴ replicates, for every family at
  (n, α) ∈ {10, 25, 50, 100} × {0.05, 0.10}.

## Trade-offs

- **Steck-Noé is mathematically beautiful but numerically narrow.**
  Hessenberg LU on the rectangle-probability matrix accumulates
  catastrophic cancellation in the (j+1)/(k+1) propagation factor
  past n ≈ 30 in double precision. We keep it for cross-validation
  rather than removing it: independent algorithms reaching the same
  answer at small n is the strongest correctness statement we can
  make without a published reference table.

- **Moscovich DP is O(n²) worst case but O(n^{1.5}) in practice.**
  The active state range at each propagation step is the band
  width, which for typical BJ / DKW bands scales like √n · log(n).
  This is what makes the algorithm tractable up to n = 10⁵.

- **The continuous-p extension is per-family.** Each family exports
  its band geometry as a closed form in p so `BandsForGrid` does
  not need to interpolate. The cost is that adding a new family
  requires implementing both the integer-rank `boundaries` and the
  continuous `bandEdgeAtP` arm.

- **Caching shares results across goroutines.** A single mutex
  protects the inversion cache; readers acquire the read lock and
  writers (cache misses) the write lock. The bisection itself is
  not parallelised — a single n=10⁴ inversion takes ~100 ms and
  amortises trivially across reuse. The cache grows unbounded
  with distinct (n, method, quantised α) keys; for typical
  workloads (a handful of α values, modest n range) the working
  set is small enough to ignore, but a long-running service that
  iterates over many (n, α) combinations would benefit from an
  LRU eviction policy. Out of scope for v1.

## Further reading

- Berk, R.H. & Jones, D.H. (1979). "Goodness-of-fit test statistics
  that dominate the Kolmogorov statistics." *Z. Wahrscheinlichkeitstheorie verw. Gebiete* 47, 47-59.
- Massart, P. (1990). "The tight constant in the
  Dvoretzky-Kiefer-Wolfowitz inequality." *Ann. Probab.* 18, 1269-1283.
- Stepanova, N. & Wang, T. (2008). "On the optimality of the
  bias-corrected goodness-of-fit test." *Electron. J. Stat.* 2, 1226-1265.
- Donoho, D. & Jin, J. (2004). "Higher criticism for detecting
  sparse heterogeneous mixtures." *Ann. Statist.* 32, 962-994.
- Moscovich, A., Nadler, B. & Spiegelman, C. (2020). "Fast
  calculation of boundary crossing probabilities for Brownian
  motion and Poisson processes." *Ann. Statist.*
- Steck, G.P. (1971). "Rectangle probabilities for uniform order
  statistics …" *Ann. Math. Statist.* 42, 1-11.
- Smirnov, N.V. (1948). "Table for estimating the goodness of fit
  of empirical distributions." *Ann. Math. Statist.* 19, 279-281.
- Shorack, G.R. & Wellner, J.A. (1986). *Empirical Processes with
  Applications to Statistics.* Wiley, Chapter 9.
