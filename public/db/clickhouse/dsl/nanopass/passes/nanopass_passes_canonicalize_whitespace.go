package passes

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CanonicalizeWhitespace collapses all whitespace sequences between tokens to
// a single space and trims leading/trailing whitespace. Newlines are
// preserved as single newlines.
var CanonicalizeWhitespace = nanopass.LiftBodyPass(
	"CanonicalizeWhitespace",
	canonicalizeWhitespaceImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

// CanonicalizeWhitespaceSingleLine collapses all whitespace (including
// newlines) to single spaces.
var CanonicalizeWhitespaceSingleLine = nanopass.LiftBodyPass(
	"CanonicalizeWhitespaceSingleLine",
	canonicalizeWhitespaceSingleLineImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func canonicalizeWhitespaceImpl(sql string) (result string, err error) {
	result, err = rewriteTokens(sql, "CanonicalizeWhitespace", whitespaceRule)
	if err != nil {
		return
	}
	result = strings.TrimSpace(result)
	return
}

// whitespaceRule collapses one WHITESPACE token to a single newline (if it
// spans a line break) or a single space.
func whitespaceRule(tok antlr.Token) (string, bool) {
	if tok.GetTokenType() != grammar1.ClickHouseLexerWHITESPACE {
		return "", false
	}
	text := tok.GetText()
	replacement := " "
	if strings.ContainsAny(text, "\n\r") {
		replacement = "\n"
	}
	if text == replacement {
		return "", false
	}
	return replacement, true
}

func canonicalizeWhitespaceSingleLineImpl(sql string) (result string, err error) {
	result, err = rewriteTokens(sql, "CanonicalizeWhitespaceSingleLine", whitespaceSingleLineRule)
	if err != nil {
		return
	}
	result = strings.TrimSpace(result)
	return
}

// whitespaceSingleLineRule collapses one WHITESPACE token to a single space.
func whitespaceSingleLineRule(tok antlr.Token) (string, bool) {
	if tok.GetTokenType() != grammar1.ClickHouseLexerWHITESPACE {
		return "", false
	}
	if tok.GetText() == " " {
		return "", false
	}
	return " ", true
}
