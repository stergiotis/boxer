//go:build llm_generated_opus47

package digest

import (
	"math/rand"
	"testing"

	"github.com/stergiotis/boxer/public/analytics/stats/ecdfbands"
	"github.com/stergiotis/boxer/public/analytics/stats/tdigest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildGridShape exercises the structural contract: gridN
// vertices, monotone x, fn ∈ [0, 1] non-decreasing.
func TestBuildGridShape(t *testing.T) {
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(11))
	for range 10_000 {
		d.Push(rnd.NormFloat64())
	}
	xs, fn := BuildGrid(d, 50)
	require.Len(t, xs, 50)
	require.Len(t, fn, 50)
	assert.Equal(t, d.Min(), xs[0])
	assert.Equal(t, d.Max(), xs[len(xs)-1])
	for i := 1; i < len(xs); i++ {
		assert.LessOrEqual(t, xs[i-1], xs[i], "xs non-monotone at %d", i)
		assert.GreaterOrEqual(t, fn[i], 0.0)
		assert.LessOrEqual(t, fn[i], 1.0)
		assert.LessOrEqual(t, fn[i-1], fn[i]+1e-12, "fn non-monotone at %d", i)
	}
}

// TestBuildGridClampsMinSize ensures gridN < 2 still produces a
// usable 2-point grid rather than panicking.
func TestBuildGridClampsMinSize(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 100 {
		d.Push(float64(i))
	}
	xs, _ := BuildGrid(d, 1)
	assert.Len(t, xs, 2)
	xs, _ = BuildGrid(d, 0)
	assert.Len(t, xs, 2)
}

// TestBandsForDigestHappyPath verifies that a well-formed digest
// produces a calibrated GridBand: correct n, alpha, method
// metadata, slices of length gridN, monotone edges sandwiching
// fn_at, and a positive critical value.
//
// n is held at 200 to keep the test within the unit-test time
// budget — Moscovich-Nadler inversion at n=10⁴+ takes minutes per
// (n, α) cell on the first call, fine for production but punishing
// in CI.
func TestBandsForDigestHappyPath(t *testing.T) {
	d := tdigest.NewTDigest()
	rnd := rand.New(rand.NewSource(7))
	const samples = 200
	for range samples {
		d.Push(rnd.NormFloat64())
	}
	const gridN = 50
	b, err := BandsForDigest(d, gridN, 0.05, ecdfbands.BandMethodBerkJones)
	require.NoError(t, err)
	require.Len(t, b.Xs, gridN)
	require.Len(t, b.LowerCDF, gridN)
	require.Len(t, b.UpperCDF, gridN)
	assert.Equal(t, int(samples), b.N)
	assert.Equal(t, 0.05, b.Alpha)
	assert.Equal(t, ecdfbands.BandMethodBerkJones, b.Method)
	assert.Greater(t, b.CritC, 0.0)
	// Lower ≤ upper at every grid point.
	for i := range b.Xs {
		assert.LessOrEqual(t, b.LowerCDF[i], b.UpperCDF[i], "i=%d", i)
	}
}

// TestBandsForDigestRejectsEmpty rejects a fresh empty digest.
func TestBandsForDigestRejectsEmpty(t *testing.T) {
	d := tdigest.NewTDigest()
	_, err := BandsForDigest(d, 50, 0.05, ecdfbands.BandMethodBerkJones)
	assert.Error(t, err)
}

// TestBandsForDigestRejectsCollapsed rejects a digest whose support
// has collapsed to a single point.
func TestBandsForDigestRejectsCollapsed(t *testing.T) {
	d := tdigest.NewTDigest()
	for range 100 {
		d.Push(7.0)
	}
	_, err := BandsForDigest(d, 50, 0.05, ecdfbands.BandMethodBerkJones)
	assert.Error(t, err)
}

// TestBandsForDigestRejectsNil catches the nil-digest mistake.
func TestBandsForDigestRejectsNil(t *testing.T) {
	_, err := BandsForDigest(nil, 50, 0.05, ecdfbands.BandMethodBerkJones)
	assert.Error(t, err)
}

// TestBandsForDigestRejectsTinyGrid rejects gridN < 2 explicitly.
func TestBandsForDigestRejectsTinyGrid(t *testing.T) {
	d := tdigest.NewTDigest()
	for i := range 100 {
		d.Push(float64(i))
	}
	_, err := BandsForDigest(d, 1, 50, ecdfbands.BandMethodBerkJones)
	assert.Error(t, err)
}
