package changepoint

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

const (
	// DefaultBocpdRunFactor sets the default truncation of the run-length
	// posterior, as a multiple of the expected run length.
	DefaultBocpdRunFactor = 8
	// BocpdRunLimit is the largest truncation accepted. The detector holds a
	// handful of arrays of that length and walks them on every sample.
	BocpdRunLimit = 1 << 20
)

// NormalGammaPrior is the conjugate prior of a Gaussian regime with unknown
// mean and variance: the precision is Gamma(Alpha, Beta), and given it the
// mean is Normal(Mean, 1/(Kappa·precision)).
//
// Read as pseudo-observations: Kappa samples said the mean was Mean, and
// 2·Alpha samples said the variance was Beta/Alpha.
type NormalGammaPrior struct {
	Mean, Kappa, Alpha, Beta float64
}

// PriorFromSample builds a weak prior centred on a reference sample: its
// mean, its variance, and the weight of one observation each.
//
// The prior decides what a new regime is expected to look like before any of
// its samples are seen, so its scale matters: far too wide and a real shift
// looks unremarkable under the old regime's rival, far too narrow and every
// sample looks like a new regime. Taking it from the data being analysed
// makes the readings depend on more than the past, unless the sample is a
// prefix that is then not scored.
func PriorFromSample(vals []float64) (prior NormalGammaPrior, err error) {
	n := len(vals)
	if n < 2 {
		err = eb.Build().Int("len", n).Errorf("a prior needs at least two samples")
		return
	}
	err = checkFinite(vals)
	if err != nil {
		return
	}
	var mean float64
	for _, v := range vals {
		mean += v
	}
	mean /= float64(n)
	var m2 float64
	for _, v := range vals {
		m2 += (v - mean) * (v - mean)
	}
	variance := m2 / float64(n-1)
	if variance <= 0 {
		err = eb.Build().Int("len", n).Errorf("the sample has no variation to scale a prior with")
		return
	}
	prior = NormalGammaPrior{Mean: mean, Kappa: 1, Alpha: 1, Beta: variance}
	return
}

// BocpdConfig parameterizes a [Bocpd].
type BocpdConfig struct {
	// ExpectedRun is the prior mean number of samples between changes; the
	// hazard, the prior chance that any given sample starts a new regime, is
	// its reciprocal. It is a statement of belief and it is not learned: a
	// value far below the true spacing makes the detector eager, far above
	// makes it slow.
	ExpectedRun float64
	// Prior is what a new regime is expected to look like.
	Prior NormalGammaPrior
	// MaxRun truncates the run lengths tracked. Zero accepts
	// [DefaultBocpdRunFactor]·ExpectedRun. Every hypothesis of a longer run
	// is merged into the last one, which then stops gaining confidence and
	// behaves as an exponentially weighted estimate of the regime.
	MaxRun int32
	// Lag is the window of [BocpdReading.RecentChange]: the reading is the
	// probability that the current regime began within the last Lag samples.
	// Zero accepts 1.
	Lag int32
}

// BocpdReading is the posterior after one sample.
type BocpdReading struct {
	// RecentChange is the posterior probability, given the samples so far,
	// that the current regime started within the last Lag samples. It is a
	// probability under the model — Gaussian regimes, independent samples, a
	// constant hazard — and only as good as that model is for the data.
	//
	// With Lag 1 a change and a single outlier are the same event; a larger
	// lag separates them at the price of that many samples of delay.
	RecentChange float64
	// MapRun is the most probable number of samples in the current regime,
	// counting this one.
	MapRun int32
}

// Bocpd is the Bayesian online change point detector of Adams and MacKay,
// "Bayesian Online Changepoint Detection", arXiv:0710.3742 (2007), for
// Gaussian regimes of unknown mean and variance under a constant hazard.
//
// It maintains the posterior over the current run length: how many of the
// most recent samples belong to the regime in force. Every reading uses the
// samples up to it and nothing later.
//
// The run length here counts the newest sample, and a sample that starts a
// regime is scored under the prior. (In the formulation where a new run's
// first sample is scored under the old regime, the posterior probability of a
// run of length zero equals the hazard whatever the data says, which is why
// that quantity is not what this type reports.)
//
// The cost per sample is proportional to MaxRun.
//
// A Bocpd is not safe for concurrent use.
type Bocpd struct {
	hazard float64
	lag    int
	maxRun int
	prior  NormalGammaPrior
	count  int64

	// Indexed by the number of samples r in the run, 0..maxRun; index 0 is the
	// prior. kappa, alpha and the Student-t constant depend on r alone.
	kappa []float64
	alpha []float64
	lnorm []float64

	// prob[r], mean[r], beta[r] for r in 1..active are the run-length
	// posterior and each hypothesis' sufficient statistics.
	prob   []float64
	mean   []float64
	beta   []float64
	active int

	scratch []float64
}

// NewBocpd validates cfg and returns a detector ready for its first sample.
func NewBocpd(cfg BocpdConfig) (inst *Bocpd, err error) {
	p := cfg.Prior
	err = checkConfig([]string{"expectedRun", "mean", "kappa", "alpha", "beta"}, cfg.ExpectedRun, p.Mean, p.Kappa, p.Alpha, p.Beta)
	if err != nil {
		return
	}
	if !(cfg.ExpectedRun > 1) {
		err = eb.Build().Float64("expectedRun", cfg.ExpectedRun).Errorf("the expected run length must exceed one sample")
		return
	}
	if !(p.Kappa > 0 && p.Alpha > 0 && p.Beta > 0) {
		err = eb.Build().Float64("kappa", p.Kappa).Float64("alpha", p.Alpha).Float64("beta", p.Beta).
			Errorf("the prior's kappa, alpha and beta must be positive")
		return
	}
	if cfg.MaxRun < 0 || cfg.Lag < 0 {
		err = eb.Build().Int32("maxRun", cfg.MaxRun).Int32("lag", cfg.Lag).Errorf("the run limit or the lag is negative")
		return
	}
	limit := float64(cfg.MaxRun)
	if cfg.MaxRun == 0 {
		limit = math.Ceil(DefaultBocpdRunFactor * cfg.ExpectedRun)
	}
	if limit > BocpdRunLimit {
		err = eb.Build().Float64("maxRun", limit).Int("limit", BocpdRunLimit).
			Errorf("the run-length truncation is above the limit; set MaxRun, which an expected run this long needs anyway")
		return
	}
	maxRun := max(int(limit), 2)
	lag := max(int(cfg.Lag), 1)
	inst = &Bocpd{
		hazard:  1 / cfg.ExpectedRun,
		lag:     lag,
		maxRun:  maxRun,
		prior:   p,
		kappa:   make([]float64, maxRun+1),
		alpha:   make([]float64, maxRun+1),
		lnorm:   make([]float64, maxRun+1),
		prob:    make([]float64, maxRun+1),
		mean:    make([]float64, maxRun+1),
		beta:    make([]float64, maxRun+1),
		scratch: make([]float64, maxRun+1),
	}
	for r := 0; r <= maxRun; r++ {
		k := p.Kappa + float64(r)
		a := p.Alpha + 0.5*float64(r)
		inst.kappa[r], inst.alpha[r] = k, a
		lgHi, _ := math.Lgamma(a + 0.5)
		lgLo, _ := math.Lgamma(a)
		// The Student-t predictive has 2a degrees of freedom and squared
		// scale beta·(k+1)/(a·k); everything in its density that does not
		// involve beta or the sample is folded in here.
		inst.lnorm[r] = lgHi - lgLo - 0.5*math.Log(2*math.Pi*(k+1)/k)
	}
	inst.mean[0], inst.beta[0] = p.Mean, p.Beta
	return
}

// predictive returns the density of v under the hypothesis holding r samples.
func (inst *Bocpd) predictive(r int, v float64) (density float64) {
	d := v - inst.mean[r]
	k := inst.kappa[r]
	b := inst.beta[r]
	density = math.Exp(inst.lnorm[r] - 0.5*math.Log(b) - (inst.alpha[r]+0.5)*math.Log1p(d*d*k/(2*b*(k+1))))
	return
}

// Push feeds one sample and returns the posterior after it. A sample that is
// not finite is refused and leaves the detector unchanged.
func (inst *Bocpd) Push(v float64) (reading BocpdReading, err error) {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		err = eb.Build().Int64("position", inst.count).Errorf("the sample is not finite")
		return
	}
	inst.count++
	top := inst.maxRun
	// Only the slots this sample can reach are written and read.
	next := inst.scratch
	clear(next[:min(inst.active+2, top+1)])
	// A change: whatever the run was, this sample starts a new one and is
	// scored under the prior. The first sample has nothing before it, so it
	// starts a run with certainty.
	total := 1.0
	if inst.active > 0 {
		total = 0
		for r := 1; r <= inst.active; r++ {
			total += inst.prob[r]
		}
		total *= inst.hazard
	}
	next[1] = total * inst.predictive(0, v)
	// Growth: the run continues and the sample is scored under what the run
	// has learned. The longest hypothesis grows into itself.
	for r := 1; r <= inst.active; r++ {
		next[min(r+1, top)] += inst.prob[r] * (1 - inst.hazard) * inst.predictive(r, v)
	}
	// Sufficient statistics, from the longest run down so each write reads a
	// value not yet overwritten. The top hypothesis keeps its own statistics
	// once it exists; the one arriving from below is merged into it by mass
	// only.
	hadTop := inst.active == top
	for r := min(inst.active, top-1); r >= 0; r-- {
		if r+1 == top && hadTop {
			continue
		}
		inst.absorb(r, r+1, v)
	}
	if hadTop {
		inst.absorb(top, top, v)
	}
	inst.active = min(inst.active+1, top)
	var sum float64
	for r := 1; r <= inst.active; r++ {
		sum += next[r]
	}
	if !(sum > 0) {
		// Every hypothesis gave the sample a density of zero in floating
		// point. The sample is then beyond what any regime on record explains,
		// which the model can only read as a new one.
		clear(next[:inst.active+1])
		next[1], sum = 1, 1
	}
	inv := 1 / sum
	mapRun, mapProb, recent := 1, -1.0, 0.0
	for r := 1; r <= inst.active; r++ {
		p := next[r] * inv
		next[r] = p
		if p > mapProb {
			mapRun, mapProb = r, p
		}
		if r <= inst.lag {
			recent += p
		}
	}
	inst.prob, inst.scratch = next, inst.prob
	reading.RecentChange = min(recent, 1)
	reading.MapRun = int32(mapRun)
	return
}

// absorb writes into slot dst the statistics of slot src after seeing v.
func (inst *Bocpd) absorb(src int, dst int, v float64) {
	k := inst.kappa[src]
	d := v - inst.mean[src]
	beta := inst.beta[src] + k*d*d/(2*(k+1))
	if src == dst {
		// The truncated hypothesis keeps the sample count of its slot, so its
		// sum of squares is scaled back to that count; otherwise it would grow
		// against a fixed alpha and read as an ever wider regime.
		a := inst.alpha[src]
		beta *= a / (a + 0.5)
	}
	inst.beta[dst] = beta
	inst.mean[dst] = inst.mean[src] + d/(k+1)
}

// Posterior copies the current run-length posterior into dst: dst[r−1] is the
// probability that the current regime holds r samples. dst is grown as
// needed and returned.
func (inst *Bocpd) Posterior(dst []float64) (out []float64) {
	out = append(dst[:0], inst.prob[1:inst.active+1]...)
	return
}
