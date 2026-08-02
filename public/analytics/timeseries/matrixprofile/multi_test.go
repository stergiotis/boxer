package matrixprofile_test

import (
	"math"
	"math/rand/v2"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/analytics/timeseries/adscore"
	"github.com/stergiotis/boxer/public/analytics/timeseries/matrixprofile"
)

// The oracle below shares no code path with the implementation. Per-channel
// distances come from oracleDistance, which materializes z-normalized
// subsequences instead of going through the dot-product identity; the
// aggregation is written out with a library sort and an explicit mean rather
// than fused into the implementation's single cumulative pass over an insertion
// sort. An oracle sharing either would confirm the code against itself.

// oracleMultiProfile computes the k-dimensional matrix profile by brute force,
// O(d·n²·window).
func oracleMultiProfile(channels [][]float64, window int32) (dist [][]float64, idx [][]int32) {
	d := len(channels)
	numWindows := int32(len(channels[0])) - window + 1
	zone := oracleExclusionZone(window)

	dist = make([][]float64, d)
	idx = make([][]int32, d)
	for k := range d {
		dist[k] = make([]float64, numWindows)
		idx[k] = make([]int32, numWindows)
		for i := range numWindows {
			dist[k][i] = math.Inf(1)
			idx[k][i] = -1
		}
	}

	perChannel := make([]float64, d)
	for i := range numWindows {
		for j := range numWindows {
			if j >= i-zone && j <= i+zone {
				continue
			}
			for c := range d {
				perChannel[c] = oracleDistance(channels[c], i, j, window)
			}
			sort.Float64s(perChannel)
			for k := range d {
				var sum float64
				for t := 0; t <= k; t++ {
					sum += perChannel[t]
				}
				dk := sum / float64(k+1)
				if dk < dist[k][i] {
					dist[k][i] = dk
					idx[k][i] = j
				}
			}
		}
	}
	return
}

// assertMultiProfileMatchesOracle checks the same two contracts
// assertProfileMatchesOracle does, at the same two accuracies, plus the one the
// univariate profile has no equivalent of: the reported channel subset must
// have exactly k channels and must be the subset the reported distance was
// actually averaged over.
func assertMultiProfileMatchesOracle(t *testing.T, channels [][]float64, window int32, prof *matrixprofile.MultiProfile) {
	t.Helper()
	wantDist, wantIdx := oracleMultiProfile(channels, window)
	d := int32(len(channels))
	require.Equal(t, d, prof.NumChannels)

	var scratch []int32
	for k := int32(1); k <= d; k++ {
		got, gotIdx, gotDims, ok := prof.K(k)
		require.True(t, ok, "k=%d should be readable", k)
		require.Len(t, got, len(wantDist[k-1]))

		for i := range got {
			if wantIdx[k-1][i] < 0 {
				assert.Equal(t, int32(-1), gotIdx[i], "k=%d position %d should have no neighbour", k, i)
				assert.True(t, math.IsInf(got[i], 1), "k=%d position %d should be +Inf", k, i)
				continue
			}
			require.GreaterOrEqual(t, gotIdx[i], int32(0), "k=%d position %d lost its neighbour", k, i)

			scratch = matrixprofile.DimChannels(gotDims[i], scratch)
			require.Len(t, scratch, int(k), "k=%d position %d reports the wrong subset size", k, i)

			var sum float64
			for _, c := range scratch {
				sum += oracleDistance(channels[c], int32(i), gotIdx[i], window)
			}
			overReported := sum / float64(k)

			assert.InDelta(t, overReported, got[i], refinedTolerance(window, overReported),
				"k=%d: reported distance at %d is not the mean over the reported channels", k, i)
			assert.InDelta(t, wantDist[k-1][i], overReported, identityTolerance(window, wantDist[k-1][i]),
				"k=%d: reported neighbour %d of %d is not a nearest one", k, gotIdx[i], i)
			assert.GreaterOrEqual(t, got[i], wantDist[k-1][i]-identityTolerance(window, wantDist[k-1][i]),
				"k=%d: position %d reports a neighbour nearer than the true minimum", k, i)
		}
	}
}

func TestMultiComputeMatchesBruteForceOracle(t *testing.T) {
	tests := []struct {
		name     string
		channels [][]float64
		window   int32
	}{
		{
			name: "unlike periods",
			channels: [][]float64{
				syntheticSine(120, 17, 0.05),
				syntheticSine(120, 23, 0.05),
				syntheticSawtooth(120, 11),
			},
			window: 12,
		},
		{
			name: "channels in unlike units",
			channels: [][]float64{
				syntheticSine(90, 13, 0.02),
				withScale(withOffset(syntheticSine(90, 13, 0.02), 1000.0), 500.0),
				withOffset(syntheticSawtooth(90, 7), -40.0),
			},
			window: 10,
		},
		{
			name: "one constant channel",
			channels: [][]float64{
				syntheticSine(80, 11, 0.03),
				make([]float64, 80),
				syntheticSawtooth(80, 9),
			},
			window: 8,
		},
		{
			name: "single channel",
			channels: [][]float64{
				syntheticSine(100, 15, 0.05),
			},
			window: 11,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ms, err := matrixprofile.NewMultiSeriesE(tt.channels, tt.window, 0.0)
			require.NoError(t, err)
			assertMultiProfileMatchesOracle(t, tt.channels, tt.window, ms.Compute())
		})
	}
}

// TestSingleChannelReproducesUnivariateProfile pins the generalization: with one
// channel there is nothing to select and nothing to average, so the
// subdimensional path must return exactly what the univariate one does — not
// approximately, bit for bit.
//
// Both drive the same recurrence through the same scanner, so any difference
// would be in the aggregation layer, which is what this is guarding.
func TestSingleChannelReproducesUnivariateProfile(t *testing.T) {
	values := syntheticSine(300, 24, 0.08)
	const window = int32(20)

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)
	want := s.Compute()

	ms, err := matrixprofile.NewMultiSeriesE([][]float64{values}, window, 0.0)
	require.NoError(t, err)

	got := ms.Compute()
	assert.Equal(t, want.Distance, got.Distance[0], "d=1 must reproduce the univariate distances exactly")
	assert.Equal(t, want.Index, got.Index[0], "d=1 must reproduce the univariate indices exactly")
	for i, dims := range got.Dims[0] {
		if got.Index[0][i] < 0 {
			continue
		}
		assert.Equal(t, uint64(1), dims, "the only channel must be the selected one at %d", i)
	}
}

func TestNewMultiSeriesRejectsBadInput(t *testing.T) {
	ok := syntheticSine(50, 9, 0.0)

	_, err := matrixprofile.NewMultiSeriesE(nil, 5, 0.0)
	assert.Error(t, err, "no channels")

	_, err = matrixprofile.NewMultiSeriesE([][]float64{ok, ok[:40]}, 5, 0.0)
	assert.Error(t, err, "ragged channels must not be silently truncated")

	_, err = matrixprofile.NewMultiSeriesE([][]float64{ok}, 1, 0.0)
	assert.Error(t, err, "window below 2")

	_, err = matrixprofile.NewMultiSeriesE([][]float64{ok}, 60, 0.0)
	assert.Error(t, err, "window longer than the series")

	tooMany := make([][]float64, matrixprofile.MaxChannels+1)
	for i := range tooMany {
		tooMany[i] = ok
	}
	_, err = matrixprofile.NewMultiSeriesE(tooMany, 5, 0.0)
	assert.Error(t, err, "the bitmask cannot carry more than MaxChannels")

	withNaN := make([]float64, len(ok))
	copy(withNaN, ok)
	withNaN[3] = math.NaN()
	_, err = matrixprofile.NewMultiSeriesE([][]float64{ok, withNaN}, 5, 0.0)
	assert.Error(t, err, "a non-finite value in any channel")
}

// plantedSubdimensional builds a d-channel series in which exactly the carrier
// channels repeat one pattern at two places and every other channel is
// unrelated noise throughout. Each carrier gets its own phase, so the carriers
// are not copies of one another — only self-similar at the two sites.
func plantedSubdimensional(n int, d int, carriers []int32, window int, at1 int, at2 int) (channels [][]float64) {
	channels = make([][]float64, d)
	for c := range channels {
		channels[c] = lcgNoise(n, uint32(0x9e3779b9)+uint32(c)*2246822519)
	}
	for ci, c := range carriers {
		phase := float64(ci) * 0.9
		for i := range window {
			t := 2.0 * math.Pi * float64(i) / float64(window)
			v := math.Sin(t+phase) + 0.5*math.Sin(3.0*t)
			channels[c][at1+i] = v
			channels[c][at2+i] = v
		}
	}
	return
}

// lcgNoise is a deterministic uniform sequence, matching syntheticSine's
// approach to staying reproducible without a seeded rand.Source.
func lcgNoise(n int, seed uint32) (out []float64) {
	out = make([]float64, n)
	state := seed
	for i := range out {
		state = state*1664525 + 1013904223
		out[i] = float64(state>>8)/float64(1<<24) - 0.5
	}
	return
}

func TestPlantedSubdimensionalMotif(t *testing.T) {
	const (
		n      = 400
		d      = 6
		window = 40
		at1    = 60
		at2    = 250
	)
	carriers := []int32{1, 3, 4}
	channels := plantedSubdimensional(n, d, carriers, window, at1, at2)

	ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
	require.NoError(t, err)
	prof := ms.Compute()

	first, second, dims, dist, found := prof.Motif(int32(len(carriers)))
	require.True(t, found)

	lo, hi := first, second
	if lo > hi {
		lo, hi = hi, lo
	}
	assert.InDelta(t, at1, int(lo), 2.0, "the 3-dimensional motif should start at the first planted site")
	assert.InDelta(t, at2, int(hi), 2.0, "…and its neighbour at the second")
	assert.Less(t, dist, 1.0e-6, "the planted windows are identical, so the distance should be ~0")

	var want uint64
	for _, c := range carriers {
		want |= uint64(1) << uint(c)
	}
	assert.Equal(t, want, dims, "the motif should span exactly the carrier channels")
	assert.Equal(t, carriers, matrixprofile.DimChannels(dims, nil))
}

func TestMDLSelectsThePlantedDimensionality(t *testing.T) {
	const (
		n      = 400
		d      = 6
		window = 40
		at1    = 60
		at2    = 250
	)
	carriers := []int32{1, 3, 4}
	channels := plantedSubdimensional(n, d, carriers, window, at1, at2)

	ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
	require.NoError(t, err)
	prof := ms.Compute()

	k, dims, bitSize, err := ms.SelectDimsMDLE(prof, 0)
	require.NoError(t, err)
	t.Logf("bit size by k: %v", bitSize)

	assert.Equal(t, int32(len(carriers)), k, "MDL should recover the planted dimensionality")

	var want uint64
	for _, c := range carriers {
		want |= uint64(1) << uint(c)
	}
	assert.Equal(t, want, dims)

	require.Len(t, bitSize, d)
	for _, b := range bitSize {
		assert.False(t, math.IsNaN(b))
	}
}

// TestMDLChargesTheReferenceSide pins the one place this implementation departs
// from the widely used reference one, because the departure moves the argmin
// and would otherwise be invisible.
//
// One channel, a pair of identical windows: the difference is constant, so it
// costs nothing, and the encoding is exactly one raw subsequence — m·bits. The
// reference implementation omits the reference side and would report 0 here,
// which is also what it would report for a pair that matched only well enough
// to have a constant difference.
func TestMDLChargesTheReferenceSide(t *testing.T) {
	const (
		n      = 200
		window = 25
		at1    = 20
		at2    = 120
		bits   = int32(8)
	)
	channels := plantedSubdimensional(n, 1, []int32{0}, window, at1, at2)

	ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
	require.NoError(t, err)
	prof := ms.Compute()

	k, _, bitSize, err := ms.SelectDimsMDLE(prof, bits)
	require.NoError(t, err)
	require.Equal(t, int32(1), k)
	assert.InDelta(t, float64(window*bits), bitSize[0], 1.0e-9,
		"a perfectly matched pair costs one raw subsequence, not nothing")
}

// TestPropertyProfileIsMonotoneInK asserts the structural property of a
// cumulative mean over sorted values: averaging in the next-smallest can only
// raise the mean. The minimum over candidate neighbours inherits it, since the
// argmin at k is still a candidate at k+1.
//
// It is asserted on well-conditioned input only, for the same reason
// TestPropertyOptimalOnWellConditioned exists. The property holds exactly on
// the values the *search* ranks, but each k picks its neighbour independently
// and is refined independently afterwards. On a series mixing local scales
// across many orders of magnitude the identity cannot resolve the ordering, two
// adjacent k refine from genuinely different pairs, and the refined values can
// cross by far more than rounding — measured at 0.34 on such an input, against
// a window of 3. That is the documented behaviour of the identity, not a defect
// in the aggregation.
func TestPropertyProfileIsMonotoneInK(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(14, 40).Draw(rt, "n")
		d := rapid.IntRange(2, 5).Draw(rt, "d")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))

		channels := make([][]float64, d)
		for c := range channels {
			quantized := rapid.SliceOfN(rapid.IntRange(-2000, 2000), n, n).Draw(rt, "channel")
			channels[c] = make([]float64, n)
			for i, q := range quantized {
				channels[c][i] = float64(q) / 100.0
			}
		}

		ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
		require.NoError(rt, err)
		prof := ms.Compute()

		for k := 1; k < d; k++ {
			for i := range prof.Distance[k] {
				lower := prof.Distance[k-1][i]
				upper := prof.Distance[k][i]
				if math.IsInf(lower, 1) || math.IsInf(upper, 1) {
					continue
				}
				require.GreaterOrEqual(rt, upper, lower-identityTolerance(window, lower),
					"k=%d must not undercut k=%d at %d", k+1, k, i)
			}
		}
	})
}

// TestPropertyPerChannelAffineInvariance asserts that the profile depends on
// each channel's shape and nothing else. Distances are z-normalized per
// channel, so rescaling one channel — a change of unit — must not move any
// value, and must not change which channels a subset contains.
func TestPropertyPerChannelAffineInvariance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(14, 36).Draw(rt, "n")
		d := rapid.IntRange(2, 4).Draw(rt, "d")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))

		base := make([][]float64, d)
		for c := range base {
			quantized := rapid.SliceOfN(rapid.IntRange(-2000, 2000), n, n).Draw(rt, "channel")
			base[c] = make([]float64, n)
			for i, q := range quantized {
				base[c][i] = float64(q) / 100.0
			}
		}
		target := rapid.IntRange(0, d-1).Draw(rt, "target")
		scale := rapid.Float64Range(0.2, 20.0).Draw(rt, "scale")
		offset := rapid.Float64Range(-100.0, 100.0).Draw(rt, "offset")

		transformed := make([][]float64, d)
		copy(transformed, base)
		transformed[target] = withOffset(withScale(base[target], scale), offset)

		msBase, err := matrixprofile.NewMultiSeriesE(base, window, 0.0)
		require.NoError(rt, err)
		msTransformed, err := matrixprofile.NewMultiSeriesE(transformed, window, 0.0)
		require.NoError(rt, err)

		profBase := msBase.Compute()
		profTransformed := msTransformed.Compute()

		for k := range d {
			for i := range profBase.Distance[k] {
				want := profBase.Distance[k][i]
				if math.IsInf(want, 1) {
					require.True(rt, math.IsInf(profTransformed.Distance[k][i], 1))
					continue
				}
				require.InDelta(rt, want, profTransformed.Distance[k][i], identityTolerance(window, want),
					"scaling channel %d changed the k=%d profile at %d", target, k+1, i)
			}
		}
	})
}

// TestPropertyChannelPermutationInvariance asserts that channel order is not
// information. Permuting the inputs must leave every distance untouched and
// carry every reported subset along with the permutation.
func TestPropertyChannelPermutationInvariance(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(14, 36).Draw(rt, "n")
		d := rapid.IntRange(2, 5).Draw(rt, "d")
		window := int32(rapid.IntRange(2, n/3).Draw(rt, "window"))

		base := make([][]float64, d)
		for c := range base {
			quantized := rapid.SliceOfN(rapid.IntRange(-2000, 2000), n, n).Draw(rt, "channel")
			base[c] = make([]float64, n)
			for i, q := range quantized {
				base[c][i] = float64(q) / 100.0
			}
		}
		identity := make([]int, d)
		for i := range identity {
			identity[i] = i
		}
		perm := rapid.Permutation(identity).Draw(rt, "perm")

		permuted := make([][]float64, d)
		for newIdx, oldIdx := range perm {
			permuted[newIdx] = base[oldIdx]
		}

		msBase, err := matrixprofile.NewMultiSeriesE(base, window, 0.0)
		require.NoError(rt, err)
		msPermuted, err := matrixprofile.NewMultiSeriesE(permuted, window, 0.0)
		require.NoError(rt, err)

		profBase := msBase.Compute()
		profPermuted := msPermuted.Compute()

		for k := range d {
			for i := range profBase.Distance[k] {
				want := profBase.Distance[k][i]
				if math.IsInf(want, 1) {
					require.True(rt, math.IsInf(profPermuted.Distance[k][i], 1))
					continue
				}
				require.InDelta(rt, want, profPermuted.Distance[k][i], identityTolerance(window, want),
					"permuting channels changed the k=%d profile at %d", k+1, i)
			}
		}
	})
}

// TestDimsAreNestedWhenTheNeighbourIsShared asserts the one nesting guarantee
// the structure actually carries.
//
// Within a single candidate pair the subsets are nested by construction — the
// k+1 smallest include the k smallest. Across k they need not be, because
// different k may pick different neighbours, which is exactly why no pruning
// over k is possible. So the invariant holds precisely when the neighbour is
// shared, and this asserts that rather than the stronger claim.
func TestDimsAreNestedWhenTheNeighbourIsShared(t *testing.T) {
	const (
		n      = 300
		d      = 5
		window = 30
	)
	channels := plantedSubdimensional(n, d, []int32{0, 2}, window, 40, 190)

	ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
	require.NoError(t, err)
	prof := ms.Compute()

	var shared int
	for k := 1; k < d; k++ {
		for i := range prof.Index[k] {
			if prof.Index[k][i] < 0 || prof.Index[k][i] != prof.Index[k-1][i] {
				continue
			}
			shared++
			assert.Equal(t, prof.Dims[k-1][i], prof.Dims[k-1][i]&prof.Dims[k][i],
				"k=%d at %d shares a neighbour with k=%d but not its channels", k+1, i, k)
		}
	}
	require.Positive(t, shared, "the fixture should exercise the shared-neighbour case")
}

func TestPositionScoresUsesCentres(t *testing.T) {
	values := syntheticSine(200, 20, 0.05)
	const window = int32(20)

	s, err := matrixprofile.NewSeriesE(values, window, 0.0)
	require.NoError(t, err)
	prof := s.Compute()

	scores := prof.PositionScores(int32(len(values)), nil)
	require.Len(t, scores, len(values))

	idx, dist, found := prof.Discord()
	require.True(t, found)
	assert.InDelta(t, dist, scores[idx+window/2], 1.0e-12,
		"the discord's score belongs at its window centre")

	for _, v := range scores {
		assert.False(t, math.IsInf(v, 0), "a position score must never be infinite")
		assert.False(t, math.IsNaN(v))
	}

	// The tail beyond the last window's centre is uncovered and stays zero.
	last := int32(len(prof.Distance)-1) + window/2
	for i := last + 1; i < int32(len(scores)); i++ {
		assert.Zero(t, scores[i], "position %d is past every window centre", i)
	}
}

func TestMultiPositionScores(t *testing.T) {
	const (
		n      = 300
		d      = 4
		window = int32(30)
	)
	channels := plantedSubdimensional(n, d, []int32{0, 2}, int(window), 40, 190)
	ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
	require.NoError(t, err)
	prof := ms.Compute()

	for k := int32(1); k <= d; k++ {
		scores := prof.PositionScores(k, n, nil)
		require.Len(t, scores, n)
		idx, _, dist, found := prof.Discord(k)
		require.True(t, found)
		assert.InDelta(t, dist, scores[idx+window/2], 1.0e-12, "k=%d discord lands at its centre", k)
	}

	assert.Empty(t, prof.PositionScores(0, n, nil), "k below 1 has no score vector")
	assert.Empty(t, prof.PositionScores(d+1, n, nil), "k above d has no score vector")
}

func TestDimChannels(t *testing.T) {
	assert.Empty(t, matrixprofile.DimChannels(0, nil))
	assert.Equal(t, []int32{0}, matrixprofile.DimChannels(1, nil))
	assert.Equal(t, []int32{0, 3, 7}, matrixprofile.DimChannels(1|1<<3|1<<7, nil))
	assert.Equal(t, []int32{63}, matrixprofile.DimChannels(1<<63, nil))

	dst := make([]int32, 0, 8)
	got := matrixprofile.DimChannels(1|1<<5, dst)
	assert.Equal(t, []int32{0, 5}, got)
}

// cleanBackground is a normal signal throughout: three sinusoids at
// incommensurate periods plus noise, the same family adscore's generator builds
// its fixtures on. It exists because that generator refuses to produce a series
// with no anomalies in it, and the channels that are supposed to be innocent
// here must be innocent everywhere.
func cleanBackground(n int, period float64, noiseStdDev float64, seed uint64) (out []float64) {
	const (
		golden    = 1.6180339887498949
		invGolden = 0.6180339887498949
	)
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	out = make([]float64, n)
	for i := range out {
		t := float64(i)
		out[i] = math.Sin(2.0*math.Pi*t/period) +
			0.6*math.Sin(2.0*math.Pi*t/(period*invGolden)+0.7) +
			0.35*math.Sin(2.0*math.Pi*t/(period*golden)+1.9) +
			rng.NormFloat64()*noiseStdDev
	}
	return
}

// multivariateFixture composes a d-channel labelled series from adscore's
// univariate generator: the carrier channels each get a fixture whose anomalies
// land at the same positions, and every other channel is clean signal.
//
// Anomaly placement is a pure function of the seed and the length/count/tail
// parameters, so varying only the period gives carriers that share their
// anomaly times without sharing a background. The test asserts that rather than
// assuming it.
func multivariateFixture(t *testing.T, kind adscore.AnomalyKindE, length int32, d int, carriers []int32, seed uint64) (channels [][]float64, labels []bool, period float64) {
	t.Helper()
	isCarrier := make([]bool, d)
	for _, c := range carriers {
		isCarrier[c] = true
	}
	period = 50.0

	channels = make([][]float64, d)
	for ci, c := range carriers {
		spec := adscore.DefaultFixtureSpec(kind, seed)
		spec.Length = length
		// A different background per carrier, leaving placement untouched.
		spec.Period = period * (1.0 + 0.11*float64(ci))
		f, err := adscore.GenerateE(spec)
		require.NoError(t, err)
		channels[c] = f.Values
		if labels == nil {
			labels = f.Labels
		} else {
			require.Equal(t, labels, f.Labels, "carriers must share their anomaly positions")
		}
	}
	for c := range channels {
		if isCarrier[c] {
			continue
		}
		channels[c] = cleanBackground(int(length), period*(1.0+0.07*float64(c)), 0.05, seed+uint64(c)+1)
	}
	return
}

// bestOneLiner returns the highest VUS-PR any of adscore's trivial baselines
// reaches on any single channel — the bar a real detector has to clear, and the
// reason no figure below is reported on its own.
func bestOneLiner(t *testing.T, channels [][]float64, labels []bool, window int32) (best float64, where string) {
	t.Helper()
	for c, values := range channels {
		for _, baseline := range adscore.AllBaselines {
			m, err := adscore.EvaluateE(adscore.BaselineScores(values, baseline, window), labels, 0)
			require.NoError(t, err)
			if m.VUSPR > best {
				best = m.VUSPR
				where = baseline.String()
				_ = c
			}
		}
	}
	return
}

// perChannelMaxScores is the obvious cheap alternative to a subdimensional
// profile: run d univariate profiles and keep the largest score at each
// position. It costs the same O(d·n²) and is the bar the joint search has to
// clear to be worth its extra machinery.
func perChannelMaxScores(t *testing.T, channels [][]float64, window int32, n int32) (scores []float64) {
	t.Helper()
	scores = make([]float64, n)
	for _, values := range channels {
		s, err := matrixprofile.NewSeriesE(values, window, 0.0)
		require.NoError(t, err)
		for i, v := range s.Compute().PositionScores(n, nil) {
			if v > scores[i] {
				scores[i] = v
			}
		}
	}
	return
}

// TestSubdimensionalDiscordPeaksAtTheAffectedChannelCount asserts the behaviour
// that makes the subdimensional profile worth computing at all: swept over k, it
// is most accurate at the number of channels the anomaly actually touches.
//
// That is what lets a caller read the affected channel count off the sweep
// rather than being told it, and it is the anomaly-side counterpart of MDL
// recovering the motif's dimensionality.
func TestSubdimensionalDiscordPeaksAtTheAffectedChannelCount(t *testing.T) {
	const (
		length = int32(2000)
		d      = 5
	)
	carriers := []int32{1, 3}

	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			channels, labels, period := multivariateFixture(t, kind, length, d, carriers, 41)
			window := int32(period)

			ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
			require.NoError(t, err)
			prof := ms.Compute()

			var best float64
			var bestK int32
			for k := int32(1); k <= d; k++ {
				m, err := adscore.EvaluateE(prof.PositionScores(k, length, nil), labels, 0)
				require.NoError(t, err)
				t.Logf("k=%d VUS-PR=%.4f", k, m.VUSPR)
				if m.VUSPR > best {
					best = m.VUSPR
					bestK = k
				}
			}
			assert.Equal(t, int32(len(carriers)), bestK, "the sweep should peak at the affected channel count")
		})
	}
}

// TestSubdimensionalDiscordAgainstOneLiners measures the detector on composed
// multivariate fixtures rather than only asserting that it is self-consistent.
//
// Every number this repository had for these algorithms came from fixtures it
// generated itself, and the one time they were checked against recorded data
// the detector did not clear the trivial baselines. So the one-liners are
// reported alongside every figure here, and so is the cheap alternative — d
// univariate profiles combined by max — because clearing a one-liner is not the
// same as being worth the joint search.
//
// The second comparison is the one that does not flatter this code: on these
// fixtures the per-channel maximum matches or beats the joint profile at every
// k. It is logged rather than asserted against, because a fixture whose
// channels are mutually independent is the worst case for a joint nearest
// neighbour and the best case for treating channels separately. What the
// subdimensional profile adds here is the channel subset and the count, not
// accuracy.
func TestSubdimensionalDiscordAgainstOneLiners(t *testing.T) {
	const (
		length = int32(2000)
		d      = 5
	)
	carriers := []int32{1, 3}

	for _, kind := range adscore.AllAnomalyKinds {
		t.Run(kind.String(), func(t *testing.T) {
			channels, labels, period := multivariateFixture(t, kind, length, d, carriers, 41)
			window := int32(period)

			ms, err := matrixprofile.NewMultiSeriesE(channels, window, 0.0)
			require.NoError(t, err)
			prof := ms.Compute()

			baseline, which := bestOneLiner(t, channels, labels, window)
			reference, err := adscore.EvaluateE(perChannelMaxScores(t, channels, window, length), labels, 0)
			require.NoError(t, err)

			var best float64
			for k := int32(1); k <= d; k++ {
				m, err := adscore.EvaluateE(prof.PositionScores(k, length, nil), labels, 0)
				require.NoError(t, err)
				if m.VUSPR > best {
					best = m.VUSPR
				}
			}
			t.Logf("subdimensional best VUS-PR=%.4f; per-channel max=%.4f; best one-liner (%s)=%.4f",
				best, reference.VUSPR, which, baseline)

			assert.Greater(t, best, baseline*1.3,
				"a subdimensional detector that does not clear a one-liner is not worth its cost")
			assert.Greater(t, reference.VUSPR, baseline,
				"the per-channel reference must clear the one-liners too, or the fixture is unsolvable")
		})
	}
}
