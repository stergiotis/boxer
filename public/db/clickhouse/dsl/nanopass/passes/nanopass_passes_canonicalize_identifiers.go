package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammarenv"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeIdentifiers converts all identifiers to double-quoted form.
//
// Walks IdentifierContext and AliasContext nodes and ensures their tokens are
// double-quoted. Keyword tokens used as names (e.g. a table named "system")
// are quoted. JSON_TRUE/JSON_FALSE are skipped (boolean literals, not names),
// as are param slots ({name: Type}) and type expressions — ClickHouse
// requires both bare. The ALL of GROUP BY ALL and ORDER BY ALL stays bare
// too: it is the keyword, which quoted would name a column "all".
//
// A setting name stays bare unless it lexes as a keyword, wherever it is
// written: in a SETTINGS clause, in a SET statement in the body, and in the
// `SET …;` prelude, which [grammarenv.Extract] splits off before the body is
// parsed and which this pass therefore spells through the environment (see
// spellPreludeSettingNames).
var CanonicalizeIdentifiers = nanopass.Pass{
	Name: "CanonicalizeIdentifiers",
	Apply: func(e *grammarenv.Environment, body string) (newBody string, err error) {
		newBody, err = canonicalizeIdentifiersImpl(body)
		if err != nil {
			return
		}
		spellPreludeSettingNames(e)
		return
	},
	Properties: nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody | nanopass.RegionSessionSettings,
		Writes:     nanopass.RegionBody | nanopass.RegionSessionSettings,
	},
}

func canonicalizeIdentifiersImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeIdentifiers: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	processed := make(map[int]bool, 64)

	guard := &fusionGuard{pr: pr, processed: processed}

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
		case *grammar1.SettingExprContext:
			// Setting names are configuration keys — they stay bare except
			// when the name lexes as a keyword (e.g. a setting literally
			// named "max"): Grammar1's keyword-tolerant identifier rule
			// accepts it bare, Grammar2's IDENTIFIER token does not, and
			// the server accepts the quoted spelling. The value side only
			// carries literals and array()/tuple() constructors whose
			// names are real functions; it is never touched. A setting
			// named true or false is a name here, not a boolean, and is
			// quoted like a keyword.
			quoteIfKeyword(rw, c, guard, true)
			return false
		case *grammar1.IdentifierOrNullContext:
			// The FORMAT clause name stays bare unless it collides with a
			// keyword (NULL_SQL is the grammar's own alternative and is
			// left alone).
			quoteIfKeyword(rw, c, guard, false)
			return false
		case *grammar1.GroupByClauseContext:
			// GROUP BY ALL: the grammar reads the ALL keyword as a column
			// name; quoted it would name a column "all".
			if c.LPAREN() == nil {
				if list, ok := c.ColumnExprList().(*grammar1.ColumnExprListContext); ok && len(list.AllColumnsExpr()) == 1 {
					keepBareAll(list.ColumnsExpr(0), guard)
				}
			}
			return true
		case *grammar1.OrderByClauseContext:
			// ORDER BY ALL, likewise.
			if list, ok := c.OrderExprList().(*grammar1.OrderExprListContext); ok && len(list.AllOrderExpr()) == 1 {
				if e := list.OrderExpr(0).ColumnExpr(); e != nil {
					keepBareAll(e, guard)
				}
			}
			return true
		case *grammar1.ParamSlotContext:
			// {name: Type} — ClickHouse parameter syntax takes a bare name
			// and a bare type; quoting either breaks the slot.
			return false
		case *grammar1.ColumnTypeExprSimpleContext,
			*grammar1.ColumnTypeExprComplexContext,
			*grammar1.ColumnTypeExprParamContext,
			*grammar1.ColumnTypeExprNestedContext,
			*grammar1.ColumnTypeExprEnumContext:
			// Type names (CAST targets, slot types) are not relations —
			// ClickHouse does not accept them quoted.
			return false
		case *grammar1.IdentifierContext:
			quoteIdentifierCtx(rw, c, guard)
			return false
		case *grammar1.AliasContext:
			quoteAliasCtx(rw, c, guard)
			return false
		}
		return true
	})

	result = nanopass.GetText(rw)
	return
}

// fusionGuard prevents adjacent quoted replacements from fusing into one
// token: two double-quoted identifiers emitted back to back with no gap
// ("a" directly followed by "b") re-lex as a single identifier because ""
// is the quote-escape sequence. When the token immediately before a quoted
// replacement is contiguous and itself ends with a double quote — either
// rewritten in this walk (every rewrite here ends with ") or spelled
// quoted in the source — the replacement gets a separating space.
type fusionGuard struct {
	pr        *nanopass.ParseResult
	processed map[int]bool
}

func (inst *fusionGuard) replacement(tok antlr.Token, quoted string) string {
	idx := tok.GetTokenIndex()
	if idx > 0 {
		prev := inst.pr.TokenStream.Get(idx - 1)
		if prev.GetStop()+1 == tok.GetStart() {
			prevText := prev.GetText()
			prevEndsQuote := len(prevText) > 0 && prevText[len(prevText)-1] == '"'
			// Every rewrite in this pass ends with a double quote, so a
			// processed contiguous predecessor is a fusion risk even when
			// its own spelling was left unchanged.
			if inst.processed[prev.GetTokenIndex()] || prevEndsQuote {
				quoted = " " + quoted
			}
		}
	}
	if idx+1 < inst.pr.TokenStream.Size() {
		next := inst.pr.TokenStream.Get(idx + 1)
		if tok.GetStop()+1 == next.GetStart() {
			nextText := next.GetText()
			// A contiguous successor that starts with a double quote fuses
			// with this replacement's closing quote; if the successor is
			// itself rewritten later it prepends its own space (a doubled
			// space is harmless).
			if len(nextText) > 0 && nextText[0] == '"' {
				quoted = quoted + " "
			}
		}
	}
	return quoted
}

// keepBareAll marks the token of ctx as done when ctx is the ALL keyword
// alone, so that the walk leaves it bare.
func keepBareAll(ctx antlr.ParserRuleContext, guard *fusionGuard) {
	start, stop := ctx.GetStart(), ctx.GetStop()
	if start == nil || stop == nil || start.GetTokenIndex() != stop.GetTokenIndex() {
		return
	}
	if start.GetTokenType() == grammar1.ClickHouseLexerALL {
		guard.processed[start.GetTokenIndex()] = true
	}
}

// quoteIfKeyword walks the direct identifier child of ctx and quotes its
// token only when it is a keyword token — names that already lex as
// IDENTIFIER stay bare. NULL stays bare, and so do true and false unless
// quoteBooleans is set.
func quoteIfKeyword(rw nanopass.RewriterI, ctx antlr.ParserRuleContext, guard *fusionGuard, quoteBooleans bool) {
	for i := 0; i < ctx.GetChildCount(); i++ {
		ident, ok := ctx.GetChild(i).(*grammar1.IdentifierContext)
		if !ok {
			continue
		}
		tok := ident.GetStart()
		if tok == nil {
			continue
		}
		idx := tok.GetTokenIndex()
		if guard.processed[idx] {
			continue
		}
		tt := tok.GetTokenType()
		boolean := tt == grammar1.ClickHouseLexerJSON_TRUE || tt == grammar1.ClickHouseLexerJSON_FALSE
		if tt == grammar1.ClickHouseLexerIDENTIFIER ||
			tt == grammar1.ClickHouseLexerNULL_SQL ||
			boolean && !quoteBooleans {
			continue
		}
		guard.processed[idx] = true
		nanopass.ReplaceToken(rw, idx, guard.replacement(tok, nanopass.QuoteIdentifier(tok.GetText())))
		return
	}
}

func quoteIdentifierCtx(rw nanopass.RewriterI, ctx *grammar1.IdentifierContext, guard *fusionGuard) {
	tok := ctx.GetStart()
	idx := tok.GetTokenIndex()
	if guard.processed[idx] {
		return
	}
	guard.processed[idx] = true

	tt := tok.GetTokenType()

	// Skip boolean literals — they appear in IdentifierContext via
	// identifier → keyword → JSON_TRUE/JSON_FALSE, but they are boolean
	// values, not names. Grammar2 handles them via the literal rule.
	if tt == grammar1.ClickHouseLexerJSON_TRUE || tt == grammar1.ClickHouseLexerJSON_FALSE {
		guard.processed[idx] = false
		return
	}

	text := tok.GetText()

	if tt == grammar1.ClickHouseLexerIDENTIFIER {
		// Bare or backtick-quoted identifier → ensure double-quoted
		quoted := ensureDoubleQuoted(text)
		if quoted != text {
			nanopass.ReplaceToken(rw, idx, guard.replacement(tok, quoted))
		}
	} else {
		// Keyword or interval token used as name → quote it
		nanopass.ReplaceToken(rw, idx, guard.replacement(tok, nanopass.QuoteIdentifier(text)))
	}
}

func quoteAliasCtx(rw nanopass.RewriterI, ctx *grammar1.AliasContext, guard *fusionGuard) {
	// In Grammar1, alias: IDENTIFIER — always an IDENTIFIER token.
	tok := ctx.GetStart()
	idx := tok.GetTokenIndex()
	if guard.processed[idx] {
		return
	}
	guard.processed[idx] = true

	text := tok.GetText()
	quoted := ensureDoubleQuoted(text)
	if quoted != text {
		nanopass.ReplaceToken(rw, idx, guard.replacement(tok, quoted))
	}
}

// ensureDoubleQuoted converts an IDENTIFIER token to canonical double-quoted
// form by decoding the spelling (bare, backquoted, or double-quoted, with
// escapes) and re-encoding:
//
//	bare_ident  → "bare_ident"
//	`backtick`  → "backtick"
//	"already"   → "already"
//	`a\"b`      → "a""b"      (escapes normalised, same denoted name)
func ensureDoubleQuoted(s string) string {
	if len(s) == 0 {
		return `""`
	}
	return nanopass.QuoteIdentifier(nanopass.DecodeIdentifier(s))
}
