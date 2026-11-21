package ops

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/science/imaging"
)

// axisStatOperator is a statistic across its inputs, pixel by pixel, over
// the valid samples (ADR-0400 §SD10): a composite over time, a stack of
// exposures.
type axisStatOperator struct{}

const (
	statMean   = "mean"
	statMedian = "median"
	statMin    = "min"
	statMax    = "max"
	statStdDev = "stddev"
	statCount  = "count"
)

var axisStatDeclaration = Declaration{
	Id: "axis-stat", Version: 1,
	Summary:          "One plane holding a statistic of each pixel's valid samples across the inputs.",
	MinInputs:        1,
	InputsDoc:        "Single-plane images of one size: the positions along an axis, such as acquisitions over time.",
	LeavesInvalidOut: true,
	Params: []Param{{
		Name: "stat", Type: ParamTypeChoice, Required: true,
		Doc: "The statistic of each pixel's valid samples; a pixel with none is invalid, except under count.",
		Choices: []Choice{
			{Name: statMean, Doc: "The arithmetic mean."},
			{Name: statMedian, Doc: "The middle value; of an even number, the lower of the two middle values, as a nearest-rank percentile gives it."},
			{Name: statMin, Doc: "The smallest value."},
			{Name: statMax, Doc: "The largest value."},
			{Name: statStdDev, Doc: "The standard deviation over the samples as the whole population: the divisor is their number."},
			{Name: statCount, Doc: "The number of valid samples, 0 where there is none."},
		},
	}},
}

func (axisStatOperator) Declaration() Declaration { return axisStatDeclaration }

func (axisStatOperator) Plan(inputs []InputDesc, params Params) (plan Plan, k KernelI, err error) {
	if err = requireSinglePlane(inputs); err != nil {
		return
	}
	stat := params.text("stat")
	plan = Plan{Model: imaging.ColorModelGray, Kind: valueKind(inputs), PlaneLabels: []string{stat}}
	return plan, axisStatKernel{stat: stat, n: len(inputs)}, nil
}

type axisStatKernel struct {
	NoFirstPass
	stat string
	n    int
}

func (inst axisStatKernel) Worker() WorkerI {
	return &axisStatWorker{stat: inst.stat, rows: make([][]float64, inst.n), valid: make([]float64, 0, inst.n)}
}

type axisStatWorker struct {
	stat  string
	rows  [][]float64
	valid []float64
}

func (inst *axisStatWorker) Row(b *Band, y int, out [][]float64) {
	for i := range inst.rows {
		inst.rows[i] = b.Row(i, 0, y)
	}
	dst := out[0]
	for x := range dst {
		v := inst.valid[:0]
		for _, r := range inst.rows {
			if s := r[x]; s == s {
				v = append(v, s)
			}
		}
		inst.valid = v
		dst[x] = axisStat(inst.stat, v)
	}
}

// axisStat is the statistic of the valid values v, in input order; NaN for
// none, except the count.
func axisStat(stat string, v []float64) float64 {
	if stat == statCount {
		return float64(len(v))
	}
	if len(v) == 0 {
		return nan
	}
	switch stat {
	case statMean:
		return meanOf(v)
	case statMin:
		return slices.Min(v)
	case statMax:
		return slices.Max(v)
	case statStdDev:
		m := meanOf(v)
		var ss float64
		for _, s := range v {
			d := s - m
			ss += d * d
		}
		return math.Sqrt(ss / float64(len(v)))
	case statMedian:
		slices.Sort(v)
		return v[(len(v)-1)/2]
	}
	return nan
}

func meanOf(v []float64) float64 {
	var sum float64
	for _, s := range v {
		sum += s
	}
	return sum / float64(len(v))
}
