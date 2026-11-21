package ops

import (
	"slices"

	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/science/imaging"
)

// exprOperator is one output plane from an expression over named
// single-plane inputs (ADR-0400 §SD10).
type exprOperator struct{}

var exprDeclaration = Declaration{
	Id: "expr", Version: 1,
	Summary:   "One plane from an expression over named single-plane inputs: arithmetic, comparisons, a few functions and a conditional.",
	MinInputs: 1,
	InputsDoc: "Single-plane images of one size, each read by the expression under its name; every input is read.",
	Params: []Param{{
		Name: "expression", Type: ParamTypeExpression, Required: true,
		Doc: "The value of each output sample, in the expression grammar, over the inputs' names.",
	}},
}

func (exprOperator) Declaration() Declaration { return exprDeclaration }

func (exprOperator) Plan(inputs []InputDesc, params Params) (plan Plan, k KernelI, err error) {
	if err = requireSinglePlane(inputs); err != nil {
		return
	}
	e, err := ParseExpression(params.text("expression"))
	if err != nil {
		return plan, nil, eb.Build().Errorf("the expression does not parse: %w", err)
	}
	names := make([]string, len(inputs))
	for i, in := range inputs {
		names[i] = in.Name
	}
	read := e.Names()
	for _, n := range names {
		if !slices.Contains(read, n) {
			return plan, nil, eb.Build().Str("input", n).Str("expression", e.String()).
				Errorf("the expression does not read an input it is given: %w", ErrRefused)
		}
	}
	prog, err := compileExpression(e, names)
	if err != nil {
		return
	}
	plan = Plan{Model: imaging.ColorModelGray, Kind: valueKind(inputs), PlaneLabels: []string{e.String()}}
	return plan, &exprKernel{prog: prog}, nil
}

type exprKernel struct {
	NoFirstPass
	prog exprProgram
}

func (inst *exprKernel) Worker() WorkerI { return &exprWorker{prog: &inst.prog} }

type exprWorker struct {
	prog *exprProgram
	m    *exprMachine
}

func (inst *exprWorker) Row(b *Band, y int, out [][]float64) {
	if inst.m == nil {
		inst.m = newExprMachine(inst.prog, b.Width())
	}
	copy(out[0], inst.m.run(func(i int) []float64 { return b.Row(i, 0, y) }))
}

// requireSinglePlane refuses an input of more than one plane, naming it.
func requireSinglePlane(inputs []InputDesc) error {
	for _, in := range inputs {
		if in.PlaneCount() != 1 {
			return eb.Build().Str("input", in.Name).Stringer("model", in.Model).Int("planes", in.PlaneCount()).
				Errorf("the operator takes single-plane inputs: take one plane of this input first: %w", ErrRefused)
		}
	}
	return nil
}

// refuseAlpha refuses an input with an alpha plane, naming it: alpha says
// how much of a pixel is there, which is not a value to smooth or rank.
func refuseAlpha(inputs []InputDesc) error {
	for _, in := range inputs {
		if in.Model.HasAlpha() {
			return eb.Build().Str("input", in.Name).Stringer("model", in.Model).
				Errorf("the operator takes no image with an alpha plane: take its colour planes first: %w", ErrRefused)
		}
	}
	return nil
}
