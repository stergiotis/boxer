//go:build llm_generated_opus47 && slow

// Empirical-coverage test battery for the simultaneous CDF bands.
//
// These tests sit behind the `slow` build tag because Monte-Carlo
// validation at K = 10⁴ replicates and n in the 20-200 range adds
// ~5 seconds per family per (n, α) cell, which is too much for the
// default `go test` run.
//
// Invoke with:
//
//	go test -tags "$(cat tags | tr -d $'\n') slow" \
//	  -run 'Coverage' \
//	  ./src/go/public/keelson/data/ecdfbands/...
//
// Rationale: the production correctness statement of the library is
// that BandsForSample returns a (1-α)·100% simultaneous confidence
// band on F. Reference critical values (Smirnov KS, Berk-Jones
// asymptotic) cover only a slice of the (n, α, method) space.
// Empirical coverage on iid uniform samples is the directest
// available end-to-end check.

package ecdfbands

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCoverageBerkJones verifies empirical coverage matches the
// nominal 1-α for the BJ band family. Expected stddev of the
// empirical coverage at K replicates is √(α(1-α)/K); we allow ±4σ
// before declaring miscalibration.
func TestCoverageBerkJones(t *testing.T) {
	for _, n := range []int{10, 25, 50, 100} {
		for _, alpha := range []float64{0.05, 0.10} {
			runCoverageCell(t, n, alpha, BandMethodBerkJones, 10000)
		}
	}
}

func TestCoverageDKW(t *testing.T) {
	for _, n := range []int{10, 25, 50, 100} {
		for _, alpha := range []float64{0.05, 0.10} {
			runCoverageCell(t, n, alpha, BandMethodDKW, 10000)
		}
	}
}

func TestCoverageEqualPrecision(t *testing.T) {
	for _, n := range []int{25, 50, 100} {
		for _, alpha := range []float64{0.05, 0.10} {
			runCoverageCell(t, n, alpha, BandMethodEqualPrecision, 10000)
		}
	}
}

func TestCoverageHigherCriticism(t *testing.T) {
	for _, n := range []int{25, 50, 100} {
		for _, alpha := range []float64{0.05, 0.10} {
			runCoverageCell(t, n, alpha, BandMethodHigherCriticism, 10000)
		}
	}
}

// runCoverageCell sims K iid uniform samples at the given n,
// computes the (1-α) simultaneous band, and checks empirical
// coverage matches the nominal value within ±4σ.
func runCoverageCell(t *testing.T, n int, alpha float64, method BandMethodE, k int) {
	t.Helper()
	lower, upper, err := QuantileBoundaries(n, alpha, method)
	require.NoError(t, err)
	rnd := rand.New(rand.NewSource(int64(n)*10000 + int64(alpha*1000) + int64(method)))
	inside := 0
	sample := make([]float64, n)
	for range k {
		for j := range n {
			sample[j] = rnd.Float64()
		}
		slices.Sort(sample)
		all := true
		for j := range n {
			if sample[j] < lower[j] || sample[j] > upper[j] {
				all = false
				break
			}
		}
		if all {
			inside++
		}
	}
	emp := float64(inside) / float64(k)
	sigma := math.Sqrt(alpha * (1 - alpha) / float64(k))
	devSigmas := math.Abs(emp-(1-alpha)) / sigma
	t.Logf("method=%d n=%d α=%v: empirical=%v nominal=%v (deviation %.2fσ)",
		method, n, alpha, emp, 1-alpha, devSigmas)
	if devSigmas > 4 {
		t.Errorf("method=%d n=%d α=%v: empirical %v differs from nominal %v by %.2fσ (limit 4σ)",
			method, n, alpha, emp, 1-alpha, devSigmas)
	}
}
