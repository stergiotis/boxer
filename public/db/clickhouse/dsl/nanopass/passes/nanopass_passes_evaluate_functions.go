//go:build llm_generated_opus47

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/slices"
)

// EvalFuncI is the signature for a Go-side function evaluator. It receives
// deserialised arguments and returns a Go value that will be serialised back
// to SQL. Supported return types: int64, int, float64, string, bool, nil,
// []any, *Tuple, TypedLiteral, VerbatimSql, ResolvedParamSlot.
//
// VerbatimSql is an escape hatch: contents are spliced into the output
// unmodified. A VerbatimSql value passed as an argument to an outer
// evaluable call is treated as opaque — the outer call will not be invoked,
// and its inner verbatim subtree is replaced via the partial-evaluation
// descent. UnresolvedParamSlot values follow the same opaque rule.
type EvalFuncI func(args []any) (any, error)

// FunctionEvaluator holds a registry of Go-evaluable functions and provides
// a Pass. Functions are expanded recursively: if a function argument is
// itself an evaluable function call with literal arguments, it is evaluated
// first.
//
// Param-slot resolution: when a `{name: Type}` slot appears as an argument
// the evaluator consults env.Params[name]. A resolved param contributes its
// Go-typed Value as if it were a literal; an unresolved param makes the
// outer call non-evaluable (the inner descent still folds independent
// literal-only sibling calls).
type FunctionEvaluator struct {
	funcs map[string]struct {
		f      EvalFuncI
		useAny bool
	}
	onObservation nanopass.ObservationFuncI
}

// OnObservation sets a callback fired for every visited call whose name is
// in the registry, regardless of whether the args were foldable
// (always-fire semantics — see nanopass.Observation). Passing nil clears
// it. Used by editor-side tooling to attach affordances (regex testers,
// time-range pickers, …) to detected call sites without altering the
// pass's rewrite behaviour.
//
// The callback runs synchronously inside the walk; keep it cheap (log,
// append, non-blocking channel send) — heavy work belongs on the consumer
// side after the pass returns.
func (inst *FunctionEvaluator) OnObservation(fn nanopass.ObservationFuncI) {
	inst.onObservation = fn
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

// Pass returns a nanopass Pass that evaluates all registered functions with
// fully-literal arguments and partially evaluates inner calls when the outer
// call has non-literal arguments. The pass declares NeedsFixedPoint —
// partial evaluation may make a previously non-evaluable outer call fully
// evaluable, requiring iteration to convergence.
func (inst *FunctionEvaluator) Pass() nanopass.Pass {
	return nanopass.Pass{
		Name:  "FunctionEvaluator",
		Apply: inst.apply,
		Properties: nanopass.PassProperties{
			NeedsFixedPoint: true,
			Reads:           nanopass.RegionBody | nanopass.RegionParams,
			Writes:          nanopass.RegionBody,
		},
	}
}

func (inst *FunctionEvaluator) apply(e *env.Environment, body string) (result string, err error) {
	pr, err := nanopass.Parse(body)
	if err != nil {
		err = eh.Errorf("FunctionEvaluator: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	inst.walkAndEval(e, pr, rw, pr.Tree)

	result = nanopass.GetText(rw)
	return
}

// walkAndEval walks the CST top-down. For each registered function call:
//  1. Try full recursive evaluation — if all args evaluate to literals,
//     replace the entire call.
//  2. If not fully evaluable — descend into children to partially evaluate
//     inner calls.
func (inst *FunctionEvaluator) walkAndEval(e *env.Environment, pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, node antlr.Tree) {
	ctx, ok := node.(antlr.ParserRuleContext)
	if !ok {
		return
	}

	if funcExpr, ok := ctx.(*grammar1.ColumnExprFunctionContext); ok {
		name := strings.ToLower(funcExpr.Identifier().GetText())
		name = strings.Trim(name, "\"`")
		if fn, found := inst.funcs[name]; found {
			val, evaluated, _ := inst.tryEval(e, pr, funcExpr)
			if inst.onObservation != nil {
				obs := nanopass.Observation{
					Name:      name,
					Evaluated: evaluated,
					Src:       nanopass.SourceRangeFromCtx(funcExpr),
				}
				if evaluated {
					// Re-extract args so the observer sees what the handler
					// saw. tryEval doesn't surface them; the cost is bounded
					// by the number of registered calls in the body.
					args, _ := inst.extractEvalArgs(e, pr, funcExpr, fn.useAny)
					obs.Args = args
				}
				inst.onObservation(obs)
			}
			if evaluated {
				serialized, serErr := marshalling.MarshalGoValueToSQL(val)
				if serErr == nil {
					nanopass.ReplaceNode(rw, funcExpr, serialized)
					return
				}
			}
		}
	}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if childTree, ok := child.(antlr.Tree); ok {
			inst.walkAndEval(e, pr, rw, childTree)
		}
	}
}

// TryEval attempts to evaluate a function call recursively against an
// environment. Returns (value, true, nil) on success; (nil, false, nil) if
// not all arguments are evaluable; (nil, false, error) on evaluation
// failure.
func (inst *FunctionEvaluator) TryEval(e *env.Environment, pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (val any, evaluated bool, err error) {
	return inst.tryEval(e, pr, funcExpr)
}

func (inst *FunctionEvaluator) tryEval(e *env.Environment, pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (val any, evaluated bool, err error) {
	name := strings.ToLower(funcExpr.Identifier().GetText())
	name = strings.Trim(name, "\"`")
	fn, found := inst.funcs[name]
	if !found {
		return
	}

	args, ok := inst.extractEvalArgs(e, pr, funcExpr, fn.useAny)
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

// extractEvalArgs extracts arguments, recursively evaluating nested function
// calls. Returns (args, true) if all args are evaluable, (nil, false)
// otherwise.
func (inst *FunctionEvaluator) extractEvalArgs(e *env.Environment, pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext, useAny bool) (args []any, ok bool) {
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
			continue
		}

		val, evalOk := inst.evalArgExpr(e, pr, argExpr)
		if !evalOk {
			return nil, false
		}
		// Verbatim SQL and unresolved param slots are opaque to outer
		// evaluators — refuse to feed them as arguments. The descent in
		// walkAndEval will still replace inner producers on its own.
		if _, isVerbatim := val.(marshalling.VerbatimSql); isVerbatim {
			return nil, false
		}
		if _, isUnres := val.(marshalling.UnresolvedParamSlot); isUnres {
			return nil, false
		}
		// Resolved param slots travel through like literals — unwrap to
		// the underlying Value before applying useAny conversion.
		if rps, isResolved := val.(marshalling.ResolvedParamSlot); isResolved {
			val = rps.Value
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
func (inst *FunctionEvaluator) evalArgExpr(e *env.Environment, pr *nanopass.ParseResult, argExpr *grammar1.ColumnArgExprContext) (val any, ok bool) {
	if argExpr.GetChildCount() == 0 {
		return nil, false
	}
	colExpr := argExpr.GetChild(0)
	return inst.evalColumnExpr(e, pr, colExpr)
}

// evalColumnExpr evaluates a column expression recursively.
func (inst *FunctionEvaluator) evalColumnExpr(e *env.Environment, pr *nanopass.ParseResult, node antlr.Tree) (val any, ok bool) {
	ctx, isCtx := node.(antlr.ParserRuleContext)
	if !isCtx {
		return nil, false
	}

	switch c := ctx.(type) {
	case *grammar1.ColumnExprLiteralContext:
		return inst.evalLiteral(c)

	case *grammar1.ColumnExprFunctionContext:
		result, evaluated, evalErr := inst.tryEval(e, pr, c)
		if evalErr != nil || !evaluated {
			return nil, false
		}
		return result, true

	case *grammar1.ColumnExprNegateContext:
		if c.GetChildCount() < 2 {
			return nil, false
		}
		inner := c.GetChild(1)
		innerVal, innerOk := inst.evalColumnExpr(e, pr, inner)
		if !innerOk {
			return nil, false
		}
		return negateValue(innerVal)

	case *grammar1.ColumnExprParensContext:
		for i := 0; i < c.GetChildCount(); i++ {
			child := c.GetChild(i)
			if prc, isPrc := child.(antlr.ParserRuleContext); isPrc {
				if prc.GetRuleIndex() == grammar1.ClickHouseParserGrammar1RULE_columnExpr {
					return inst.evalColumnExpr(e, pr, prc)
				}
			}
		}
		return nil, false

	case *grammar1.ColumnExprParamSlotContext:
		return resolveParamSlot(e, c)

	default:
		return nil, false
	}
}

// resolveParamSlot looks the slot up in env.Params. Returns ResolvedParamSlot
// when both type and value are known; UnresolvedParamSlot when only the type
// is known. A slot the env has no record of is treated as unresolved-with-
// blank-type rather than non-evaluable, so the caller's opaque-arg rule
// applies.
//
// Lazy hydration: when the env entry has Raw and Type populated but Value
// is nil (the typical post-Extract state), the Raw text is deserialised on
// demand and cached back into the env. This avoids forcing env.Extract to
// import marshalling (which would create a cycle through nanopass).
func resolveParamSlot(e *env.Environment, ctx *grammar1.ColumnExprParamSlotContext) (val any, ok bool) {
	name, typ := splitParamSlotText(ctx.GetText())
	if name == "" {
		return nil, false
	}
	var entry env.Param
	if e != nil {
		entry = e.Params[name]
	}
	if entry.Type == "" {
		entry.Type = typ
	}
	if entry.Value == nil && entry.Raw != "" {
		if hydrated, hydrateErr := hydrateParamRaw(entry.Raw); hydrateErr == nil {
			entry.Value = hydrated
			if e != nil {
				e.Params[name] = entry
			}
		}
	}
	if entry.IsResolved() && entry.Value != nil {
		return marshalling.ResolvedParamSlot{Name: name, Type: entry.Type, Value: entry.Value}, true
	}
	return marshalling.UnresolvedParamSlot{Name: name, Type: entry.Type}, true
}

// hydrateParamRaw deserialises a Param.Raw value into a Go-typed marshalling
// representation. Composite forms (`[…]`, `(…)`) go through
// UnmarshalCompositeLiteral; everything else through UnmarshalScalarLiteral.
func hydrateParamRaw(raw string) (val any, err error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		err = eh.Errorf("hydrateParamRaw: empty raw")
		return
	}
	first := trimmed[0]
	last := trimmed[len(trimmed)-1]
	if (first == '[' && last == ']') || (first == '(' && last == ')') {
		val, err = marshalling.UnmarshalCompositeLiteral(trimmed)
		if err != nil {
			err = eh.Errorf("hydrateParamRaw composite: %w", err)
		}
		return
	}
	val, err = marshalling.UnmarshalScalarLiteral(trimmed)
	if err != nil {
		err = eh.Errorf("hydrateParamRaw scalar: %w", err)
	}
	return
}

// splitParamSlotText takes the textual form of a param slot — e.g.
// `{a:UInt64}` — and returns name and type.
func splitParamSlotText(s string) (name, typ string) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "{") || !strings.HasSuffix(s, "}") {
		return
	}
	inner := s[1 : len(s)-1]
	colon := strings.IndexByte(inner, ':')
	if colon < 0 {
		return
	}
	name = strings.TrimSpace(inner[:colon])
	typ = strings.TrimSpace(inner[colon+1:])
	return
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
