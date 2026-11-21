package ops

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// standardizeOperator rescales each plane by its own mean and standard
// deviation, found in a first pass (ADR-0400 §SD7, §SD10).
type standardizeOperator struct{}

var standardizeDeclaration = Declaration{
	Id: "standardize", Version: 1,
	Summary:   "Rescales each plane to (value − mean) / standard deviation, with the plane's own mean and standard deviation over its valid samples.",
	MinInputs: 1, MaxInputs: 1,
	InputsDoc: "One image without an alpha plane; each plane is rescaled by its own statistics.",
}

func (standardizeOperator) Declaration() Declaration { return standardizeDeclaration }

func (standardizeOperator) Plan(inputs []InputDesc, _ Params) (plan Plan, k KernelI, err error) {
	if err = refuseAlpha(inputs); err != nil {
		return
	}
	in := inputs[0]
	n := in.PlaneCount()
	plan = Plan{Model: in.Model, Kind: valueKind(inputs), PlaneLabels: in.PlaneLabels, FirstPasses: 2,
		Notes: []string{"The mean and the standard deviation (divisor n) are taken over each plane's valid samples, " +
			"in two passes of compensated sums in row-major order: the mean, then the squared deviations from it."}}
	return plan, &standardizeKernel{labels: in.PlaneLabels, count: make([]int64, n), sum: make([]neumaier, n),
		ss: make([]neumaier, n), mean: make([]float64, n), sd: make([]float64, n)}, nil
}

type standardizeKernel struct {
	labels   []string
	count    []int64
	sum, ss  []neumaier
	mean, sd []float64
}

func (inst *standardizeKernel) FirstPass(pass int, b *Band) error {
	y0, y1 := b.Rows()
	for p := range inst.count {
		for y := y0; y < y1; y++ {
			row := b.Row(0, p, y)
			if pass == 0 {
				for _, v := range row {
					if v == v {
						inst.sum[p].add(v)
						inst.count[p]++
					}
				}
				continue
			}
			m := inst.mean[p]
			for _, v := range row {
				if v == v {
					d := v - m
					inst.ss[p].add(d * d)
				}
			}
		}
	}
	return nil
}

func (inst *standardizeKernel) EndPass(pass int) error {
	for p := range inst.count {
		label := inst.labels[p]
		if pass == 0 {
			if inst.count[p] == 0 {
				return eb.Build().Int("plane", p).Str("label", label).Errorf("a plane has no valid sample to standardise by: %w", ErrRefused)
			}
			inst.mean[p] = inst.sum[p].value() / float64(inst.count[p])
			if !isFinite(inst.mean[p]) {
				return eb.Build().Int("plane", p).Str("label", label).Errorf("a plane's mean is not finite: %w", ErrRefused)
			}
			continue
		}
		inst.sd[p] = math.Sqrt(inst.ss[p].value() / float64(inst.count[p]))
		switch {
		case inst.sd[p] == 0:
			return eb.Build().Int("plane", p).Str("label", label).Float64("mean", inst.mean[p]).
				Errorf("a plane's standard deviation is zero: every valid sample is its mean: %w", ErrRefused)
		case !isFinite(inst.sd[p]):
			return eb.Build().Int("plane", p).Str("label", label).Errorf("a plane's standard deviation is not finite: %w", ErrRefused)
		}
	}
	return nil
}

func (inst *standardizeKernel) Quantities() []Quantity {
	q := make([]Quantity, 0, 3*len(inst.count))
	for p := range inst.count {
		q = append(q, Quantity{Name: "count", Plane: p, Value: float64(inst.count[p])},
			Quantity{Name: "mean", Plane: p, Value: inst.mean[p]},
			Quantity{Name: "stddev", Plane: p, Value: inst.sd[p]})
	}
	return q
}

func (inst *standardizeKernel) Worker() WorkerI { return standardizeWorker{k: inst} }

type standardizeWorker struct {
	k *standardizeKernel
}

func (inst standardizeWorker) Row(b *Band, y int, out [][]float64) {
	for p, dst := range out {
		m, sd := inst.k.mean[p], inst.k.sd[p]
		for x, v := range b.Row(0, p, y) {
			dst[x] = (v - m) / sd
		}
	}
}
