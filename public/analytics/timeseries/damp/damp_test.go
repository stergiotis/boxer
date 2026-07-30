package damp_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/analytics/timeseries/damp"
)

// The oracle materializes each z-normalized subsequence and takes the Euclidean
// distance directly, sharing no code path with the implementation's dot-product
// identity. It applies no constant-window floor, so tests using it must supply
// data whose windows all carry real variance; TestConstantHistory covers the
// degenerate case separately.

func oracleZNorm(values []float64, idx int32, window int32) (out []float64) {
	seg := values[idx : idx+window]
	out = make([]float64, window)

	var mean float64
	for _, v := range seg {
		mean += v
	}
	mean /= float64(window)

	var ss float64
	for _, v := range seg {
		d := v - mean
		ss += d * d
	}
	std := math.Sqrt(ss / float64(window))
	if std == 0.0 {
		return
	}
	for i, v := range seg {
		out[i] = (v - mean) / std
	}
	return
}

func oracleDistance(values []float64, i int32, j int32, window int32) (dist float64) {
	a := oracleZNorm(values, i, window)
	b := oracleZNorm(values, j, window)
	var ss float64
	for k := range a {
		d := a[k] - b[k]
		ss += d * d
	}
	dist = math.Sqrt(ss)
	return
}

// oracleLeftMP computes each window's true left-discord distance by brute
// force: the nearest neighbour among windows starting at least zone earlier.
func oracleLeftMP(values []float64, window int32) (dist []float64) {
	numWindows := int32(len(values)) - window + 1
	zone := (window + 3) / 4
	dist = make([]float64, numWindows)
	for q := range numWindows {
		dist[q] = math.Inf(1)
		for j := int32(0); j <= q-zone; j++ {
			if d := oracleDistance(values, q, j, window); d < dist[q] {
				dist[q] = d
			}
		}
	}
	return
}

// tolerance mirrors the accuracy the package documents: distances are
// recomputed from materialized z-normalized values, so ordinary rounding
// applies rather than the identity's floor near ρ = 1.
func tolerance(window int32, want float64) (tol float64) {
	tol = 1.0e-9*math.Sqrt(float64(window)) + 1.0e-9*math.Abs(want)
	return
}

// quasiPeriodic is a non-repeating background with healthy local variance, so
// no window trips the constant-window floor.
func quasiPeriodic(n int, period float64, noise float64) (out []float64) {
	out = make([]float64, n)
	rng := rand.New(rand.NewPCG(0x5eed, 0xf00d))
	for i := range out {
		t := float64(i)
		out[i] = math.Sin(2.0*math.Pi*t/period) +
			0.6*math.Sin(2.0*math.Pi*t/(period*0.618)+0.7) +
			0.35*math.Sin(2.0*math.Pi*t/(period*1.618)+1.9)
		if noise > 0.0 {
			out[i] += rng.NormFloat64() * noise
		}
	}
	return
}

func TestExactModeMatchesBruteForceOracle(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
		window int32
	}{
		{name: "quasi-periodic", values: quasiPeriodic(400, 37, 0.05), window: 20},
		{name: "short window", values: quasiPeriodic(300, 23, 0.02), window: 5},
		{name: "large offset", values: addOffset(quasiPeriodic(300, 29, 0.03), 1.0e9), window: 16},
		{name: "tiny scale", values: scaleBy(quasiPeriodic(300, 29, 0.03), 1.0e-6), window: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := damp.Config{Window: tt.window, TrainLength: tt.window * 4, Exact: true}
			readings, err := damp.ScoreE(tt.values, cfg)
			require.NoError(t, err)
			require.NotEmpty(t, readings)

			want := oracleLeftMP(tt.values, tt.window)
			for _, r := range readings {
				require.True(t, r.Exact, "exact mode must mark every reading exact")
				w := want[r.Start]
				require.False(t, math.IsInf(w, 1), "oracle has no neighbour at %d", r.Start)
				assert.InDelta(t, w, r.Score, tolerance(tt.window, w), "left-discord distance at %d", r.Start)
			}
		})
	}
}

func TestDAMPFindsTheSameDiscordAsExact(t *testing.T) {
	// Early abandoning must not change *where* the discord is, which is the one
	// thing DAMP promises.
	values := quasiPeriodic(600, 41, 0.04)
	// Plant something that belongs nowhere: a stretch from a different phase.
	for i := range 30 {
		values[420+i] = math.Sin(2.0*math.Pi*float64(i)/7.0) * 1.5
	}
	const window = int32(30)

	exact, err := damp.ScoreE(values, damp.Config{Window: window, TrainLength: 120, Exact: true})
	require.NoError(t, err)
	fast, err := damp.ScoreE(values, damp.Config{Window: window, TrainLength: 120})
	require.NoError(t, err)
	require.Len(t, fast, len(exact))

	assert.Equal(t, argmaxStart(exact), argmaxStart(fast), "DAMP and exact must agree on the discord")
	assert.InDelta(t, maxScore(exact), maxScore(fast), tolerance(window, maxScore(exact)),
		"and on its score")
}

func TestDAMPScoresAreUpperBounds(t *testing.T) {
	// The documented contract for a non-exact reading: it never *understates*
	// the true left-discord distance, and it sits below the running maximum —
	// which together are exactly what makes the argmax safe.
	values := quasiPeriodic(500, 31, 0.05)
	const window = int32(24)

	readings, err := damp.ScoreE(values, damp.Config{Window: window, TrainLength: 100})
	require.NoError(t, err)
	want := oracleLeftMP(values, window)

	var abandoned int
	best := math.Inf(-1)
	for _, r := range readings {
		w := want[r.Start]
		if r.Exact {
			assert.InDelta(t, w, r.Score, tolerance(window, w), "exact reading at %d", r.Start)
			if r.Score > best {
				best = r.Score
			}
			continue
		}
		abandoned++
		assert.GreaterOrEqual(t, r.Score, w-tolerance(window, w),
			"an abandoned reading at %d understates the true distance", r.Start)
		assert.Less(t, r.Score, best+tolerance(window, best),
			"an abandoned reading at %d is not below the running maximum", r.Start)
	}
	assert.Greater(t, abandoned, len(readings)/2,
		"most subsequences on ordinary data should abandon, or DAMP buys nothing")
}

func TestCentreIsHalfAWindowAfterStart(t *testing.T) {
	values := quasiPeriodic(300, 29, 0.03)
	const window = int32(20)
	readings, err := damp.ScoreE(values, damp.Config{Window: window, TrainLength: 80, Exact: true})
	require.NoError(t, err)
	require.NotEmpty(t, readings)

	for _, r := range readings {
		assert.Equal(t, r.Start+int64(window/2), r.Centre)
	}
	// Readings arrive in order and cover every position after the warm-up.
	for i := 1; i < len(readings); i++ {
		assert.Equal(t, readings[i-1].Start+1, readings[i].Start, "readings must be contiguous")
	}
}

func TestPushMatchesScore(t *testing.T) {
	values := quasiPeriodic(350, 31, 0.04)
	cfg := damp.Config{Window: 18, TrainLength: 90, Exact: true}

	want, err := damp.ScoreE(values, cfg)
	require.NoError(t, err)

	inst, err := damp.NewDetectorE(cfg)
	require.NoError(t, err)
	got := make([]damp.Reading, 0, len(want))
	for _, v := range values {
		if r, ok := inst.Push(v); ok {
			got = append(got, r)
		}
	}
	assert.Equal(t, want, got, "ScoreE must be exactly the push loop")
	assert.Equal(t, int64(len(values)), inst.Count())

	best, found := inst.BestSoFar()
	assert.True(t, found)
	assert.InDelta(t, maxScore(want), best, 1.0e-12)
}

func TestHistoryLimitLargeEnoughChangesNothing(t *testing.T) {
	values := quasiPeriodic(500, 37, 0.04)
	cfg := damp.Config{Window: 20, TrainLength: 100, Exact: true}

	unlimited, err := damp.ScoreE(values, cfg)
	require.NoError(t, err)

	capped := cfg
	capped.HistoryLimit = int32(len(values))
	limited, err := damp.ScoreE(values, capped)
	require.NoError(t, err)

	assert.Equal(t, unlimited, limited, "a limit above the stream length must be inert")
}

func TestHistoryLimitBoundsMemoryAndNarrowsTheGuarantee(t *testing.T) {
	// A short horizon is not merely an approximation of the unlimited answer: a
	// pattern that recurs only outside the horizon reads as novel again, which
	// raises scores rather than lowering them.
	values := quasiPeriodic(900, 31, 0.03)
	cfg := damp.Config{Window: 20, TrainLength: 100, Exact: true}

	unlimited, err := damp.ScoreE(values, cfg)
	require.NoError(t, err)

	capped := cfg
	capped.HistoryLimit = 150
	limited, err := damp.ScoreE(values, capped)
	require.NoError(t, err)
	require.Len(t, limited, len(unlimited))

	var raised int
	for i := range limited {
		assert.GreaterOrEqual(t, limited[i].Score, unlimited[i].Score-1.0e-9,
			"a shorter horizon can only lose neighbours, never gain them, at %d", i)
		if limited[i].Score > unlimited[i].Score+1.0e-9 {
			raised++
		}
	}
	assert.Greater(t, raised, 0, "a 150-sample horizon should forget something over 900 samples")
}

func TestConstantHistory(t *testing.T) {
	values := make([]float64, 300)
	for i := range values {
		values[i] = 7.0
	}
	readings, err := damp.ScoreE(values, damp.Config{Window: 16, TrainLength: 64, Exact: true})
	require.NoError(t, err)
	require.NotEmpty(t, readings)

	for _, r := range readings {
		assert.InDelta(t, 0.0, r.Score, 1.0e-12,
			"constant windows all normalize to the zero vector, so they coincide")
	}
}

func TestNewDetectorRejectsBadConfig(t *testing.T) {
	_, err := damp.NewDetectorE(damp.Config{Window: 1})
	assert.Error(t, err, "window below 2")

	_, err = damp.NewDetectorE(damp.Config{Window: 20, TrainLength: 30})
	assert.Error(t, err, "train length below two windows")

	_, err = damp.NewDetectorE(damp.Config{Window: 20, TrainLength: 200, HistoryLimit: 100})
	assert.Error(t, err, "history limit below train length")

	_, err = damp.NewDetectorE(damp.Config{Window: 20})
	assert.NoError(t, err, "train length should default")
}

func TestNoReadingsBeforeTraining(t *testing.T) {
	inst, err := damp.NewDetectorE(damp.Config{Window: 10, TrainLength: 100})
	require.NoError(t, err)

	values := quasiPeriodic(200, 23, 0.02)
	for i, v := range values {
		_, ok := inst.Push(v)
		if i < 100 {
			assert.False(t, ok, "nothing may be reported at sample %d, still training", i)
		}
	}
	best, found := inst.BestSoFar()
	assert.True(t, found)
	assert.Greater(t, best, 0.0)
}

func TestExactModeScoresTheADScoreFixtures(t *testing.T) {
	// End to end against M2's flaw-resistant fixtures. Exact mode produces a
	// real score vector, so it should land in the same range the batch matrix
	// profile reached — and well clear of the one-liners.
	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			spec := adscore.DefaultFixtureSpec(kind, 23)
			f, err := adscore.GenerateE(spec)
			require.NoError(t, err)

			window := int32(spec.Period)
			readings, err := damp.ScoreE(f.Values, damp.Config{
				Window:      window,
				TrainLength: window * 8,
				Exact:       true,
			})
			require.NoError(t, err)

			scores := damp.PositionScores(readings, int32(len(f.Values)), nil)
			m, err := adscore.EvaluateE(scores, f.Labels, 0)
			require.NoError(t, err)

			_, worst, err := adscore.TrivialityE(f, 0)
			require.NoError(t, err)

			t.Logf("left-discord VUS-PR=%.4f, best one-liner VUS-PR=%.4f", m.VUSPR, worst)
			assert.Greater(t, m.VUSPR, 0.3, "a causal detector must still find these")
			assert.Greater(t, m.VUSPR, worst*2.0, "and clear the one-liners")
		})
	}
}

func TestDAMPLocatesTheAnomalyEvenThoughItsScoreVectorIsMeaningless(t *testing.T) {
	// The package's central caveat, asserted rather than only documented: DAMP's
	// argmax lands on a labelled anomaly, while its score *vector* scores far
	// worse than exact mode's under VUS-PR because abandoned positions carry
	// upper bounds instead of distances.
	spec := adscore.DefaultFixtureSpec(adscore.AnomalyKindTransplant, 23)
	f, err := adscore.GenerateE(spec)
	require.NoError(t, err)

	window := int32(spec.Period)
	cfg := damp.Config{Window: window, TrainLength: window * 8}

	fast, err := damp.ScoreE(f.Values, cfg)
	require.NoError(t, err)
	exactCfg := cfg
	exactCfg.Exact = true
	exact, err := damp.ScoreE(f.Values, exactCfg)
	require.NoError(t, err)

	// The located discord's window overlaps a labelled range.
	//
	// Overlap rather than containment, because the window here is 50 samples
	// against a 20-sample anomaly: the highest-scoring window brackets the
	// anomaly instead of centring on it, starting just before the novel content
	// enters. Its centre lands past the label's trailing edge. Centre attribution
	// still wins on average — the plateau of high-scoring windows is what a
	// per-position scorer integrates — but the single argmax is a span, not a
	// point, whenever the window outruns the anomaly.
	at := argmaxStart(fast)
	assert.True(t, overlapsLabel(f.Labels, at, int64(window)),
		"the window DAMP reports as the discord should overlap a labelled range")

	fastM, err := adscore.EvaluateE(damp.PositionScores(fast, int32(len(f.Values)), nil), f.Labels, 0)
	require.NoError(t, err)
	exactM, err := adscore.EvaluateE(damp.PositionScores(exact, int32(len(f.Values)), nil), f.Labels, 0)
	require.NoError(t, err)

	t.Logf("VUS-PR: damp=%.4f exact=%.4f", fastM.VUSPR, exactM.VUSPR)
	assert.Less(t, fastM.VUSPR, exactM.VUSPR,
		"DAMP's score vector must not be mistaken for a calibrated one")
}

func TestPositionScoresUsesCentres(t *testing.T) {
	readings := []damp.Reading{
		{Score: 1.0, Start: 0, Centre: 5},
		{Score: 3.0, Start: 1, Centre: 6},
		{Score: 2.0, Start: 2, Centre: 6},
		{Score: 9.0, Start: 3, Centre: 100},
	}
	got := damp.PositionScores(readings, 10, nil)
	require.Len(t, got, 10)
	assert.Equal(t, 1.0, got[5])
	assert.Equal(t, 3.0, got[6], "colliding centres keep the larger score")
	assert.Equal(t, 0.0, got[0], "nothing lands on a start position")
	for _, v := range got {
		assert.LessOrEqual(t, v, 3.0, "an out-of-range centre must be dropped, not clamped")
	}
}

func TestPropertyDAMPAgreesWithExactOnTheDiscord(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(150, 400).Draw(rt, "n")
		window := int32(rapid.IntRange(4, 20).Draw(rt, "window"))
		period := rapid.Float64Range(9.0, 45.0).Draw(rt, "period")
		values := quasiPeriodic(n, period, 0.05)

		cfg := damp.Config{Window: window, TrainLength: window * 4}
		fast, err := damp.ScoreE(values, cfg)
		require.NoError(rt, err)
		exactCfg := cfg
		exactCfg.Exact = true
		exact, err := damp.ScoreE(values, exactCfg)
		require.NoError(rt, err)
		require.Len(rt, fast, len(exact))
		if len(exact) == 0 {
			return
		}

		require.InDelta(rt, maxScore(exact), maxScore(fast),
			tolerance(window, maxScore(exact)), "DAMP must not miss the discord's score")
	})
}

func TestPropertyExactScoresAreNonNegativeAndBounded(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(120, 350).Draw(rt, "n")
		window := int32(rapid.IntRange(3, 18).Draw(rt, "window"))
		quantized := rapid.SliceOfN(rapid.IntRange(-2000, 2000), n, n).Draw(rt, "values")
		values := make([]float64, n)
		for i, q := range quantized {
			values[i] = float64(q) / 100.0
		}

		readings, err := damp.ScoreE(values, damp.Config{
			Window: window, TrainLength: window * 4, Exact: true,
		})
		require.NoError(rt, err)

		maxDist := 2.0 * math.Sqrt(float64(window))
		for _, r := range readings {
			require.False(rt, math.IsNaN(r.Score), "NaN score at %d", r.Start)
			require.GreaterOrEqual(rt, r.Score, 0.0)
			require.LessOrEqual(rt, r.Score, maxDist+1.0e-9)
		}
	})
}

func BenchmarkPushExact(b *testing.B) {
	values := quasiPeriodic(20000, 50, 0.05)
	cfg := damp.Config{Window: 50, TrainLength: 400, HistoryLimit: 8000, Exact: true}
	benchmarkPush(b, values, cfg)
}

func BenchmarkPushDAMP(b *testing.B) {
	values := quasiPeriodic(20000, 50, 0.05)
	cfg := damp.Config{Window: 50, TrainLength: 400, HistoryLimit: 8000}
	benchmarkPush(b, values, cfg)
}

func benchmarkPush(b *testing.B, values []float64, cfg damp.Config) {
	b.Helper()
	guardThrottling(b)
	b.ReportAllocs()
	for b.Loop() {
		inst, err := damp.NewDetectorE(cfg)
		if err != nil {
			b.Fatal(err)
		}
		for _, v := range values {
			inst.Push(v)
		}
	}
	b.ReportMetric(float64(b.N*len(values))/b.Elapsed().Seconds(), "samples/s")
}

func addOffset(values []float64, offset float64) (out []float64) {
	out = make([]float64, len(values))
	for i, v := range values {
		out[i] = v + offset
	}
	return
}

func scaleBy(values []float64, scale float64) (out []float64) {
	out = make([]float64, len(values))
	for i, v := range values {
		out[i] = v * scale
	}
	return
}

// overlapsLabel reports whether [start, start+window) touches any true label.
func overlapsLabel(labels []bool, start int64, window int64) (hit bool) {
	for i := start; i < start+window && int(i) < len(labels); i++ {
		if i >= 0 && labels[i] {
			hit = true
			return
		}
	}
	return
}

func argmaxStart(readings []damp.Reading) (start int64) {
	best := math.Inf(-1)
	start = -1
	for _, r := range readings {
		if r.Score > best {
			best = r.Score
			start = r.Start
		}
	}
	return
}

func maxScore(readings []damp.Reading) (best float64) {
	best = math.Inf(-1)
	for _, r := range readings {
		if r.Score > best {
			best = r.Score
		}
	}
	return
}
