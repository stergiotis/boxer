package matrixprofile_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
)

// The oracle below is deliberately naive and shares no code path with the
// implementation: it materializes each z-normalized subsequence and takes the
// Euclidean distance between them directly, rather than going through the
// 2m(1−ρ) dot-product identity that STOMP exploits. An oracle built on the same
// identity would confirm the recurrence against itself and prove nothing.

// oracleStdDev is the two-pass standard deviation of a slice.
func oracleStdDev(seg []float64) (std float64) {
	var mean float64
	for _, v := range seg {
		mean += v
	}
	mean /= float64(len(seg))

	var ss float64
	for _, v := range seg {
		d := v - mean
		ss += d * d
	}
	std = math.Sqrt(ss / float64(len(seg)))
	return
}

// oracleFloor mirrors the package's *specification* of a constant window — a
// standard deviation at or below a fixed fraction of the series' own — computed
// independently of the implementation. Sharing the contract is intended;
// sharing the arithmetic that realizes it would not be.
func oracleFloor(values []float64) (floor float64) {
	floor = matrixprofile.DefaultStdDevFloorRel * oracleStdDev(values)
	return
}

func oracleZNorm(values []float64, idx int32, window int32, floor float64) (out []float64) {
	seg := values[idx : idx+window]
	out = make([]float64, window)

	var mean float64
	for _, v := range seg {
		mean += v
	}
	mean /= float64(window)

	std := oracleStdDev(seg)
	if std <= floor {
		// Constant window normalizes to the zero vector.
		return
	}
	for i, v := range seg {
		out[i] = (v - mean) / std
	}
	return
}

func oracleDistance(values []float64, i int32, j int32, window int32) (dist float64) {
	floor := oracleFloor(values)
	a := oracleZNorm(values, i, window, floor)
	b := oracleZNorm(values, j, window, floor)
	var ss float64
	for k := range a {
		d := a[k] - b[k]
		ss += d * d
	}
	dist = math.Sqrt(ss)
	return
}

func oracleExclusionZone(window int32) (zone int32) {
	zone = (window + 3) / 4
	return
}

// identityTolerance is the agreement to expect from values that came out of the
// 2m(1-rho) identity without refinement -- that is, from
// [matrixprofile.Series.DistanceProfile].
//
// The absolute term is not slack for sloppiness. It is the identity's documented
// behaviour near rho = 1, where a correlation error d surfaces as sqrt(2md) of
// distance, and it degrades further when the two windows differ by many orders
// of magnitude in scale. Property-based generation reaches exactly those inputs.
func identityTolerance(window int32, want float64) (tol float64) {
	tol = 1.0e-2*math.Sqrt(float64(window)) + 1.0e-6*math.Abs(want)
	return
}

// refinedTolerance is the agreement to expect from [matrixprofile.Profile]
// distances, which are recomputed from materialized z-normalized values and so
// carry ordinary accumulated rounding rather than the identity's floor.
func refinedTolerance(window int32, want float64) (tol float64) {
	tol = 1.0e-9*math.Sqrt(float64(window)) + 1.0e-9*math.Abs(want)
	return
}

// oracleProfile computes the matrix profile by brute force, O(n²·window).
func oracleProfile(values []float64, window int32) (dist []float64, idx []int32) {
	numWindows := int32(len(values)) - window + 1
	zone := oracleExclusionZone(window)
	dist = make([]float64, numWindows)
	idx = make([]int32, numWindows)
	for i := range numWindows {
		dist[i] = math.Inf(1)
		idx[i] = -1
		for j := range numWindows {
			if j >= i-zone && j <= i+zone {
				continue
			}
			d := oracleDistance(values, i, j, window)
			if d < dist[i] {
				dist[i] = d
				idx[i] = j
			}
		}
	}
	return
}

// assertProfileMatchesOracle checks the two contracts a profile carries, at the
// two different accuracies they hold to.
//
// The reported distance must be the true distance to the reported *neighbour*,
// tightly — that is the refinement pass doing its job, and the oracle reaches it
// by different arithmetic (from the original values, not the centered ones).
//
// The reported neighbour must be a nearest one only to within the search's
// resolution. Neighbour selection runs on the 2m(1−ρ) identity, so where several
// candidates sit within its floor of the true minimum, which one is picked is
// arbitrary. Property generation produces exactly that: a window matching one
// candidate at 1e-16 and another at 1e-8 is free to report either.
//
// Index equality is never asserted; ties are common in synthetic data.
func assertProfileMatchesOracle(t *testing.T, values []float64, window int32, prof *matrixprofile.Profile) {
	t.Helper()
	wantDist, wantIdx := oracleProfile(values, window)
	require.Len(t, prof.Distance, len(wantDist))

	for i := range wantDist {
		if wantIdx[i] < 0 {
			assert.Equal(t, int32(-1), prof.Index[i], "position %d should have no neighbour", i)
			assert.True(t, math.IsInf(prof.Distance[i], 1), "position %d should be +Inf", i)
			continue
		}
		require.GreaterOrEqual(t, prof.Index[i], int32(0), "position %d lost its neighbour", i)

		toNeighbour := oracleDistance(values, int32(i), prof.Index[i], window)
		assert.InDelta(t, toNeighbour, prof.Distance[i], refinedTolerance(window, toNeighbour),
			"reported distance at %d is not the distance to reported neighbour %d", i, prof.Index[i])

		assert.InDelta(t, wantDist[i], toNeighbour, identityTolerance(window, wantDist[i]),
			"reported neighbour %d of %d is not a nearest one", prof.Index[i], i)
		assert.GreaterOrEqual(t, prof.Distance[i], wantDist[i]-identityTolerance(window, wantDist[i]),
			"position %d reports a neighbour nearer than the true minimum", i)
	}
}

func TestComputeMatchesBruteForceOracle(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		window int32
	}{
		{
			name:   "sine with noise",
			values: syntheticSine(200, 17, 0.05),
			window: 16,
		},
		{
			name:   "sawtooth",
			values: syntheticSawtooth(150, 13),
			window: 8,
		},
		{
			name:   "large offset",
			values: withOffset(syntheticSine(120, 11, 0.02), 1.0e9),
			window: 12,
		},
		{
			name:   "tiny scale",
			values: withScale(syntheticSine(120, 11, 0.02), 1.0e-6),
			window: 12,
		},
		{
			name:   "window equals two",
			values: syntheticSawtooth(60, 7),
			window: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := matrixprofile.NewSeriesE(tt.values, tt.window, 0.0)
			require.NoError(t, err)
			assertProfileMatchesOracle(t, tt.values, tt.window, s.Compute())
		})
	}
}

func TestDistanceProfileMatchesOracle(t *testing.T) {
	values := syntheticSine(140, 19, 0.03)
	const window = int32(15)

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)

	for _, q := range []int32{0, 1, 37, s.NumWindows() - 1} {
		got := s.DistanceProfile(q, nil)
		require.Len(t, got, int(s.NumWindows()))
		for j := range s.NumWindows() {
			want := oracleDistance(values, q, j, window)
			assert.InDelta(t, want, got[j], identityTolerance(window, want),
				"query %d against %d", q, j)
		}
	}
}

func TestDistanceProfileReusesDestination(t *testing.T) {
	s, err := matrixprofile.NewSeriesE(syntheticSine(80, 9, 0.01), 10, 0.0)
	require.NoError(t, err)

	dst := make([]float64, 0, s.NumWindows())
	out := s.DistanceProfile(3, dst)
	assert.Equal(t, &dst[:1][0], &out[0], "destination with capacity should be reused")

	tooSmall := make([]float64, 0, 2)
	out = s.DistanceProfile(3, tooSmall)
	assert.Len(t, out, int(s.NumWindows()))
}

func TestSelfDistanceIsNearZero(t *testing.T) {
	// A subsequence compared against itself is the ρ = 1 case, where the
	// distance identity is at its least accurate. The result is not bit-zero and
	// the package documents that; what must hold is that it is zero to within
	// the identity's floor, several orders below any real match.
	const window = int32(12)
	values := syntheticSine(90, 11, 0.02)
	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)

	prof := s.DistanceProfile(20, nil)
	assert.InDelta(t, 0.0, prof[20], identityTolerance(window, 0.0),
		"a subsequence must be at distance 0 from itself")
}

func TestExclusionZoneIsRespected(t *testing.T) {
	values := syntheticSine(120, 13, 0.01)
	const window = int32(20)

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)
	prof := s.Compute()

	zone := s.ExclusionZone()
	assert.Equal(t, int32(5), zone, "window/4 rounded up")
	for i, nb := range prof.Index {
		if nb < 0 {
			continue
		}
		delta := int32(i) - nb
		if delta < 0 {
			delta = -delta
		}
		assert.Greater(t, delta, zone, "neighbour of %d is a trivial match", i)
	}
}

func TestConstantSeries(t *testing.T) {
	values := make([]float64, 60)
	for i := range values {
		values[i] = 42.0
	}

	s, err := matrixprofile.NewSeriesE(values, 8, 0.0)
	require.NoError(t, err)
	assert.True(t, s.IsConstantAt(0))

	prof := s.Compute()
	for i, d := range prof.Distance {
		assert.InDelta(t, 0.0, d, 1.0e-12, "constant windows must be identical after normalization, at %d", i)
	}
}

func TestConstantSegmentAgainstVaryingSegment(t *testing.T) {
	// First half flat, second half a ramp. A flat window and a varying window
	// must sit at sqrt(m): the flat one normalizes to the zero vector, the
	// varying one to a vector of norm sqrt(m).
	const window = int32(10)
	values := make([]float64, 80)
	for i := 40; i < 80; i++ {
		values[i] = float64(i - 40)
	}

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)
	require.True(t, s.IsConstantAt(0), "leading window should read as constant")
	require.False(t, s.IsConstantAt(50), "ramp window should not read as constant")

	prof := s.DistanceProfile(0, nil)
	assert.InDelta(t, math.Sqrt(float64(window)), prof[50], 1.0e-9)
	assert.InDelta(t, 0.0, prof[5], 1.0e-9, "two flat windows coincide")
}

func TestPlantedMotifIsFound(t *testing.T) {
	// A distinctive pattern planted twice. The background carries noise on
	// purpose: a noiseless sine of integer period repeats itself exactly every
	// period, so background windows would tie with the planted pair at distance
	// 0 and the global minimum would be arbitrary among them.
	values := syntheticSine(300, 41, 0.2)
	pattern := []float64{0.0, 3.0, -3.0, 3.0, -3.0, 3.0, 0.0, -1.5, 1.5, 0.0}
	copy(values[50:], pattern)
	copy(values[200:], pattern)

	s, err := matrixprofile.NewSeriesE(values, int32(len(pattern)), 0.0)
	require.NoError(t, err)

	first, second, dist, found := s.Compute().Motif()
	require.True(t, found)
	assert.InDelta(t, 0.0, dist, 1.0e-6, "planted copies are identical")

	lo, hi := first, second
	if lo > hi {
		lo, hi = hi, lo
	}
	assert.Equal(t, int32(50), lo)
	assert.Equal(t, int32(200), hi)
}

func TestPlantedDiscordIsFound(t *testing.T) {
	// A clean periodic signal whose period divides evenly, with one window
	// overwritten by a shape that appears nowhere else.
	const window = int32(20)
	values := syntheticSine(400, 20, 0.0)
	for i := range window {
		values[180+i] = 5.0 * math.Cos(float64(i)*0.7)
	}

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)

	idx, dist, found := s.Compute().Discord()
	require.True(t, found)
	assert.Greater(t, dist, 0.0)
	assert.InDelta(t, 180.0, float64(idx), float64(window),
		"discord should land within one window of the planted anomaly")
}

func TestNewSeriesRejectsBadInput(t *testing.T) {
	_, err := matrixprofile.NewSeriesE([]float64{1, 2, 3}, 1, 0.0)
	assert.Error(t, err, "window below 2")

	_, err = matrixprofile.NewSeriesE([]float64{1, 2, 3}, 8, 0.0)
	assert.Error(t, err, "series shorter than window")

	_, err = matrixprofile.NewSeriesE([]float64{1, 2, math.NaN(), 4}, 2, 0.0)
	assert.Error(t, err, "NaN in series")

	_, err = matrixprofile.NewSeriesE([]float64{1, 2, math.Inf(1), 4}, 2, 0.0)
	assert.Error(t, err, "infinity in series")
}

func TestSeriesExactlyOneWindow(t *testing.T) {
	s, err := matrixprofile.NewSeriesE([]float64{1, 2, 3, 4}, 4, 0.0)
	require.NoError(t, err)
	require.Equal(t, int32(1), s.NumWindows())

	prof := s.Compute()
	assert.Equal(t, int32(-1), prof.Index[0], "a lone window has no neighbour")
	assert.True(t, math.IsInf(prof.Distance[0], 1))

	_, _, _, foundMotif := prof.Motif()
	assert.False(t, foundMotif)
	_, _, foundDiscord := prof.Discord()
	assert.False(t, foundDiscord)
}

func TestNewSeriesDoesNotMutateInput(t *testing.T) {
	values := []float64{5, 1, 4, 1, 5, 9, 2, 6}
	before := make([]float64, len(values))
	copy(before, values)

	_, err := matrixprofile.NewSeriesE(values, 3, 0.0)
	require.NoError(t, err)
	assert.Equal(t, before, values, "centering must not touch the caller's slice")
}

// TestPropertySoundOnAnyInput asserts what holds for *every* series, including
// the pathological ones an unconstrained float64 generator reaches.
//
// Optimality is not among them. Neighbour selection runs on the dot-product
// identity over globally centered values, and on a series whose local scales
// differ by many orders of magnitude — a stretch of 1.0 followed by a tail of
// 1e-8 — the centered representation cannot resolve the tail's shape at all, so
// the search can return a genuinely worse neighbour. That is a property of
// STOMP, documented in the package; TestPropertyOptimalOnWellConditioned covers
// the case where it does not apply.
//
// What must hold unconditionally is soundness: the reported distance is the
// real distance to the reported neighbour, and it never understates the true
// minimum. Under-reporting would mean inventing a match that is not there,
// which for a motif or discord search is the failure that matters.
func TestPropertySoundOnAnyInput(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(12, 45).Draw(rt, "n")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))
		values := rapid.SliceOfN(
			rapid.Float64Range(-50.0, 50.0), n, n,
		).Draw(rt, "values")

		s, err := matrixprofile.NewSeriesE(values, window, 0.0)
		require.NoError(rt, err)
		prof := s.Compute()

		wantDist, wantIdx := oracleProfile(values, window)
		for i := range wantDist {
			if wantIdx[i] < 0 {
				continue
			}
			toNeighbour := oracleDistance(values, int32(i), prof.Index[i], window)
			require.InDelta(rt, toNeighbour, prof.Distance[i],
				refinedTolerance(window, toNeighbour),
				"reported distance at %d is not the distance to reported neighbour %d", i, prof.Index[i])
			require.GreaterOrEqual(rt, prof.Distance[i],
				wantDist[i]-refinedTolerance(window, wantDist[i]),
				"position %d reports a neighbour nearer than the true minimum", i)
		}
	})
}

// TestPropertyOptimalOnWellConditioned asserts that the search finds the true
// nearest neighbour once the input is within the conditioning the dot-product
// identity can resolve.
//
// Values are quantized to hundredths over a bounded range, which is what
// ordinary recorded signals look like and what the generator in
// TestPropertySoundOnAnyInput deliberately is not.
func TestPropertyOptimalOnWellConditioned(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(12, 45).Draw(rt, "n")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))
		quantized := rapid.SliceOfN(
			rapid.IntRange(-5000, 5000), n, n,
		).Draw(rt, "values")
		values := make([]float64, n)
		for i, q := range quantized {
			values[i] = float64(q) / 100.0
		}

		s, err := matrixprofile.NewSeriesE(values, window, 0.0)
		require.NoError(rt, err)
		prof := s.Compute()

		wantDist, wantIdx := oracleProfile(values, window)
		for i := range wantDist {
			if wantIdx[i] < 0 {
				continue
			}
			require.InDelta(rt, wantDist[i], prof.Distance[i],
				refinedTolerance(window, wantDist[i]),
				"position %d did not find the true nearest neighbour", i)
		}
	})
}

func TestPropertyAffineInvariance(t *testing.T) {
	// Z-normalization is invariant to a positive or negative affine transform of
	// the whole series, so the profile must be too.
	//
	// Values are drawn quantized to hundredths rather than as free float64s.
	// That is not to dodge hard cases — TestPropertyProfileMatchesOracle keeps
	// the unconstrained generator — but because the property being asserted is
	// arithmetic, not floating-point. An unconstrained draw produces series
	// whose structure lives in the last few ULPs (a run of 1.0 with one
	// 1.0000000000000107 among them); adding an offset rounds that structure
	// away, so two windows that differed in the base series become bit-identical
	// in the transformed one. Invariance genuinely fails there, in any
	// implementation, and asserting otherwise would test IEEE-754 rather than
	// this package.
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(16, 40).Draw(rt, "n")
		window := int32(rapid.IntRange(3, n/3).Draw(rt, "window"))
		quantized := rapid.SliceOfN(
			rapid.IntRange(-2000, 2000), n, n,
		).Draw(rt, "values")
		values := make([]float64, n)
		for i, q := range quantized {
			values[i] = float64(q) / 100.0
		}
		scale := rapid.Float64Range(-8.0, 8.0).
			Filter(func(v float64) bool { return math.Abs(v) > 0.25 }).
			Draw(rt, "scale")
		offset := rapid.Float64Range(-100.0, 100.0).Draw(rt, "offset")

		transformed := make([]float64, n)
		for i, v := range values {
			transformed[i] = v*scale + offset
		}

		base, err := matrixprofile.NewSeriesE(values, window, 0.0)
		require.NoError(rt, err)
		other, err := matrixprofile.NewSeriesE(transformed, window, 0.0)
		require.NoError(rt, err)

		baseProf := base.Compute()
		otherProf := other.Compute()
		for i := range baseProf.Distance {
			if math.IsInf(baseProf.Distance[i], 1) {
				continue
			}
			require.InDelta(rt, baseProf.Distance[i], otherProf.Distance[i],
				refinedTolerance(window, baseProf.Distance[i]),
				"affine transform changed the profile at %d", i)
		}
	})
}

func TestPropertyDistanceBounds(t *testing.T) {
	// A z-normalized subsequence has norm sqrt(m), so any pair is at most
	// 2·sqrt(m) apart and never less than 0.
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(12, 50).Draw(rt, "n")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))
		values := rapid.SliceOfN(
			rapid.Float64Range(-100.0, 100.0), n, n,
		).Draw(rt, "values")

		s, err := matrixprofile.NewSeriesE(values, window, 0.0)
		require.NoError(rt, err)

		maxDist := 2.0 * math.Sqrt(float64(window))
		prof := s.Compute()
		for i, d := range prof.Distance {
			if math.IsInf(d, 1) {
				require.Equal(rt, int32(-1), prof.Index[i])
				continue
			}
			require.GreaterOrEqual(rt, d, 0.0, "negative distance at %d", i)
			require.LessOrEqual(rt, d, maxDist+1.0e-9, "distance exceeds 2*sqrt(m) at %d", i)
		}
	})
}

func syntheticSine(n int, period int, noise float64) (out []float64) {
	out = make([]float64, n)
	// A fixed linear-congruential sequence keeps the test deterministic without
	// pulling in a seeded rand.Source.
	state := uint32(0x9e3779b9)
	for i := range out {
		out[i] = math.Sin(2.0 * math.Pi * float64(i) / float64(period))
		if noise > 0.0 {
			state = state*1664525 + 1013904223
			out[i] += noise * (float64(state>>8)/float64(1<<24) - 0.5)
		}
	}
	return
}

func syntheticSawtooth(n int, period int) (out []float64) {
	out = make([]float64, n)
	for i := range out {
		out[i] = float64(i%period) / float64(period)
	}
	return
}

func withOffset(values []float64, offset float64) (out []float64) {
	out = make([]float64, len(values))
	for i, v := range values {
		out[i] = v + offset
	}
	return
}

func withScale(values []float64, scale float64) (out []float64) {
	out = make([]float64, len(values))
	for i, v := range values {
		out[i] = v * scale
	}
	return
}
