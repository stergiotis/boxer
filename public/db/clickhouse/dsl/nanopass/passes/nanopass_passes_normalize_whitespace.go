//go:build llm_generated_opus46

package passes

import (
	"strings"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// NormalizeWhitespace collapses all whitespace sequences between tokens to a single space
// and trims leading/trailing whitespace. Newlines are preserved as single newlines
// when collapseNewlines is false.
func NormalizeWhitespace(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("NormalizeWhitespace: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		if tok.GetTokenType() != grammar.ClickHouseLexerWHITESPACE {
			continue
		}

		text := tok.GetText()

		{ // Collapse to a single space
			var replacement string
			if strings.ContainsAny(text, "\n\r") {
				replacement = "\n"
			} else {
				replacement = " "
			}
			if text != replacement {
				nanopass.ReplaceToken(rw, tok.GetTokenIndex(), replacement)
			}
		}
	}

	result = nanopass.GetText(rw)
	result = strings.TrimSpace(result)
	return
}

// NormalizeWhitespaceSingleLine collapses all whitespace (including newlines) to single spaces.
func NormalizeWhitespaceSingleLine(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("NormalizeWhitespaceSingleLine: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		if tok.GetTokenType() != grammar.ClickHouseLexerWHITESPACE {
			continue
		}
		text := tok.GetText()
		if text != " " {
			nanopass.ReplaceToken(rw, tok.GetTokenIndex(), " ")
		}
	}

	result = nanopass.GetText(rw)
	result = strings.TrimSpace(result)
	return
}
