//go:build llm_generated_opus47

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
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
	canonicalizeSugarImpl,
	nanopass.PassProperties{
		NeedsFixedPoint: true,
		Reads:           nanopass.RegionBody,
		Writes:          nanopass.RegionBody,
	},
)

func canonicalizeSugarImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeSugar: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.ColumnExprDateContext:
			rewriteDate(rw, pr, c)
			return false
		case *grammar1.ColumnExprTimestampContext:
			rewriteTimestamp(rw, pr, c)
			return false
		case *grammar1.ColumnExprExtractContext:
			rewriteExtract(rw, pr, c)
			return false
		case *grammar1.ColumnExprSubstringContext:
			rewriteSubstring(rw, pr, c)
			return false
		case *grammar1.ColumnExprTrimContext:
			rewriteTrim(rw, pr, c)
			return false
		}
		return true
	})

	result = nanopass.GetText(rw)
	return
}

// DATE 'str' → toDate('str')
func rewriteDate(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprDateContext) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerSTRING_LITERAL {
				nanopass.ReplaceNode(rw, ctx, "toDate("+term.GetText()+")")
				return
			}
		}
	}
}

// TIMESTAMP 'str' → toDateTime('str')
func rewriteTimestamp(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprTimestampContext) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		if term, ok := ctx.GetChild(i).(*antlr.TerminalNodeImpl); ok {
			if term.GetSymbol().GetTokenType() == grammar1.ClickHouseLexerSTRING_LITERAL {
				nanopass.ReplaceNode(rw, ctx, "toDateTime("+term.GetText()+")")
				return
			}
		}
	}
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
func rewriteExtract(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprExtractContext) {
	var intervalText string
	var exprText string

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if iv, ok := child.(*grammar1.IntervalContext); ok {
			intervalText = strings.ToUpper(iv.GetText())
		}
		if ce, ok := child.(grammar1.IColumnExprContext); ok {
			exprText = nanopass.NodeText(pr, ce.(antlr.ParserRuleContext))
		}
	}

	fn, known := extractUnitFunction[intervalText]
	if !known {
		// Unknown unit — leave the sugar in place rather than invent a call.
		return
	}
	nanopass.ReplaceNode(rw, ctx, fn+"("+exprText+")")
}

// SUBSTRING(expr FROM expr [FOR expr]) → substring(expr, expr [, expr])
// Grammar: SUBSTRING LPAREN columnExpr FROM columnExpr (FOR columnExpr)? RPAREN
func rewriteSubstring(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprSubstringContext) {
	exprs := make([]string, 0, 3)
	for i := 0; i < ctx.GetChildCount(); i++ {
		if ce, ok := ctx.GetChild(i).(grammar1.IColumnExprContext); ok {
			exprs = append(exprs, nanopass.NodeText(pr, ce.(antlr.ParserRuleContext)))
		}
	}

	nanopass.ReplaceNode(rw, ctx, "substring("+strings.Join(exprs, ", ")+")")
}

// TRIM(BOTH|LEADING|TRAILING str FROM expr) → trimBoth|trimLeft|trimRight(expr, str)
// (the server's own canonical functions; trimLeading/trimTrailing do not exist)
// Grammar: TRIM LPAREN (BOTH|LEADING|TRAILING) STRING_LITERAL FROM columnExpr RPAREN
func rewriteTrim(rw *antlr.TokenStreamRewriter, pr *nanopass.ParseResult, ctx *grammar1.ColumnExprTrimContext) {
	funcName := "trimBoth" // default
	var strLit string
	var exprText string

	for i := 0; i < ctx.GetChildCount(); i++ {
		child := ctx.GetChild(i)
		if term, ok := child.(*antlr.TerminalNodeImpl); ok {
			tt := term.GetSymbol().GetTokenType()
			switch tt {
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
			exprText = nanopass.NodeText(pr, ce.(antlr.ParserRuleContext))
		}
	}

	nanopass.ReplaceNode(rw, ctx, funcName+"("+exprText+", "+strLit+")")
}
