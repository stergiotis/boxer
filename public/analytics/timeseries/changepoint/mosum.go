package changepoint

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// DefaultMosumAlpha is the significance level of the MOSUM critical value
	// when the configuration leaves it zero.
	DefaultMosumAlpha = 0.05
	// DefaultMosumEta is the default width, as a fraction of the bandwidth, of
	// the neighbourhood a statistic must dominate to be reported as a change.
	// The value is the one the reference implementation ships.
	DefaultMosumEta = 0.4
)

// MosumConfig parameterizes [Mosum].
type MosumConfig struct {
	// Bandwidth is the number of samples on each side of a candidate position
	// that are compared. It is the method's one real choice: it must be
	// shorter than the distance between neighbouring changes, and long enough
	// that a mean over it is steadier than the shift to be found.
	Bandwidth int32
	// Alpha is the asymptotic significance level of the critical value. Zero
	// accepts [DefaultMosumAlpha].
	Alpha float64
	// Eta sets the neighbourhood, Eta·Bandwidth on each side, in which a
	// position's statistic must be the largest for the position to be
	// reported. Zero accepts [DefaultMosumEta].
	Eta float64
}

// MosumResult is the moving-sum statistic over a series and the changes read
// off it.
type MosumResult struct {
	// Stat[k] compares the Bandwidth samples before position k with the
	// Bandwidth samples from k on, as a difference of means in units of its
	// own standard error. Defined for k in [First, Last]; zero elsewhere.
	Stat []float64
	// First and Last bound the positions with a full window on both sides.
	First, Last int32
	// Critical is the level Stat exceeds with probability about Alpha
	// anywhere in a series with no change.
	Critical float64
	// Starts holds the detected changes, as in [Segmentation.Starts].
	Starts []int32
}

// Mosum detects changes in mean with the moving-sum procedure of Eichinger
// and Kirch, "A MOSUM procedure for the estimation of multiple random change
// points", Bernoulli 24 (2018), doi:10.3150/16-BEJ887, as implemented in
// Meier, Kirch and Cho, "mosum: A Package for Moving Sums in Change-Point
// Analysis", J. Stat. Softw. 97 (2021), doi:10.18637/jss.v097.i08.
//
// At every position the mean of the window before is compared with the mean
// of the window after, scaled by a variance estimated from those same two
// windows, each around its own mean, so a change does not inflate it. A
// position is reported when its statistic exceeds the critical value and is
// the largest within Eta·Bandwidth on either side.
//
// The critical value is asymptotic and assumes independent noise. It controls
// the chance of any false report in a series without change; it says nothing
// of the kind once there are changes, and it is too low for positively
// autocorrelated noise, whose window means differ more than independent ones
// would. Two changes closer than a bandwidth blur into one or cancel.
func Mosum(vals []float64, cfg MosumConfig) (res MosumResult, err error) {
	n := len(vals)
	g := int(cfg.Bandwidth)
	if g < 2 {
		err = eb.Build().Int32("bandwidth", cfg.Bandwidth).Errorf("the bandwidth is below two samples")
		return
	}
	if n < 3*g {
		err = eb.Build().Int("len", n).Int32("bandwidth", cfg.Bandwidth).
			Errorf("the series is shorter than three bandwidths, where the critical value is not defined")
		return
	}
	alpha := cfg.Alpha
	if alpha == 0 {
		alpha = DefaultMosumAlpha
	}
	if !(alpha > 0 && alpha < 1) {
		err = eb.Build().Float64("alpha", cfg.Alpha).Errorf("the significance level is outside (0, 1)")
		return
	}
	eta := cfg.Eta
	if eta == 0 {
		eta = DefaultMosumEta
	}
	if !(eta > 0 && eta <= 1) {
		err = eb.Build().Float64("eta", cfg.Eta).Errorf("the neighbourhood fraction is outside (0, 1]")
		return
	}
	err = checkFinite(vals)
	if err != nil {
		return
	}
	table := newCostTable(CostMean, vals, 1)
	res.Stat = make([]float64, n)
	res.First, res.Last = int32(g), int32(n-g)
	res.Critical = MosumCritical(n, g, alpha)
	gf := float64(g)
	for k := g; k <= n-g; k++ {
		left := table.sum[k] - table.sum[k-g]
		right := table.sum[k+g] - table.sum[k]
		variance := (table.cost(k-g, k) + table.cost(k, k+g)) / (2 * gf)
		if variance <= 0 {
			// Both windows are constant. Equal constants are no change; unequal
			// ones are a change the noise scale cannot be asked about, and it
			// is reported as exceeding any level.
			if left != right {
				res.Stat[k] = math.Inf(1)
			}
			continue
		}
		res.Stat[k] = math.Abs(right-left) / math.Sqrt(2*gf*variance)
	}
	reach := max(int(eta*gf), 1)
	for k := g; k <= n-g; k++ {
		s := res.Stat[k]
		if !(s > res.Critical) {
			continue
		}
		peak := true
		for j := max(g, k-reach); j <= min(n-g, k+reach); j++ {
			// Strict on the left only, so a plateau reports its first position
			// once rather than every position or none.
			if res.Stat[j] > s || (j < k && res.Stat[j] == s) {
				peak = false
				break
			}
		}
		if peak {
			res.Starts = append(res.Starts, int32(k))
		}
	}
	return
}

// MosumCritical returns the asymptotic critical value of the symmetric MOSUM
// statistic for a series of n samples at bandwidth g and level alpha: the
// Gumbel-type limit of Eichinger and Kirch (2018), Theorem 2.1.
func MosumCritical(n int, g int, alpha float64) (critical float64) {
	ratio := math.Log(float64(n) / float64(g))
	a := math.Sqrt(2 * ratio)
	b := 2*ratio + 0.5*math.Log(ratio) + math.Log(1.5) - 0.5*math.Log(math.Pi)
	c := -math.Log(math.Log(1 / math.Sqrt(1-alpha)))
	critical = (b + c) / a
	return
}
