//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// NormalizeKeywordCase uppercases all SQL keywords while preserving identifier and literal case.
func NormalizeKeywordCase(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("NormalizeKeywordCase: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		tokenType := tok.GetTokenType()
		if isKeywordToken(tokenType) {
			nanopass.ReplaceToken(rw, tok.GetTokenIndex(), strings.ToUpper(tok.GetText()))
		}
	}

	result = nanopass.GetText(rw)
	return
}

// isKeywordToken returns true if the token type corresponds to a SQL keyword.
func isKeywordToken(tokenType int) bool {
	return tokenType >= grammar.ClickHouseLexerADD &&
		tokenType <= grammar.ClickHouseLexerJSON_TRUE
}
