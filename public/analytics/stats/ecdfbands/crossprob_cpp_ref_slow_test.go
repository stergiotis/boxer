//go:build slow

// Cross-validation against Moscovich's published C++ reference
// implementation of the boundary-crossing-probability algorithms.
// The binary is the ecdf2-mn2017 algorithm from
// github.com/mosco/crossing-probability — the same algorithm
// described in the Moscovich-Nadler 2017 paper our Go Moscovich
// engine derives from.
//
// This file lives under the `slow` build tag because it shells out
// to an external binary; pure-Go fast tests must not depend on it.
//
// The binary path is taken from CROSSPROB_BIN env var, defaulting to
// /tmp/crossing-probability/bin/crossprob. Tests skip cleanly when
// the binary is missing so CI environments without it stay green.

package ecdfbands

import (
	"bytes"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const defaultCrossprobBin = "/tmp/crossing-probability/bin/crossprob"

func crossprobBin(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("CROSSPROB_BIN")
	if bin == "" {
		bin = defaultCrossprobBin
	}
	if _, err := os.Stat(bin); err != nil {
		t.Skipf("Moscovich C++ binary not found at %s: %v\n"+
			"Build via: cd /tmp && git clone https://github.com/mosco/crossing-probability && cd crossing-probability && make",
			bin, err)
	}
	return bin
}

// runCrossprobCPP invokes the Moscovich C++ CLI on the given
// boundaries and returns the reference probability. The CLI reads
// two comma-separated lines from a file and prints the probability
// to stdout.
func runCrossprobCPP(t *testing.T, bin string, lower, upper []float64) float64 {
	t.Helper()
	tmp, err := os.CreateTemp("", "ecdfbands-bounds-*.txt")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	var b strings.Builder
	for i, v := range lower {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(v, 'g', 17, 64))
	}
	b.WriteByte('\n')
	for i, v := range upper {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(v, 'g', 17, 64))
	}
	b.WriteByte('\n')
	_, err = tmp.WriteString(b.String())
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	var out bytes.Buffer
	cmd := exec.Command(bin, "ecdf2-mn2017", tmp.Name())
	cmd.Stdout = &out
	cmd.Stderr = &out
	require.NoError(t, cmd.Run(), "CLI output: %s", out.String())

	s := strings.TrimSpace(out.String())
	p, err := strconv.ParseFloat(s, 64)
	require.NoError(t, err, "could not parse CLI output %q", s)
	return p
}

// genBJBounds returns Berk-Jones-shaped bands for (n, c). The c is
// chosen so the bands are non-degenerate (neither trivial nor
// pinned to {0, 1}).
func genBJBounds(n int, c float64) (lower, upper []float64) {
	lower = make([]float64, n)
	upper = make([]float64, n)
	berkJonesFamily{}.boundaries(n, c, lower, upper)
	return
}

// genRandomMonotoneBounds returns a random monotone (a, b) pair
// with a_i ≤ b_i. Useful for stress-testing the cross-validation
// across irregular boundary geometries.
func genRandomMonotoneBounds(rnd *rand.Rand, n int) (lower, upper []float64) {
	lower = make([]float64, n)
	upper = make([]float64, n)
	for i := range n {
		p := float64(i+1) / float64(n)
		w := 0.1 + 0.1*rnd.Float64()
		lower[i] = math.Max(0, p-w)
		upper[i] = math.Min(1, p+w)
	}
	clampMonotone(lower, upper)
	return
}

func TestMoscovichVsCPP(t *testing.T) {
	bin := crossprobBin(t)
	cases := []struct {
		name  string
		bound func() (lower, upper []float64)
	}{
		{"BJ n=10 c=2", func() ([]float64, []float64) { return genBJBounds(10, 2.0) }},
		{"BJ n=10 c=5", func() ([]float64, []float64) { return genBJBounds(10, 5.0) }},
		{"BJ n=25 c=2", func() ([]float64, []float64) { return genBJBounds(25, 2.0) }},
		{"BJ n=25 c=5", func() ([]float64, []float64) { return genBJBounds(25, 5.0) }},
		{"BJ n=50 c=2", func() ([]float64, []float64) { return genBJBounds(50, 2.0) }},
		{"BJ n=50 c=5", func() ([]float64, []float64) { return genBJBounds(50, 5.0) }},
		{"BJ n=100 c=3", func() ([]float64, []float64) { return genBJBounds(100, 3.0) }},
		{"BJ n=100 c=5", func() ([]float64, []float64) { return genBJBounds(100, 5.0) }},
		{"BJ n=200 c=4", func() ([]float64, []float64) { return genBJBounds(200, 4.0) }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lower, upper := c.bound()
			pCPP := runCrossprobCPP(t, bin, lower, upper)
			pGo, err := crossingProbabilityMoscovich(lower, upper)
			require.NoError(t, err)
			// CLI prints to 3-4 significant digits in this build; allow
			// a generous tolerance against that, plus our own ulp budget.
			tol := math.Max(1e-3, pCPP*1e-3)
			absDiff := math.Abs(pCPP - pGo)
			t.Logf("CPP=%.10f Go=%.10f |Δ|=%.3e", pCPP, pGo, absDiff)
			if absDiff > tol {
				t.Errorf("CPP=%v Go=%v differ by %v (tol %v)", pCPP, pGo, absDiff, tol)
			}
		})
	}
}

func TestMoscovichVsCPPRandom(t *testing.T) {
	bin := crossprobBin(t)
	rnd := rand.New(rand.NewSource(2026))
	for _, n := range []int{10, 25, 50, 100} {
		for trial := range 3 {
			lower, upper := genRandomMonotoneBounds(rnd, n)
			pCPP := runCrossprobCPP(t, bin, lower, upper)
			pGo, err := crossingProbabilityMoscovich(lower, upper)
			require.NoError(t, err)
			absDiff := math.Abs(pCPP - pGo)
			tol := math.Max(1e-3, pCPP*1e-3)
			label := fmt.Sprintf("n=%d trial=%d", n, trial)
			t.Logf("%s: CPP=%.10f Go=%.10f |Δ|=%.3e", label, pCPP, pGo, absDiff)
			if absDiff > tol {
				t.Errorf("%s: CPP=%v Go=%v differ by %v (tol %v)", label, pCPP, pGo, absDiff, tol)
			}
		}
	}
}

// TestCriticalValuesAllFamiliesVsCPP verifies the inverted critical
// values for *every* band family by feeding the resulting boundaries
// back through Moscovich's independent C++ engine. If the inversion
// produced the right c, the C++ result must equal 1-α to the
// precision of the CLI output.
//
// This is the strongest available end-to-end check that the EP, HC,
// and BJ families return correctly-calibrated bands — independent of
// our own Moscovich engine.
func TestCriticalValuesAllFamiliesVsCPP(t *testing.T) {
	bin := crossprobBin(t)
	families := []struct {
		name   string
		method BandMethodE
	}{
		{"DKW", BandMethodDKW},
		{"BJ", BandMethodBerkJones},
		{"EP", BandMethodEqualPrecision},
		{"HC", BandMethodHigherCriticism},
	}
	type cell struct {
		n     int
		alpha float64
	}
	cases := []cell{
		{10, 0.05}, {25, 0.05}, {50, 0.05}, {100, 0.05},
		{10, 0.01}, {25, 0.01}, {50, 0.01}, {100, 0.01},
	}
	for _, f := range families {
		for _, c := range cases {
			t.Run(fmt.Sprintf("%s/n=%d_α=%v", f.name, c.n, c.alpha), func(t *testing.T) {
				lower, upper, err := QuantileBoundaries(c.n, c.alpha, f.method)
				require.NoError(t, err)
				pCPP := runCrossprobCPP(t, bin, lower, upper)
				absDiff := math.Abs(pCPP - (1 - c.alpha))
				t.Logf("%s n=%d α=%v: CPP-P=%.6f target=%v |Δ|=%.3e",
					f.name, c.n, c.alpha, pCPP, 1-c.alpha, absDiff)
				// 2e-3 covers (a) the CLI's print precision (~1e-6) and
				// (b) our inversion bisection's residual (~1e-6) — the
				// rest is headroom for boundary-clip ulp drift.
				if absDiff > 2e-3 {
					t.Errorf("%s inverted bands at n=%d α=%v give CPP-P=%v (want %v)",
						f.name, c.n, c.alpha, pCPP, 1-c.alpha)
				}
			})
		}
	}
}

// TestKSCriticalValueAsymptotic verifies that as n grows, the DKW
// critical value converges to the well-known Kolmogorov asymptotic
// quantile: √n · ε(n, α) → x_α where x_α solves
//
//	K(x) = 1 - 2 Σ_{k=1}^∞ (-1)^{k-1} exp(-2 k² x²) = 1 - α.
//
// For α = 0.05: x ≈ 1.35810. For α = 0.01: x ≈ 1.62762. Tolerance is
// loose at 0.05 because the finite-sample correction at n = 1000 is
// O(1/√n) ≈ 0.03.
func TestKSCriticalValueAsymptotic(t *testing.T) {
	cases := []struct {
		alpha     float64
		asympX    float64
		tolerance float64
	}{
		{0.05, 1.35810, 0.04},
		{0.01, 1.62762, 0.04},
		{0.10, 1.22385, 0.04},
	}
	for _, c := range cases {
		eps, err := CriticalValue(1000, c.alpha, BandMethodDKW)
		require.NoError(t, err)
		gotX := eps * math.Sqrt(1000)
		t.Logf("α=%v: √n·ε(1000) = %.6f, asymptotic K^{-1}(1-α) = %v, |Δ|=%.4f",
			c.alpha, gotX, c.asympX, math.Abs(gotX-c.asympX))
		if math.Abs(gotX-c.asympX) > c.tolerance {
			t.Errorf("KS asymptotic mismatch at α=%v: got %v want %v",
				c.alpha, gotX, c.asympX)
		}
	}
}
