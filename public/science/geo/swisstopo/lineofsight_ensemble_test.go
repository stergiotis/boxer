package swisstopo

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mkSweep builds a synthetic LOSSweepResult with the given per-ray visibility
// and terrain profiles (LOSElev is set equal to terrain — it is irrelevant to
// aggregateEnsemble).
func mkSweep(angles []float64, visible []bool, terrains [][]float32) LOSSweepResult {
	rays := make([]LOSResult, len(angles))
	for i := range angles {
		dist := make([]float64, len(terrains[i]))
		for k := range dist {
			dist[k] = float64(k)
		}
		rays[i] = LOSResult{
			Visible:     visible[i],
			ProfileDist: dist,
			ProfileElev: terrains[i],
			LOSElev:     terrains[i],
		}
	}
	return LOSSweepResult{AngleDeg: angles, Rays: rays}
}

func TestAggregateEnsemble(t *testing.T) {
	angles := []float64{-1, 0, 1}
	nominal := mkSweep(angles, []bool{true, true, true},
		[][]float32{{10, 10}, {10, 10}, {10, 10}})
	m1 := mkSweep(angles, []bool{true, false, true},
		[][]float32{{12, 8}, {14, 8}, {12, 8}})
	m2 := mkSweep(angles, []bool{false, false, true},
		[][]float32{{9, 11}, {9, 13}, {9, 11}})

	res := aggregateEnsemble(nominal, []LOSSweepResult{m1, m2})

	assert.Equal(t, 2, res.Samples)
	require.Len(t, res.Distance, 2)

	// VisProb over members only: ray0 visible in m1 not m2 → 0.5; ray1 blocked
	// in both → 0; ray2 visible in both → 1.
	require.Len(t, res.VisProb, 3)
	assert.InDelta(t, 0.5, res.VisProb[0], 1e-9)
	assert.InDelta(t, 0.0, res.VisProb[1], 1e-9)
	assert.InDelta(t, 1.0, res.VisProb[2], 1e-9)

	// Envelope ray0 over nominal{10,10}, m1{12,8}, m2{9,11} → min{9,8} max{12,11}.
	assert.Equal(t, []float32{9, 8}, res.TerrainMin[0])
	assert.Equal(t, []float32{12, 11}, res.TerrainMax[0])
	for j := range res.AngleDeg {
		for k := range res.Distance {
			assert.LessOrEqual(t, res.TerrainMin[j][k], nominal.Rays[j].ProfileElev[k])
			assert.GreaterOrEqual(t, res.TerrainMax[j][k], nominal.Rays[j].ProfileElev[k])
		}
	}
}

func TestAggregateEnsemble_NoMembers(t *testing.T) {
	angles := []float64{-1, 0, 1}
	nominal := mkSweep(angles, []bool{true, false, true},
		[][]float32{{10, 10}, {10, 10}, {10, 10}})

	res := aggregateEnsemble(nominal, nil)

	assert.Equal(t, 0, res.Samples)
	assert.Equal(t, []float64{1, 0, 1}, res.VisProb)
	assert.Equal(t, nominal.Rays[1].ProfileElev, res.TerrainMin[1])
	assert.Equal(t, nominal.Rays[1].ProfileElev, res.TerrainMax[1])
}

func TestAggregateEnsemble_RaggedTrimsToShortest(t *testing.T) {
	angles := []float64{0}
	nominal := mkSweep(angles, []bool{true}, [][]float32{{1, 2, 3}})
	short := mkSweep(angles, []bool{true}, [][]float32{{5, 6}})

	res := aggregateEnsemble(nominal, []LOSSweepResult{short})

	require.Len(t, res.Distance, 2)
	assert.Equal(t, []float32{1, 2}, res.TerrainMin[0])
	assert.Equal(t, []float32{5, 6}, res.TerrainMax[0])
}

func TestLineOfSightSweepEnsemble_Integration(t *testing.T) {
	sampler := newTestSampler(t) // skips when tiles absent

	from := LV95Coord{E: 2_600_000, N: 1_200_500}
	to := LV95Coord{E: 2_600_998, N: 1_200_500}

	spec := EnsembleSpec{
		HalfRangeDeg: 2, StepDeg: 0.5, Samples: 8, Seed: 42,
		SigmaObsPosM: 5, SigmaTgtPosM: 3, SigmaObsHeightM: 1, SigmaTgtHeightM: 1,
	}
	res, err := sampler.LineOfSightSweepEnsemble(from, 1.7, to, 0, spec)
	require.NoError(t, err)

	nRays := len(res.Nominal.Rays)
	require.Equal(t, 9, nRays)
	assert.Equal(t, 8, res.Samples)
	require.Len(t, res.VisProb, nRays)
	require.Greater(t, len(res.Distance), 1)

	for j := range nRays {
		assert.GreaterOrEqual(t, res.VisProb[j], 0.0)
		assert.LessOrEqual(t, res.VisProb[j], 1.0)
		require.Len(t, res.TerrainMin[j], len(res.Distance))
		for k := range res.Distance {
			assert.LessOrEqual(t, res.TerrainMin[j][k], res.TerrainMax[j][k], "ray %d pt %d", j, k)
		}
	}

	// Every randomised input is recorded with one draw per sample; position
	// offsets are radial (non-negative).
	require.Len(t, res.Inputs, 4)
	for _, in := range res.Inputs {
		require.Lenf(t, in.Dev, 8, "var %q", in.Name)
	}
	assert.Equal(t, "observer position", res.Inputs[0].Name)
	for _, d := range res.Inputs[0].Dev {
		assert.GreaterOrEqual(t, d, 0.0, "radial offset must be non-negative")
	}

	// Reproducible: same spec → identical visibility fractions and draws.
	res2, err := sampler.LineOfSightSweepEnsemble(from, 1.7, to, 0, spec)
	require.NoError(t, err)
	assert.Equal(t, res.VisProb, res2.VisProb)
	assert.Equal(t, res.Inputs[0].Dev, res2.Inputs[0].Dev)

	// All σ = 0 collapses to the nominal binary verdict with no recorded inputs.
	z, err := sampler.LineOfSightSweepEnsemble(from, 1.7, to, 0,
		EnsembleSpec{HalfRangeDeg: 2, StepDeg: 0.5, Samples: 8, Seed: 42})
	require.NoError(t, err)
	assert.Equal(t, 0, z.Samples)
	assert.Empty(t, z.Inputs)
	for j := range nRays {
		want := 0.0
		if z.Nominal.Rays[j].Visible {
			want = 1.0
		}
		assert.InDelta(t, want, z.VisProb[j], 1e-9)
	}
}
