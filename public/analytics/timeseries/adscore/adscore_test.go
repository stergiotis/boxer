package adscore_test

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
)

// labelsFrom builds a boolean vector of length n with the given inclusive
// ranges marked.
func labelsFrom(n int, ranges ...[2]int) (labels []bool) {
	labels = make([]bool, n)
	for _, r := range ranges {
		for i := r[0]; i <= r[1]; i++ {
			labels[i] = true
		}
	}
	return
}

// scoresFromLabels turns labels into a perfect score vector.
func scoresFromLabels(labels []bool) (scores []float64) {
	scores = make([]float64, len(labels))
	for i, l := range labels {
		if l {
			scores[i] = 1.0
		}
	}
	return
}

func TestRangesFromLabels(t *testing.T) {
	tests := []struct {
		name      string
		labels    []bool
		wantStart []int32
		wantEnd   []int32
	}{
		{
			name:      "single interior range",
			labels:    labelsFrom(10, [2]int{3, 5}),
			wantStart: []int32{3},
			wantEnd:   []int32{5},
		},
		{
			name:      "two ranges",
			labels:    labelsFrom(20, [2]int{2, 4}, [2]int{11, 15}),
			wantStart: []int32{2, 11},
			wantEnd:   []int32{4, 15},
		},
		{
			name:      "range touching both boundaries",
			labels:    []bool{true, true, false, true},
			wantStart: []int32{0, 3},
			wantEnd:   []int32{1, 3},
		},
		{
			name:      "no anomalies",
			labels:    make([]bool, 5),
			wantStart: []int32{},
			wantEnd:   []int32{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := adscore.RangesFromLabels(tt.labels)
			assert.Equal(t, tt.wantStart, got.Start)
			assert.Equal(t, tt.wantEnd, got.End)
		})
	}
}

func TestBufferLabelsShape(t *testing.T) {
	const n = int32(40)
	ranges := adscore.RangesFromLabels(labelsFrom(int(n), [2]int{20, 24}))

	t.Run("zero buffer reduces to the binary label", func(t *testing.T) {
		got := adscore.BufferLabels(ranges, n, 0, nil)
		for i := range got {
			want := 0.0
			if i >= 20 && i <= 24 {
				want = 1.0
			}
			assert.Equal(t, want, got[i], "position %d", i)
		}
	})

	t.Run("buffer decays outward and stops", func(t *testing.T) {
		const buffer = int32(8)
		got := adscore.BufferLabels(ranges, n, buffer, nil)

		for i := 20; i <= 24; i++ {
			assert.Equal(t, 1.0, got[i], "inside the range at %d", i)
		}
		// Half-width is buffer/2 = 4 positions on each side.
		for k := int32(1); k <= 4; k++ {
			want := math.Sqrt(1.0 - float64(k)/float64(buffer))
			assert.InDelta(t, want, got[20-k], 1.0e-12, "left ramp at distance %d", k)
			assert.InDelta(t, want, got[24+k], 1.0e-12, "right ramp at distance %d", k)
		}
		assert.Equal(t, 0.0, got[20-5], "beyond the buffer on the left")
		assert.Equal(t, 0.0, got[24+5], "beyond the buffer on the right")
	})

	t.Run("ramp is monotone and never exceeds one", func(t *testing.T) {
		got := adscore.BufferLabels(ranges, n, 12, nil)
		for i := range got {
			assert.LessOrEqual(t, got[i], 1.0, "position %d", i)
			assert.GreaterOrEqual(t, got[i], 0.0, "position %d", i)
		}
		for k := int32(1); k < 6; k++ {
			assert.Greater(t, got[20-k], got[20-k-1], "left ramp should decay outward at %d", k)
		}
	})
}

func TestBufferLabelsOverlapTakesMaximum(t *testing.T) {
	// Two ranges close enough that their buffers overlap. The shared positions
	// must take the larger contribution, not the sum.
	const n = int32(40)
	ranges := adscore.RangesFromLabels(labelsFrom(int(n), [2]int{10, 12}, [2]int{20, 22}))
	got := adscore.BufferLabels(ranges, n, 16, nil)

	for i := range got {
		require.LessOrEqual(t, got[i], 1.0, "superposition must clamp at 1, position %d", i)
	}
	// Midway between the two ranges both buffers reach; the value is the max of
	// two equal ramps, so it equals a single ramp at that distance.
	want := math.Sqrt(1.0 - 4.0/16.0)
	assert.InDelta(t, want, got[16], 1.0e-12)
}

func TestBufferLabelsClipsAtSeriesEdges(t *testing.T) {
	const n = int32(12)
	ranges := adscore.RangesFromLabels(labelsFrom(int(n), [2]int{1, 2}))
	got := adscore.BufferLabels(ranges, n, 8, nil)

	require.Len(t, got, int(n))
	assert.Greater(t, got[0], 0.0, "the buffer should reach position 0")
	for _, v := range got {
		assert.False(t, math.IsNaN(v))
	}
}

func TestPerfectDetectorCapsBelowOneUnderVUS(t *testing.T) {
	// A detector that fires on exactly the labelled extent and nowhere else
	// scores a clean 1.0 point-wise but caps around 0.92 under VUS, and that is
	// the measure working as defined rather than a defect.
	//
	// Widening the buffer adds positive label mass at positions the detector
	// scored 0, so recall cannot reach 1 at the top threshold. VUS therefore
	// does not reward exact localisation with a perfect score; it rewards
	// approximate localisation and treats exactness as one point on that scale.
	// Anyone reading a VUS value as a fraction of attainable skill needs this.
	labels := labelsFrom(4000, [2]int{500, 549}, [2]int{1500, 1549}, [2]int{2500, 2549})
	m, err := adscore.EvaluateE(scoresFromLabels(labels), labels, 0)
	require.NoError(t, err)

	assert.InDelta(t, 1.0, m.AUCROC, 1.0e-9, "point-wise ROC is exactly 1")
	assert.InDelta(t, 1.0, m.AUCPR, 1.0e-9, "point-wise PR is exactly 1")
	assert.Greater(t, m.VUSROC, 0.85)
	assert.Less(t, m.VUSROC, 1.0, "VUS-ROC cannot reach 1 for any detector once the buffer is non-zero")
	assert.Greater(t, m.VUSPR, 0.85)
	assert.Less(t, m.VUSPR, 1.0)
}

func TestInvertedDetectorScoresNearZero(t *testing.T) {
	labels := labelsFrom(500, [2]int{100, 119}, [2]int{300, 319})
	scores := scoresFromLabels(labels)
	for i := range scores {
		scores[i] = 1.0 - scores[i]
	}

	m, err := adscore.EvaluateE(scores, labels, 0)
	require.NoError(t, err)
	assert.Less(t, m.AUCROC, 0.05, "an inverted detector should sit far below chance")
	assert.Less(t, m.VUSROC, 0.15)
}

func TestConstantScoreIsChanceLevel(t *testing.T) {
	// Every point scored identically: one tie group, so the curve has a single
	// operating point and AUC-ROC must land at 0.5.
	labels := labelsFrom(1000, [2]int{200, 229}, [2]int{600, 629})
	scores := make([]float64, len(labels))
	for i := range scores {
		scores[i] = 0.7
	}

	m, err := adscore.EvaluateE(scores, labels, 0)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, m.AUCROC, 1.0e-9, "a constant score is exactly chance under ROC")

	prevalence := 60.0 / 1000.0
	assert.InDelta(t, prevalence, m.AUCPR, 1.0e-9, "PR area for a constant score is the prevalence")
}

func TestChanceLevelDriftsUpwardWithBuffer(t *testing.T) {
	// Point-wise ROC puts a random scorer at 0.5. The range-based version does
	// not, and the drift is large enough to mislead.
	//
	// Positives are counted as the mean of the binary and buffered label mass
	// while true positives are credited against the full buffered label, so a
	// uniformly random prediction set earns recall faster than it earns false
	// positive rate. The gap widens with the buffer. The existence reward damps
	// it but does not remove it.
	//
	// The practical consequence: VUS-ROC near 0.6 on this fixture is chance, not
	// skill. VUS-PR is far less affected, which is a good part of why TSB-AD
	// leads with it.
	labels := labelsFrom(4000, [2]int{500, 549}, [2]int{1500, 1549}, [2]int{2500, 2549})
	rng := rand.New(rand.NewPCG(7, 11))
	scores := make([]float64, len(labels))
	for i := range scores {
		scores[i] = rng.Float64()
	}

	m, err := adscore.EvaluateE(scores, labels, 0)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, m.AUCROC, 0.05, "point-wise ROC is unbiased at chance")

	var prev float64
	for _, buffer := range []int32{0, 10, 25, 50, 100} {
		roc, _, rerr := adscore.RangeAUCE(scores, labels, buffer)
		require.NoError(t, rerr)
		if buffer > 0 {
			assert.Greater(t, roc, prev, "chance level should rise with the buffer (buffer %d)", buffer)
		}
		prev = roc
	}
	assert.Greater(t, prev, 0.6, "by a buffer of 100 a random scorer reads as clearly skilled")

	prevalence := 150.0 / 4000.0
	assert.Less(t, m.VUSPR, prevalence*2.5, "VUS-PR stays close to the prevalence for a random scorer")
}

func TestEarlyDetectionIsRewardedByBufferButNotByPointwise(t *testing.T) {
	// A detector that fires just before the labelled range and stops before it
	// starts, so the overlap is exactly zero. This is the case the whole
	// range-based apparatus exists for: point-wise measures score it as a total
	// miss, and a human looking at the plot would not.
	labels := labelsFrom(1000, [2]int{400, 429})
	scores := make([]float64, len(labels))
	for i := 392; i <= 399; i++ {
		scores[i] = 1.0
	}

	m, err := adscore.EvaluateE(scores, labels, 0)
	require.NoError(t, err)

	assert.Less(t, m.AUCPR, 0.05, "point-wise PR sees no overlap at all")
	assert.Greater(t, m.VUSPR, 0.25, "VUS-PR must credit a detection this close")
	assert.Greater(t, m.VUSPR, m.AUCPR*10.0, "and the gap between them is the point of the measure")
}

func TestVUSIsTheMeanOfRangeAUCOverBuffers(t *testing.T) {
	// The defining relation: VUS averages the range-based area over every buffer
	// length from 0 to maxBuffer inclusive. Computing that average directly from
	// RangeAUCE must reproduce it.
	const maxBuffer = int32(24)
	labels := labelsFrom(600, [2]int{100, 129}, [2]int{400, 419})
	rng := rand.New(rand.NewPCG(3, 5))
	scores := make([]float64, len(labels))
	for i := range scores {
		scores[i] = rng.Float64()
	}

	var rocSum, prSum float64
	for buffer := int32(0); buffer <= maxBuffer; buffer++ {
		roc, pr, err := adscore.RangeAUCE(scores, labels, buffer)
		require.NoError(t, err)
		rocSum += roc
		prSum += pr
	}
	count := float64(maxBuffer + 1)

	m, err := adscore.EvaluateE(scores, labels, maxBuffer)
	require.NoError(t, err)
	assert.InDelta(t, rocSum/count, m.VUSROC, 1.0e-12)
	assert.InDelta(t, prSum/count, m.VUSPR, 1.0e-12)
}

func TestDefaultMaxBufferIsMeanRangeLength(t *testing.T) {
	ranges := adscore.RangesFromLabels(labelsFrom(200, [2]int{10, 19}, [2]int{50, 69}))
	assert.Equal(t, int32(15), adscore.DefaultMaxBuffer(ranges), "lengths 10 and 20 average to 15")

	assert.Equal(t, int32(0), adscore.DefaultMaxBuffer(adscore.RangesFromLabels(make([]bool, 10))))
}

func TestRangeAUCGrowsWithBufferForANearMiss(t *testing.T) {
	// With zero overlap, widening the buffer can only add credit. (That is not
	// true once the prediction already overlaps the label: a wider buffer also
	// inflates the positive class, which dilutes precision elsewhere, so the
	// relation is not monotone in general.)
	labels := labelsFrom(1000, [2]int{400, 429})
	scores := make([]float64, len(labels))
	for i := 392; i <= 399; i++ {
		scores[i] = 1.0
	}

	var prev float64
	for _, buffer := range []int32{0, 4, 8, 16, 24, 32, 48, 64} {
		_, pr, err := adscore.RangeAUCE(scores, labels, buffer)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, pr, prev-1.0e-12,
			"widening the buffer reduced credit for a zero-overlap near miss (buffer %d)", buffer)
		prev = pr
	}
	assert.Greater(t, prev, 0.4, "a wide buffer should end up crediting most of the detection")
}

func TestCurveIsMonotone(t *testing.T) {
	labels := labelsFrom(700, [2]int{100, 139}, [2]int{500, 529})
	rng := rand.New(rand.NewPCG(13, 17))
	scores := make([]float64, len(labels))
	for i := range scores {
		scores[i] = rng.Float64()
	}

	c, err := adscore.CurveE(scores, labels, 12)
	require.NoError(t, err)
	require.NotEmpty(t, c.FPR)

	for i := 1; i < len(c.FPR); i++ {
		assert.GreaterOrEqual(t, c.FPR[i], c.FPR[i-1]-1.0e-12, "FPR must not decrease at %d", i)
		assert.GreaterOrEqual(t, c.Recall[i], c.Recall[i-1]-1.0e-12, "recall must not decrease at %d", i)
	}
	for i := range c.Precision {
		assert.GreaterOrEqual(t, c.Precision[i], 0.0)
		assert.LessOrEqual(t, c.Precision[i], 1.0)
		assert.LessOrEqual(t, c.Recall[i], 1.0)
		assert.LessOrEqual(t, c.FPR[i], 1.0+1.0e-12)
	}
}

func TestTiedScoresDoNotDependOnOrder(t *testing.T) {
	// Points sharing a score must enter the prediction set together, or the
	// result depends on how the sort broke the tie.
	labels := labelsFrom(200, [2]int{50, 69})
	scores := make([]float64, len(labels))
	for i := range scores {
		if i%2 == 0 {
			scores[i] = 0.5
		} else {
			scores[i] = 0.5
		}
	}
	m, err := adscore.EvaluateE(scores, labels, 0)
	require.NoError(t, err)
	assert.InDelta(t, 0.5, m.AUCROC, 1.0e-9, "all scores tied is exactly chance")
}

func TestEvaluateRejectsBadInput(t *testing.T) {
	labels := labelsFrom(20, [2]int{5, 7})

	_, err := adscore.EvaluateE(nil, nil, 0)
	assert.Error(t, err, "empty input")

	_, err = adscore.EvaluateE(make([]float64, 19), labels, 0)
	assert.Error(t, err, "length mismatch")

	_, err = adscore.EvaluateE(make([]float64, 20), make([]bool, 20), 0)
	assert.Error(t, err, "no anomaly labelled")

	allAnomaly := make([]bool, 20)
	for i := range allAnomaly {
		allAnomaly[i] = true
	}
	_, err = adscore.EvaluateE(make([]float64, 20), allAnomaly, 0)
	assert.Error(t, err, "everything labelled anomalous")

	nan := make([]float64, 20)
	nan[3] = math.NaN()
	_, err = adscore.EvaluateE(nan, labels, 0)
	assert.Error(t, err, "NaN score")

	_, err = adscore.EvaluateE(make([]float64, 20), labels, -1)
	assert.Error(t, err, "negative buffer")
}

func TestPropertyMeasuresStayInRange(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(60, 300).Draw(rt, "n")
		start := rapid.IntRange(5, n-25).Draw(rt, "start")
		length := rapid.IntRange(2, 15).Draw(rt, "length")
		labels := labelsFrom(n, [2]int{start, start + length})

		scores := rapid.SliceOfN(rapid.Float64Range(-5.0, 5.0), n, n).Draw(rt, "scores")
		maxBuffer := int32(rapid.IntRange(0, 20).Draw(rt, "maxBuffer"))

		m, err := adscore.EvaluateE(scores, labels, maxBuffer)
		require.NoError(rt, err)

		for _, v := range []float64{m.AUCROC, m.AUCPR, m.VUSROC, m.VUSPR} {
			require.False(rt, math.IsNaN(v), "measure is NaN")
			require.GreaterOrEqual(rt, v, 0.0)
			require.LessOrEqual(rt, v, 1.0+1.0e-9)
		}
	})
}

func TestPropertyMonotoneInScoreOrder(t *testing.T) {
	// Applying a strictly increasing transform to every score cannot change any
	// ranking, so no measure may move.
	//
	// Scores are drawn quantized. An unconstrained float64 draw produces values
	// separated by less than an ULP of the transformed result — exp(1.4e-17) and
	// exp(2.1e-17) are both exactly 1.0 — so the transform collapses distinct
	// scores into a tie and genuinely changes the ranking. That is a fact about
	// IEEE-754, not about this package.
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(60, 250).Draw(rt, "n")
		start := rapid.IntRange(5, n-25).Draw(rt, "start")
		length := rapid.IntRange(2, 15).Draw(rt, "length")
		labels := labelsFrom(n, [2]int{start, start + length})

		quantized := rapid.SliceOfN(rapid.IntRange(-300, 300), n, n).Draw(rt, "scores")
		scores := make([]float64, n)
		for i, q := range quantized {
			scores[i] = float64(q) / 100.0
		}
		maxBuffer := int32(rapid.IntRange(0, 16).Draw(rt, "maxBuffer"))

		transformed := make([]float64, n)
		for i, v := range scores {
			transformed[i] = math.Exp(v)
		}

		base, err := adscore.EvaluateE(scores, labels, maxBuffer)
		require.NoError(rt, err)
		other, err := adscore.EvaluateE(transformed, labels, maxBuffer)
		require.NoError(rt, err)

		require.InDelta(rt, base.AUCROC, other.AUCROC, 1.0e-9)
		require.InDelta(rt, base.AUCPR, other.AUCPR, 1.0e-9)
		require.InDelta(rt, base.VUSROC, other.VUSROC, 1.0e-9)
		require.InDelta(rt, base.VUSPR, other.VUSPR, 1.0e-9)
	})
}

func TestPropertyPerfectBeatsRandom(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(200, 600).Draw(rt, "n")
		start := rapid.IntRange(20, n-60).Draw(rt, "start")
		length := rapid.IntRange(10, 40).Draw(rt, "length")
		labels := labelsFrom(n, [2]int{start, start + length})

		seed := uint64(rapid.IntRange(1, 1<<20).Draw(rt, "seed"))
		rng := rand.New(rand.NewPCG(seed, seed^0x5deece66d))
		random := make([]float64, n)
		for i := range random {
			random[i] = rng.Float64()
		}

		perfect, err := adscore.EvaluateE(scoresFromLabels(labels), labels, 0)
		require.NoError(rt, err)
		noise, err := adscore.EvaluateE(random, labels, 0)
		require.NoError(rt, err)

		require.Greater(rt, perfect.VUSPR, noise.VUSPR, "a perfect detector must beat noise under VUS-PR")
		require.Greater(rt, perfect.VUSROC, noise.VUSROC, "and under VUS-ROC")
	})
}
