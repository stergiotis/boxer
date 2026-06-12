//go:build llm_generated_opus47

package nanopass

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// LiteralTypeE represents the type of a literal argument.
type LiteralTypeE int8

const (
	LiteralTypeUnknown LiteralTypeE = 0
	LiteralTypeString  LiteralTypeE = 1
	LiteralTypeInt     LiteralTypeE = 2
	LiteralTypeFloat   LiteralTypeE = 3
	LiteralTypeBool    LiteralTypeE = 4
	LiteralTypeNull    LiteralTypeE = 5
)

// MacroFuncI is the function signature for macro expansion.
// It receives the literal arguments and returns the SQL fragment to substitute.
type MacroFuncI func(args []LiteralArg) (string, error)

// LiteralArg represents a constant/literal argument to a macro function.
type LiteralArg struct {
	Type  LiteralTypeE
	Value string // raw text including quotes for strings
}

// MacroExpander holds a registry of macro functions and provides a Pass.
//
// Not safe for concurrent use: Register must not be called while a Pass
// returned by [MacroExpander.Pass] is running.
type MacroExpander struct {
	macros map[string]MacroFuncI // normalised name → function
}

// NewMacroExpander creates a new MacroExpander.
func NewMacroExpander() (inst *MacroExpander) {
	inst = &MacroExpander{
		macros: make(map[string]MacroFuncI, 8),
	}
	return
}

// NormalizeCallName maps a function-call identifier (possibly quoted or
// backquoted) to a registry key. Matching is case-insensitive — note this
// deviates from ClickHouse, where most function names are case-sensitive;
// registered names should not collide with real functions in any case
// variant. Shared by MacroExpander and passes.FunctionEvaluator so both
// registries match the same spellings.
func NormalizeCallName(name string) string {
	return strings.ToLower(DecodeIdentifier(name))
}

// Register adds a macro function. Name matching is case-insensitive and
// quoting-insensitive: `myMacro(…)`, `MYMACRO(…)` and `"myMacro"(…)` all
// match a registration under "myMacro".
func (inst *MacroExpander) Register(name string, fn MacroFuncI) {
	inst.macros[NormalizeCallName(name)] = fn
}

// Pass returns a nanopass Pass that expands all registered macros.
//
// NeedsFixedPoint: a macro nested inside another macro's argument list only
// becomes expandable after the inner call has been replaced by its literal
// expansion, so a single Apply is not sufficient for nested invocations.
//
// A registered macro invoked with arguments that never become literals
// (column references, lambdas, …) is an error: macros are not real
// ClickHouse functions, so leaving the call in the output would fail at
// query time, far from the cause. The error is raised at the iteration
// where no further expansion progress is possible.
func (inst *MacroExpander) Pass() Pass {
	return Pass{
		Name:  "MacroExpander",
		Apply: inst.apply,
		Properties: PassProperties{
			NeedsFixedPoint: true,
			Reads:           RegionBody,
			Writes:          RegionBody,
		},
	}
}

// apply is the body-only macro expansion implementation.
func (inst *MacroExpander) apply(_ *env.Environment, body string) (result string, err error) {
	pr, err := Parse(body)
	if err != nil {
		err = eh.Errorf("MacroExpander: %w", err)
		return
	}
	rw := NewRewriter(pr)

	// Collect replacements first, then apply. Matched macro subtrees are not
	// descended into, so replacement ranges never overlap. A registered call
	// whose arguments are not (yet) literals is recorded as skipped and its
	// subtree IS descended — an inner macro expansion may make it literal on
	// the next fixpoint iteration.
	type replacement struct {
		node antlr.ParserRuleContext
		text string
	}
	type skipped struct {
		name   string
		reason error
	}
	var replacements []replacement
	var skips []skipped

	WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		if err != nil {
			return false
		}
		funcExpr, ok := ctx.(*grammar1.ColumnExprFunctionContext)
		if !ok {
			return true
		}
		ident := funcExpr.Identifier()
		if ident == nil {
			return true
		}

		name := NormalizeCallName(ident.GetText())
		fn, found := inst.macros[name]
		if !found {
			return true
		}

		args, extractErr := ExtractLiteralArgs(funcExpr)
		if extractErr != nil {
			skips = append(skips, skipped{name: name, reason: extractErr})
			return true
		}

		expanded, expandErr := fn(args)
		if expandErr != nil {
			err = eb.Build().Str("macro", name).Errorf("macro expansion failed: %w", expandErr)
			return false
		}

		replacements = append(replacements, replacement{
			node: funcExpr,
			text: expanded,
		})

		return false
	})

	if err != nil {
		return
	}

	// No expansion progress is possible and registered macros remain with
	// non-literal arguments: fail now instead of shipping a call ClickHouse
	// cannot resolve.
	if len(replacements) == 0 && len(skips) > 0 {
		first := skips[0]
		err = eb.Build().
			Str("macro", first.name).
			Int("skippedCalls", len(skips)).
			Errorf("registered macro invoked with non-literal arguments: %w", first.reason)
		return
	}

	for _, r := range replacements {
		ReplaceNode(rw, r.node, r.text)
	}

	result = GetText(rw)
	return
}

// ExtractLiteralArgs extracts all arguments from a ColumnExprFunctionContext.
// Returns an error if any argument is not a constant literal.
func ExtractLiteralArgs(funcExpr *grammar1.ColumnExprFunctionContext) (args []LiteralArg, err error) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		// No arguments: func()
		return
	}

	argListCtx, ok := argList.(*grammar1.ColumnArgListContext)
	if !ok {
		err = eb.Build().Type("argList", argList).Errorf("unexpected argument list context")
		return
	}
	argExprs := argListCtx.AllColumnArgExpr()
	args = make([]LiteralArg, 0, len(argExprs))

	for _, argExpr := range argExprs {
		ae, aeOk := argExpr.(*grammar1.ColumnArgExprContext)
		if !aeOk {
			err = eb.Build().Type("argExpr", argExpr).Errorf("unexpected argument context")
			return
		}
		colExpr := ae.ColumnExpr()
		if colExpr == nil {
			// columnArgExpr: columnLambdaExpr | columnExpr — nil means lambda.
			err = eh.Errorf("argument is a lambda, not a literal")
			return
		}
		arg, literalErr := extractLiteralFromExpr(colExpr)
		if literalErr != nil {
			err = literalErr
			return
		}
		args = append(args, arg)
	}
	return
}

func extractLiteralFromExpr(expr grammar1.IColumnExprContext) (arg LiteralArg, err error) {
	switch c := expr.(type) {
	case *grammar1.ColumnExprLiteralContext:
		literal := c.Literal()
		if literal == nil {
			err = eh.Errorf("empty literal")
			return
		}
		litCtx, ok := literal.(*grammar1.LiteralContext)
		if !ok {
			err = eb.Build().Type("literal", literal).Errorf("unexpected literal context")
			return
		}
		return classifyLiteral(litCtx)
	case *grammar1.ColumnExprNegateContext:
		// Negation: -(expr). The inner value may itself carry a sign (the
		// numberLiteral rule owns an optional sign, and nesting like -(-5)
		// is legal) — toggle instead of blindly prepending, or "--5" would
		// open a SQL line comment when spliced back.
		inner := c.ColumnExpr()
		innerArg, innerErr := extractLiteralFromExpr(inner)
		if innerErr != nil {
			err = innerErr
			return
		}
		if innerArg.Type != LiteralTypeInt && innerArg.Type != LiteralTypeFloat {
			err = eh.Errorf("cannot negate non-numeric literal")
			return
		}
		arg = LiteralArg{Type: innerArg.Type, Value: negateNumericText(innerArg.Value)}
		return
	case *grammar1.ColumnExprParensContext:
		// Unwrap parentheses
		return extractLiteralFromExpr(c.ColumnExpr())
	case *grammar1.ColumnExprIdentifierContext:
		// Boolean keywords (true/false) reach expression position as
		// identifiers, not via the literal rule.
		if tok := c.GetStart(); tok != nil {
			tt := tok.GetTokenType()
			if tt == grammar1.ClickHouseLexerJSON_TRUE || tt == grammar1.ClickHouseLexerJSON_FALSE {
				arg = LiteralArg{Type: LiteralTypeBool, Value: tok.GetText()}
				return
			}
		}
		err = eb.Build().Type("exprType", expr).Errorf("argument is not a literal")
		return
	default:
		err = eb.Build().Type("exprType", expr).Errorf("argument is not a literal")
		return
	}
}

// negateNumericText toggles the sign of a numeric literal's text form.
func negateNumericText(s string) string {
	switch {
	case strings.HasPrefix(s, "-"):
		return s[1:]
	case strings.HasPrefix(s, "+"):
		return "-" + s[1:]
	default:
		return "-" + s
	}
}

func classifyLiteral(lit *grammar1.LiteralContext) (arg LiteralArg, err error) {
	if lit.NULL_SQL() != nil {
		arg = LiteralArg{Type: LiteralTypeNull, Value: "NULL"}
		return
	}
	if lit.STRING_LITERAL() != nil {
		arg = LiteralArg{Type: LiteralTypeString, Value: lit.STRING_LITERAL().GetText()}
		return
	}
	if lit.NumberLiteral() != nil {
		numCtx, ok := lit.NumberLiteral().(*grammar1.NumberLiteralContext)
		if !ok {
			err = eb.Build().Type("numberLiteral", lit.NumberLiteral()).Errorf("unexpected number literal context")
			return
		}
		return classifyNumberLiteral(numCtx)
	}
	err = eh.Errorf("unrecognized literal type")
	return
}

func classifyNumberLiteral(num *grammar1.NumberLiteralContext) (arg LiteralArg, err error) {
	// Build the sign prefix
	sign := ""
	if num.DASH() != nil {
		sign = "-"
	}

	if num.FloatingLiteral() != nil {
		arg = LiteralArg{Type: LiteralTypeFloat, Value: sign + num.FloatingLiteral().GetText()}
		return
	}
	if num.DECIMAL_LITERAL() != nil {
		arg = LiteralArg{Type: LiteralTypeInt, Value: sign + num.DECIMAL_LITERAL().GetText()}
		return
	}
	if num.HEXADECIMAL_LITERAL() != nil {
		arg = LiteralArg{Type: LiteralTypeInt, Value: sign + num.HEXADECIMAL_LITERAL().GetText()}
		return
	}
	if num.OCTAL_LITERAL() != nil {
		arg = LiteralArg{Type: LiteralTypeInt, Value: sign + num.OCTAL_LITERAL().GetText()}
		return
	}
	if num.INF() != nil {
		arg = LiteralArg{Type: LiteralTypeFloat, Value: sign + "inf"}
		return
	}
	if num.NAN_SQL() != nil {
		arg = LiteralArg{Type: LiteralTypeFloat, Value: sign + "nan"}
		return
	}
	err = eh.Errorf("unrecognized number literal type")
	return
}
