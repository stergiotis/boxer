package adscore_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
)

func TestGenerateIsDeterministic(t *testing.T) {
	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			a, err := adscore.GenerateE(adscore.DefaultFixtureSpec(kind, 42))
			require.NoError(t, err)
			b, err := adscore.GenerateE(adscore.DefaultFixtureSpec(kind, 42))
			require.NoError(t, err)
			assert.Equal(t, a.Values, b.Values, "same seed must give the same series")
			assert.Equal(t, a.Labels, b.Labels)

			c, err := adscore.GenerateE(adscore.DefaultFixtureSpec(kind, 43))
			require.NoError(t, err)
			assert.NotEqual(t, a.Values, c.Values, "a different seed must give a different series")
		})
	}
}

func TestGenerateAvoidsTheFourFlaws(t *testing.T) {
	// Flaws 2, 3 and 4 from Wu and Keogh are structural and checkable here.
	// Flaw 1 — anomalies a one-liner finds — is what TestFixturesResistOneLiners
	// covers, since it cannot be checked without scoring.
	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			spec := adscore.DefaultFixtureSpec(kind, 7)
			f, err := adscore.GenerateE(spec)
			require.NoError(t, err)

			t.Run("anomaly density is realistic", func(t *testing.T) {
				frac := f.AnomalyFraction()
				assert.Greater(t, frac, 0.0)
				assert.Less(t, frac, 0.05, "anomalies must stay rare")
			})

			t.Run("ground truth matches what was injected", func(t *testing.T) {
				assert.Equal(t, spec.AnomalyCount, f.Ranges.Len(),
					"ranges must not have merged into fewer, longer ones")
				for i := range f.Ranges.Len() {
					length := f.Ranges.End[i] - f.Ranges.Start[i] + 1
					assert.Equal(t, spec.AnomalyLength, length, "range %d has the wrong length", i)
				}
			})

			t.Run("nothing lands in the excluded tail", func(t *testing.T) {
				cutoff := int32(float64(spec.Length) * (1.0 - spec.TailExclusionFrac))
				for i := range f.Ranges.Len() {
					assert.Less(t, f.Ranges.End[i], cutoff,
						"range %d reaches into the run-to-failure tail", i)
				}
			})

			t.Run("nothing touches a boundary", func(t *testing.T) {
				for i := range f.Ranges.Len() {
					assert.Greater(t, f.Ranges.Start[i], int32(0))
					assert.Less(t, f.Ranges.End[i], spec.Length-1)
				}
			})
		})
	}
}

func TestFixturesResistOneLiners(t *testing.T) {
	// The check the benchmark literature implies but does not perform. If a
	// one-liner scores well here, the fixture is measuring the wrong thing.
	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			f, err := adscore.GenerateE(adscore.DefaultFixtureSpec(kind, 11))
			require.NoError(t, err)

			results, worst, err := adscore.TrivialityE(f, 0)
			require.NoError(t, err)
			require.Len(t, results, len(adscore.AllBaselines))

			for _, r := range results {
				t.Logf("%-24s VUS-PR=%.4f AUC-PR=%.4f", r.Baseline, r.Measures.VUSPR, r.Measures.AUCPR)
			}
			assert.Less(t, worst, adscore.TrivialityThreshold,
				"a one-liner solved this fixture, so it cannot validate a detector")
			// Measured at the time of writing: reversal 0.096, transplant 0.035,
			// phase-jump 0.064, frequency-shift 0.116. Well clear of the bar, and
			// worth noticing if a change moves them.
			assert.Less(t, worst, 0.20, "one-liner performance drifted upward")
		})
	}
}

func TestBaselineScoresAreFinite(t *testing.T) {
	f, err := adscore.GenerateE(adscore.DefaultFixtureSpec(adscore.AnomalyKindTransplant, 3))
	require.NoError(t, err)

	for _, b := range adscore.AllBaselines {
		scores := adscore.BaselineScores(f.Values, b, 20)
		require.Len(t, scores, len(f.Values))
		for i, v := range scores {
			require.False(t, math.IsNaN(v), "%s produced NaN at %d", b, i)
			require.False(t, math.IsInf(v, 0), "%s produced Inf at %d", b, i)
			require.GreaterOrEqual(t, v, 0.0, "%s produced a negative score at %d", b, i)
		}
	}
}

// matrixProfileScores turns a matrix profile into a per-position anomaly score.
//
// Two choices here are not incidental, and getting either wrong costs more than
// half the achievable accuracy on these fixtures:
//
// The score for the window starting at i describes the whole span [i, i+window),
// so it belongs at that span's centre. Leaving it at the start displaces every
// peak by half a window — measured at VUS-PR 0.255 start-aligned against 0.587
// centre-aligned, same data, same window.
//
// The window tracks the signal's period rather than the expected anomaly
// length. A window shorter than the pattern being violated cannot see the
// violation: at window 20 against a period of 50 the profile scores about 0.15
// regardless of alignment, which is barely above the one-liners.
func matrixProfileScores(t *testing.T, values []float64, window int32) (scores []float64) {
	t.Helper()
	series, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)
	prof := series.Compute()

	scores = make([]float64, len(values))
	centre := window / 2
	for i, d := range prof.Distance {
		p := int32(i) + centre
		if int(p) < len(scores) && d > scores[p] {
			scores[p] = d
		}
	}
	return
}

func TestMatrixProfileBeatsTheOneLiners(t *testing.T) {
	// End-to-end: the detector these fixtures exist to validate must clear the
	// one-liners by a wide margin on every kind. Without this, "resists
	// one-liners" would also be satisfied by labelling pure noise.
	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			spec := adscore.DefaultFixtureSpec(kind, 23)
			f, err := adscore.GenerateE(spec)
			require.NoError(t, err)

			scores := matrixProfileScores(t, f.Values, int32(spec.Period))
			m, err := adscore.EvaluateE(scores, f.Labels, 0)
			require.NoError(t, err)

			_, worst, err := adscore.TrivialityE(f, 0)
			require.NoError(t, err)

			t.Logf("matrix profile VUS-PR=%.4f, best one-liner VUS-PR=%.4f", m.VUSPR, worst)
			assert.Greater(t, m.VUSPR, 0.45, "the fixture must be solvable by a shape-aware detector")
			assert.Greater(t, m.VUSPR, worst*3.0, "and by a clear margin over the one-liners")
		})
	}
}

func TestGenerateRejectsBadSpec(t *testing.T) {
	base := adscore.DefaultFixtureSpec(adscore.AnomalyKindReversal, 1)

	short := base
	short.Length = 8
	_, err := adscore.GenerateE(short)
	assert.Error(t, err, "series too short")

	badPeriod := base
	badPeriod.Period = 1.0
	_, err = adscore.GenerateE(badPeriod)
	assert.Error(t, err, "degenerate period")

	badLen := base
	badLen.AnomalyLength = 1
	_, err = adscore.GenerateE(badLen)
	assert.Error(t, err, "anomaly too short")

	badCount := base
	badCount.AnomalyCount = 0
	_, err = adscore.GenerateE(badCount)
	assert.Error(t, err, "no anomalies requested")

	badTail := base
	badTail.TailExclusionFrac = 1.0
	_, err = adscore.GenerateE(badTail)
	assert.Error(t, err, "tail exclusion of the whole series")

	crowded := base
	crowded.AnomalyCount = 200
	_, err = adscore.GenerateE(crowded)
	assert.Error(t, err, "too many anomalies to place disjointly")
}

func TestAnomalyKindStringsAreDistinct(t *testing.T) {
	seen := make(map[string]struct{}, 4)
	for _, kind := range adscore.AllAnomalyKinds {
		name := kind.String()
		assert.NotEqual(t, "unknown", name)
		_, dup := seen[name]
		assert.False(t, dup, "duplicate name %q", name)
		seen[name] = struct{}{}
	}
	for _, b := range adscore.AllBaselines {
		assert.NotEqual(t, "unknown", b.String())
	}
}
