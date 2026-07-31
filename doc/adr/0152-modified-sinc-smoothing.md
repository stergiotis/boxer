---
type: adr
status: accepted
date: 2026-07-31
reviewed-by: "p@stergiotis"
reviewed-date: 2026-07-31
---

# ADR-0152: modified-sinc smoothing as the series-smoothing primitive

## Context

The repository plots and post-processes a growing amount of equidistant series
data — the `sysmetrics` samplers, the imztop/imzrt dashboards, play's query
results, and the anomaly-detection substrate of
[ADR-0150](./0150-timeseries-subsequence-anomaly-detection.md) — and until now
had no smoothing primitive at all:
[`github.com/stergiotis/boxer/public/analytics/stats`](../../public/analytics/stats)
summarizes distributions, ADR-0150's packages read sequence structure, and
nothing in the Go tree low-passes a series or estimates a derivative from noisy
samples.

The field's default answer is the Savitzky–Golay (SG) filter — a sliding
local polynomial fit, in every signal-processing library since 1964. Before
adopting the default we read the case against it: Schmid, Rath and Diebold,
*Why and How Savitzky–Golay Filters Should Be Replaced*, ACS Meas. Sci. Au 2,
185–196 (2022), doi:10.1021/acsmeasuresciau.1c00054. Its findings, all
verified against the paper's own printed measurements during implementation:

- **SG's virtue is real**: a flat passband with a steep cutoff preserves peak
  shapes and heights better than most filters of equal bandwidth. Any
  replacement must keep this.
- **SG's stopband is poor**: the kernel jumps discontinuously to zero at its
  ends, so the first sidelobe passes about a quarter of the amplitude (−11 to
  −13 dB) and decays only as 1/f, nearly independent of window size. Noise
  above the cutoff survives; narrow features beyond it can return
  phase-inverted.
- **Derivatives inherit the worst of it**: differentiation amplifies exactly
  the frequencies SG fails to remove. Filters with a proper stopband halve the
  derivative noise.
- **Boundaries ring**: the standard near-boundary treatment produces artifacts
  reaching beyond a full kernel width, worst when differentiating.

The paper offers three replacements — SG with window-function fitting weights
(SGW), a modified-sinc kernel (MS), and Whittaker–Henderson penalized least
squares (WH) — with plug-in parameter translations. A 2023 correction
(doi:10.1021/acsmeasuresciau.3c00017) affects only a helper in its
supplementary Java code, not the equations.

An ecosystem check (2026-07-30) found **no Go implementation of any of the
three**, and MS essentially unproductized anywhere: the paper's own Java and
GNU Octave supplementary code, one two-star Julia package, an open scipy
enhancement issue for the SGW half. The WH family is widespread in other
ecosystems (statsmodels, pybaselines, an R and a Rust implementation). So the
choice is which method to implement from its equations, not which artifact to
adopt.

## Design space (QOC)

**Question.** Which smoothing family becomes the primitive for equidistant
series under `public/analytics/timeseries`, given that peaks must survive
smoothing, derivatives are a first-class use, series ends matter for live
data, and [ADR-0080](./0080-packageprops-per-package-declarations.md) keeps
`public/analytics/` pure-Go and WASM-freestanding?

**Options.**

- **O1 — Savitzky–Golay**, the incumbent default.
- **O2 — SGW**: SG fitted under a Hann-square weight window.
- **O3 — MS**: windowed-sinc convolution kernel with passband corrections,
  boundaries by weighted linear extrapolation.
- **O4 — WH**: whole-series penalized least squares (the Whittaker–Eilers
  smoother; the Hodrick–Prescott filter is its p = 2 case).
- **O5 — Gaussian/binomial convolution**, the no-overshoot baseline.

**Criteria.**

- **C1 — Stopband suppression**, and with it derivative noise.
- **C2 — Peak-height fidelity** at a given noise bandwidth (flat passband).
- **C3 — Boundary behaviour**: artifacts and noise near the series ends.
- **C4 — Statelessness and numerical robustness**: FIR convolution versus a
  whole-series solve; behaviour at strong smoothing.
- **C5 — Parameter ergonomics**: published fits for cutoff matching and
  peak-fidelity targeting; drop-in translation from SG habits.

**Assessment** (from the paper's measurements; `++` strong positive to `−−`
strong negative):

|    | O1 | O2 | O3 | O4 | O5 |
|----|----|----|----|----|----|
| C1 | −− | +  | ++ | ++ | +  |
| C2 | +  | +  | ++ | ++ | −− |
| C3 | −− | −  | ++ | +  | +  |
| C4 | ++ | ++ | ++ | −  | ++ |
| C5 | ++ | +  | ++ | +  | +  |

O3 and O4 are near-twins on frequency response (C1/C2); they separate on C3
— where MS with linear extrapolation measures fewer artifacts *and* lower
noise for degree ≥ 4 — and on C4, where WH is a banded solve with numeric
noise at extreme smoothing parameters while MS is a stateless convolution.
This matches the paper's own conclusion: "convolution with the MS kernels,
together with linear extrapolation at the boundaries, [is] the best method
currently available."

## Decision

Adopt **O3**: the modified-sinc kernel with weighted linear extrapolation at
the boundaries, implemented from the paper's equations as
[`github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth`](../../public/analytics/timeseries/mssmooth)
(committed at `ba1b6ebd`). Scope and shape:

- **Kernel** per paper eq 7: sinc under the eq 4 Gaussian-like window (α = 4),
  Table 1 passband corrections for degree ≥ 6. Even degrees 2–10, half-width
  ≥ n/2 + 2, coefficients normalized to sum 1.
- **Boundaries** per eq 17–18, with one documented completion: the Hann fit
  span is floored at two samples, because eq 17 degenerates below the two
  points a line fit needs at the minimum half-widths of the high degrees. The
  floor preserves exact reproduction of straight lines at every legal
  parameter combination.
- **Parameter routes**: peak-fidelity targeting (eq 19 + Table 2), SG
  replacement at equal −3 dB cutoff (eq 14 + 16), and raw bandwidth (eq 16).
- **Derivatives**: none provided; smooth first, then difference numerically,
  per the paper's §3.2 measurement that this order wins on both noise and
  boundary artifacts.
- **Verification** is anchored to paper-printed numbers rather than the
  implementation's own output: the n = 6, m = 96 kernel reproduces Figure 1's
  −71.7 dB first sidelobe and the cutoff of the SG(6, 50) filter it replaces;
  the fidelity route lands Gaussian peaks on their promised heights.

## Alternatives

- **O1 — Savitzky–Golay.** Rejected on C1/C3: the stopband and boundary
  failures are the documented cost of the default, and the flat passband —
  the reason SG is chosen — is exactly what MS keeps. Deliberately *not*
  offered alongside MS: two smoothing primitives with the same passband and
  different stopbands invite reaching for the familiar worse one.
- **O2 — SGW.** Strictly dominated in the paper's measurements: −30 dB first
  sidelobe against MS's −70, near-SG boundary artifacts when differentiating,
  and no closed-form kernels (Gram–Schmidt construction). Its one advantage —
  a kernel ~30% smaller than MS at equal cutoff — is served better by the
  paper's MS1 variant if a consumer ever needs it.
- **O4 — WH.** The serious contender; rejected as the *default* on C3 and C4:
  MS measures better at boundaries for degree ≥ 4, and a whole-series banded
  solve is stateful, O(N) per refresh, and numerically noisy at extreme λ
  where MS is unconditionally stable. Revisit if the boundary extrapolation
  proves troublesome in practice — the trigger is recorded in the package
  documentation.
- **O5 — Gaussian.** The paper's Figure 6 shows it needs the largest noise
  bandwidth at any peak-height fidelity — the wrong trade for a repository
  whose series are plotted with peaks that must survive. It remains the right
  tool for step-like signals where ringing is unacceptable; that use has no
  consumer here today.
- **Filter sharpening** (Kaiser–Hamming). The paper found no advantage over
  the above and it has no boundary story. Not pursued.
- **External dependency.** Nothing to depend on: no Go implementation exists,
  and the method is ~300 lines from the equations. Implementing beats
  wrapping, and keeps ADR-0080's freestanding declaration trivially.

## Consequences

### Positive

- Smoothing and derivative estimation stop being the missing primitive, with
  stopband behaviour that keeps derivatives usable — the capability ADR-0150's
  consumers (dashboards, implot overlays, play results) most plausibly reach
  for next. Wiring any consumer is out of scope here.
- Zero new dependencies; `WASMCompiles` on all three targets is empirically
  confirmed (TinyGo 0.41.1), not merely declared.
- SG habits transfer: an existing SG(n, m) choice translates to the equal-cutoff
  MS kernel by published fit, so the primitive is a drop-in for imported
  expectations.
- A stateless, symmetric FIR kernel is trivially safe for concurrent use and
  fits both batch and streaming shapes.

### Negative

- **Twice the kernel** of SG at equal cutoff, which for streaming is an
  m-sample structural lag — the same shape of lag ADR-0150's detector already
  carries (Window/2), but longer per unit of smoothing.
- **Boundary noise stays 2–3× the interior** under any method; the paper's and
  our documentation both say near-end values deserve suspicion. The
  extrapolation is a bolt-on with a tuned β, not an intrinsic property as it
  would be under WH.
- **A fitted, not continuous, parameter surface**: even degrees 2–10 only,
  four peak-fidelity levels only, coefficients from the paper's least-squares
  fits. Requests outside the fitted region are errors by design rather than
  extrapolations.
- **Ahead of the ecosystem.** MS has effectively no adoption in mainstream
  libraries (192 citations, four "influential" per Semantic Scholar;
  implementations are the paper's own SI and a two-star Julia package), so
  there is no reference implementation to cross-check against beyond the
  paper's printed values — which is what the test suite anchors to. One
  inconsistency was found doing so: Figure 7's caption states m = 38 for MS
  where the paper's own eq 19 + Table 2 give ≈ 48 (the SG and SGW values in
  the same caption check out). The implementation follows the equations,
  which are self-consistent with Figures 1, 3 and 6.

### Neutral

- The package follows the `analytics` leaf-package shape (ADR-0150's
  precedent): one concern, `doc.go` carrying the why, `package_props.go`,
  paper-anchored tests.
- The two-sample floor on the boundary-fit span is a deviation from the
  letter of eq 17, taken where the equation degenerates; it is documented at
  the code site and here, and reduces to the paper's behaviour everywhere the
  paper's formula is well-defined.
- Degrees beyond 6 are supported because the fits cover them, but the paper
  finds nothing beyond n = 6 worth having for peak-shaped signals; the
  package documentation carries that guidance rather than the API enforcing
  it.

## Status

Accepted 2026-07-31, same day as the implementation commit (`ba1b6ebd`); the
investigation, implementation and this record were sequential steps of one
adoption decision.

Status lifecycle: `Proposed → Accepted → (Deferred | Deprecated | Superseded by ADR-XXXX)`.
See [DOCUMENTATION_STANDARD §1 ADR](../DOCUMENTATION_STANDARD.md#architecture-decision-records-why-it-is-this-way)
for the edit-policy tiers. Subsequent refinements land as dated `## Updates`
entries, not as silent rewrites.

Deliberately left open, each until a consumer makes it concrete:

1. **Consumer wiring.** Which of the candidate consumers first gets a
   smoothing control, and whether smoothed series render alongside or instead
   of raw ones, is a UI decision owned by the consuming app.
2. **MS1 small kernels.** The paper's reduced-size variant trades stopband
   for footprint; adopt only when a consumer demonstrates the 2× kernel is a
   real cost.
3. **WH as a boundary fallback.** If extrapolation artifacts show up on real
   data, WH is the recorded alternative; its adoption would be a dated update
   here, not a new ADR, unless it displaces MS as the default.

## Updates

### 2026-07-31 — smoothed derivatives ship as `DerivativeE`; live demo

The Decision above listed derivatives as "none provided; smooth first, then
difference numerically". That guidance is now packaged rather than left to
callers: `Kernel.DerivativeE` returns the smoothed first derivative as the
*centered* difference of the MS-smoothed series, computed over a one-sample
margin of the boundary extrapolation so the ends need no special casing and
the result stays zero-phase (no half-sample shift). What remains deliberately
absent is unchanged: analytic derivative kernels, which the paper measures as
equal-or-worse than the numeric route. Tests anchor the addition to the
paper's §3.2 setup — filter parameters at 95% peak fidelity attenuate the
derivative peaks of a Gaussian to ≈90% — plus exact slope reproduction on
linear series, boundaries included.

A first consumer-visible surface also landed: the demo-registry entry
`mssmooth` (Charts & plots), a live strip chart of a scrolling noisy peak
train against its smoothed curve and, below, the derivative — raw
differencing against smooth-then-difference against the analytic truth —
with degree/half-width/noise controls.

Later the same day, the open sub-decision on app consumer wiring (Status
point 1) closed with imzrt as the first app consumer: an optional smoothing
overlay on its rate/latency trend plots, raw kept visible as a faint
underlay, degree fixed at 4 with only the half-width exposed, and the heap
sawtooth plus the forced-GC spike train deliberately excluded as structure
a low-pass would misreport. The consuming-app record is the dated update in
[ADR-0061](./0061-imzero2-imzrt-go-runtime-dashboard.md); the exclusions
there are a worked example of this ADR's scope note that peak-*shaped*
signals are the design center, not peak-*valued* event trains.

## References

- M. Schmid, D. Rath, U. Diebold, *Why and How Savitzky–Golay Filters Should
  Be Replaced*, ACS Meas. Sci. Au 2022, 2, 185–196.
  doi:10.1021/acsmeasuresciau.1c00054 (open access; supplementary Java and
  GNU Octave implementations).
- Correction: ACS Meas. Sci. Au 2023, 3, 236. doi:10.1021/acsmeasuresciau.3c00017.
- A. Savitzky, M. J. E. Golay, Anal. Chem. 1964, 36, 1627–1639.
  doi:10.1021/ac60214a047.
- R. W. Schafer, *What is a Savitzky-Golay filter?*, IEEE Signal Processing
  Magazine 2011, 28, 111–117. doi:10.1109/MSP.2011.941097.
- P. H. C. Eilers, *A perfect smoother*, Anal. Chem. 2003, 75, 3631–3636.
  doi:10.1021/ac034173t (the WH lineage).
- Implementation:
  [`github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth`](../../public/analytics/timeseries/mssmooth),
  commit `ba1b6ebd`.
