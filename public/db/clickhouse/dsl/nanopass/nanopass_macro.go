package nanopass

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/observability/eh"
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
type MacroExpander struct {
	macros map[string]MacroFuncI // lowercase name → function
}

// NewMacroExpander creates a new MacroExpander.
func NewMacroExpander() (inst *MacroExpander) {
	inst = &MacroExpander{
		macros: make(map[string]MacroFuncI, 8),
	}
	return
}

// Register adds a macro function. Name matching is case-insensitive.
func (inst *MacroExpander) Register(name string, fn MacroFuncI) {
	inst.macros[strings.ToLower(name)] = fn
}

// Pass returns a nanopass Pass that expands all registered macros.
func (inst *MacroExpander) Pass() Pass {
	return func(sql string) (result string, err error) {
		pr, err := Parse(sql)
		if err != nil {
			err = eh.Errorf("MacroExpander: %w", err)
			return
		}
		rw := NewRewriter(pr)

		// Collect replacements first, then apply.
		// This avoids issues with nested macros where inner nodes
		// are invalidated by outer replacements.
		type replacement struct {
			node antlr.ParserRuleContext
			text string
		}
		var replacements []replacement

		WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			funcExpr, ok := ctx.(*grammar.ColumnExprFunctionContext)
			if !ok {
				return true
			}

			name := funcExpr.Identifier().GetText()
			fn, found := inst.macros[strings.ToLower(name)]
			if !found {
				return true
			}

			args, extractErr := ExtractLiteralArgs(funcExpr)
			if extractErr != nil {
				// Not all arguments are literals — skip this call
				return true
			}

			expanded, expandErr := fn(args)
			if expandErr != nil {
				err = eh.Errorf("MacroExpander: macro %s failed: %w", name, expandErr)
				return false
			}

			replacements = append(replacements, replacement{
				node: funcExpr,
				text: expanded,
			})

			// Don't descend into the function — we're replacing the whole thing
			return false
		})

		if err != nil {
			return
		}

		for _, r := range replacements {
			ReplaceNode(rw, r.node, r.text)
		}

		result = GetText(rw)
		return
	}
}

// ExtractLiteralArgs extracts all arguments from a ColumnExprFunctionContext.
// Returns an error if any argument is not a constant literal.
func ExtractLiteralArgs(funcExpr *grammar.ColumnExprFunctionContext) (args []LiteralArg, err error) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		// No arguments: func()
		return
	}

	argExprs := argList.(*grammar.ColumnArgListContext).AllColumnArgExpr()
	args = make([]LiteralArg, 0, len(argExprs))

	for _, argExpr := range argExprs {
		ae := argExpr.(*grammar.ColumnArgExprContext)
		colExpr := ae.ColumnExpr()
		if colExpr == nil {
			err = eh.Errorf("argument is not an expression")
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

func extractLiteralFromExpr(expr grammar.IColumnExprContext) (arg LiteralArg, err error) {
	switch c := expr.(type) {
	case *grammar.ColumnExprLiteralContext:
		literal := c.Literal()
		if literal == nil {
			err = eh.Errorf("empty literal")
			return
		}
		return classifyLiteral(literal.(*grammar.LiteralContext))
	case *grammar.ColumnExprNegateContext:
		// Handle negative numbers: -(literal)
		inner := c.ColumnExpr()
		innerArg, innerErr := extractLiteralFromExpr(inner)
		if innerErr != nil {
			err = innerErr
			return
		}
		if innerArg.Type == LiteralTypeInt || innerArg.Type == LiteralTypeFloat {
			arg = LiteralArg{
				Type:  innerArg.Type,
				Value: "-" + innerArg.Value,
			}
			return
		}
		err = eh.Errorf("cannot negate non-numeric literal")
		return
	case *grammar.ColumnExprParensContext:
		// Unwrap parentheses
		return extractLiteralFromExpr(c.ColumnExpr())
	default:
		err = eh.Errorf("argument is not a literal: %T", expr)
		return
	}
}

func classifyLiteral(lit *grammar.LiteralContext) (arg LiteralArg, err error) {
	if lit.NULL_SQL() != nil {
		arg = LiteralArg{Type: LiteralTypeNull, Value: "NULL"}
		return
	}
	if lit.STRING_LITERAL() != nil {
		arg = LiteralArg{Type: LiteralTypeString, Value: lit.STRING_LITERAL().GetText()}
		return
	}
	if lit.NumberLiteral() != nil {
		return classifyNumberLiteral(lit.NumberLiteral().(*grammar.NumberLiteralContext))
	}
	err = eh.Errorf("unrecognized literal type")
	return
}

func classifyNumberLiteral(num *grammar.NumberLiteralContext) (arg LiteralArg, err error) {
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
