//go:build llm_generated_opus47

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

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
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
			quoteIdentifierCtx(rw, c, processed)
			return false
		case *grammar1.AliasContext:
			quoteAliasCtx(rw, c, processed)
			return false
		}
		return true
	})

	result = nanopass.GetText(rw)
	return
}

func quoteIdentifierCtx(rw nanopass.RewriterI, ctx *grammar1.IdentifierContext, processed map[int]bool) {
	tok := ctx.GetStart()
	idx := tok.GetTokenIndex()
	if processed[idx] {
		return
	}
	processed[idx] = true

	tt := tok.GetTokenType()

	// Skip boolean literals — they appear in IdentifierContext via
	// identifier → keyword → JSON_TRUE/JSON_FALSE, but they are boolean
	// values, not names. Grammar2 handles them via the literal rule.
	if tt == grammar1.ClickHouseLexerJSON_TRUE || tt == grammar1.ClickHouseLexerJSON_FALSE {
		return
	}

	text := tok.GetText()

	if tt == grammar1.ClickHouseLexerIDENTIFIER {
		// Bare or backtick-quoted identifier → ensure double-quoted
		quoted := ensureDoubleQuoted(text)
		if quoted != text {
			nanopass.ReplaceToken(rw, idx, quoted)
		}
	} else {
		// Keyword or interval token used as name → quote it
		nanopass.ReplaceToken(rw, idx, nanopass.QuoteIdentifier(text))
	}
}

func quoteAliasCtx(rw nanopass.RewriterI, ctx *grammar1.AliasContext, processed map[int]bool) {
	// In Grammar1, alias: IDENTIFIER — always an IDENTIFIER token.
	tok := ctx.GetStart()
	idx := tok.GetTokenIndex()
	if processed[idx] {
		return
	}
	processed[idx] = true

	text := tok.GetText()
	quoted := ensureDoubleQuoted(text)
	if quoted != text {
		nanopass.ReplaceToken(rw, idx, quoted)
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
