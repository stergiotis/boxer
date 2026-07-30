// Package mssmooth smooths equidistant series by convolution with a
// modified-sinc (MS) kernel — the Savitzky–Golay replacement of Schmid, Rath
// and Diebold, ACS Meas. Sci. Au 2, 185–196 (2022),
// doi:10.1021/acsmeasuresciau.1c00054.
//
// # Why not Savitzky–Golay
//
// Savitzky–Golay smoothing is a sliding local polynomial fit, and its virtue
// is a flat passband with a steep cutoff: peaks and their heights survive
// better than under most filters of similar bandwidth. Its kernel, however,
// jumps discontinuously to zero at its ends, and the jump costs the stopband:
// the first sidelobe passes about a quarter of the amplitude (−11 to −13 dB)
// and later ones decay only as 1/f, almost independently of the window size.
// Noise above the cutoff largely survives, narrow features beyond it can come
// back phase-inverted, and derivatives — the flagship Savitzky–Golay
// application — amplify exactly the frequencies it fails to remove.
//
// The MS kernel is a sinc — the ideal low-pass — under a Gaussian-like window
// chosen so that the kernel and its first derivative reach zero exactly at the
// ends. It keeps the flat passband (deviation below about 1e-5 of unity) and
// pushes the first sidelobe under −70 dB with a 1/f⁴ decay. The price is a
// kernel roughly twice the Savitzky–Golay size for the same cutoff, which is
// also its lag: the filter is zero-phase and symmetric, so a value m samples
// from the end of the data is the last one computed without extrapolation.
//
// # Degrees
//
// The degree n (even, 2–10) plays the role of the Savitzky–Golay polynomial
// degree and shapes the cutoff steepness; both filter families share the
// 1 − c·f^(n+2) passband form, so a Savitzky–Golay filter of degree n is
// replaced by an MS kernel of the same degree. For degrees 6 and up the plain
// windowed sinc overshoots slightly in the passband and eq 7/8 correction
// terms flatten it; the paper fits their coefficients for m ≥ n/2 + 2, which
// this package enforces as [MinHalfWidth]. The paper finds nothing beyond
// n = 6 worth having for peak-shaped signals: interior noise stops improving
// past n = 4 while boundary noise and artifact reach keep growing.
//
// # Boundaries
//
// Convolution is undefined within m of the data ends. [Kernel.SmoothE] first
// extends the series by a weighted linear fit anchored at each end (paper
// eq 17–18, one-sided Hann weights). Mirroring would force a zero slope at
// the ends; quadratic extrapolation the paper measured as much worse. With
// this treatment the near-boundary artifacts for degree ≥ 4 are below those
// of Savitzky–Golay, weighted Savitzky–Golay and Whittaker–Henderson
// smoothing — but boundary noise remains 2–3× the interior under any method,
// so near-end values deserve suspicion regardless.
//
// # Choosing the half-width
//
// Three routes to the smoothing strength, strongest claim first:
//
//   - [HalfWidthForPeakE]: preserve a Gaussian-like peak of known FWHM to a
//     chosen height fidelity (paper eq 19, Table 2).
//   - [HalfWidthForSGE]: replace an existing Savitzky–Golay filter of the
//     same degree at equal −3 dB cutoff (paper eq 14 + 16).
//   - [HalfWidthForBandwidthE]: hit a −3 dB cutoff given as a fraction of the
//     sampling frequency (paper eq 16).
//
// # Derivatives
//
// Smooth first, then difference numerically; the operations commute in the
// interior, and this order has both the lower noise and the smaller boundary
// artifacts of the two (paper §3.2). No derivative kernels are provided.
//
// # Why not Whittaker–Henderson
//
// The paper's runner-up smooths by penalized least squares over the whole
// series. Its interior frequency response is nearly identical to MS and it
// needs no boundary treatment at all, but it is a whole-series banded solve —
// stateful, and numerically noisy at extreme smoothing parameters — where MS
// is a stateless FIR convolution. The paper's overall recommendation, adopted
// here, is MS with linear extrapolation; Whittaker–Henderson would earn its
// place only if the boundary extrapolation proved troublesome in practice.
//
// The 2023 correction to the paper (doi:10.1021/acsmeasuresciau.3c00017)
// affected only a helper in its supplementary Java code, not the equations
// implemented here.
package mssmooth
