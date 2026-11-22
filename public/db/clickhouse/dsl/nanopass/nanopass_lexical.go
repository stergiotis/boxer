package nanopass

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
)

// A number written directly against a word — `1A`, `00FROM`, `0b101`,
// `1_000` — is one token to ClickHouse: its lexer reads digits and the word
// characters after them together, as a bare-word identifier (`1A`), or as the
// number the spelling denotes where it has one (`0b101` is 5, `1_000` is
// 1000). The grammars' shared lexer reads a number and then a separate word,
// so the parser sees `1 A`, an aliased 1, and `00 FROM`. A pass would then
// write the two tokens apart (`1"A"`), which ClickHouse runs as a different
// query, and some such statements leave the canonical grammar altogether.
//
// Parse and ParseCanonical refuse the spelling instead. Teaching the lexer
// ClickHouse's rule is the fuller fix; it needs the grammars regenerated.
// deferred: lex digit-led bare words, binary literals and digit separators
// as ClickHouse does, in ClickHouseLexer.g4.

var grammar1NumberTokens = numberTokenSet(
	grammar1.ClickHouseLexerDECIMAL_LITERAL,
	grammar1.ClickHouseLexerOCTAL_LITERAL,
	grammar1.ClickHouseLexerHEXADECIMAL_LITERAL,
	grammar1.ClickHouseLexerFLOATING_LITERAL,
)

var grammar2NumberTokens = numberTokenSet(
	grammar2.ClickHouseLexerDECIMAL_LITERAL,
	grammar2.ClickHouseLexerOCTAL_LITERAL,
	grammar2.ClickHouseLexerHEXADECIMAL_LITERAL,
	grammar2.ClickHouseLexerFLOATING_LITERAL,
)

func numberTokenSet(types ...int) (set map[int]struct{}) {
	set = make(map[int]struct{}, len(types))
	for _, t := range types {
		set[t] = struct{}{}
	}
	return
}

// refuseNumberGluedToWord returns a syntax error, of the kind Parse and
// ParseCanonical report, for the first number token in pr's stream that the
// next token touches and continues with a word character.
func refuseNumberGluedToWord(pr *ParseResult, numberTokens map[int]struct{}, kind string) (err error) {
	pr.TokenStream.Fill()
	tokens := pr.TokenStream.GetAllTokens()
	for i := 0; i+1 < len(tokens); i++ {
		tok, next := tokens[i], tokens[i+1]
		if _, isNumber := numberTokens[tok.GetTokenType()]; !isNumber {
			continue
		}
		if next.GetTokenType() == antlr.TokenEOF || next.GetStart() != tok.GetStop()+1 {
			continue
		}
		text := next.GetText()
		if text == "" || !isWordStartByte(text[0]) {
			continue
		}
		l := &errorListener{}
		l.errors = append(l.errors, fmt.Sprintf("%d:%d: the number %s runs into %s; ClickHouse reads %s%s as one token, "+
			"which this parser does not: put a space between them, or backquote the identifier",
			tok.GetLine(), tok.GetColumn(), tok.GetText(), text, tok.GetText(), text))
		err = l.buildError(kind)
		return
	}
	return
}

// isWordStartByte reports whether c continues a ClickHouse bare word after a
// number: an ASCII letter, an underscore or a dollar sign.
func isWordStartByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
