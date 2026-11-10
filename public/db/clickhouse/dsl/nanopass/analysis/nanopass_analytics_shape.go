package analysis

import (
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// shapeLiteral stands for one literal in a statement shape, and shapeRun for
// a comma-separated run of them.
const (
	shapeLiteral = "?"
	shapeRun     = "?,…"
)

// StatementShape is sql with what varies between two sends of the same
// statement taken out (ADR-0367 §SD4): every string and number literal reads
// `?`, a comma-separated run of literals reads `?,…` whatever its length,
// and comments and the layout of whitespace are gone. Two statements a
// program composes from one template with different inlined values have one
// shape.
//
// It works on tokens, not on a parse, so it answers for the statements the
// grammar does not read as well: a DDL statement has a shape. Identifiers
// are kept as written; a statement that interpolates a table name has one
// shape per table.
//
// Total: input past the parser's guards, or holding no token, has the empty
// shape.
func StatementShape(sql string) (shape string) {
	if nanopass.CheckInputGuards(sql) != nil {
		return
	}
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	lexer.RemoveErrorListeners()
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	stream.Fill()
	b := strings.Builder{}
	b.Grow(len(sql))
	// run counts the literals of the current run; pendingComma says a comma
	// followed the last of them and has not been written.
	run := 0
	pendingComma := false
	flush := func() {
		if run == 0 {
			return
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		if run == 1 {
			b.WriteString(shapeLiteral)
		} else {
			b.WriteString(shapeRun)
		}
		if pendingComma {
			b.WriteString(" ,")
		}
		run = 0
		pendingComma = false
	}
	toks := stream.GetAllTokens()
	for i := 0; i < len(toks); i++ {
		tok := toks[i]
		if tok.GetChannel() != antlr.TokenDefaultChannel {
			continue
		}
		switch tok.GetTokenType() {
		case antlr.TokenEOF:
			continue
		case grammar1.ClickHouseLexerSTRING_LITERAL,
			grammar1.ClickHouseLexerDECIMAL_LITERAL,
			grammar1.ClickHouseLexerFLOATING_LITERAL,
			grammar1.ClickHouseLexerHEXADECIMAL_LITERAL,
			grammar1.ClickHouseLexerOCTAL_LITERAL:
			if run > 0 && !pendingComma {
				// Two literals with no comma between them are not a list.
				flush()
			}
			run++
			pendingComma = false
			// The lexer hands a fraction over as number, dot, number; the
			// three are one literal when nothing separates them.
			if i+2 < len(toks) && isNumberToken(tok) &&
				toks[i+1].GetTokenType() == grammar1.ClickHouseLexerDOT && toks[i+1].GetStart() == tok.GetStop()+1 &&
				isNumberToken(toks[i+2]) && toks[i+2].GetStart() == toks[i+1].GetStop()+1 {
				i += 2
			}
			continue
		case grammar1.ClickHouseLexerCOMMA:
			if run > 0 && !pendingComma {
				pendingComma = true
				continue
			}
		}
		flush()
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(tok.GetText())
	}
	flush()
	shape = b.String()
	return
}

func isNumberToken(tok antlr.Token) bool {
	switch tok.GetTokenType() {
	case grammar1.ClickHouseLexerDECIMAL_LITERAL,
		grammar1.ClickHouseLexerFLOATING_LITERAL,
		grammar1.ClickHouseLexerOCTAL_LITERAL:
		return true
	}
	return false
}
