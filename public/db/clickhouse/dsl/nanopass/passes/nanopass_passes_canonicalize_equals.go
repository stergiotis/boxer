package passes

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// CanonicalizeEquals replaces all double-equals (==) with single-equals (=).
//
//	a == b → a = b
//
// Pure token-level pass; runs over the entire token stream regardless of context
// (WHERE, JOIN ON, HAVING, SELECT list, CASE conditions, etc.).
var CanonicalizeEquals = nanopass.LiftBodyPass(
	"CanonicalizeEquals",
	func(sql string) (string, error) { return rewriteTokens(sql, "CanonicalizeEquals", equalsRule) },
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func equalsRule(tok antlr.Token) (string, bool) {
	if tok.GetTokenType() == grammar1.ClickHouseLexerEQ_DOUBLE {
		return "=", true
	}
	return "", false
}
