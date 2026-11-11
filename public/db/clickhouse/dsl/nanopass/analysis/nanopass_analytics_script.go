package analysis

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// SplitStatements cuts a script at its statement separators and returns the
// statements in order, trimmed, without the separators. It works on tokens,
// so a semicolon inside a string literal, a quoted identifier or a comment
// does not cut, and it needs none of the statements to parse.
//
// A text with no separator comes back as its one statement; empty statements
// are dropped. Input past the parser's guards is not split and comes back
// whole.
func SplitStatements(sql string) (stmts []string) {
	if nanopass.CheckInputGuards(sql) != nil {
		return []string{sql}
	}
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	// Token offsets count runes, as the input stream does.
	runes := []rune(sql)
	start := 0
	add := func(end int) {
		if s := strings.TrimSpace(string(runes[start:end])); s != "" {
			stmts = append(stmts, s)
		}
	}
	for _, tok := range stream.GetAllTokens() {
		if tok.GetChannel() != antlr.TokenDefaultChannel || tok.GetTokenType() != grammar1.ClickHouseLexerSEMICOLON {
			continue
		}
		add(tok.GetStart())
		start = tok.GetStop() + 1
	}
	add(len(runes))
	return
}
