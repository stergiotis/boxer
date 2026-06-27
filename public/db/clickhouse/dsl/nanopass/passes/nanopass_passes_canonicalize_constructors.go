package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
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
// construction and access syntax to the chosen canonical form, in both column
// expressions and SETTINGS values. Nested constructors require fixpoint
// convergence — declares NeedsFixedPoint. The direction selects a rule set; one
// walk applies it (vs. the prior four).
func CanonicalizeConstructors(form ConstructorFormE) nanopass.Pass {
	var rules []nodeRule
	switch form {
	case ConstructorFormLiteral:
		rules = []nodeRule{columnFuncToLiteralRule, settingFuncToLiteralRule, settingFuncEmptyToLiteralRule}
	case ConstructorFormFunction:
		rules = []nodeRule{
			tupleToFunctionRule, arrayToFunctionRule,
			tupleAccessToFunctionRule, arrayAccessToFunctionRule,
			settingArrayToFunctionRule, settingTupleToFunctionRule, settingEmptyArrayToFunctionRule,
		}
	}
	return nanopass.LiftBodyPass(
		"CanonicalizeConstructors",
		func(sql string) (string, error) {
			if rules == nil {
				return "", eb.Build().Int("form", int(form)).Errorf("unknown constructor form")
			}
			return rewriteNodes(sql, "CanonicalizeConstructors", rules...)
		},
		nanopass.PassProperties{
			NeedsFixedPoint: true,
			Reads:           nanopass.RegionBody,
			Writes:          nanopass.RegionBody,
		},
	)
}

// --- ToLiteral direction: function → syntax ---

// columnFuncToLiteralRule lowers tuple/array constructor and element-access
// function calls to their literal spellings.
func columnFuncToLiteralRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	fn, ok := node.(*grammar1.ColumnExprFunctionContext)
	if !ok {
		return "", false
	}
	switch strings.ToLower(fn.Identifier().GetText()) {
	case "tuple":
		// tuple() has no literal spelling ("()" does not parse) and tuple(x)
		// would collapse to scalar parens "(x)" — both stay in function form.
		if args := extractFunctionArgs(pr, fn); len(args) < 2 {
			return "", false
		}
		return "(" + nanopass.NodeText(pr, fn.ColumnArgList().(antlr.ParserRuleContext)) + ")", true
	case "array":
		argList := fn.ColumnArgList()
		if argList == nil {
			return "[]", true
		}
		return "[" + nanopass.NodeText(pr, argList.(antlr.ParserRuleContext)) + "]", true
	case "tupleelement":
		if args := extractFunctionArgs(pr, fn); len(args) == 2 {
			return args[0] + "." + args[1], true
		}
		return "", false
	case "arrayelement":
		if args := extractFunctionArgs(pr, fn); len(args) == 2 {
			return args[0] + "[" + args[1] + "]", true
		}
		return "", false
	}
	return "", false
}

func settingFuncToLiteralRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.SettingFunctionContext)
	if !ok {
		return "", false
	}
	values := strings.Join(settingValues(pr, c), ", ")
	switch settingFuncName(c) {
	case "array":
		return "[" + values + "]", true
	case "tuple":
		return "(" + values + ")", true
	}
	return "", false
}

func settingFuncEmptyToLiteralRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.SettingFunctionEmptyContext)
	if !ok {
		return "", false
	}
	if settingFuncName(c) == "array" {
		return "[]", true
	}
	// tuple() with 0 args is not valid as a literal — leave as-is.
	return "", false
}

// --- ToFunction direction: syntax → function ---

// rewriteTupleToFunction lowers a tuple syntactic form to its function call.
// IN-list arguments (`x IN (1, 2, 3)`) are rewritten as `array(...)` because
// ClickHouse accepts arrays on the RHS of IN, and the array shape lets
// downstream passes (e.g. ExtractLiterals) emit Array-typed parameters that
// match the conventional IN semantics.
func tupleToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprTupleContext)
	if !ok {
		return "", false
	}
	inner := columnExprListText(pr, c)
	if isINTupleArg(c) {
		return "array(" + inner + ")", true
	}
	return "tuple(" + inner + ")", true
}

func arrayToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprArrayContext)
	if !ok {
		return "", false
	}
	return "array(" + columnExprListText(pr, c) + ")", true
}

func tupleAccessToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	// ColumnExprTupleAccess: columnExpr . DECIMAL_LITERAL
	c, ok := node.(*grammar1.ColumnExprTupleAccessContext)
	if !ok || c.GetChildCount() < 3 {
		return "", false
	}
	exprCtx, ok := c.GetChild(0).(antlr.ParserRuleContext)
	if !ok {
		return "", false
	}
	idxTn, ok := c.GetChild(c.GetChildCount() - 1).(antlr.TerminalNode)
	if !ok {
		return "", false
	}
	return "tupleElement(" + nanopass.NodeText(pr, exprCtx) + ", " + idxTn.GetText() + ")", true
}

func arrayAccessToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	// ColumnExprArrayAccess: columnExpr [ columnExpr ]
	c, ok := node.(*grammar1.ColumnExprArrayAccessContext)
	if !ok || c.GetChildCount() < 4 {
		return "", false
	}
	arrCtx, ok := c.GetChild(0).(antlr.ParserRuleContext)
	if !ok {
		return "", false
	}
	idxCtx, ok := c.GetChild(2).(antlr.ParserRuleContext)
	if !ok {
		return "", false
	}
	return "arrayElement(" + nanopass.NodeText(pr, arrCtx) + ", " + nanopass.NodeText(pr, idxCtx) + ")", true
}

func settingArrayToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.SettingArrayContext)
	if !ok {
		return "", false
	}
	return "array(" + strings.Join(settingValues(pr, c), ", ") + ")", true
}

func settingTupleToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.SettingTupleContext)
	if !ok {
		return "", false
	}
	return "tuple(" + strings.Join(settingValues(pr, c), ", ") + ")", true
}

func settingEmptyArrayToFunctionRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	if _, ok := node.(*grammar1.SettingEmptyArrayContext); !ok {
		return "", false
	}
	return "array()", true
}

// --- Helpers ---

// isINTupleArg reports whether the tuple is the RIGHT operand of an `IN`
// (or `NOT IN` / `GLOBAL IN`) expression. The left operand of a tuple-IN
// ((a, b) IN (…)) is a row tuple and must stay a tuple — only the
// list-of-candidates side becomes array(…).
func isINTupleArg(ctx *grammar1.ColumnExprTupleContext) bool {
	parent, ok := ctx.GetParent().(*grammar1.ColumnExprPrecedence3Context)
	if !ok {
		return false
	}
	inIdx := -1
	for i := 0; i < parent.GetChildCount(); i++ {
		term, isTerm := parent.GetChild(i).(*antlr.TerminalNodeImpl)
		if !isTerm {
			continue
		}
		if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerIN {
			inIdx = term.GetSymbol().GetTokenIndex()
			break
		}
	}
	if inIdx < 0 {
		return false
	}
	start := ctx.GetStart()
	return start != nil && start.GetTokenIndex() > inIdx
}

// columnExprListText returns the source text of node's columnExprList child, or
// "" if it has none (an empty [] / () constructor).
func columnExprListText(pr *nanopass.ParseResult, node antlr.ParserRuleContext) string {
	for i := 0; i < node.GetChildCount(); i++ {
		if list, ok := node.GetChild(i).(*grammar1.ColumnExprListContext); ok {
			return nanopass.NodeText(pr, list)
		}
	}
	return ""
}

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

// settingFuncName returns the lowercased name of a setting function node
// (its first IdentifierContext child), or "".
func settingFuncName(node antlr.ParserRuleContext) string {
	for i := 0; i < node.GetChildCount(); i++ {
		if ident, ok := node.GetChild(i).(*grammar1.IdentifierContext); ok {
			return strings.ToLower(ident.GetText())
		}
	}
	return ""
}

// settingValues returns the source text of each settingValue child of node, in
// order (skipping the function name, parens/brackets, and commas).
func settingValues(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (values []string) {
	for i := 0; i < node.GetChildCount(); i++ {
		if sv, ok := node.GetChild(i).(antlr.ParserRuleContext); ok && isSettingValueNode(sv) {
			values = append(values, nanopass.NodeText(pr, sv))
		}
	}
	return
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
