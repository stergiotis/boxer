//go:build llm_generated_opus46

package nanopass

import (
	"fmt"
	"strings"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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

	// Source is the original input SQL. Token positions reported by ANTLR are
	// rune offsets into this string; [ParseResult.SourceRangeOf] converts them
	// to byte offsets.
	Source string
}

// errorListener collects syntax errors (with positions) during lexing and
// parsing.
type errorListener struct {
	antlr.DefaultErrorListener
	errors []string
}

func (inst *errorListener) SyntaxError(recognizer antlr.Recognizer, offendingSymbol any,
	line, column int, msg string, e antlr.RecognitionException) {
	inst.errors = append(inst.errors, fmt.Sprintf("%d:%d: %s", line, column, msg))
}

// maxReportedErrors caps how many collected diagnostics are rendered into the
// returned error; the total count is always included.
const maxReportedErrors = 5

func (inst *errorListener) buildError(kind string) error {
	n := len(inst.errors)
	shown := inst.errors
	if n > maxReportedErrors {
		shown = shown[:maxReportedErrors]
	}
	// The line:column detail goes into the message itself — eb fields are
	// structured-only and would not surface in Error().
	return eb.Build().
		Int("errorCount", n).
		Errorf("%s: %s", kind, strings.Join(shown, "; "))
}

// Parse parses SQL using Grammar1 (full ClickHouse SELECT surface, no keywordForAlias).
// This is the parser used by all normalization passes.
//
// Both lexer and parser diagnostics are collected: input that fails to lex
// (stray control characters, unterminated strings) is rejected instead of
// being silently dropped from the token stream. The error message includes
// line:column positions for up to five diagnostics.
//
// Trust boundary: the parser is recursive-descent with no depth guard.
// Inputs are expected to be developer-authored queries; pathologically
// nested input (tens of thousands of parentheses) exhausts the goroutine
// stack, which is not recoverable. Do not feed unvetted external input.
func Parse(sql string) (pr *ParseResult, err error) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)

	// Remove default error listeners (which print to stderr), collect instead.
	// The lexer needs its own listener: lexical errors never reach the parser
	// — the offending characters are simply absent from the token stream.
	errListener := &errorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errListener)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errListener)

	tree := parser.QueryStmt()

	if len(errListener.errors) > 0 {
		err = errListener.buildError("syntax error")
		return
	}

	pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
		Source:      sql,
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
// Lexer diagnostics are collected like in [Parse]. The same trust boundary
// applies: no recursion-depth guard.
//
// Used by the AST converter as its input parser.
func ParseCanonical(sql string) (pr *ParseResult, err error) {
	input := antlr.NewInputStream(sql)
	lexer := grammar2.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar2.NewClickHouseParserGrammar2(stream)

	errListener := &errorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(errListener)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(errListener)

	tree := parser.QueryStmt()

	if len(errListener.errors) > 0 {
		err = errListener.buildError("canonical parse failed, non-canonical SQL")
		return
	}

	pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
		Source:      sql,
	}
	return
}
