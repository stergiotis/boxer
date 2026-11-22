package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
)

// scalarWithItemHint is the sentence added to the diagnostics of a statement
// that writes a scalar the way a subquery is named.
const scalarWithItemHint = "hint: WITH name AS (…) names a subquery; a scalar or an array is written WITH <expression> AS name, e.g. WITH [1, 2] AS name"

// syntaxHints are sentences that say how to write a shape the grammar
// refused, for the shapes known to be written by mistake; nil when sql
// holds none of them. It reads tokens only and is called after a failed
// parse.
func syntaxHints(sql string) (hints []string) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	var toks []int
	for tok := lexer.NextToken(); tok.GetTokenType() != antlr.TokenEOF; tok = lexer.NextToken() {
		if tok.GetChannel() == antlr.TokenDefaultChannel {
			toks = append(toks, tok.GetTokenType())
		}
	}
	for i := 1; i+2 < len(toks); i++ {
		if toks[i] == grammar1.ClickHouseLexerIDENTIFIER && toks[i+1] == grammar1.ClickHouseLexerAS &&
			startsWithItem(toks, i) && scalarAfterAs(toks, i+2) {
			return []string{scalarWithItemHint}
		}
	}
	return
}

// startsWithItem reports whether the token at i begins an item of a WITH
// clause: it follows WITH, or a comma of a WITH clause's item list.
func startsWithItem(toks []int, i int) (start bool) {
	switch toks[i-1] {
	case grammar1.ClickHouseLexerWITH:
		return true
	case grammar1.ClickHouseLexerCOMMA:
	default:
		return false
	}
	depth := 0
	for j := i - 2; j >= 0; j-- {
		switch toks[j] {
		case grammar1.ClickHouseLexerRPAREN:
			depth++
		case grammar1.ClickHouseLexerLPAREN:
			if depth == 0 {
				return false
			}
			depth--
		case grammar1.ClickHouseLexerWITH:
			if depth == 0 {
				return true
			}
		case grammar1.ClickHouseLexerSELECT:
			if depth == 0 {
				return false
			}
		}
	}
	return false
}

// scalarAfterAs reports whether what follows `name AS` at k is an
// expression rather than an alias or a subquery: a literal, an array, a
// sign, a parameter slot or a call, or a parenthesis that opens something
// other than a query.
func scalarAfterAs(toks []int, k int) (scalar bool) {
	switch toks[k] {
	case grammar1.ClickHouseLexerLPAREN:
		if k+1 >= len(toks) {
			return false
		}
		switch toks[k+1] {
		case grammar1.ClickHouseLexerSELECT, grammar1.ClickHouseLexerWITH, grammar1.ClickHouseLexerLPAREN:
			return false
		}
		return true
	case grammar1.ClickHouseLexerLBRACKET, grammar1.ClickHouseLexerLBRACE,
		grammar1.ClickHouseLexerDASH, grammar1.ClickHouseLexerPLUS,
		grammar1.ClickHouseLexerSTRING_LITERAL, grammar1.ClickHouseLexerDECIMAL_LITERAL,
		grammar1.ClickHouseLexerFLOATING_LITERAL, grammar1.ClickHouseLexerOCTAL_LITERAL,
		grammar1.ClickHouseLexerHEXADECIMAL_LITERAL:
		return true
	case grammar1.ClickHouseLexerIDENTIFIER:
		return k+1 < len(toks) && (toks[k+1] == grammar1.ClickHouseLexerLPAREN || toks[k+1] == grammar1.ClickHouseLexerDOT)
	}
	return false
}
