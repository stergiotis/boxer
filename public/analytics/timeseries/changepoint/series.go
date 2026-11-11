package changepoint

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// Config parameterizes a [Series].
type Config struct {
	// Cost is what each segment is fitted with.
	Cost CostE

	// MinSegment is the fewest samples a segment may hold. Zero accepts the
	// cost's own minimum (1, or 2 for [CostMeanVar]). Raising it is the direct
	// way to say "a regime shorter than this is not a regime", and it is also
	// what keeps [CostMeanVar] from fitting a tiny variance to a handful of
	// samples that happen to agree.
	MinSegment int32

	// NoiseScale is the noise standard deviation [CostMean] measures a shift
	// against. Zero accepts [NoiseScaleMAD] of the series. Ignored by the
	// other costs: [CostMeanVar] fits a variance per segment and [CostRank]
	// scales by the variance of the ranks.
	NoiseScale float64
}

// Series is a validated series prepared for segmentation: the cost of any
// segment is a constant-time lookup. It is immutable after construction and
// safe for concurrent use.
type Series struct {
	cfg        Config
	n          int
	minSegment int
	noiseScale float64
	table      costTable
}

// NewSeries prepares vals for segmentation under cfg.
func NewSeries(vals []float64, cfg Config) (inst *Series, err error) {
	if cfg.Cost > CostRank {
		err = eb.Build().Uint8("cost", uint8(cfg.Cost)).Errorf("unknown cost")
		return
	}
	if cfg.MinSegment < 0 {
		err = eb.Build().Int32("minSegment", cfg.MinSegment).Errorf("the minimum segment length is negative")
		return
	}
	if cfg.NoiseScale < 0 || math.IsNaN(cfg.NoiseScale) || math.IsInf(cfg.NoiseScale, 0) {
		err = eb.Build().Float64("noiseScale", cfg.NoiseScale).Errorf("the noise scale is not a positive finite number")
		return
	}
	minSegment := max(cfg.MinSegment, cfg.Cost.minSegment())
	n := len(vals)
	if n < 2*int(minSegment) {
		err = eb.Build().Int("len", n).Int32("minSegment", minSegment).
			Errorf("the series is shorter than two minimum segments, so it has no place for a change")
		return
	}
	err = checkFinite(vals)
	if err != nil {
		return
	}
	inst = &Series{cfg: cfg, n: n, minSegment: int(minSegment)}
	switch cfg.Cost {
	case CostMean:
		inst.noiseScale = cfg.NoiseScale
		if inst.noiseScale == 0 {
			inst.noiseScale = NoiseScaleMAD(vals)
		}
		inst.table = newCostTable(CostMean, vals, inst.noiseScale)
	case CostMeanVar:
		inst.table = newCostTable(CostMeanVar, vals, 1)
	case CostRank:
		ranks := midRanks(vals)
		var sumSq float64
		centre := 0.5 * float64(n+1)
		for _, r := range ranks {
			sumSq += (r - centre) * (r - centre)
		}
		inst.table = newCostTable(CostMean, ranks, math.Sqrt(sumSq/float64(n)))
	}
	return
}

// Len returns the number of samples.
func (inst *Series) Len() (n int) {
	return inst.n
}

// MinSegment returns the minimum segment length in effect.
func (inst *Series) MinSegment() (length int32) {
	return int32(inst.minSegment)
}

// NoiseScale returns the noise standard deviation [CostMean] divides by, and 0
// for the other costs.
func (inst *Series) NoiseScale() (sigma float64) {
	return inst.noiseScale
}

// PenaltyBIC returns the Schwarz penalty for one more change: (p+1)·ln n,
// where p is the number of parameters a segment carries (1, or 2 for
// [CostMeanVar]) and the extra one is the change's location.
//
// It is the conventional default and it is derived for independent Gaussian
// noise. For [CostRank] the same number is used by analogy — the rank cost
// behaves like a unit-variance mean cost under no change — and is a starting
// point, not a derivation. Any single penalty is one point on a curve; see
// [Series.PenaltyPath] for the curve.
func (inst *Series) PenaltyBIC() (penalty float64) {
	penalty = float64(inst.cfg.Cost.freeParams()+1) * math.Log(float64(inst.n))
	return
}

// Segmentation is one division of a series into contiguous segments.
type Segmentation struct {
	// Starts holds the index of the first sample of every segment after the
	// first, ascending. A change "at" Starts[i] means sample Starts[i]−1 is the
	// last of the old segment. Empty when no change was found.
	Starts []int32
	// Cost is the summed segment cost, without any penalty.
	Cost float64
	// Penalty is the per-change penalty the segmentation was found under.
	Penalty float64
}

// NumChanges returns the number of changes.
func (inst Segmentation) NumChanges() (count int32) {
	count = int32(len(inst.Starts))
	return
}

// costOf sums the segment costs of a set of starts.
func (inst *Series) costOf(starts []int32) (cost float64) {
	prev := 0
	for _, s := range starts {
		cost += inst.table.cost(prev, int(s))
		prev = int(s)
	}
	cost += inst.table.cost(prev, inst.n)
	return
}

func checkPenalty(penalty float64) (err error) {
	if penalty < 0 || math.IsNaN(penalty) || math.IsInf(penalty, 0) {
		err = eb.Build().Float64("penalty", penalty).Errorf("the penalty is not a non-negative finite number")
	}
	return
}
