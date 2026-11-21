package ops

import "math"

// neumaier is a compensated sum (Neumaier's variant of Kahan's): it carries
// the low-order part each addition loses, so that a sum over millions of
// samples keeps nearly the precision of one addition. Its result depends on
// the order of the additions, which the first pass fixes as row-major.
type neumaier struct {
	sum, c float64
}

func (inst *neumaier) add(x float64) {
	t := inst.sum + x
	if math.Abs(inst.sum) >= math.Abs(x) {
		inst.c += (inst.sum - t) + x
	} else {
		inst.c += (x - t) + inst.sum
	}
	inst.sum = t
}

func (inst *neumaier) value() float64 { return inst.sum + inst.c }
