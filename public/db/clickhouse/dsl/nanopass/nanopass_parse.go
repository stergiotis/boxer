//go:build llm_generated_opus46

package nanopass

import (
	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/observability/eh"
)

// ParseResult holds the result of parsing SQL with either Grammar1 or Grammar2.
//
// Tree and Parser are interface-typed to support both grammars. Callers that
// need grammar-specific context types use type assertions:
//
//	root := pr.Tree.(*grammar1.QueryStmtContext)  // for Grammar1 results
//	root := pr.Tree.(*grammar2.QueryStmtContext)  // for Grammar2 results
//
// The TokenStream is shared between both grammars — it's produced by the
// lexer which is identical in both grammar packages.
type ParseResult struct {
	// Tree is the root CST node. Its concrete type depends on which grammar
	// was used for parsing:
	//   - Parse():          *grammar1.QueryStmtContext
	//   - ParseCanonical(): *grammar2.QueryStmtContext
	Tree antlr.ParserRuleContext

	// TokenStream is the lexed token stream including hidden-channel tokens.
	TokenStream *antlr.CommonTokenStream

	// Parser is the ANTLR parser instance used to produce the CST. Useful for
	// accessing rule names and vocabulary during debugging.
	Parser antlr.Parser
}

// errorListener collects syntax errors during parsing.
type errorListener struct {
	*antlr.DefaultErrorListener
	errors []string
}

func (inst *errorListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol interface{},
	line, column int, msg string, e antlr.RecognitionException) {
	inst.errors = append(inst.errors, msg)
}

// Parse parses SQL using Grammar1 (full ClickHouse SELECT surface, no keywordForAlias).
// This is the parser used by all normalization passes.
//
// Returns an error if the SQL contains syntax errors. The error message includes
// the first syntax error from the parser.
func Parse(sql string) (pr *ParseResult, err error) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)

	// Remove default error listeners, add our collector
	errListener := &errorListener{}
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errListener)

	tree := parser.QueryStmt()

	if len(errListener.errors) > 0 {
		err = eh.Errorf("Parse: syntax error: %s", errListener.errors[0])
		return
	}

	pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
	}
	return
}

// ParseCanonical parses SQL using Grammar2 (canonical forms only).
//
// Grammar2 accepts only normalized SQL:
//   - All identifiers double-quoted
//   - No CASE/CAST/DATE/TIMESTAMP/EXTRACT/SUBSTRING/TRIM sugar
//   - No array/tuple literal syntax
//   - No ternary operator
//   - No ==, no OUTER, no comma join
//   - JOIN strictness before direction
//   - USING with parentheses
//
// If the SQL contains any non-canonical form, Grammar2 will reject it with a
// parse error. This serves as structural validation that the normalization
// pipeline is complete.
//
// Used by the AST converter as its input parser.
func ParseCanonical(sql string) (pr *ParseResult, err error) {
	input := antlr.NewInputStream(sql)
	lexer := grammar2.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar2.NewClickHouseParserGrammar2(stream)

	// Remove default error listeners, add our collector
	errListener := &errorListener{}
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errListener)

	tree := parser.QueryStmt()

	if len(errListener.errors) > 0 {
		err = eh.Errorf("ParseCanonical: syntax error (non-canonical SQL?): %s", errListener.errors[0])
		return
	}

	pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
	}
	return
}
