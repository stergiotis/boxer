package nanopasstest

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// ContainsEmptyIdentifier reports whether sql contains an empty quoted or
// backquoted identifier (`""` / “). Grammar1's IDENTIFIER lexer rule
// permits a zero-length quoted name, but ClickHouse rejects empty
// identifiers in every position (column, alias, window reference, table —
// verified against the server). Such input is accepted by grammar1 only, has
// no canonical form, and the fuzz targets over the pipeline skip it.
func ContainsEmptyIdentifier(sql string) bool {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	for {
		tok := lexer.NextToken()
		if tok.GetTokenType() == antlr.TokenEOF {
			return false
		}
		if tok.GetTokenType() == grammar1.ClickHouseLexerIDENTIFIER {
			if nanopass.DecodeIdentifier(tok.GetText()) == "" {
				return true
			}
		}
	}
}
