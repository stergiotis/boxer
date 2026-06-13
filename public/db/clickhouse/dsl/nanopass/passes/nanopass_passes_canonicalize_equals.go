package passes

import (
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// CanonicalizeEquals replaces all double-equals (==) with single-equals (=).
//
//	a == b → a = b
//
// Pure token-level pass; runs over the entire token stream regardless of
// context (WHERE, JOIN ON, HAVING, SELECT list, CASE conditions, etc.).
var CanonicalizeEquals = nanopass.LiftBodyPass(
	"CanonicalizeEquals",
	canonicalizeEqualsImpl,
	nanopass.PassProperties{
		Idempotent: true,
		Reads:      nanopass.RegionBody,
		Writes:     nanopass.RegionBody,
	},
)

func canonicalizeEqualsImpl(sql string) (result string, err error) {
	pr, err := nanopass.Parse(sql)
	if err != nil {
		err = eh.Errorf("CanonicalizeEquals: %w", err)
		return
	}
	rw := nanopass.NewRewriter(pr)

	for i := 0; i < pr.TokenStream.Size(); i++ {
		tok := pr.TokenStream.Get(i)
		if tok.GetTokenType() == grammar1.ClickHouseLexerEQ_DOUBLE {
			nanopass.ReplaceToken(rw, tok.GetTokenIndex(), "=")
		}
	}

	result = nanopass.GetText(rw)
	return
}

var _ nanopass.Pass = CanonicalizeEquals
