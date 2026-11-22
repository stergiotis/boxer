package analysis

import (
	"strings"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// NestedBlockComment reports the first block comment of pr, as boxer's
// lexer read it, whose text opens another block comment after its own
// opening `/*`.
//
// The lexer ends a block comment at the first `*/`; ClickHouse nests block
// comments and ends one only when every `/*` inside it is closed. A
// statement with such a comment is therefore one the server may read
// differently from its parse: text the grammar took for code can be a
// comment to the server, and text the grammar took for a comment can be
// code. Where no comment token contains an inner `/*`, the server's nesting
// depth never passes one, so both lexers end every block comment at the
// same `*/`; strings and quoted identifiers that contain `/*` are tokens of
// their own, not comments, and are not looked at.
//
// The classifiers that decide a read — [ClassifyStatementKind] and
// [ClassifyQuerySecurity] — refuse a read on such a statement. src is the
// comment's byte range in pr.Source.
func NestedBlockComment(pr *nanopass.ParseResult) (src nanopass.SourceRange, found bool) {
	if pr == nil || pr.TokenStream == nil {
		return
	}
	return nestedBlockCommentAmong(pr.Source, pr.TokenStream.GetAllTokens())
}

// nestedBlockCommentIn is [NestedBlockComment] over sql the grammar did not
// parse, lexed with grammar1's lexer; lexical errors are ignored, since the
// tokens the lexer did produce are what a keyword-level decision reads.
func nestedBlockCommentIn(sql string) (src nanopass.SourceRange, found bool) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	return nestedBlockCommentAmong(sql, lexer.GetAllTokens())
}

// nestedBlockCommentAmong finds the first hidden-channel token of tokens
// that is a block comment holding an inner `/*`. A block comment is told by
// its text, which no other hidden token starts with, so the check holds for
// either grammar's token types.
func nestedBlockCommentAmong(source string, tokens []antlr.Token) (src nanopass.SourceRange, found bool) {
	for _, t := range tokens {
		if t.GetChannel() == antlr.TokenDefaultChannel {
			continue
		}
		text := t.GetText()
		if len(text) >= 4 && strings.HasPrefix(text, "/*") && strings.Contains(text[2:], "/*") {
			return runeRange(source, t.GetStart(), t.GetStop()), true
		}
	}
	return
}

// runeRange converts the inclusive rune offsets ANTLR reports into a
// half-open byte range of source; an offset outside it yields an empty
// range.
func runeRange(source string, startRune int, stopRune int) (r nanopass.SourceRange) {
	if startRune < 0 || stopRune < startRune {
		return
	}
	runeIdx := 0
	for byteIdx := range source {
		if runeIdx == startRune {
			r.Start = byteIdx
		}
		if runeIdx == stopRune {
			_, size := utf8.DecodeRuneInString(source[byteIdx:])
			r.End = byteIdx + size
			return
		}
		runeIdx++
	}
	return nanopass.SourceRange{}
}
