package ops

// medianFilterOperator is the median over a square window, plane by plane
// (ADR-0400 §SD10).
type medianFilterOperator struct{}

var medianFilterDeclaration = Declaration{
	Id: "median-filter", Version: 1,
	Summary:   "Replaces each sample by the median of the (2r+1) × (2r+1) window around it, plane by plane.",
	MinInputs: 1, MaxInputs: 1,
	InputsDoc: "One image without an alpha plane; each plane is filtered apart from the others.",
	Params: []Param{
		{Name: "radius", Type: ParamTypeInteger, Unit: UnitPixels, HasRange: true, Min: 1, Max: 16, Default: Integer(1),
			Doc: "The window's half-width r, in pixels of the input grid."},
		borderParam,
	},
}

func (medianFilterOperator) Declaration() Declaration { return medianFilterDeclaration }

func (medianFilterOperator) Plan(inputs []InputDesc, params Params) (plan Plan, k KernelI, err error) {
	if err = refuseAlpha(inputs); err != nil {
		return
	}
	in := inputs[0]
	r := int(params.integer("radius"))
	plan = Plan{Model: in.Model, Kind: valueKind(inputs), PlaneLabels: in.PlaneLabels, Halo: r,
		Border: parseBorder(params.text("border")),
		Notes: []string{"A window holding an invalid sample, or under border=invalid reaching outside the image, gives an invalid sample, " +
			"so every median is of a full, odd-sized window."}}
	return plan, medianKernel{r: r}, nil
}

type medianKernel struct {
	NoFirstPass
	r int
}

func (inst medianKernel) Worker() WorkerI {
	n := 2*inst.r + 1
	return &medianWorker{r: inst.r, win: make([]float64, 0, n*n), rows: make([][]float64, n)}
}

type medianWorker struct {
	r    int
	win  []float64
	rows [][]float64
}

func (inst *medianWorker) Row(b *Band, y int, out [][]float64) {
	r, h := inst.r, b.Halo()
	n := 2*r + 1
	for p, dst := range out {
		for j := range n {
			inst.rows[j] = b.Row(0, p, y-r+j)
		}
	pixels:
		for x := range dst {
			win := inst.win[:0]
			for _, row := range inst.rows {
				for _, v := range row[x+h-r : x+h+r+1] {
					if v != v {
						dst[x] = nan
						continue pixels
					}
					win = append(win, v)
				}
			}
			dst[x] = selectNth(win, len(win)/2)
		}
	}
}

// selectNth is the k-th smallest of v (0-based), reordering v: a
// quickselect with a median-of-three pivot down to 17 values, then an
// insertion sort.
// The value it returns does not depend on the order v arrives in.
func selectNth(v []float64, k int) float64 {
	lo, hi := 0, len(v)-1
	for hi-lo > 16 {
		mid := lo + (hi-lo)/2
		if v[mid] < v[lo] {
			v[mid], v[lo] = v[lo], v[mid]
		}
		if v[hi] < v[lo] {
			v[hi], v[lo] = v[lo], v[hi]
		}
		if v[hi] < v[mid] {
			v[hi], v[mid] = v[mid], v[hi]
		}
		pivot := v[mid]
		i, j := lo, hi
		for i <= j {
			for v[i] < pivot {
				i++
			}
			for v[j] > pivot {
				j--
			}
			if i <= j {
				v[i], v[j] = v[j], v[i]
				i++
				j--
			}
		}
		switch {
		case k <= j:
			hi = j
		case k >= i:
			lo = i
		default:
			return v[k]
		}
	}
	// An insertion sort of what is left, at most 17 values.
	for i := lo + 1; i <= hi; i++ {
		x := v[i]
		j := i
		for ; j > lo && v[j-1] > x; j-- {
			v[j] = v[j-1]
		}
		v[j] = x
	}
	return v[k]
}
