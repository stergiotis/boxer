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
//
// The target functions are the server's own canonical rewrites (verified
// against `clickhouse format`, ClickHouse 26.x). In particular
// EXTRACT must NOT lower to `extract(expr, 'unit')` — ClickHouse's
// extract(haystack, pattern) is the regex-extraction function, an illegal
// type error on dates — and trimLeading/trimTrailing do not exist.
//
// Sugar forms nest (SUBSTRING(DATE '…' FROM 1), EXTRACT(DAY FROM DATE '…'));
// each apply rewrites the outermost occurrences only, so the pass declares
// NeedsFixedPoint and converges layer by layer.
var CanonicalizeSugar = nanopass.LiftBodyPass(
	"CanonicalizeSugar",
	func(sql string) (string, error) {
		return rewriteNodes(sql, "CanonicalizeSugar",
			sugarDateRule, sugarTimestampRule, sugarExtractRule, sugarSubstringRule, sugarTrimRule)
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
