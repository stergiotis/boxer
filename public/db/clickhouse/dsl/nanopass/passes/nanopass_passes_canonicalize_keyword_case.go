//go:build llm_generated_opus47

package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeKeywordCase uppercases all SQL keywords while preserving
// identifier and literal case. ClickHouse identifiers are case-sensitive, so
// context-dependent keywords used as identifiers (e.g. `system.tables`) keep
// their original case — the pass collects every IdentifierContext leaf's
// token indices and skips them when uppercasing.
var CanonicalizeKeywordCase = nanopass.LiftBodyPass(
	"CanonicalizeKeywordCase",
	canonicalizeKeywordCaseImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func canonicalizeKeywordCaseImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeKeywordCase: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	identifierTokens := collectIdentifierTokenIndices(pr.Tree)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		tokenType := tok.GetTokenType()
		if !isKeywordToken(tokenType) {
			continue
		}
		if identifierTokens[tok.GetTokenIndex()] {
			continue
		}
		nanopass.ReplaceToken(rw, tok.GetTokenIndex(), strings.ToUpper(tok.GetText()))
	}

	result = nanopass.GetText(rw)
	return
}

// collectIdentifierTokenIndices returns the set of token indices that sit under
// an IdentifierContext, i.e. tokens the parser bound as names rather than as
// structural keywords. Per the grammar `identifier: IDENTIFIER | interval | keyword`,
// such a context wraps exactly one terminal token (directly, or via the single-token
// `interval` / `keyword` sub-rules).
func collectIdentifierTokenIndices(tree antlr.Tree) map[int]bool {
	indices := make(map[int]bool, 16)
	nanopass.WalkCST(tree, func(ctx antlr.ParserRuleContext) bool {
		if _, ok := ctx.(*grammar1.IdentifierContext); ok {
			if tok := ctx.GetStart(); tok != nil {
				indices[tok.GetTokenIndex()] = true
			}
			return false
		}
		return true
	})
	return indices
}

// isKeywordToken returns true if the token type corresponds to a SQL keyword.
func isKeywordToken(tokenType int) bool {
	if tokenType == grammar1.ClickHouseLexerJSON_TRUE || tokenType == grammar1.ClickHouseLexerJSON_FALSE {
		return false
	}
	return tokenType >= grammar1.ClickHouseLexerADD &&
		tokenType <= grammar1.ClickHouseLexerJSON_TRUE
}
