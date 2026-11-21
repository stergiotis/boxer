package ops

import (
	"math"
	"strconv"
)

// gaussianOperator smooths each plane with a Gaussian (ADR-0400 §SD10).
type gaussianOperator struct{}

var gaussianDeclaration = Declaration{
	Id: "gaussian", Version: 1,
	Summary:   "Smooths each plane with a Gaussian of a stated standard deviation, truncated at a radius of ceil(4σ) pixels.",
	MinInputs: 1, MaxInputs: 1,
	InputsDoc: "One image without an alpha plane; each plane is smoothed apart from the others.",
	Params: []Param{
		{Name: "sigma", Type: ParamTypeNumber, Unit: UnitPixels, HasRange: true, Min: 0.1, Max: 64, Default: Number(1),
			Doc: "The Gaussian's standard deviation, in pixels of the input grid."},
		borderParam,
	},
}

func (gaussianOperator) Declaration() Declaration { return gaussianDeclaration }

// gaussianWeights is the kernel of standard deviation sigma over the taps
// −r … r, r = ⌈4σ⌉, normalised so that the taps used sum to one: the sum is
// taken in tap order and each weight divided by it.
func gaussianWeights(sigma float64) []float64 {
	r := int(math.Ceil(4 * sigma))
	w := make([]float64, 2*r+1)
	var sum float64
	for i := range w {
		d := float64(i - r)
		w[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += w[i]
	}
	for i := range w {
		w[i] /= sum
	}
	return w
}

func (gaussianOperator) Plan(inputs []InputDesc, params Params) (plan Plan, k KernelI, err error) {
	if err = refuseAlpha(inputs); err != nil {
		return
	}
	in := inputs[0]
	w := gaussianWeights(params.number("sigma"))
	r := len(w) / 2
	plan = Plan{Model: in.Model, Kind: valueKind(inputs), PlaneLabels: in.PlaneLabels, Halo: r,
		Border: parseBorder(params.text("border")),
		Notes: []string{"The Gaussian is truncated at a radius of " + strconv.Itoa(r) + " px (ceil(4σ)), its " +
			strconv.Itoa(len(w)) + " taps normalised to sum to one; it is applied down the columns, then along the rows."}}
	return plan, gaussianKernel{w: w, planes: in.PlaneCount()}, nil
}

type gaussianKernel struct {
	NoFirstPass
	w      []float64
	planes int
}

func (inst gaussianKernel) Worker() WorkerI { return &gaussianWorker{w: inst.w} }

type gaussianWorker struct {
	w   []float64
	col []float64
}

// Row smooths down the columns of the window into one row of the band's
// full stride, then along it. Every output sample is the same sum in the
// same order whatever the band, so the result does not depend on the band
// size; NaN anywhere in the window makes it NaN.
func (inst *gaussianWorker) Row(b *Band, y int, out [][]float64) {
	r := len(inst.w) / 2
	h := b.Halo()
	if inst.col == nil {
		inst.col = make([]float64, b.Width()+2*h)
	}
	col := inst.col
	for p, dst := range out {
		src := b.Row(0, p, y-r)
		w0 := inst.w[0]
		for x := range col {
			col[x] = w0 * src[x]
		}
		for j := 1; j < len(inst.w); j++ {
			src = b.Row(0, p, y-r+j)
			wj := inst.w[j]
			for x := range col {
				col[x] += wj * src[x]
			}
		}
		for x := range dst {
			// Column x of the image is element x+h of the row.
			win := col[x+h-r : x+h+r+1]
			s := inst.w[0] * win[0]
			for i := 1; i < len(win); i++ {
				s += inst.w[i] * win[i]
			}
			dst[x] = s
		}
	}
}
