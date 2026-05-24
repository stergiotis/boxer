//go:build llm_generated_opus47

// Package ecdfbands implements finite-sample exact simultaneous
// confidence bands on the empirical cumulative distribution function
// of an iid univariate sample.
//
// Multiple band-shape families are supported. Each is defined by a
// pivot statistic whose acceptance region maps to a per-order-statistic
// pair of bounds (L_i, H_i) on the unit interval; the simultaneous
// 100(1-α)% band is the locus of those bounds:
//
//   - BandMethodBerkJones — Berk & Jones (1979) binomial-KL pointwise
//     statistic; tail-tight, dominates the Kolmogorov-Smirnov statistic
//     in the Bahadur sense.
//   - BandMethodDKW — Dvoretzky-Kiefer-Wolfowitz with the tight Massart
//     (1990) constant; closed-form ε = √(ln(2/α)/(2n)). Used as the
//     classical sanity baseline.
//   - BandMethodEqualPrecision — Stepanova & Wang (2008) weighted KS,
//     T_n = sup_t |√n(F_n(t)-F(t))|/√(F(t)(1-F(t))) on a central
//     interval; uniform precision across F.
//   - BandMethodHigherCriticism — Donoho & Jin (2004) higher-criticism
//     statistic; adaptive in the tails.
//
// The boundary-crossing probability that drives critical-value inversion
// is computed via two independent O(n²) algorithms, both in log-space:
//
//   - Moscovich, Nadler & Spiegelman (Annals of Statistics, 2020),
//     Algorithm 2: Poissonized DP propagating log-PMF along boundary
//     jump times, with the order-statistic probability recovered by
//     conditioning on N(1)=n. Strictly positive PMF entries make this
//     algorithm immune to catastrophic cancellation; it is the default
//     engine.
//   - Steck (1971) / Noé (1972) rectangle-probability determinant of
//     a lower-Hessenberg matrix, evaluated via the Hessenberg-Hyman
//     recursion in log-space with rescaling. Used as an independent
//     cross-check during testing.
//
// Both routines satisfy CrossingProbability(L, H) =
// P(L_i ≤ U_{(i)} ≤ H_i for all i=1..n) when U_{(1)} ≤ … ≤ U_{(n)} are
// the order statistics of n iid Uniform(0,1) draws.
//
// Numerical envelope: usable for n up to ~10⁵. Above that, expect
// either pathological cancellation in Steck-Noé or excessive runtime
// in the Moscovich DP — the streaming entry point BandsForGrid is the
// supported alternative for large samples.
//
// Scope: one-sample, continuous, exchangeable observations only.
// Two-sample tests (Kolmogorov, Goldman-Kaplan), censored-data
// adaptations, and dependent-sample variants are out of scope for v1
// — none of the band families implemented here generalise without
// further work, and the crossing-probability machinery would need
// extension to handle the additional structure.
//
// References:
//
//   - Berk, R.H. & Jones, D.H. (1979). "Goodness-of-fit test statistics
//     that dominate the Kolmogorov statistics."
//     Z. Wahrscheinlichkeitstheorie verw. Gebiete 47, 47-59.
//   - Massart, P. (1990). "The tight constant in the
//     Dvoretzky-Kiefer-Wolfowitz inequality." Ann. Probab. 18, 1269-1283.
//   - Stepanova, N. & Wang, T. (2008). "On the optimality of the
//     bias-corrected goodness-of-fit test." Electron. J. Stat. 2,
//     1226-1265.
//   - Donoho, D. & Jin, J. (2004). "Higher criticism for detecting
//     sparse heterogeneous mixtures." Ann. Statist. 32, 962-994.
//   - Moscovich, A., Nadler, B. & Spiegelman, C. (2020). "Fast
//     calculation of boundary crossing probabilities for Brownian
//     motion and Poisson processes." Ann. Statist. (preprint
//     arXiv:1503.04363).
//   - Steck, G.P. (1971). "Rectangle probabilities for uniform order
//     statistics and the probability that the empirical distribution
//     function lies between two distribution functions." Ann. Math.
//     Statist. 42, 1-11.
//   - Noé, M. (1972). "The calculation of distributions of two-sided
//     Kolmogorov-Smirnov type statistics." Ann. Math. Statist. 43,
//     58-64.
//   - Shorack, G.R. & Wellner, J.A. (1986). "Empirical Processes with
//     Applications to Statistics." Wiley, Chapter 9.
package ecdfbands
