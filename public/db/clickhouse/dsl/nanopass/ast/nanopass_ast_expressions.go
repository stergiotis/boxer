//go:build llm_generated_opus46

package ast

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// convertColumnExpr converts a single column expression CST node to an AST Expr.
// Returns an error for non-canonical CST nodes (e.g. expr::Type, [1,2], t.1).
func convertColumnExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	switch c := ctx.(type) {

	// --- Literal ---
	case *grammar.ColumnExprLiteralContext:
		expr = Expr{
			Kind:    KindLiteral,
			Literal: &LiteralData{SQL: nanopass.NodeText(pr, c)},
		}

	// --- Parameter slot: {name: Type} ---
	case *grammar.ColumnExprParamSlotContext:
		expr, err = convertParamSlot(pr, c)

	// --- Column identifier: [db.][table.]column[.nested] ---
	case *grammar.ColumnExprIdentifierContext:
		expr, err = convertColumnIdentifier(pr, c)

	// --- Function call (includes all normalized sugar) ---
	case *grammar.ColumnExprFunctionContext:
		expr, err = convertFunctionCall(pr, c)

	// --- Window function: f(args) OVER (windowExpr) ---
	case *grammar.ColumnExprWinFunctionContext:
		expr, err = convertWindowFunction(pr, c, false)

	// --- Window function with named ref: f(args) OVER name ---
	case *grammar.ColumnExprWinFunctionTargetContext:
		expr, err = convertWindowFunction(pr, c, true)

	// --- Binary precedence operators ---
	case *grammar.ColumnExprPrecedence1Context:
		expr, err = convertBinaryExpr(pr, c)
	case *grammar.ColumnExprPrecedence2Context:
		expr, err = convertBinaryExpr(pr, c)
	case *grammar.ColumnExprPrecedence3Context:
		expr, err = convertPrecedence3Expr(pr, c)

	// --- AND / OR ---
	case *grammar.ColumnExprAndContext:
		expr, err = convertLogicalBinaryExpr(pr, c, "AND")
	case *grammar.ColumnExprOrContext:
		expr, err = convertLogicalBinaryExpr(pr, c, "OR")

	// --- NOT ---
	case *grammar.ColumnExprNotContext:
		expr, err = convertUnaryExpr(pr, c, "NOT")

	// --- Negate: -expr ---
	case *grammar.ColumnExprNegateContext:
		expr, err = convertUnaryExpr(pr, c, "-")

	// --- IS [NOT] NULL ---
	case *grammar.ColumnExprIsNullContext:
		expr, err = convertIsNull(pr, c)

	// --- BETWEEN ---
	case *grammar.ColumnExprBetweenContext:
		expr, err = convertBetween(pr, c)

	// --- Ternary: cond ? then : else ---
	case *grammar.ColumnExprTernaryOpContext:
		expr, err = convertTernary(pr, c)

	// --- CASE ---
	case *grammar.ColumnExprCaseContext:
		expr, err = convertCase(pr, c)

	// --- INTERVAL ---
	case *grammar.ColumnExprIntervalContext:
		expr, err = convertInterval(pr, c)

	// --- Alias: expr AS name ---
	case *grammar.ColumnExprAliasContext:
		expr, err = convertAlias(pr, c)

	// --- Subquery: (SELECT ...) ---
	case *grammar.ColumnExprSubqueryContext:
		expr, err = convertSubqueryExpr(pr, c)

	// --- Parenthesized: (expr) --- unwrap
	case *grammar.ColumnExprParensContext:
		for i := 0; i < c.GetChildCount(); i++ {
			if inner, ok := c.GetChild(i).(grammar.IColumnExprContext); ok {
				return convertColumnExpr(pr, inner.(antlr.ParserRuleContext))
			}
		}
		err = eh.Errorf("convertColumnExpr: empty ColumnExprParens")

	// --- Asterisk: [table.]* ---
	case *grammar.ColumnExprAsteriskContext:
		table := ""
		for i := 0; i < c.GetChildCount(); i++ {
			if ti, ok := c.GetChild(i).(*grammar.TableIdentifierContext); ok {
				_, table = extractTableIdentifier(ti)
			}
		}
		expr = Expr{Kind: KindAsterisk, Asterisk: &AsteriskData{Table: table}}

	// --- Dynamic columns: COLUMNS('regex') ---
	case *grammar.ColumnExprDynamicContext:
		pattern := ""
		for i := 0; i < c.GetChildCount(); i++ {
			if dcs, ok := c.GetChild(i).(*grammar.DynamicColumnSelectionContext); ok {
				pattern = extractDynamicPattern(dcs)
			}
		}
		expr = Expr{Kind: KindDynColumn, DynCol: &DynColumnData{Pattern: pattern}}

	// === NON-CANONICAL FORMS — ERROR ===

	// expr::Type — should have been normalized to CAST(expr, 'Type') function call
	case *grammar.ColumnExprCastContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprCast (expr::Type or CAST(expr AS Type)); must be normalized to CAST(expr, 'Type') function form")

	// [1, 2, 3] — should have been normalized to array(1, 2, 3)
	case *grammar.ColumnExprArrayContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprArray ([...]); must be normalized to array() function form")

	// (1, 2, 3) as tuple — should have been normalized to tuple(1, 2, 3)
	case *grammar.ColumnExprTupleContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprTuple ((...)); must be normalized to tuple() function form")

	// a[i] — should have been normalized to arrayElement(a, i)
	case *grammar.ColumnExprArrayAccessContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprArrayAccess (a[i]); must be normalized to arrayElement() function form")

	// t.1 — should have been normalized to tupleElement(t, 1)
	case *grammar.ColumnExprTupleAccessContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprTupleAccess (t.N); must be normalized to tupleElement() function form")

	// DATE 'str' — should have been normalized to toDate('str')
	case *grammar.ColumnExprDateContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprDate (DATE 'str'); must be normalized to toDate() function form")

	// TIMESTAMP 'str' — should have been normalized to toDateTime('str')
	case *grammar.ColumnExprTimestampContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprTimestamp (TIMESTAMP 'str'); must be normalized to toDateTime() function form")

	// EXTRACT(interval FROM expr) — should have been normalized to extract() function
	case *grammar.ColumnExprExtractContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprExtract; must be normalized to extract() function form")

	// SUBSTRING(expr FROM expr FOR expr) — should have been normalized to substring()
	case *grammar.ColumnExprSubstringContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprSubstring; must be normalized to substring() function form")

	// TRIM(BOTH str FROM expr) — should have been normalized to trimBoth/trimLeading/trimTrailing()
	case *grammar.ColumnExprTrimContext:
		err = eh.Errorf("convertColumnExpr: non-canonical ColumnExprTrim; must be normalized to trimBoth/trimLeading/trimTrailing() function form")

	default:
		err = eh.Errorf("convertColumnExpr: unsupported expression type %T", ctx)
	}
	return
}

// --- Parameter slot ---

func convertParamSlot(pr *nanopass.ParseResult, ctx *grammar.ColumnExprParamSlotContext) (expr Expr, err error) {
	// Find the paramSlot child
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ps, ok := ctx.GetChild(i).(*grammar.ParamSlotContext); ok {
			name := ""
			typeStr := ""
			for j := 0; j < ps.GetChildCount(); j++ {
				child := ps.GetChild(j)
				if ident, ok := child.(*grammar.IdentifierContext); ok {
					name = identText(ident)
				}
				if cte, ok := child.(grammar.IColumnTypeExprContext); ok {
					typeStr = cte.(antlr.ParserRuleContext).GetText()
				}
			}
			expr = Expr{
				Kind:  KindParamSlot,
				Param: &ParamSlotData{Name: name, Type: typeStr},
			}
			return
		}
	}
	err = eh.Errorf("convertParamSlot: no paramSlot child found")
	return
}

// --- Column identifier ---

func convertColumnIdentifier(pr *nanopass.ParseResult, ctx *grammar.ColumnExprIdentifierContext) (expr Expr, err error) {
	text := nanopass.NodeText(pr, ctx)
	lower := strings.ToLower(strings.TrimSpace(text))

	// true/false are parsed as identifiers by the grammar
	if lower == "true" || lower == "false" {
		expr = Expr{Kind: KindLiteral, Literal: &LiteralData{SQL: lower}}
		return
	}

	ref := &ColumnRefData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		if ci, ok := ctx.GetChild(i).(*grammar.ColumnIdentifierContext); ok {
			// columnIdentifier: (tableIdentifier DOT)? nestedIdentifier
			for j := 0; j < ci.GetChildCount(); j++ {
				child := ci.GetChild(j)
				if ti, ok := child.(*grammar.TableIdentifierContext); ok {
					ref.Database, ref.Table = extractTableIdentifier(ti)
				}
				if ni, ok := child.(*grammar.NestedIdentifierContext); ok {
					parts := extractNestedIdentifier(ni)
					if len(parts) >= 1 {
						ref.Column = parts[0]
					}
					if len(parts) >= 2 {
						ref.Nested = parts[1]
					}
				}
			}
		}
	}

	expr = Expr{Kind: KindColumnRef, ColRef: ref}
	return
}

func extractNestedIdentifier(ctx *grammar.NestedIdentifierContext) (parts []string) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
			parts = append(parts, identText(ident))
		}
	}
	return
}

// --- Function call ---

func convertFunctionCall(pr *nanopass.ParseResult, ctx *grammar.ColumnExprFunctionContext) (expr Expr, err error) {
	fd := &FuncCallData{}

	// Extract function name
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ident, ok := ctx.GetChild(i).(*grammar.IdentifierContext); ok {
			fd.Name = identText(ident)
			break
		}
	}

	// Detect parametric function: f(params)(args)
	// Grammar: identifier (LPAREN columnExprList? RPAREN)? LPAREN DISTINCT? columnArgList? RPAREN
	// The first LPAREN...RPAREN is params (optional), the second is args.
	parenGroups := collectParenGroups(ctx)

	if len(parenGroups) == 2 {
		// Parametric: first group is params, second is args
		fd.Params, err = convertColumnExprListFromChildren(pr, parenGroups[0])
		if err != nil {
			return
		}
		fd.Args, fd.Distinct, err = convertFuncArgs(pr, parenGroups[1])
		if err != nil {
			return
		}
	} else if len(parenGroups) == 1 {
		// Non-parametric: single group is args
		fd.Args, fd.Distinct, err = convertFuncArgs(pr, parenGroups[0])
		if err != nil {
			return
		}
	}

	expr = Expr{Kind: KindFunctionCall, Func: fd}
	return
}

// collectParenGroups finds the content between matched LPAREN/RPAREN pairs.
// Returns slices of child indices for each group.
type parenGroup struct {
	children []antlr.Tree
}

func collectParenGroups(ctx antlr.ParserRuleContext) (groups []parenGroup) {
	inParen := false
	var current parenGroup
	depth := 0

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			if tt == grammar.ClickHouseLexerLPAREN {
				if depth == 0 {
					inParen = true
					current = parenGroup{}
				} else {
					current.children = append(current.children, child)
				}
				depth++
				continue
			}
			if tt == grammar.ClickHouseLexerRPAREN {
				depth--
				if depth == 0 {
					groups = append(groups, current)
					inParen = false
				} else {
					current.children = append(current.children, child)
				}
				continue
			}
		}
		if inParen && depth == 1 {
			current.children = append(current.children, child)
		}
	}
	return
}

func convertFuncArgs(pr *nanopass.ParseResult, pg parenGroup) (args []Expr, distinct bool, err error) {
	for _, child := range pg.children {
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerDISTINCT {
				distinct = true
			}
		}
		if cal, ok := child.(*grammar.ColumnArgListContext); ok {
			args, err = convertColumnArgList(pr, cal)
			if err != nil {
				return
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			args, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
	}
	return
}

func convertColumnExprListFromChildren(pr *nanopass.ParseResult, pg parenGroup) (exprs []Expr, err error) {
	for _, child := range pg.children {
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			return convertColumnExprList(pr, cel)
		}
	}
	return
}

func convertColumnArgList(pr *nanopass.ParseResult, ctx *grammar.ColumnArgListContext) (args []Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if cae, ok := ctx.GetChild(i).(*grammar.ColumnArgExprContext); ok {
			arg, argErr := convertColumnArgExpr(pr, cae)
			if argErr != nil {
				err = argErr
				return
			}
			args = append(args, arg)
		}
	}
	return
}

func convertColumnArgExpr(pr *nanopass.ParseResult, ctx *grammar.ColumnArgExprContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if cle, ok := child.(*grammar.ColumnLambdaExprContext); ok {
			return convertLambdaExpr(pr, cle)
		}
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			return convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
		}
	}
	err = eh.Errorf("convertColumnArgExpr: empty")
	return
}

// --- Lambda ---

func convertLambdaExpr(pr *nanopass.ParseResult, ctx *grammar.ColumnLambdaExprContext) (expr Expr, err error) {
	ld := &LambdaData{}

	// Collect parameter identifiers (before ARROW)
	seenArrow := false
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerARROW {
				seenArrow = true
				continue
			}
		}
		if !seenArrow {
			if ident, ok := child.(*grammar.IdentifierContext); ok {
				ld.Params = append(ld.Params, identText(ident))
			}
		} else {
			if ce, ok := child.(grammar.IColumnExprContext); ok {
				ld.Body, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
				if err != nil {
					return
				}
			}
		}
	}

	expr = Expr{Kind: KindLambda, Lambda: ld}
	return
}

// --- Window function ---

func convertWindowFunction(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, isTarget bool) (expr Expr, err error) {
	wfd := &WindowFuncData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ident, ok := child.(*grammar.IdentifierContext); ok {
			if wfd.Name == "" {
				wfd.Name = identText(ident)
			} else if isTarget {
				// Second identifier is the window reference name
				wfd.WindowRef = identText(ident)
			}
		}
		if cel, ok := child.(*grammar.ColumnExprListContext); ok {
			wfd.Args, err = convertColumnExprList(pr, cel)
			if err != nil {
				return
			}
		}
		if we, ok := child.(*grammar.WindowExprContext); ok {
			ws, wsErr := convertWindowExpr(pr, we)
			if wsErr != nil {
				err = wsErr
				return
			}
			wfd.Window = &ws
		}
	}

	expr = Expr{Kind: KindWindowFunc, WinFunc: wfd}
	return
}

// --- Binary operators ---

func convertBinaryExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext) (expr Expr, err error) {
	bd := &BinaryData{}

	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			exprs = append(exprs, e)
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerASTERISK:
				bd.Op = "*"
			case grammar.ClickHouseLexerSLASH:
				bd.Op = "/"
			case grammar.ClickHouseLexerPERCENT:
				bd.Op = "%"
			case grammar.ClickHouseLexerPLUS:
				bd.Op = "+"
			case grammar.ClickHouseLexerDASH:
				bd.Op = "-"
			case grammar.ClickHouseLexerCONCAT:
				bd.Op = "||"
			}
		}
	}

	if len(exprs) >= 1 {
		bd.Left = exprs[0]
	}
	if len(exprs) >= 2 {
		bd.Right = exprs[1]
	}

	expr = Expr{Kind: KindBinary, Binary: bd}
	return
}

func convertPrecedence3Expr(pr *nanopass.ParseResult, ctx *grammar.ColumnExprPrecedence3Context) (expr Expr, err error) {
	bd := &BinaryData{}

	exprs := make([]Expr, 0, 2)
	hasNot := false
	hasGlobal := false

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			exprs = append(exprs, e)
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar.ClickHouseLexerEQ_SINGLE, grammar.ClickHouseLexerEQ_DOUBLE:
				bd.Op = "="
			case grammar.ClickHouseLexerNOT_EQ:
				bd.Op = "!="
			case grammar.ClickHouseLexerLT:
				bd.Op = "<"
			case grammar.ClickHouseLexerGT:
				bd.Op = ">"
			case grammar.ClickHouseLexerLE:
				bd.Op = "<="
			case grammar.ClickHouseLexerGE:
				bd.Op = ">="
			case grammar.ClickHouseLexerIN:
				bd.Op = "IN"
			case grammar.ClickHouseLexerLIKE:
				bd.Op = "LIKE"
			case grammar.ClickHouseLexerILIKE:
				bd.Op = "ILIKE"
			case grammar.ClickHouseLexerNOT:
				hasNot = true
			case grammar.ClickHouseLexerGLOBAL:
				hasGlobal = true
			}
		}
	}

	// Compose operator name
	if hasGlobal && hasNot && bd.Op == "IN" {
		bd.Op = "GLOBAL NOT IN"
	} else if hasGlobal && bd.Op == "IN" {
		bd.Op = "GLOBAL IN"
	} else if hasNot && bd.Op == "IN" {
		bd.Op = "NOT IN"
	} else if hasNot && bd.Op == "LIKE" {
		bd.Op = "NOT LIKE"
	} else if hasNot && bd.Op == "ILIKE" {
		bd.Op = "NOT ILIKE"
	}

	if len(exprs) >= 1 {
		bd.Left = exprs[0]
	}
	if len(exprs) >= 2 {
		bd.Right = exprs[1]
	}

	expr = Expr{Kind: KindBinary, Binary: bd}
	return
}

func convertLogicalBinaryExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, op string) (expr Expr, err error) {
	bd := &BinaryData{Op: op}

	exprs := make([]Expr, 0, 2)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			exprs = append(exprs, e)
		}
	}

	if len(exprs) >= 1 {
		bd.Left = exprs[0]
	}
	if len(exprs) >= 2 {
		bd.Right = exprs[1]
	}

	expr = Expr{Kind: KindBinary, Binary: bd}
	return
}

// --- Unary ---

func convertUnaryExpr(pr *nanopass.ParseResult, ctx antlr.ParserRuleContext, op string) (expr Expr, err error) {
	ud := &UnaryData{Op: op}

	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar.IColumnExprContext); ok {
			ud.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
			break
		}
	}

	expr = Expr{Kind: KindUnary, Unary: ud}
	return
}

// --- IS [NOT] NULL ---

func convertIsNull(pr *nanopass.ParseResult, ctx *grammar.ColumnExprIsNullContext) (expr Expr, err error) {
	ind := &IsNullData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			ind.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerNOT {
				ind.Negate = true
			}
		}
	}

	expr = Expr{Kind: KindIsNull, IsNull: ind}
	return
}

// --- BETWEEN ---

func convertBetween(pr *nanopass.ParseResult, ctx *grammar.ColumnExprBetweenContext) (expr Expr, err error) {
	bd := &BetweenData{}

	exprs := make([]Expr, 0, 3)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			exprs = append(exprs, e)
		}
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar.ClickHouseLexerNOT {
				bd.Negate = true
			}
		}
	}

	if len(exprs) >= 1 {
		bd.Expr = exprs[0]
	}
	if len(exprs) >= 2 {
		bd.Low = exprs[1]
	}
	if len(exprs) >= 3 {
		bd.High = exprs[2]
	}

	expr = Expr{Kind: KindBetween, Between: bd}
	return
}

// --- Ternary ---

func convertTernary(pr *nanopass.ParseResult, ctx *grammar.ColumnExprTernaryOpContext) (expr Expr, err error) {
	td := &TernaryData{}

	exprs := make([]Expr, 0, 3)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			exprs = append(exprs, e)
		}
	}

	if len(exprs) >= 1 {
		td.Cond = exprs[0]
	}
	if len(exprs) >= 2 {
		td.Then = exprs[1]
	}
	if len(exprs) >= 3 {
		td.Else = exprs[2]
	}

	expr = Expr{Kind: KindTernary, Ternary: td}
	return
}

// --- CASE ---

func convertCase(pr *nanopass.ParseResult, ctx *grammar.ColumnExprCaseContext) (expr Expr, err error) {
	cd := &CaseData{}

	// Grammar: CASE columnExpr? (WHEN columnExpr THEN columnExpr)+ (ELSE columnExpr)? END
	// Walk children and track state
	state := "start" // start, operand, when, then, else

	allExprs := make([]Expr, 0)
	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
			case grammar.ClickHouseLexerCASE:
				state = "operand"
			case grammar.ClickHouseLexerWHEN:
				state = "when"
			case grammar.ClickHouseLexerTHEN:
				state = "then"
			case grammar.ClickHouseLexerELSE:
				state = "else"
			case grammar.ClickHouseLexerEND:
				// done
			}
			continue
		}
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			e, eErr := convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if eErr != nil {
				err = eErr
				return
			}
			switch state {
			case "operand":
				cd.Operand = &e
				// Next WHEN will switch state
			case "when":
				allExprs = append(allExprs, e)
			case "then":
				if len(allExprs) > 0 {
					whenExpr := allExprs[len(allExprs)-1]
					allExprs = allExprs[:len(allExprs)-1]
					cd.Whens = append(cd.Whens, CaseWhen{When: whenExpr, Then: e})
				}
			case "else":
				cd.Else = &e
			}
		}
	}

	expr = Expr{Kind: KindCase, Case: cd}
	return
}

// --- INTERVAL ---

func convertInterval(pr *nanopass.ParseResult, ctx *grammar.ColumnExprIntervalContext) (expr Expr, err error) {
	id := &IntervalData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			id.Value, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if iv, ok := child.(*grammar.IntervalContext); ok {
			id.Unit = strings.ToUpper(iv.GetText())
		}
	}

	expr = Expr{Kind: KindInterval, Interval: id}
	return
}

// --- Alias ---

func convertAlias(pr *nanopass.ParseResult, ctx *grammar.ColumnExprAliasContext) (expr Expr, err error) {
	ad := &AliasData{}

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if ce, ok := child.(grammar.IColumnExprContext); ok {
			ad.Expr, err = convertColumnExpr(pr, ce.(antlr.ParserRuleContext))
			if err != nil {
				return
			}
		}
		if ident, ok := child.(*grammar.IdentifierContext); ok {
			ad.Name = identText(ident)
		}
		if al, ok := child.(*grammar.AliasContext); ok {
			ad.Name = aliasText(al)
		}
	}

	expr = Expr{Kind: KindAlias, Alias: ad}
	return
}

// --- Subquery ---

func convertSubqueryExpr(pr *nanopass.ParseResult, ctx *grammar.ColumnExprSubqueryContext) (expr Expr, err error) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if sus, ok := ctx.GetChild(i).(*grammar.SelectUnionStmtContext); ok {
			su, suErr := convertSelectUnion(pr, sus)
			if suErr != nil {
				err = suErr
				return
			}
			expr = Expr{Kind: KindSubquery, Subquery: &SubqueryData{Query: su}}
			return
		}
	}
	err = eh.Errorf("convertSubqueryExpr: no selectUnionStmt found")
	return
}
