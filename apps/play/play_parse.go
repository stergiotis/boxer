package play

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// syntaxErrorPos locates the first syntax error grammar1 reported, in ANTLR's
// own coordinates: Line is 1-based, Column is a 0-based RUNE offset within
// that line (antlr.NewInputStream is rune-backed). Zero value means "no
// error"; callers test with Ok.
type syntaxErrorPos struct {
	Line   int
	Column int
	Msg    string
	Ok     bool
}

// firstSyntaxError parses sql via grammar1 with a listener that captures
// (line, column, msg) for the first error. nanopass.Parse uses a private
// listener that drops line/col; we need them for the preview banner and for
// the editor's error underline (ADR-0130 L3), so we reparse.
func firstSyntaxError(sql string) (pos syntaxErrorPos) {
	listener := antlr4utils.NewStoringErrListener(0, 0, 0, 4)
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(listener)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)
	_ = parser.QueryStmt()

	if len(listener.SyntaxErrorsMessage) == 0 {
		return
	}
	return syntaxErrorPos{
		Line:   listener.SyntaxErrorsLine[0],
		Column: listener.SyntaxErrorsColumn[0],
		Msg:    listener.SyntaxErrorsMessage[0],
		Ok:     true,
	}
}

// Error renders the position as the compact "line L:C: msg" the preview
// banner shows.
func (inst syntaxErrorPos) Error() string {
	return fmt.Sprintf("line %d:%d: %s", inst.Line, inst.Column, inst.Msg)
}

// formatSyntaxError returns nil when the SQL parses cleanly, else the first
// error as a [syntaxErrorPos] — which is both an error and the position the
// error underline needs.
func formatSyntaxError(sql string) error {
	if pos := firstSyntaxError(sql); pos.Ok {
		return pos
	}
	return nil
}
