//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// ConstructorFormE controls the canonical direction.
type ConstructorFormE int8

const (
	// ConstructorFormLiteral canonicalizes to syntax form.
	//
	//   tuple(1,2) → (1,2)
	//   array(1,2) → [1,2]
	//   tupleElement(t,1) → t.1
	//   arrayElement(arr,1) → arr[1]
	ConstructorFormLiteral ConstructorFormE = 1

	// ConstructorFormFunction canonicalizes to function form.
	//
	//   (1,2) → tuple(1,2)
	//   [1,2] → array(1,2)
	//   t.1 → tupleElement(t,1)
	//   arr[1] → arrayElement(arr,1)
	ConstructorFormFunction ConstructorFormE = 2
)

// CanonicalizeConstructors returns a Pass that normalizes tuple and array
// construction and access syntax to the chosen canonical form.
func CanonicalizeConstructors(form ConstructorFormE) nanopass.Pass {
	return func(sql string) (result string, err error) {
		pr, err := nanopass.Parse(sql)
		if err != nil {
			err = eh.Errorf("CanonicalizeConstructors: %w", err)
			return
		}
		rw := nanopass.NewRewriter(pr)

		switch form {
		case ConstructorFormLiteral:
			canonicalizeToLiteral(pr, rw)
			canonicalizeSettingsToLiteral(pr, rw)
		case ConstructorFormFunction:
			canonicalizeToFunction(pr, rw)
			canonicalizeSettingsToFunction(pr, rw)
		default:
			err = eb.Build().Int("form", int(form)).Errorf("unknown constructor form")
			return
		}

		result = nanopass.GetText(rw)
		return
	}
}

// --- ToLiteral direction: function → syntax ---

func canonicalizeToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		funcExpr, ok := ctx.(*grammar1.ColumnExprFunctionContext)
		if !ok {
			return true
		}

		name := strings.ToLower(funcExpr.Identifier().GetText())
		switch name {
		case "tuple":
			rewriteTupleToLiteral(pr, rw, funcExpr)
			return false
		case "array":
			rewriteArrayToLiteral(pr, rw, funcExpr)
			return false
		case "tupleelement":
			rewriteTupleElementToAccess(pr, rw, funcExpr)
			return false
		case "arrayelement":
			rewriteArrayElementToAccess(pr, rw, funcExpr)
			return false
		}
		return true
	})
}

func rewriteTupleToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, funcExpr *grammar1.ColumnExprFunctionContext) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		nanopass.ReplaceNode(rw, funcExpr, "()")
		return
	}
	argsText := nanopass.NodeText(pr, argList.(antlr.ParserRuleContext))
	nanopass.ReplaceNode(rw, funcExpr, "("+argsText+")")
}

func rewriteArrayToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, funcExpr *grammar1.ColumnExprFunctionContext) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		nanopass.ReplaceNode(rw, funcExpr, "[]")
		return
	}
	argsText := nanopass.NodeText(pr, argList.(antlr.ParserRuleContext))
	nanopass.ReplaceNode(rw, funcExpr, "["+argsText+"]")
}

func rewriteTupleElementToAccess(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, funcExpr *grammar1.ColumnExprFunctionContext) {
	args := extractFunctionArgs(pr, funcExpr)
	if len(args) != 2 {
		return // not a simple tupleElement(expr, index)
	}
	nanopass.ReplaceNode(rw, funcExpr, args[0]+"."+args[1])
}

func rewriteArrayElementToAccess(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, funcExpr *grammar1.ColumnExprFunctionContext) {
	args := extractFunctionArgs(pr, funcExpr)
	if len(args) != 2 {
		return // not a simple arrayElement(expr, index)
	}
	nanopass.ReplaceNode(rw, funcExpr, args[0]+"["+args[1]+"]")
}

// --- ToFunction direction: syntax → function ---

func canonicalizeToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.ColumnExprTupleContext:
			rewriteTupleToFunction(pr, rw, c)
			return false
		case *grammar1.ColumnExprArrayContext:
			rewriteArrayToFunction(pr, rw, c)
			return false
		case *grammar1.ColumnExprTupleAccessContext:
			rewriteTupleAccessToFunction(pr, rw, c)
			return false
		case *grammar1.ColumnExprArrayAccessContext:
			rewriteArrayAccessToFunction(pr, rw, c)
			return false
		}
		return true
	})
}

func rewriteTupleToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.ColumnExprTupleContext) {
	// ColumnExprTuple: ( ColumnExprList )
	// Extract the inner expression list text
	var innerText string
	for i := 0; i < ctx.GetChildCount(); i++ {
		if list, ok := ctx.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			innerText = nanopass.NodeText(pr, list)
			break
		}
	}
	nanopass.ReplaceNode(rw, ctx, "tuple("+innerText+")")
}

func rewriteArrayToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.ColumnExprArrayContext) {
	// ColumnExprArray: [ ColumnExprList ]
	var innerText string
	for i := 0; i < ctx.GetChildCount(); i++ {
		if list, ok := ctx.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			innerText = nanopass.NodeText(pr, list)
			break
		}
	}
	nanopass.ReplaceNode(rw, ctx, "array("+innerText+")")
}

func rewriteTupleAccessToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.ColumnExprTupleAccessContext) {
	// ColumnExprTupleAccess: columnExpr . DECIMAL_LITERAL
	// Children: ColumnExprIdentifier, ".", "1"
	if ctx.GetChildCount() < 3 {
		return
	}

	// First child is the expression
	exprChild := ctx.GetChild(0)
	exprCtx, ok := exprChild.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	exprText := nanopass.NodeText(pr, exprCtx)

	// Last child is the index (DECIMAL_LITERAL terminal)
	indexChild := ctx.GetChild(ctx.GetChildCount() - 1)
	indexTn, ok := indexChild.(antlr.TerminalNode)
	if !ok {
		return
	}
	indexText := indexTn.GetText()

	nanopass.ReplaceNode(rw, ctx, "tupleElement("+exprText+", "+indexText+")")
}

func rewriteArrayAccessToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.ColumnExprArrayAccessContext) {
	// ColumnExprArrayAccess: columnExpr [ columnExpr ]
	// Children: ColumnExprIdentifier, "[", ColumnExprLiteral, "]"
	if ctx.GetChildCount() < 4 {
		return
	}

	// First child is the array expression
	arrChild := ctx.GetChild(0)
	arrCtx, ok := arrChild.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	arrText := nanopass.NodeText(pr, arrCtx)

	// Third child is the index expression (between [ and ])
	idxChild := ctx.GetChild(2)
	idxCtx, ok := idxChild.(antlr.ParserRuleContext)
	if !ok {
		return
	}
	idxText := nanopass.NodeText(pr, idxCtx)

	nanopass.ReplaceNode(rw, ctx, "arrayElement("+arrText+", "+idxText+")")
}

// --- Helpers ---

// extractFunctionArgs extracts the text of each argument from a ColumnExprFunctionContext.
func extractFunctionArgs(pr *nanopass.ParseResult, funcExpr *grammar1.ColumnExprFunctionContext) (args []string) {
	argList := funcExpr.ColumnArgList()
	if argList == nil {
		return
	}
	argListCtx := argList.(*grammar1.ColumnArgListContext)

	for i := 0; i < argListCtx.GetChildCount(); i++ {
		child := argListCtx.GetChild(i)
		argExpr, ok := child.(*grammar1.ColumnArgExprContext)
		if !ok {
			continue // skip commas
		}
		args = append(args, nanopass.NodeText(pr, argExpr))
	}
	return
}

// --- Settings canonicalization ---

func canonicalizeSettingsToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.SettingFunctionContext:
			rewriteSettingFunctionToLiteral(pr, rw, c)
			return false
		case *grammar1.SettingFunctionEmptyContext:
			rewriteSettingFunctionEmptyToLiteral(pr, rw, c)
			return false
		}
		return true
	})
}

func canonicalizeSettingsToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter) {
	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.SettingArrayContext:
			rewriteSettingArrayToFunction(pr, rw, c)
			return false
		case *grammar1.SettingTupleContext:
			rewriteSettingTupleToFunction(pr, rw, c)
			return false
		case *grammar1.SettingEmptyArrayContext:
			nanopass.ReplaceNode(rw, c, "array()")
			return false
		}
		return true
	})
}

func rewriteSettingFunctionToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.SettingFunctionContext) {
	// Get the function name from the first IdentifierContext child
	funcName := ""
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			funcName = strings.ToLower(ident.GetText())
			break
		}
	}

	// Collect setting value texts (skip identifier, parens, commas)
	var values []string
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if sv, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(sv) {
				values = append(values, nanopass.NodeText(pr, sv))
			}
		}
	}

	valuesText := strings.Join(values, ", ")

	switch funcName {
	case "array":
		nanopass.ReplaceNode(rw, ctx, "["+valuesText+"]")
	case "tuple":
		nanopass.ReplaceNode(rw, ctx, "("+valuesText+")")
	}
}

func rewriteSettingFunctionEmptyToLiteral(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.SettingFunctionEmptyContext) {
	funcName := ""
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext); ok {
			funcName = strings.ToLower(ident.GetText())
			break
		}
	}

	switch funcName {
	case "array":
		nanopass.ReplaceNode(rw, ctx, "[]")
	}
	// tuple() with 0 args is not valid as a literal — leave as-is
}

func rewriteSettingArrayToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.SettingArrayContext) {
	// Collect inner setting values (skip brackets and commas)
	var values []string
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if sv, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(sv) {
				values = append(values, nanopass.NodeText(pr, sv))
			}
		}
	}
	valuesText := strings.Join(values, ", ")
	nanopass.ReplaceNode(rw, ctx, "array("+valuesText+")")
}

func rewriteSettingTupleToFunction(pr *nanopass.ParseResult, rw *antlr.TokenStreamRewriter, ctx *grammar1.SettingTupleContext) {
	var values []string
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if sv, ok := child.(antlr.ParserRuleContext); ok {
			if isSettingValueNode(sv) {
				values = append(values, nanopass.NodeText(pr, sv))
			}
		}
	}
	valuesText := strings.Join(values, ", ")
	nanopass.ReplaceNode(rw, ctx, "tuple("+valuesText+")")
}

// isSettingValueNode returns true if the node is a settingValue alternative.
func isSettingValueNode(ctx antlr.ParserRuleContext) bool {
	switch ctx.(type) {
	case *grammar1.SettingLiteralContext,
		*grammar1.SettingArrayContext,
		*grammar1.SettingTupleContext,
		*grammar1.SettingEmptyArrayContext,
		*grammar1.SettingFunctionContext,
		*grammar1.SettingFunctionEmptyContext:
		return true
	}
	return false
}
