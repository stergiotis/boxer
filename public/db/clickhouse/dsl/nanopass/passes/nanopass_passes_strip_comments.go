//go:build llm_generated_opus46

package passes

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// StripComments removes all single-line and multi-line comments from the SQL.
func StripComments(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("StripComments: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		tokenType := tok.GetTokenType()
		switch tokenType {
		case grammar1.ClickHouseLexerMULTI_LINE_COMMENT,
			grammar1.ClickHouseLexerSINGLE_LINE_COMMENT:
			nanopass.ReplaceToken(rw, tok.GetTokenIndex(), " ")
		}
	}

	result = nanopass.GetText(rw)
	return
}
