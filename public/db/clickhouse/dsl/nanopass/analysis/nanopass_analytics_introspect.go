package analysis

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// introspected looks at a statement the grammar did not read and whose
// leading keyword says it reports on something rather than returning its
// rows. It recognises two shapes:
//
//   - a wrapper around a statement — `EXPLAIN [kind] [options] <statement>`
//     and `DESCRIBE [TABLE] (<statement>) [tail]` — for which inner is the
//     statement wrapped, with a DESCRIBE's trailing clauses appended so its
//     SETTINGS are classified with it;
//   - a question about a named object — `EXISTS …` or `DESCRIBE [TABLE]
//     <name>` — for which catalogue is true.
//
// ok is false for anything else, among them a DESCRIBE of a table function
// (it may reach beyond the endpoint, and nothing here reads its arguments),
// an EXPLAIN whose options name a mutating keyword, and a text holding a
// statement separator.
func introspected(sql string) (inner string, catalogue bool, ok bool) {
	if nanopass.CheckInputGuards(sql) != nil {
		return
	}
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	var toks []antlr.Token
	for _, tok := range stream.GetAllTokens() {
		if tok.GetChannel() != antlr.TokenDefaultChannel || tok.GetTokenType() == antlr.TokenEOF {
			continue
		}
		switch tok.GetTokenType() {
		case grammar1.ClickHouseLexerSEMICOLON, grammar1.ClickHouseLexerINTO:
			return
		}
		toks = append(toks, tok)
	}
	if len(toks) < 2 {
		return
	}
	// Token offsets count runes, as the input stream does.
	runes := []rune(sql)
	firstParen := -1
	for i, tok := range toks {
		if tok.GetTokenType() == grammar1.ClickHouseLexerLPAREN {
			firstParen = i
			break
		}
	}
	switch toks[0].GetTokenType() {
	case grammar1.ClickHouseLexerEXISTS:
		return "", firstParen < 0, firstParen < 0
	case grammar1.ClickHouseLexerDESCRIBE, grammar1.ClickHouseLexerDESC:
		at := 1
		if toks[at].GetTokenType() == grammar1.ClickHouseLexerTABLE {
			at++
		}
		if firstParen < 0 {
			return "", true, true
		}
		if firstParen != at {
			return
		}
		depth := 0
		for i := at; i < len(toks); i++ {
			switch toks[i].GetTokenType() {
			case grammar1.ClickHouseLexerLPAREN:
				depth++
			case grammar1.ClickHouseLexerRPAREN:
				depth--
			}
			if depth == 0 {
				inner = string(runes[toks[at].GetStop()+1:toks[i].GetStart()]) + " " + string(runes[toks[i].GetStop()+1:])
				return strings.TrimSpace(inner), false, true
			}
		}
		return
	case grammar1.ClickHouseLexerEXPLAIN:
		for _, tok := range toks[1:] {
			switch tok.GetTokenType() {
			case grammar1.ClickHouseLexerSELECT, grammar1.ClickHouseLexerWITH, grammar1.ClickHouseLexerLPAREN:
				return strings.TrimSpace(string(runes[tok.GetStart():])), false, true
			}
			if _, mutating := mutatingLeadingTokens[tok.GetTokenType()]; mutating {
				return
			}
		}
	}
	return
}
