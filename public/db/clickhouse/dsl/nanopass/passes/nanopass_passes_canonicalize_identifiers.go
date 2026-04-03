//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeIdentifiers converts all identifiers to double-quoted form.
//
// This pass assumes parsing with Grammar1 where alias: IDENTIFIER (no
// keywordForAlias). This means:
//   - Every IdentifierContext is a genuine name (column, table, function, etc.)
//   - Every AliasContext contains an IDENTIFIER token (never a keyword)
//   - Structural keywords (DESC, ASC, ROWS, OUTER, etc.) are never parsed
//     as identifiers or aliases — they're terminals of their respective rules
//
// The pass walks all IdentifierContext and AliasContext nodes and ensures
// their tokens are double-quoted. Keyword tokens used as names (e.g. a table
// named "system") are quoted. JSON_TRUE/JSON_FALSE are skipped (they are
// boolean literals, not names).
func CanonicalizeIdentifiers(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeIdentifiers: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	processed := make(map[int]bool, 64)

	nanopass.WalkCST(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		switch c := ctx.(type) {
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

var _ nanopass.Pass = CanonicalizeIdentifiers

func quoteIdentifierCtx(rw *antlr.TokenStreamRewriter, ctx *grammar1.IdentifierContext, processed map[int]bool) {
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
		nanopass.ReplaceToken(rw, idx, quoteIdentifier(text))
	}
}

func quoteAliasCtx(rw *antlr.TokenStreamRewriter, ctx *grammar1.AliasContext, processed map[int]bool) {
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

// ensureDoubleQuoted converts an IDENTIFIER token to double-quoted form.
//
//	bare_ident  → "bare_ident"
//	`backtick`  → "backtick"
//	"already"   → "already" (no change)
func ensureDoubleQuoted(s string) string {
	if len(s) == 0 {
		return `""`
	}
	if s[0] == '"' && s[len(s)-1] == '"' {
		return s
	}
	if s[0] == '`' && s[len(s)-1] == '`' {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, "``", "`")
		return quoteIdentifier(inner)
	}
	return quoteIdentifier(s)
}

// quoteIdentifier wraps a string in double quotes, escaping internal double quotes.
func quoteIdentifier(s string) string {
	escaped := strings.ReplaceAll(s, `"`, `""`)
	return `"` + escaped + `"`
}
