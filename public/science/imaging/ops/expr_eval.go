package ops

import (
	"math"
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// An expression compiles to a flat program for a stack machine whose slots
// are rows: every instruction runs over a whole row of the band, so a row
// costs one pass per instruction and nothing is allocated per pixel. NaN is
// an invalid sample throughout (ADR-0400 §SD5): every instruction but where
// and isvalid gives NaN when an operand is NaN, and every arithmetic result
// that is not finite is made NaN, so 1/0 is invalid and isvalid(1/0) is 0.

type exprInstr struct {
	op exprOpE
	// slot is where the result goes; the operands are slot, slot+1, slot+2.
	slot int
	// input is the input a name reads.
	input int
	num   float64
}

// exprProgram is a compiled expression.
type exprProgram struct {
	code  []exprInstr
	slots int
}

// compileExpression compiles e over inputs of the given names, in order.
// A name that is no input is refused, naming it.
func compileExpression(e Expression, names []string) (prog exprProgram, err error) {
	var emit func(n *exprNode, slot int) error
	emit = func(n *exprNode, slot int) error {
		prog.slots = max(prog.slots, slot+1)
		switch n.op {
		case exprOpNumber:
			prog.code = append(prog.code, exprInstr{op: exprOpNumber, slot: slot, num: n.num})
			return nil
		case exprOpName:
			i := slices.Index(names, n.name)
			if i < 0 {
				return eb.Build().Str("name", n.name).Int("column", n.pos+1).Strs("inputs", slices.Clone(names)).
					Errorf("the expression reads a name that is no input: %w", ErrRefused)
			}
			prog.code = append(prog.code, exprInstr{op: exprOpName, slot: slot, input: i})
			return nil
		}
		for k, a := range n.args {
			if err := emit(a, slot+k); err != nil {
				return err
			}
		}
		prog.code = append(prog.code, exprInstr{op: n.op, slot: slot})
		return nil
	}
	if e.root == nil {
		return prog, eb.Build().Errorf("an empty expression: %w", ErrRefused)
	}
	err = emit(e.root, 0)
	return
}

var nan = math.NaN()

// finite is v, or NaN when v is not finite: v−v is 0 for every finite v and
// NaN for ±Inf and NaN.
func finite(v float64) float64 {
	if v-v != 0 {
		return nan
	}
	return v
}

func truth(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// exprMachine evaluates a program over rows; one per goroutine.
type exprMachine struct {
	prog  *exprProgram
	tmp   [][]float64
	slots [][]float64
}

func newExprMachine(prog *exprProgram, width int) *exprMachine {
	m := &exprMachine{prog: prog, tmp: make([][]float64, prog.slots), slots: make([][]float64, prog.slots)}
	for i := range m.tmp {
		m.tmp[i] = make([]float64, width)
	}
	return m
}

// run evaluates the program over one row; input(i) is input i's row. The
// result is the returned row, which belongs to the machine.
func (inst *exprMachine) run(input func(i int) []float64) []float64 {
	for _, in := range inst.prog.code {
		s := in.slot
		d := inst.tmp[s]
		switch in.op {
		case exprOpNumber:
			for x := range d {
				d[x] = in.num
			}
		case exprOpName:
			inst.slots[s] = input(in.input)
			continue
		case exprOpNeg:
			a := inst.slots[s]
			for x := range d {
				d[x] = finite(-a[x])
			}
		case exprOpNot:
			a := inst.slots[s]
			for x := range d {
				v := a[x]
				if v == v {
					v = truth(v == 0)
				}
				d[x] = v
			}
		case exprOpAbs:
			unary(d, inst.slots[s], math.Abs)
		case exprOpSqrt:
			unary(d, inst.slots[s], math.Sqrt)
		case exprOpExp:
			unary(d, inst.slots[s], math.Exp)
		case exprOpLog:
			unary(d, inst.slots[s], math.Log)
		case exprOpLog10:
			unary(d, inst.slots[s], math.Log10)
		case exprOpIsValid:
			a := inst.slots[s]
			for x := range d {
				d[x] = truth(a[x] == a[x])
			}
		case exprOpAdd:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = finite(a[x] + b[x])
			}
		case exprOpSub:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = finite(a[x] - b[x])
			}
		case exprOpMul:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = finite(a[x] * b[x])
			}
		case exprOpDiv:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = finite(a[x] / b[x])
			}
		case exprOpPow:
			// math.Pow gives 1 for pow(NaN, 0) and pow(1, NaN); an invalid
			// operand stays invalid here.
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				u, v := a[x], b[x]
				if u != u || v != v {
					d[x] = nan
					continue
				}
				d[x] = finite(math.Pow(u, v))
			}
		case exprOpMin:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = min(a[x], b[x])
			}
		case exprOpMax:
			a, b := inst.slots[s], inst.slots[s+1]
			for x := range d {
				d[x] = max(a[x], b[x])
			}
		case exprOpClamp:
			a, lo, hi := inst.slots[s], inst.slots[s+1], inst.slots[s+2]
			for x := range d {
				d[x] = min(max(a[x], lo[x]), hi[x])
			}
		case exprOpWhere:
			c, a, b := inst.slots[s], inst.slots[s+1], inst.slots[s+2]
			for x := range d {
				switch v := c[x]; {
				case v != v:
					d[x] = nan
				case v != 0:
					d[x] = a[x]
				default:
					d[x] = b[x]
				}
			}
		case exprOpLt, exprOpLe, exprOpGt, exprOpGe, exprOpEq, exprOpNe, exprOpAnd, exprOpOr:
			compare(in.op, d, inst.slots[s], inst.slots[s+1])
		}
		inst.slots[s] = d
	}
	return inst.slots[0]
}

func unary(d, a []float64, f func(float64) float64) {
	for x := range d {
		d[x] = finite(f(a[x]))
	}
}

func compare(op exprOpE, d, a, b []float64) {
	for x := range d {
		u, v := a[x], b[x]
		if u != u || v != v {
			d[x] = nan
			continue
		}
		var r bool
		switch op {
		case exprOpLt:
			r = u < v
		case exprOpLe:
			r = u <= v
		case exprOpGt:
			r = u > v
		case exprOpGe:
			r = u >= v
		case exprOpEq:
			r = u == v
		case exprOpNe:
			r = u != v
		case exprOpAnd:
			r = u != 0 && v != 0
		case exprOpOr:
			r = u != 0 || v != 0
		}
		d[x] = truth(r)
	}
}
