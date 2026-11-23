package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CanonicalizeSugar converts SQL syntactic sugar to canonical function call form.
//
//	DATE 'str'                            → toDate('str')
//	TIMESTAMP 'str'                       → toDateTime('str')
//	EXTRACT(unit FROM expr)               → toYear/toQuarter/toMonth/toISOWeek/
//	                                        toDayOfMonth/toHour/toMinute/toSecond(expr)
//	SUBSTRING(expr FROM expr)             → substring(expr, expr)
//	SUBSTRING(expr FROM expr FOR expr)    → substring(expr, expr, expr)
//	TRIM(BOTH str FROM expr)              → trimBoth(expr, str)
//	TRIM(LEADING str FROM expr)           → trimLeft(expr, str)
//	TRIM(TRAILING str FROM expr)          → trimRight(expr, str)
//	f(args) FILTER (WHERE c)              → fIf(args, c)
//	f(params)(args) FILTER (WHERE c)      → fIf(params)(args, c)
//	f(DISTINCT args) FILTER (WHERE c)     → fDistinctIf(args, c)
//	f(args) FILTER (WHERE c) OVER …       → fIf(args, c) OVER …
//	EXISTS (query), exists(query)         → "exists"((query))
//
// The target functions are the server's own canonical rewrites (verified
// against `clickhouse format`, ClickHouse 26.x). In particular
// EXTRACT must NOT lower to `extract(expr, 'unit')` — ClickHouse's
// extract(haystack, pattern) is the regex-extraction function, an illegal
// type error on dates — and trimLeading/trimTrailing do not exist.
// ClickHouse's parser rewrites the aggregate filter to the -If combinator
// itself: it appends If to the name whatever combinators the name already
// carries (sumArray → sumArrayIf, uniqExactIf → uniqExactIfIf), turns
// DISTINCT into the Distinct combinator ahead of If, and for a call named
// exactly count without DISTINCT drops the asterisk arguments
// (count(*) FILTER (WHERE c) → countIf(c)). The rule writes the same call.
// EXISTS the server holds as the call exists with the query as a subquery
// argument. The rule writes the name quoted: bare, the word followed by a
// parenthesis is the predicate again, quoted it is a call like any other,
// which the server reads alike.
//
// Sugar forms nest (SUBSTRING(DATE '…' FROM 1), EXTRACT(DAY FROM DATE '…'));
// each apply rewrites the outermost occurrences only, so the pass declares
// NeedsFixedPoint and converges layer by layer.
var CanonicalizeSugar = nanopass.LiftBodyPass(
	"CanonicalizeSugar",
	func(sql string) (string, error) {
		return rewriteNodes(sql, "CanonicalizeSugar",
			sugarDateRule, sugarTimestampRule, sugarExtractRule, sugarSubstringRule, sugarTrimRule, sugarFilterRule, sugarExistsRule)
	},
	nanopass.PassProperties{
		NeedsFixedPoint: true,
		Reads:           nanopass.RegionBody,
		Writes:          nanopass.RegionBody,
	},
)

// DATE 'str' → toDate('str')
func sugarDateRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprDateContext)
	if !ok {
		return "", false
	}
	lit := terminalText(c, grammar1.ClickHouseLexerSTRING_LITERAL)
	if lit == "" {
		return "", false
	}
	return callForm("toDate", lit), true
}

// TIMESTAMP 'str' → toDateTime('str')
func sugarTimestampRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprTimestampContext)
	if !ok {
		return "", false
	}
	lit := terminalText(c, grammar1.ClickHouseLexerSTRING_LITERAL)
	if lit == "" {
		return "", false
	}
	return callForm("toDateTime", lit), true
}

// extractUnitFunction maps an EXTRACT unit to the ClickHouse function the
// server itself lowers it to (per `clickhouse format`).
var extractUnitFunction = map[string]string{
	"SECOND":  "toSecond",
	"MINUTE":  "toMinute",
	"HOUR":    "toHour",
	"DAY":     "toDayOfMonth",
	"WEEK":    "toISOWeek",
	"MONTH":   "toMonth",
	"QUARTER": "toQuarter",
	"YEAR":    "toYear",
}

// EXTRACT(unit FROM expr) → <unitFunction>(expr)
// Grammar: EXTRACT LPAREN interval FROM columnExpr RPAREN
func sugarExtractRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprExtractContext)
	if !ok {
		return "", false
	}
	var unit, expr string
	for i := 0; i < c.GetChildCount(); i++ {
		child := c.GetChild(i)
		if iv, ok := child.(*grammar1.IntervalContext); ok {
			unit = strings.ToUpper(iv.GetText())
		}
		if ce, ok := child.(grammar1.IColumnExprContext); ok {
			expr = spanOf(pr, ce.(antlr.ParserRuleContext))
		}
	}
	fn, known := extractUnitFunction[unit]
	if !known {
		// Unknown unit — leave the sugar in place rather than invent a call.
		return "", false
	}
	return callForm(fn, expr), true
}

// SUBSTRING(expr FROM expr [FOR expr]) → substring(expr, expr [, expr])
// Grammar: SUBSTRING LPAREN columnExpr FROM columnExpr (FOR columnExpr)? RPAREN
func sugarSubstringRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprSubstringContext)
	if !ok {
		return "", false
	}
	ops := columnExprOperands(pr, c)
	if len(ops) < 2 {
		return "", false
	}
	return callForm("substring", ops...), true
}

// TRIM(BOTH|LEADING|TRAILING str FROM expr) → trimBoth|trimLeft|trimRight(expr, str)
// (the server's own canonical functions; trimLeading/trimTrailing do not exist)
// Grammar: TRIM LPAREN (BOTH|LEADING|TRAILING) STRING_LITERAL FROM columnExpr RPAREN
func sugarTrimRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprTrimContext)
	if !ok {
		return "", false
	}
	funcName := "trimBoth" // default
	var strLit, expr string
	for i := 0; i < c.GetChildCount(); i++ {
		child := c.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			switch term.GetSymbol().GetTokenType() {
			case grammar1.ClickHouseLexerLEADING:
				funcName = "trimLeft"
			case grammar1.ClickHouseLexerTRAILING:
				funcName = "trimRight"
			case grammar1.ClickHouseLexerBOTH:
				funcName = "trimBoth"
			case grammar1.ClickHouseLexerSTRING_LITERAL:
				strLit = term.GetText()
			}
		}
		if ce, ok := child.(grammar1.IColumnExprContext); ok {
			expr = spanOf(pr, ce.(antlr.ParserRuleContext))
		}
	}
	return callForm(funcName, expr, strLit), true
}

// f(args) FILTER (WHERE c) → fIf(args, c), as ClickHouse's parser rewrites it.
// Grammar: the filterClause of a ColumnExprFunction, ColumnExprWinFunction or
// ColumnExprWinFunctionTarget, between the call and OVER.
func sugarFilterRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	var ident grammar1.IIdentifierContext
	var filter grammar1.IFilterClauseContext
	var argsOpen, argsClose antlr.TerminalNode
	var args []antlr.ParserRuleContext
	var distinct antlr.TerminalNode
	switch c := node.(type) {
	case *grammar1.ColumnExprFunctionContext:
		ident, filter = c.Identifier(), c.FilterClause()
		lparens, rparens := c.AllLPAREN(), c.AllRPAREN()
		if len(lparens) == 0 || len(rparens) == 0 {
			return "", false
		}
		argsOpen, argsClose = lparens[len(lparens)-1], rparens[len(rparens)-1]
		distinct = c.DISTINCT()
		if l, ok := c.ColumnArgList().(*grammar1.ColumnArgListContext); ok {
			for _, a := range l.AllColumnArgExpr() {
				args = append(args, a)
			}
		}
	case *grammar1.ColumnExprWinFunctionContext:
		ident, filter = c.Identifier(), c.FilterClause()
		argsOpen, argsClose = c.LPAREN(0), c.RPAREN(0)
		args = columnsExprArgs(c.ColumnExprList())
	case *grammar1.ColumnExprWinFunctionTargetContext:
		ident, filter = c.Identifier(0), c.FilterClause()
		argsOpen, argsClose = c.LPAREN(), c.RPAREN()
		args = columnsExprArgs(c.ColumnExprList())
	default:
		return "", false
	}
	fc, ok := filter.(*grammar1.FilterClauseContext)
	if !ok || ident == nil || argsOpen == nil || argsClose == nil || fc.ColumnExpr() == nil {
		return "", false
	}
	identCtx := ident.(antlr.ParserRuleContext)
	name := nanopass.DecodeIdentifier(spanOf(pr, identCtx))
	text := func(start, stop int) string {
		if start > stop {
			return ""
		}
		return pr.TokenStream.GetTextFromInterval(antlr.NewInterval(start, stop))
	}
	kept := make([]string, 0, len(args))
	for _, a := range args {
		if name == "count" && distinct == nil && isAsteriskArg(a) {
			continue
		}
		kept = append(kept, spanOf(pr, a))
	}
	identStop := identCtx.GetStop().GetTokenIndex()
	var b strings.Builder
	switch {
	case distinct != nil && len(args) == 0:
		// `count(DISTINCT)` names a column called DISTINCT.
		b.WriteString(filterCallName(name + "If"))
		b.WriteString(text(identStop+1, argsOpen.GetSymbol().GetTokenIndex()))
		kept = append(kept, nanopass.QuoteIdentifier(distinct.GetText()))
		b.WriteString(strings.Join(kept, ", "))
	case distinct != nil || len(kept) != len(args):
		suffix := "If"
		if distinct != nil {
			suffix = "DistinctIf"
		}
		b.WriteString(filterCallName(name + suffix))
		b.WriteString(text(identStop+1, argsOpen.GetSymbol().GetTokenIndex()))
		b.WriteString(strings.Join(kept, ", "))
	default:
		b.WriteString(filterCallName(name + "If"))
		b.WriteString(text(identStop+1, argsClose.GetSymbol().GetTokenIndex()-1))
	}
	if len(kept) > 0 {
		b.WriteString(", ")
	}
	b.WriteString(spanOf(pr, fc.ColumnExpr().(antlr.ParserRuleContext)))
	b.WriteString(")")
	b.WriteString(text(fc.GetStop().GetTokenIndex()+1, node.GetStop().GetTokenIndex()))
	return b.String(), true
}

// EXISTS (query) → "exists"((query)), the call ClickHouse's parser holds.
// Grammar: EXISTS LPAREN selectUnionStmt RPAREN
// A query already in parentheses of its own, `exists((SELECT 1))`, keeps
// them and gains none. A comment between the keyword and its parenthesis is
// kept; whitespace alone is not.
func sugarExistsRule(pr *nanopass.ParseResult, node antlr.ParserRuleContext) (string, bool) {
	c, ok := node.(*grammar1.ColumnExprExistsContext)
	if !ok {
		return "", false
	}
	union, ok := c.SelectUnionStmt().(*grammar1.SelectUnionStmtContext)
	if !ok || c.EXISTS() == nil || c.LPAREN() == nil || c.RPAREN() == nil {
		return "", false
	}
	text := func(start, stop int) string {
		if start > stop {
			return ""
		}
		return pr.TokenStream.GetTextFromInterval(antlr.NewInterval(start, stop))
	}
	open, shut := c.LPAREN().GetSymbol().GetTokenIndex(), c.RPAREN().GetSymbol().GetTokenIndex()
	gap := text(c.EXISTS().GetSymbol().GetTokenIndex()+1, open-1)
	if strings.TrimSpace(gap) == "" {
		gap = ""
	}
	query := text(open+1, shut-1)
	if !isParenthesisedQuery(union) {
		query = "(" + query + ")"
	}
	return nanopass.QuoteIdentifier("exists") + gap + "(" + query + ")", true
}

// isParenthesisedQuery reports whether a query is one parenthesised query
// and nothing else: no WITH ahead of it and no set operation after it.
func isParenthesisedQuery(union *grammar1.SelectUnionStmtContext) (parenthesised bool) {
	if union.Ctes() != nil || len(union.AllSelectUnionStmtItem()) > 0 {
		return false
	}
	head, ok := union.SelectStmtWithParens().(*grammar1.SelectStmtWithParensContext)
	return ok && head.LPAREN() != nil
}

// columnsExprArgs is the argument list of a window call, one node per argument.
func columnsExprArgs(list grammar1.IColumnExprListContext) (args []antlr.ParserRuleContext) {
	l, ok := list.(*grammar1.ColumnExprListContext)
	if !ok {
		return
	}
	for _, a := range l.AllColumnsExpr() {
		args = append(args, a)
	}
	return
}

// isAsteriskArg reports whether a call argument is `*` or `t.*`.
func isAsteriskArg(arg antlr.ParserRuleContext) (asterisk bool) {
	var expr antlr.Tree = arg
	switch a := arg.(type) {
	case *grammar1.ColumnArgExprContext:
		expr = a.ColumnExpr()
	case *grammar1.ColumnsExprAsteriskContext:
		return true
	case *grammar1.ColumnsExprColumnContext:
		expr = a.ColumnExpr()
	}
	_, asterisk = expr.(*grammar1.ColumnExprAsteriskContext)
	return
}

// filterCallName spells a combinator name bare when it is a plain word and
// double-quoted otherwise.
func filterCallName(name string) (spelled string) {
	for i, r := range name {
		word := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (i > 0 && ((r >= '0' && r <= '9') || r == '$'))
		if !word {
			return nanopass.QuoteIdentifier(name)
		}
	}
	return name
}
