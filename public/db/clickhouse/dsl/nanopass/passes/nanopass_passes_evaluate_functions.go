//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/slices"
)

// EvalFuncI is the signature for a Go-side function evaluator.
// It receives deserialized arguments and returns a Go value that will be
// serialized back to SQL.
// Supported return types: int64, int, float64, string, bool, nil, []any, *Tuple, TypedLiteral.
type EvalFuncI func(args []any) (any, error)

// FunctionEvaluator holds a registry of Go-evaluable functions and provides a Pass.
// Functions are expanded recursively: if a function argument is itself an evaluable
// function call with literal arguments, it is evaluated first.
//
// Partial evaluation: if a registered function has a mix of evaluable and non-evaluable
// arguments, the evaluable inner calls are still replaced with their results while the
// outer call is left untouched. For example:
//
//	myAdd(a, myMul(2, 3)) → myAdd(a, 6)
//
// For cases where partial evaluation makes a previously non-evaluable outer call
// fully evaluable, use FixedPoint(eval.Pass(), maxIterations).
type FunctionEvaluator struct {
	funcs map[string]struct {
		f      EvalFuncI
		useAny bool
	}
}

// NewFunctionEvaluator creates a new FunctionEvaluator.
func NewFunctionEvaluator() (inst *FunctionEvaluator) {
	inst = &FunctionEvaluator{
		funcs: make(map[string]struct {
			f      EvalFuncI
			useAny bool
		}, 16),
	}
	return
}

// Register adds a function evaluator. Name matching is case-insensitive.
func (inst *FunctionEvaluator) Register(name string, fn EvalFuncI, useAny bool) {
	inst.funcs[strings.ToLower(name)] = struct {
		f      EvalFuncI
		useAny bool
	}{f: fn, useAny: useAny}
}

func checkHomogenous[T any](args []any) bool {
	for _, arg := range args {
		_, ok := arg.(T)
		if !ok {
			return false
		}
	}
	return true
}

// RegisterBuiltins registers the built-in array() and tuple() constructors
// so that literal array/tuple construction can participate in evaluation.
func (inst *FunctionEvaluator) RegisterBuiltins() {
	inst.Register("array", func(args []any) (any, error) {
		if checkHomogenous[string](args) {
			result := make([]string, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[uint64](args) {
			result := make([]uint64, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[uint32](args) {
			result := make([]uint32, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[uint16](args) {
			result := make([]uint16, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[uint8](args) {
			result := make([]uint8, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[int64](args) {
			result := make([]int64, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[int32](args) {
			result := make([]int32, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[int16](args) {
			result := make([]int16, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[int8](args) {
			result := make([]int8, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[float64](args) {
			result := make([]float64, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[float32](args) {
			result := make([]float32, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		if checkHomogenous[bool](args) {
			result := make([]bool, 0, len(args))
			return slices.CopySliceInterfaceCastable(args, result), nil
		}
		result := make([]any, len(args))
		copy(result, args)
		return result, nil
	}, true)
	inst.Register("tuple", func(args []any) (any, error) {
		return marshalling.NewUnnamedTuple(args...), nil
	}, true)
}

// Pass returns a nanopass Pass that evaluates all registered functions
// with fully-literal arguments and partially evaluates inner calls
// when the outer call has non-literal arguments.
func (inst *FunctionEvaluator) Pass() nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("FunctionEvaluator: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		inst.walkAndEval(pr, rw, pr.Tree)

		result = nanopass.GetText(rw)
		return
	}
}

// walkAndEval walks the CST top-down. For each registered function call:
//  1. Try full recursive evaluation — if all args evaluate to literals, replace the entire call.
//  2. If not fully evaluable — descend into children to partially evaluate inner calls.
func (inst *FunctionEvaluator) walkAndEval(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, node antlr.Tree) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}

	if funcExpr, ok := ctx.(*grammar1.ColumnExprFunctionContext); ok {
		name := strings.ToLower(funcExpr.Identifier().GetText())
		if _, found := inst.funcs[name]; found {
			// Try full recursive evaluation
			val, evaluated, _ := inst.tryEval(pr, funcExpr)
			if evaluated {
				serialized, serErr := marshalling.MarshalGoValueToSQL(val)
				if serErr == nil {
					nanopass.ReplaceNode(rw, funcExpr, serialized)
					return // entire subtree replaced — don't descend
				}
			}
			// Not fully evaluable — fall through to descend into children
		}
	}

	// Descend into children for partial evaluation
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if childTree, ok := child.(antlr.Tree); ok {
			inst.walkAndEval(pr, rw, childTree)
		}
	}
}

// TryEval attempts to evaluate a function call recursively.
// Returns (value, true, nil) on success.
// Returns (nil, false, nil) if not all arguments are evaluable.
// Returns (nil, false, error) on evaluation failure.
func (inst *FunctionEvaluator) TryEval(pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (val any, evaluated bool, err error) {
	return inst.tryEval(pr, funcExpr)
}

func (inst *FunctionEvaluator) tryEval(pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (val any, evaluated bool, err error) {
	name := strings.ToLower(funcExpr.Identifier().GetText())
	fn, found := inst.funcs[name]
	if !found {
		return
	}

	args, ok := inst.extractEvalArgs(pr, funcExpr, fn.useAny)
	if !ok {
		return
	}

	val, err = fn.f(args)
	if err != nil {
		err = eb.Build().Str("function", name).Errorf("evaluation failed: %w", err)
		return
	}
	evaluated = true
	return
}

// extractEvalArgs extracts arguments, recursively evaluating nested function calls.
// Returns (args, true) if all args are evaluable, (nil, false) otherwise.
func (inst *FunctionEvaluator) extractEvalArgs(pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext, useAny bool) (args []any, ok bool) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		args = make([]any, 0)
		ok = true
		return
	}

	argListCtx := argList.(*grammar1.ColumnArgListContext)
	args = make([]any, 0, argListCtx.GetChildCount())

	for i := 0; i < argListCtx.GetChildCount(); i++ {
		child := argListCtx.GetChild(i)
		argExpr, isArg := child.(*grammar1.ColumnArgExprContext)
		if !isArg {
			continue // skip commas
		}

		val, evalOk := inst.evalArgExpr(pr, argExpr)
		if !evalOk {
			return nil, false
		}
		if useAny {
			switch t := val.(type) {
			case marshalling.TypedLiteral:
				a, err := t.ToAny()
				if err != nil {
					return nil, false
				}
				args = append(args, a)
			default:
				args = append(args, val)
			}
		} else {
			args = append(args, val)
		}
	}

	ok = true
	return
}

// evalArgExpr evaluates a single argument expression.
func (inst *FunctionEvaluator) evalArgExpr(pr *nanopass.ParseResult, argExpr *grammar1.ColumnArgExprContext) (val any, ok bool) {
	if argExpr.GetChildCount() == 0 {
		return nil, false
	}
	colExpr := argExpr.GetChild(0)
	return inst.evalColumnExpr(pr, colExpr)
}

// evalColumnExpr evaluates a column expression recursively.
func (inst *FunctionEvaluator) evalColumnExpr(pr *nanopass.ParseResult, node antlr.Tree) (val any, ok bool) {
	ctx, isCtx := node.(antlr.ParserRuleContext)
	if !isCtx {
		return nil, false
	}

	switch c := ctx.(type) {
	case *grammar1.ColumnExprLiteralContext:
		return inst.evalLiteral(c)

	case *grammar1.ColumnExprFunctionContext:
		result, evaluated, evalErr := inst.tryEval(pr, c)
		if evalErr != nil || !evaluated {
			return nil, false
		}
		return result, true

	case *grammar1.ColumnExprNegateContext:
		if c.GetChildCount() < 2 {
			return nil, false
		}
		inner := c.GetChild(1)
		innerVal, innerOk := inst.evalColumnExpr(pr, inner)
		if !innerOk {
			return nil, false
		}
		return negateValue(innerVal)

	case *grammar1.ColumnExprParensContext:
		for i := 0; i < c.GetChildCount(); i++ {
			child := c.GetChild(i)
			if prc, isPrc := child.(antlr.ParserRuleContext); isPrc {
				if prc.GetRuleIndex() == grammar1.ClickHouseParserGrammar1RULE_columnExpr {
					return inst.evalColumnExpr(pr, prc)
				}
			}
		}
		return nil, false

	default:
		return nil, false
	}
}

func (inst *FunctionEvaluator) evalLiteral(ctx *grammar1.ColumnExprLiteralContext) (val any, ok bool) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if lit, isLit := ctx.GetChild(i).(*grammar1.LiteralContext); isLit {
			v, err := DeserializeLiteralContext(lit)
			if err != nil {
				return nil, false
			}
			return v, true
		}
	}
	return nil, false
}

func negateValue(val any) (result any, ok bool) {
	switch v := val.(type) {
	case int64:
		return -v, true
	case float64:
		return -v, true
	default:
		return nil, false
	}
}
