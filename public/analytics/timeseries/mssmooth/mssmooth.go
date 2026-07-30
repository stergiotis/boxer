package mssmooth

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// alpha is the width parameter of the Gaussian-like window (paper eq 4). The
// paper fixes it at 4 so that the cutoff steepness matches a Savitzky–Golay
// filter of the same degree; the Table 1 correction coefficients are fitted
// under this value, so it is not a free parameter here.
const alpha = 4.0

// MinDegree and MaxDegree bound the admissible filter degrees. The degree
// must be even: like Savitzky–Golay kernels, the MS construction is symmetric,
// and the paper defines and fits the family for even n only.
const MinDegree = 2
const MaxDegree = 10

// FidelityE names the peak-height fidelity levels for which the paper fits
// half-width parameters (Table 2): the smoothed height of a Gaussian peak as a
// percentage of its original height. Only these four levels are available —
// the coefficients are per-level least-squares fits, not a continuum.
type FidelityE uint8

const (
	Fidelity90 FidelityE = 90
	Fidelity95 FidelityE = 95
	Fidelity98 FidelityE = 98
	Fidelity99 FidelityE = 99
)

var AllFidelities = []FidelityE{Fidelity90, Fidelity95, Fidelity98, Fidelity99}

func (inst FidelityE) String() (name string) {
	switch inst {
	case Fidelity90:
		name = "90%"
	case Fidelity95:
		name = "95%"
	case Fidelity98:
		name = "98%"
	case Fidelity99:
		name = "99%"
	default:
		name = "invalid"
	}
	return
}

// correction is one j-term of the passband correction: κ = a + b/(c−m)³
// (paper eq 8), applied as κ·x·sin((2j+ν)πx) inside the kernel (paper eq 7).
type correction struct {
	a float64
	b float64
	c float64
}

// corrections returns the Table 1 coefficients and the harmonic offset ν for
// a degree. Degrees 2 and 4 need no correction: their windowed sinc is already
// flat in the passband. Without these terms the n ≥ 6 kernels overshoot unity
// by up to 0.13%; with them the deviation stays below about 1e-5.
func corrections(degree int32) (terms []correction, nu float64) {
	switch degree {
	case 6:
		terms = []correction{{a: 0.00172, b: 0.02437, c: 1.64375}}
		nu = 1.0
	case 8:
		terms = []correction{
			{a: 0.00440, b: 0.08821, c: 2.35938},
			{a: 0.00615, b: 0.02472, c: 3.63594},
		}
		nu = 2.0
	case 10:
		terms = []correction{
			{a: 0.00118, b: 0.04219, c: 2.74688},
			{a: 0.00367, b: 0.12780, c: 2.77031},
		}
		nu = 1.0
	}
	return
}

// MinHalfWidth returns the smallest admissible kernel half-width for a
// degree, degree/2 + 2. Below it the kernel has fewer points than sinc
// oscillations, and the Table 1 and Table 2 fits do not cover it.
func MinHalfWidth(degree int32) (halfWidth int32) {
	halfWidth = degree/2 + 2
	return
}

func validateDegreeE(degree int32) (err error) {
	if degree < MinDegree || degree > MaxDegree || degree%2 != 0 {
		err = eb.Build().Int32("degree", degree).Errorf("degree must be even and between 2 and 10")
		return
	}
	return
}

// Kernel is a modified-sinc smoothing kernel: 2m+1 normalized convolution
// coefficients together with the parameters that produced them. A Kernel is
// immutable once built and safe for concurrent use.
type Kernel struct {
	// coeffs holds the full symmetric kernel; index m+i carries lag i for
	// i in −m…m. The coefficients sum to exactly 1 by construction, which is
	// what makes the filter preserve constants and, with the symmetry,
	// straight lines.
	coeffs []float64
	degree int32
	m      int32
}

// NewKernelE builds the MS kernel of the given degree (even, 2–10) and
// half-width (≥ [MinHalfWidth]); the kernel spans 2·halfWidth+1 points.
//
// This is paper eq 7: a windowed sinc whose window (eq 4) brings the kernel
// and its first derivative to exactly zero at the first point outside the
// kernel, plus the Table 1 passband corrections for degree ≥ 6.
func NewKernelE(degree int32, halfWidth int32) (inst *Kernel, err error) {
	err = validateDegreeE(degree)
	if err != nil {
		return
	}
	if halfWidth < MinHalfWidth(degree) {
		err = eb.Build().Int32("degree", degree).Int32("halfWidth", halfWidth).
			Int32("min", MinHalfWidth(degree)).Errorf("half-width too small for the degree")
		return
	}

	m := int(halfWidth)
	terms, nu := corrections(degree)
	kappa := make([]float64, len(terms))
	for j, t := range terms {
		d := t.c - float64(m)
		kappa[j] = t.a + t.b/(d*d*d)
	}

	// x runs over i/(m+1), so x = ±1 falls on the first point outside the
	// kernel — where the window (eq 4) and the sinc both reach zero.
	coeffs := make([]float64, 2*m+1)
	sincScale := (float64(degree) + 4.0) / 2.0 * math.Pi
	var sum float64
	for i := 0; i <= m; i++ {
		x := float64(i) / float64(m+1)
		v := sinc(sincScale * x)
		for j, k := range kappa {
			v += k * x * math.Sin((2.0*float64(j)+nu)*math.Pi*x)
		}
		v *= windowAt(x)
		coeffs[m+i] = v
		coeffs[m-i] = v
		if i == 0 {
			sum += v
		} else {
			sum += 2.0 * v
		}
	}
	inv := 1.0 / sum
	for i := range coeffs {
		coeffs[i] *= inv
	}

	inst = &Kernel{
		coeffs: coeffs,
		degree: degree,
		m:      halfWidth,
	}
	return
}

// windowAt evaluates the window function of paper eq 4 at x ∈ [−1, 1]: a
// Gaussian plus two out-of-window Gaussians and an offset, arranged so the sum
// and its first derivative vanish at x = ±1. That continuity is the whole
// stopband story — it is what turns the ~1/f sidelobe decay of a truncated
// kernel into ~1/f⁴.
func windowAt(x float64) (w float64) {
	w = math.Exp(-alpha*x*x) +
		math.Exp(-alpha*(x+2.0)*(x+2.0)) +
		math.Exp(-alpha*(x-2.0)*(x-2.0)) -
		2.0*math.Exp(-alpha) - math.Exp(-9.0*alpha)
	return
}

func sinc(z float64) (s float64) {
	if z == 0.0 {
		s = 1.0
		return
	}
	s = math.Sin(z) / z
	return
}

// Degree returns the filter degree n.
func (inst *Kernel) Degree() (degree int32) {
	degree = inst.degree
	return
}

// HalfWidth returns the kernel half-width m; the kernel spans 2m+1 points.
func (inst *Kernel) HalfWidth() (halfWidth int32) {
	halfWidth = inst.m
	return
}

// Bandwidth returns the −3 dB cutoff of this kernel as a fraction of the
// sampling frequency, from the paper's eq 16 fit: (0.745 + 0.249·n)/(m+1).
// It is that fit, not a measured response; the two agree to a few tenths of a
// percent.
func (inst *Kernel) Bandwidth() (bandwidth float64) {
	bandwidth = (0.745 + 0.249*float64(inst.degree)) / (float64(inst.m) + 1.0)
	return
}

// Coeffs returns a copy of the 2m+1 kernel coefficients, index m+i carrying
// lag i. They sum to 1.
func (inst *Kernel) Coeffs() (coeffs []float64) {
	coeffs = make([]float64, len(inst.coeffs))
	copy(coeffs, inst.coeffs)
	return
}

// SmoothE convolves values with the kernel and returns a series of the same
// length. Within m points of either end the data are first continued by a
// weighted linear extrapolation (paper eq 17–18), so the output is defined up
// to the boundaries; see the package documentation for what near-boundary
// values are worth.
//
// dst is filled and returned when it has the capacity for len(values)
// results; otherwise a fresh slice is allocated. values itself is not
// modified, and dst may not alias it.
func (inst *Kernel) SmoothE(values []float64, dst []float64) (out []float64, err error) {
	n := len(values)
	if n == 0 {
		err = eb.Build().Errorf("empty series")
		return
	}
	for i, v := range values {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			err = eb.Build().Int("index", i).Float64("value", v).Errorf("series contains a non-finite value")
			return
		}
	}

	m := int(inst.m)
	ext := make([]float64, n+2*m)
	copy(ext[m:m+n], values)
	icept, slope := inst.fitBoundary(values, 0, 1)
	for d := 1; d <= m; d++ {
		ext[m-d] = icept - slope*float64(d)
	}
	icept, slope = inst.fitBoundary(values, n-1, -1)
	for d := 1; d <= m; d++ {
		ext[m+n-1+d] = icept - slope*float64(d)
	}

	if cap(dst) >= n {
		out = dst[:n]
	} else {
		out = make([]float64, n)
	}
	k := inst.coeffs
	for p := range n {
		seg := ext[p : p+2*m+1]
		var acc float64
		for i, kv := range k {
			acc += kv * seg[i]
		}
		out[p] = acc
	}
	return
}

// fitBoundary fits a straight line to the data next to one boundary, weighted
// by one side of a Hann window (paper eq 17) so that the points nearest the
// end dominate. start is the boundary index and stride (±1) the inward
// direction; the fit is over inward distance t, and the extrapolated value at
// outward distance d is icept − slope·d.
//
// β (paper eq 18) sets how much data the fit consumes, chosen by the paper so
// that boundary noise stays at or below that of the alternatives. The span is
// floored at two samples: a line needs two points, and near the minimum
// half-widths of the high degrees the paper's window would otherwise cover
// fewer — the floor is the minimal completion of eq 17 where it degenerates.
// Only a one-point series still falls back to continuing the boundary value.
func (inst *Kernel) fitBoundary(values []float64, start int, stride int) (icept float64, slope float64) {
	nf := float64(inst.degree)
	beta := 0.70 + 0.14*math.Exp(-0.6*(nf-4.0))
	// Positive weight exactly for t < span: the Hann argument reaches π/2 at
	// t = span, so ceil(span) points carry weight.
	span := max(2.0*beta*(float64(inst.m)+1.0)/(nf+3.0), 2.0)
	fitLen := min(int(math.Ceil(span)), len(values))
	if fitLen < 2 {
		icept = values[start]
		return
	}

	var s0, s1, s2, t0, t1 float64
	for t := range fitLen {
		c := math.Cos(math.Pi * float64(t) / (2.0 * span))
		w := c * c
		y := values[start+stride*t]
		tf := float64(t)
		s0 += w
		s1 += w * tf
		s2 += w * tf * tf
		t0 += w * y
		t1 += w * tf * y
	}
	det := s0*s2 - s1*s1
	if det <= 0.0 {
		icept = values[start]
		return
	}
	icept = (s2*t0 - s1*t1) / det
	slope = (s0*t1 - s1*t0) / det
	return
}

// SGBandwidthE returns the −3 dB cutoff of a traditional Savitzky–Golay
// smoothing filter of the given degree and half-width, as a fraction of the
// sampling frequency (paper eq 14, a fit accurate for all m).
//
// This is the bridge for replacing an existing Savitzky–Golay filter: its
// (degree, halfWidth) determine a bandwidth, and [HalfWidthForBandwidthE]
// turns that bandwidth into the MS half-width. [HalfWidthForSGE] composes the
// two.
func SGBandwidthE(degree int32, halfWidth int32) (bandwidth float64, err error) {
	err = validateDegreeE(degree)
	if err != nil {
		return
	}
	if halfWidth < degree/2+1 {
		err = eb.Build().Int32("degree", degree).Int32("halfWidth", halfWidth).
			Errorf("half-width too small for a Savitzky–Golay fit of the degree")
		return
	}
	mm := float64(halfWidth) + 0.5
	nn := float64(degree)
	bandwidth = 1.0 / (6.352*mm/(nn+1.379) - (0.513+0.316*nn)/mm)
	return
}

// HalfWidthForBandwidthE returns the MS half-width whose −3 dB cutoff is
// closest to bandwidth, a fraction of the sampling frequency in (0, 0.5]
// (paper eq 16). A bandwidth too close to Nyquist for the degree — one that
// would need a kernel below [MinHalfWidth] — is an error: the family cannot
// smooth that weakly, and clamping would silently smooth more than asked.
func HalfWidthForBandwidthE(degree int32, bandwidth float64) (halfWidth int32, err error) {
	err = validateDegreeE(degree)
	if err != nil {
		return
	}
	if !(bandwidth > 0.0) || bandwidth > 0.5 {
		err = eb.Build().Float64("bandwidth", bandwidth).Errorf("bandwidth must be in (0, 0.5], a fraction of the sampling frequency")
		return
	}
	halfWidth = int32(math.Round((0.745+0.249*float64(degree))/bandwidth - 1.0))
	if halfWidth < MinHalfWidth(degree) {
		err = eb.Build().Int32("degree", degree).Float64("bandwidth", bandwidth).
			Int32("halfWidth", halfWidth).Int32("min", MinHalfWidth(degree)).
			Errorf("bandwidth too high for the degree")
		halfWidth = 0
		return
	}
	return
}

// HalfWidthForSGE returns the MS half-width that replaces a traditional
// Savitzky–Golay smoothing filter of the given degree and half-width at equal
// −3 dB cutoff. Use the same degree for [NewKernelE]; the passbands then
// match and only the stopband improves. Expect roughly twice the
// Savitzky–Golay half-width.
func HalfWidthForSGE(degree int32, sgHalfWidth int32) (halfWidth int32, err error) {
	var bandwidth float64
	bandwidth, err = SGBandwidthE(degree, sgHalfWidth)
	if err != nil {
		return
	}
	halfWidth, err = HalfWidthForBandwidthE(degree, bandwidth)
	return
}

// fidelityCoeffs are the MS rows of paper Table 2, one per fidelity level,
// entering eq 19 as m = fwhm·(a + b·n + c·ln n) − 1.
type fidelityCoeffs struct {
	a float64
	b float64
	c float64
}

// HalfWidthForPeakE returns the half-width that smooths a Gaussian-like peak
// of the given full width at half-maximum (in samples) down to the chosen
// height fidelity (paper eq 19, Table 2). It is the strongest smoothing that
// still keeps the peak above that height — the right way to pick the
// parameter when the narrowest feature of interest is known.
//
// A peak so narrow that even the weakest legal kernel of the degree would
// undershoot the fidelity is an error; a lower degree may still admit it.
func HalfWidthForPeakE(degree int32, fwhm float64, fidelity FidelityE) (halfWidth int32, err error) {
	err = validateDegreeE(degree)
	if err != nil {
		return
	}
	if !(fwhm > 0.0) || math.IsInf(fwhm, 0) {
		err = eb.Build().Float64("fwhm", fwhm).Errorf("fwhm must be positive and finite")
		return
	}
	var fc fidelityCoeffs
	switch fidelity {
	case Fidelity90:
		fc = fidelityCoeffs{a: 1.2354, b: 0.4060, c: 0.1015}
	case Fidelity95:
		fc = fidelityCoeffs{a: 0.8874, b: 0.3402, c: 0.1290}
	case Fidelity98:
		fc = fidelityCoeffs{a: 0.5739, b: 0.2881, c: 0.1495}
	case Fidelity99:
		fc = fidelityCoeffs{a: 0.4013, b: 0.2642, c: 0.1470}
	default:
		err = eb.Build().Uint("fidelity", uint(fidelity)).Errorf("fidelity must be one of the Table 2 levels: 90, 95, 98, 99")
		return
	}
	nn := float64(degree)
	halfWidth = int32(math.Round(fwhm*(fc.a+fc.b*nn+fc.c*math.Log(nn)) - 1.0))
	if halfWidth < MinHalfWidth(degree) {
		err = eb.Build().Int32("degree", degree).Float64("fwhm", fwhm).Str("fidelity", fidelity.String()).
			Int32("halfWidth", halfWidth).Int32("min", MinHalfWidth(degree)).
			Errorf("peak too narrow for the degree at this fidelity")
		halfWidth = 0
		return
	}
	return
}
