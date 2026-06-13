package play

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// formatSyntaxError parses sql via grammar1 with a listener that captures
// (line, column, msg) and returns a compact "line L:C: msg" error. Returns
// nil when the SQL parses cleanly. nanopass.Parse uses a private listener
// that drops line/col; we need them for the preview banner so we reparse.
func formatSyntaxError(sql string) error {
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
		return nil
	}
	return fmt.Errorf("line %d:%d: %s",
		listener.SyntaxErrorsLine[0],
		listener.SyntaxErrorsColumn[0],
		listener.SyntaxErrorsMessage[0])
}
