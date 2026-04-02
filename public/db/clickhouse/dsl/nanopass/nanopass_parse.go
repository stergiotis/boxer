//go:build llm_generated_opus46

package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/rs/zerolog"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ParseResult holds the CST and token stream for a pass to operate on.
type ParseResult struct {
	Tree        *grammar1.QueryStmtContext
	TokenStream *antlr.CommonTokenStream
	Lexer       *grammar1.ClickHouseLexer
	Parser      *grammar1.ClickHouseParserGrammar1
}

// Parse parses a ClickHouse SELECT statement into a CST.
// Any syntax error is fatal — partial/error-recovering parses are not allowed.
func Parse(sql string) (result *ParseResult, err error) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)

	// Use TokenDefaultChannel for the parser's filtered view.
	// The underlying BufferedTokenStream stores all tokens regardless.
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)

	p := grammar1.NewClickHouseParserGrammar1(stream)

	collector := &errorCollector{}
	p.RemoveErrorListeners()
	p.AddErrorListener(collector)
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(collector)

	tree := p.QueryStmt()

	if len(collector.errors) > 0 {
		err = eh.Errorf("parse error: %s", collector.errors[0])
		return
	}

	result = &ParseResult{
		Tree:        tree.(*grammar1.QueryStmtContext),
		TokenStream: stream,
		Lexer:       lexer,
		Parser:      p,
	}
	return
}

// errorCollector implements antlr.ErrorListener, collecting syntax errors.
type errorCollector struct {
	antlr.DefaultErrorListener
	errors []string
}

func (inst *errorCollector) SyntaxError(_ antlr.Recognizer, _ interface{}, line, col int, msg string, _ antlr.RecognitionException) {
	inst.errors = append(inst.errors, eh.Errorf("line %d:%d %s", line, col, msg).Error())
}

// LogParseResult logs a summary of a parse result at debug level.
func LogParseResult(logger zerolog.Logger, pr *ParseResult) {
	logger.Debug().
		Int("tokenCount", pr.TokenStream.Size()).
		Str("rootRule", pr.Tree.ToStringTree(nil, pr.Parser)).
		Msg("parse result")
}
