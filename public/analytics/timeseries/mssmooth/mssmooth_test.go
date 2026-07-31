package mssmooth_test

import (
	"fmt"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/mssmooth"
)

// response evaluates the kernel's frequency response at f (a fraction of the
// sampling frequency), using the symmetry: H(f) = a₀ + 2·Σ aᵢ·cos(2πfi).
func response(coeffs []float64, f float64) (h float64) {
	m := len(coeffs) / 2
	h = coeffs[m]
	for i := 1; i <= m; i++ {
		h += 2.0 * coeffs[m+i] * math.Cos(2.0*math.Pi*f*float64(i))
	}
	return
}

// responseScan measures the response on a dense grid: the −3 dB crossing, the
// maximum response anywhere (unity plus any passband overshoot), and the
// highest sidelobe past the first deep null.
func responseScan(coeffs []float64) (f3dB float64, peak float64, sidelobe float64) {
	const steps = 40000
	target := 1.0 / math.Sqrt2
	prev := response(coeffs, 0.0)
	peak = prev
	inStopband := false
	for i := 1; i <= steps; i++ {
		f := 0.5 * float64(i) / float64(steps)
		h := response(coeffs, f)
		if h > peak {
			peak = h
		}
		if f3dB == 0.0 && h < target {
			// Linear interpolation across the crossing step.
			fPrev := 0.5 * float64(i-1) / float64(steps)
			f3dB = fPrev + (f-fPrev)*(prev-target)/(prev-h)
		}
		if !inStopband && h < 1e-5 {
			// Signed: the response's first zero crossing triggers this even
			// when no grid point lands near the null itself.
			inStopband = true
		}
		if inStopband && math.Abs(h) > sidelobe {
			sidelobe = math.Abs(h)
		}
		prev = h
	}
	return
}

// TestKernelAgainstPaperFigure1 pins the implementation to the two numbers
// the paper prints for the n = 6, m = 96 kernel (its Figure 1): a first
// sidelobe of −71.7 dB, and a −3 dB cutoff equal to that of the SG filter
// with n = 6, m = 50 it was constructed to replace.
func TestKernelAgainstPaperFigure1(t *testing.T) {
	inst, err := mssmooth.NewKernelE(6, 96)
	require.NoError(t, err)

	f3dB, _, sidelobe := responseScan(inst.Coeffs())
	sidelobeDb := 20.0 * math.Log10(sidelobe)
	assert.InDelta(t, -71.7, sidelobeDb, 0.5, "first sidelobe (dB)")

	sgBandwidth, err := mssmooth.SGBandwidthE(6, 50)
	require.NoError(t, err)
	assert.InEpsilon(t, sgBandwidth, f3dB, 0.01, "cutoff must match the replaced SG filter")
	assert.InEpsilon(t, inst.Bandwidth(), f3dB, 0.01, "eq 16 fit must match the measured cutoff")
}

// TestFrequencyResponseInvariants checks, across the degree range, the two
// properties that justify the filter's existence: a passband that never
// exceeds unity by more than ~1e-5 (the paper claims <3.5e-6 for the
// corrected degrees; degree 10 measures ~7e-6 here, still forty times below
// the uncorrected 0.13%) and a first sidelobe below the paper's stated
// 3e-4 (−70 dB) — where a traditional SG filter has ~1/4 (−12 dB).
func TestFrequencyResponseInvariants(t *testing.T) {
	for _, degree := range []int32{2, 4, 6, 8, 10} {
		for _, m := range []int32{mssmooth.MinHalfWidth(degree) + 6, 25, 80} {
			t.Run(fmt.Sprintf("n=%d/m=%d", degree, m), func(t *testing.T) {
				inst, err := mssmooth.NewKernelE(degree, m)
				require.NoError(t, err)
				coeffs := inst.Coeffs()

				var sum float64
				for _, c := range coeffs {
					sum += c
				}
				assert.InDelta(t, 1.0, sum, 1e-12, "coefficients must sum to 1")
				for i := range m {
					assert.Equal(t, coeffs[m+1+i], coeffs[m-1-i], "kernel must be symmetric")
				}

				f3dB, peak, sidelobe := responseScan(coeffs)
				assert.Less(t, peak, 1.0+1e-5, "passband overshoot")
				assert.Less(t, sidelobe, 3e-4, "first sidelobe amplitude")
				assert.InEpsilon(t, inst.Bandwidth(), f3dB, 0.015, "eq 16 fit vs measured cutoff")
			})
		}
	}
}

// TestHalfWidthForSGAnchor pins the parameter translation to the pairing the
// paper prints in Figure 1: the SG filter n = 6, m = 50 is replaced by the MS
// kernel with m = 96.
func TestHalfWidthForSGAnchor(t *testing.T) {
	halfWidth, err := mssmooth.HalfWidthForSGE(6, 50)
	require.NoError(t, err)
	assert.Equal(t, int32(96), halfWidth)
}

// TestLineReproducedExactly: a normalized symmetric kernel passes constants
// and straight lines through unchanged, and the boundary extrapolation is a
// line fit, so an exactly linear series must come back identical to rounding —
// at every point, including all 2m boundary points, and for series shorter
// than the kernel.
func TestLineReproducedExactly(t *testing.T) {
	cases := []struct {
		degree int32
		m      int32
		n      int
	}{
		{degree: 2, m: 3, n: 1},
		{degree: 2, m: 8, n: 2},
		{degree: 4, m: 20, n: 5},
		{degree: 6, m: 25, n: 200},
		{degree: 10, m: 7, n: 40},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("n=%d/m=%d/len=%d", tc.degree, tc.m, tc.n), func(t *testing.T) {
			inst, err := mssmooth.NewKernelE(tc.degree, tc.m)
			require.NoError(t, err)
			values := make([]float64, tc.n)
			for i := range values {
				values[i] = 3.25 - 0.7*float64(i)
			}
			out, err := inst.SmoothE(values, nil)
			require.NoError(t, err)
			require.Len(t, out, tc.n)
			for i := range values {
				assert.InDelta(t, values[i], out[i], 1e-9, "index %d", i)
			}
		})
	}
}

// TestGaussianPeakFidelity closes the loop on HalfWidthForPeakE: smoothing a
// Gaussian of fwhm 20 with the half-width the fidelity fit prescribes must
// attenuate its peak to the promised height. Table 2 is a fit, so the check
// carries half a percent of slack.
func TestGaussianPeakFidelity(t *testing.T) {
	cases := []struct {
		degree   int32
		fidelity mssmooth.FidelityE
		want     float64
	}{
		{degree: 4, fidelity: mssmooth.Fidelity90, want: 0.90},
		{degree: 6, fidelity: mssmooth.Fidelity90, want: 0.90},
		{degree: 4, fidelity: mssmooth.Fidelity99, want: 0.99},
	}
	const fwhm = 20.0
	for _, tc := range cases {
		t.Run(fmt.Sprintf("n=%d/%s", tc.degree, tc.fidelity), func(t *testing.T) {
			halfWidth, err := mssmooth.HalfWidthForPeakE(tc.degree, fwhm, tc.fidelity)
			require.NoError(t, err)
			inst, err := mssmooth.NewKernelE(tc.degree, halfWidth)
			require.NoError(t, err)

			// Peak in the middle of a series long enough that the boundary
			// treatment cannot reach it.
			const n = 601
			values := make([]float64, n)
			for i := range values {
				x := float64(i - n/2)
				values[i] = math.Exp(-4.0 * math.Ln2 * x * x / (fwhm * fwhm))
			}
			out, err := inst.SmoothE(values, nil)
			require.NoError(t, err)
			assert.InDelta(t, tc.want, out[n/2], 0.005, "smoothed peak height")
		})
	}
}

// TestDerivativeOfLineIsExactSlope: the derivative path shares the
// line-exactness argument of smoothing — the extrapolation continues an exact
// line and the centered difference of a line is its slope — so the derivative
// of a linear series must be the slope at every point, boundaries included.
func TestDerivativeOfLineIsExactSlope(t *testing.T) {
	for _, n := range []int{1, 2, 40, 200} {
		inst, err := mssmooth.NewKernelE(6, 25)
		require.NoError(t, err)
		values := make([]float64, n)
		for i := range values {
			values[i] = -2.5 + 0.375*float64(i)
		}
		out, err := inst.DerivativeE(values, nil)
		require.NoError(t, err)
		require.Len(t, out, n)
		want := 0.375
		if n == 1 {
			// A single point extends as a constant, whose derivative is zero.
			want = 0.0
		}
		for i := range out {
			assert.InDelta(t, want, out[i], 1e-9, "len %d index %d", n, i)
		}
	}
}

// TestDerivativeGaussianAttenuation reproduces the paper's §3.2 setup: filter
// parameters at 95% peak fidelity for the Gaussian attenuate the peaks of its
// derivative to about 90%.
func TestDerivativeGaussianAttenuation(t *testing.T) {
	const fwhm = 20.0
	halfWidth, err := mssmooth.HalfWidthForPeakE(4, fwhm, mssmooth.Fidelity95)
	require.NoError(t, err)
	inst, err := mssmooth.NewKernelE(4, halfWidth)
	require.NoError(t, err)

	const n = 601
	values := make([]float64, n)
	analytic := make([]float64, n)
	c := 4.0 * math.Ln2 / (fwhm * fwhm)
	for i := range values {
		x := float64(i - n/2)
		values[i] = math.Exp(-c * x * x)
		analytic[i] = -2.0 * c * x * values[i]
	}
	out, err := inst.DerivativeE(values, nil)
	require.NoError(t, err)

	var gotMax, wantMax float64
	for i := range out {
		gotMax = math.Max(gotMax, math.Abs(out[i]))
		wantMax = math.Max(wantMax, math.Abs(analytic[i]))
	}
	assert.InDelta(t, 0.90, gotMax/wantMax, 0.03, "derivative peak attenuation")
}

// TestSmoothReusesDst covers the destination-buffer contract.
func TestSmoothReusesDst(t *testing.T) {
	inst, err := mssmooth.NewKernelE(4, 10)
	require.NoError(t, err)
	values := []float64{1, 2, 4, 8, 4, 2, 1, 0, 1, 2}
	dst := make([]float64, 0, len(values))
	out, err := inst.SmoothE(values, dst)
	require.NoError(t, err)
	require.Len(t, out, len(values))
	assert.Same(t, &dst[:1][0], &out[0], "dst with sufficient capacity must be reused")

	fresh, err := inst.SmoothE(values, make([]float64, 2))
	require.NoError(t, err)
	require.Len(t, fresh, len(values))
}

func TestErrors(t *testing.T) {
	_, err := mssmooth.NewKernelE(3, 20)
	assert.Error(t, err, "odd degree")
	_, err = mssmooth.NewKernelE(12, 20)
	assert.Error(t, err, "degree beyond Table 1")
	_, err = mssmooth.NewKernelE(6, 4)
	assert.Error(t, err, "half-width below minimum")

	inst, err := mssmooth.NewKernelE(4, 10)
	require.NoError(t, err)
	_, err = inst.SmoothE(nil, nil)
	assert.Error(t, err, "empty series")
	_, err = inst.SmoothE([]float64{1, math.NaN(), 3}, nil)
	assert.Error(t, err, "NaN in series")
	_, err = inst.SmoothE([]float64{1, math.Inf(1), 3}, nil)
	assert.Error(t, err, "Inf in series")

	_, err = mssmooth.HalfWidthForBandwidthE(4, 0.0)
	assert.Error(t, err, "zero bandwidth")
	_, err = mssmooth.HalfWidthForBandwidthE(4, 0.7)
	assert.Error(t, err, "bandwidth beyond Nyquist")
	_, err = mssmooth.HalfWidthForBandwidthE(10, 0.45)
	assert.Error(t, err, "bandwidth unreachable for the degree")

	_, err = mssmooth.HalfWidthForPeakE(4, 0.0, mssmooth.Fidelity90)
	assert.Error(t, err, "non-positive fwhm")
	_, err = mssmooth.HalfWidthForPeakE(4, 20.0, mssmooth.FidelityE(93))
	assert.Error(t, err, "fidelity level not in Table 2")
	_, err = mssmooth.HalfWidthForPeakE(10, 1.0, mssmooth.Fidelity99)
	assert.Error(t, err, "peak too narrow for the degree")

	_, err = mssmooth.SGBandwidthE(4, 1)
	assert.Error(t, err, "SG half-width below fit order")
}

// TestPropertyAffineEquivariance: smoothing is linear and preserves
// constants and lines, so Smooth(a·y + b) must equal a·Smooth(y) + b — for
// any data, any admissible kernel, at every point including the extrapolated
// boundaries.
func TestPropertyAffineEquivariance(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		degree := rapid.SampledFrom([]int32{2, 4, 6, 8, 10}).Draw(t, "degree")
		m := rapid.Int32Range(mssmooth.MinHalfWidth(degree), 40).Draw(t, "m")
		inst, err := mssmooth.NewKernelE(degree, m)
		require.NoError(t, err)

		n := rapid.IntRange(1, 200).Draw(t, "len")
		values := make([]float64, n)
		for i := range values {
			values[i] = rapid.Float64Range(-1e6, 1e6).Draw(t, fmt.Sprintf("y%d", i))
		}
		scale := rapid.Float64Range(-8.0, 8.0).Draw(t, "scale")
		offset := rapid.Float64Range(-1e4, 1e4).Draw(t, "offset")

		base, err := inst.SmoothE(values, nil)
		require.NoError(t, err)
		require.Len(t, base, n)

		transformed := make([]float64, n)
		for i, v := range values {
			transformed[i] = scale*v + offset
		}
		got, err := inst.SmoothE(transformed, nil)
		require.NoError(t, err)

		for i := range base {
			want := scale*base[i] + offset
			tol := 1e-9 * (1.0 + math.Abs(want))
			if math.Abs(want-got[i]) > tol {
				t.Fatalf("index %d: want %g, got %g", i, want, got[i])
			}
		}

		// The derivative is linear too, and the offset must vanish:
		// D(a·y + b) = a·D(y).
		baseD, err := inst.DerivativeE(values, nil)
		require.NoError(t, err)
		gotD, err := inst.DerivativeE(transformed, nil)
		require.NoError(t, err)
		for i := range baseD {
			want := scale * baseD[i]
			tol := 1e-9 * (1.0 + math.Abs(want))
			if math.Abs(want-gotD[i]) > tol {
				t.Fatalf("derivative index %d: want %g, got %g", i, want, gotD[i])
			}
		}
	})
}

func TestFidelityStrings(t *testing.T) {
	assert.Equal(t, "90%", mssmooth.Fidelity90.String())
	assert.Equal(t, "95%", mssmooth.Fidelity95.String())
	assert.Equal(t, "98%", mssmooth.Fidelity98.String())
	assert.Equal(t, "99%", mssmooth.Fidelity99.String())
	assert.Equal(t, "invalid", mssmooth.FidelityE(42).String())
	assert.Len(t, mssmooth.AllFidelities, 4)
}

func BenchmarkSmooth(b *testing.B) {
	inst, err := mssmooth.NewKernelE(6, 25)
	if err != nil {
		b.Fatal(err)
	}
	values := make([]float64, 10000)
	for i := range values {
		values[i] = math.Sin(float64(i)*0.05) + 0.1*math.Cos(float64(i)*1.7)
	}
	dst := make([]float64, len(values))
	b.ReportAllocs()
	for b.Loop() {
		_, err = inst.SmoothE(values, dst)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(b.N*len(values))/b.Elapsed().Seconds(), "samples/s")
}
