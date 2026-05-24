//go:build llm_generated_opus47 && slow

// Expanded Monte-Carlo coverage grid: K = 10⁵ replicates across a
// 6×5 grid of (n, α) per family. Tightens the calibration evidence
// by ~3× over the standard slow-tag coverage test.
//
// Runtime: ~10 minutes on a workstation-class machine. The default
// slow run skips this — set ECDFBANDS_GRID=1 in the environment to
// enable.

package ecdfbands

import (
	"math"
	"math/rand"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCoverageGridAllFamilies(t *testing.T) {
	if os.Getenv("ECDFBANDS_GRID") == "" {
		t.Skip("set ECDFBANDS_GRID=1 to run the expanded coverage grid (~10 min)")
	}

	const k = 100_000
	families := []struct {
		name   string
		method BandMethodE
	}{
		{"BJ", BandMethodBerkJones},
		{"DKW", BandMethodDKW},
		{"EP", BandMethodEqualPrecision},
		{"HC", BandMethodHigherCriticism},
	}
	ns := []int{10, 25, 50, 100, 250, 500}
	alphas := []float64{0.001, 0.01, 0.05, 0.10, 0.25}

	type result struct {
		family   string
		n        int
		alpha    float64
		emp      float64
		nominal  float64
		sigma    float64
		devSigma float64
	}
	var results []result
	for _, fam := range families {
		for _, n := range ns {
			for _, alpha := range alphas {
				emp := coverageCell(t, n, alpha, fam.method, k)
				sigma := math.Sqrt(alpha * (1 - alpha) / float64(k))
				dev := math.Abs(emp-(1-alpha)) / sigma
				results = append(results, result{
					family: fam.name, n: n, alpha: alpha,
					emp: emp, nominal: 1 - alpha, sigma: sigma, devSigma: dev,
				})
				t.Logf("%-3s n=%-4d α=%-6v emp=%.5f nominal=%.5f σ=%.5f dev=%4.2fσ",
					fam.name, n, alpha, emp, 1-alpha, sigma, dev)
			}
		}
	}
	// Report aggregate stats.
	var maxDev float64
	var maxLabel string
	worstFamily := map[string]float64{}
	for _, r := range results {
		if r.devSigma > maxDev {
			maxDev = r.devSigma
			maxLabel = r.family + " n=" + itoaSimple(r.n) + " α=" + floatSimple(r.alpha)
		}
		if r.devSigma > worstFamily[r.family] {
			worstFamily[r.family] = r.devSigma
		}
		// Fail any cell deviating beyond 4σ.
		require.LessOrEqual(t, r.devSigma, 4.0,
			"%s n=%d α=%v: empirical %v vs nominal %v (deviation %.2fσ)",
			r.family, r.n, r.alpha, r.emp, r.nominal, r.devSigma)
	}
	t.Logf("---- aggregate ----")
	t.Logf("max deviation: %.2fσ at %s", maxDev, maxLabel)
	for fam, dev := range worstFamily {
		t.Logf("worst %s: %.2fσ", fam, dev)
	}
	t.Logf("total cells: %d, all within 4σ", len(results))
}

func coverageCell(t *testing.T, n int, alpha float64, method BandMethodE, k int) float64 {
	t.Helper()
	lower, upper, err := QuantileBoundaries(n, alpha, method)
	require.NoError(t, err)
	rnd := rand.New(rand.NewSource(int64(n)*100003 + int64(alpha*1e6) + int64(method)))
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
	return float64(inside) / float64(k)
}

func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func floatSimple(x float64) string {
	// Tiny formatter: avoids importing strconv just for log output.
	if x == 0 {
		return "0"
	}
	if x == math.Floor(x) {
		return itoaSimple(int(x))
	}
	// 4 decimal places.
	scaled := math.Round(x*10000) / 10000
	whole := int(scaled)
	frac := int(math.Round((scaled - float64(whole)) * 10000))
	if frac < 0 {
		frac = -frac
	}
	s := itoaSimple(whole) + "."
	// Pad fractional with leading zeros.
	digits := itoaSimple(frac)
	for len(digits) < 4 {
		digits = "0" + digits
	}
	// Strip trailing zeros from fractional.
	for len(digits) > 1 && digits[len(digits)-1] == '0' {
		digits = digits[:len(digits)-1]
	}
	return s + digits
}
