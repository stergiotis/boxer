package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeIdentifiers converts all identifiers to double-quoted form.
//
// Walks IdentifierContext and AliasContext nodes and ensures their tokens are
// double-quoted. Keyword tokens used as names (e.g. a table named "system")
// are quoted. JSON_TRUE/JSON_FALSE are skipped (boolean literals, not names),
// as are param slots ({name: Type}) and type expressions — ClickHouse
// requires both bare.
var CanonicalizeIdentifiers = nanopass.LiftBodyPass(
	"CanonicalizeIdentifiers",
	canonicalizeIdentifiersImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

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
			// names are real functions; it is never touched.
			quoteIfKeyword(rw, c, guard)
			return false
		case *grammar1.IdentifierOrNullContext:
			// The FORMAT clause name stays bare unless it collides with a
			// keyword (NULL_SQL is the grammar's own alternative and is
			// left alone).
			quoteIfKeyword(rw, c, guard)
			return false
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

// quoteIfKeyword walks the direct identifier child of ctx and quotes its
// token only when it is a keyword token — names that already lex as
// IDENTIFIER stay bare.
func quoteIfKeyword(rw nanopass.RewriterI, ctx antlr.ParserRuleContext, guard *fusionGuard) {
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
		if tt == grammar1.ClickHouseLexerIDENTIFIER ||
			tt == grammar1.ClickHouseLexerNULL_SQL ||
			tt == grammar1.ClickHouseLexerJSON_TRUE ||
			tt == grammar1.ClickHouseLexerJSON_FALSE {
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
