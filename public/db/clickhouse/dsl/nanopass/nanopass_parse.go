package nanopass

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
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

// InsertStmt returns the INSERT wrapper when this result parsed one
// (ADR-0181 §SD8), nil for a plain SELECT or a grammar2 tree. The wrapper is
// grammar1's unlabeled queryStmt alternative, so the check is one child
// accessor away — passes use it to refuse schema-changing rewrites under a
// wrapper, and hosts to pick statement-kind-aware behaviour (FORMAT
// appending, write gating) without re-parsing.
func (inst *ParseResult) InsertStmt() *grammar1.InsertStmtContext {
	qs, ok := inst.Tree.(*grammar1.QueryStmtContext)
	if !ok {
		return nil
	}
	ins, _ := qs.InsertStmt().(*grammar1.InsertStmtContext)
	return ins
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
		Str("kind", kind).Strs("errors", shown).Errorf("the parser reported errors")
}

// attempt carries one parse attempt's outcome. Both stages of a two-stage
// parse produce one; only the surviving stage's listener is ever consulted.
type attempt struct {
	pr       *ParseResult
	listener *errorListener
}

// parseGrammar1 runs one attempt at the given prediction mode. A fresh lexer,
// token stream and parser per attempt is not an optimisation miss — an ANTLR
// parser cannot be re-run, and the LL stage must re-lex anyway.
func parseGrammar1(sql string, predictionMode int) (a attempt, ok bool) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)

	// Point the parser at the shared bounded DFA cache instead of the grammar's
	// unbounded package global (ADR-0084, ADR-0196 §SD3). release ends the parse
	// and periodically rebuilds the cache if it has grown past MaxDFAStates.
	sim, release := grammar1.SharedDFA.Acquire(parser)
	sim.SetPredictionMode(predictionMode)
	parser.Interpreter = sim
	defer release()

	// Remove default error listeners (which print to stderr), collect instead.
	// The lexer needs its own listener: lexical errors never reach the parser
	// — the offending characters are simply absent from the token stream.
	a.listener = &errorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(a.listener)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(a.listener)

	tree := parser.QueryStmt()
	if len(a.listener.errors) > 0 {
		return a, false
	}
	a.pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
		Source:      sql,
	}
	return a, true
}

// parseGrammar2 is parseGrammar1 against the canonical grammar.
func parseGrammar2(sql string, predictionMode int) (a attempt, ok bool) {
	input := antlr.NewInputStream(sql)
	lexer := grammar2.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar2.NewClickHouseParserGrammar2(stream)

	sim, release := grammar2.SharedDFA.Acquire(parser)
	sim.SetPredictionMode(predictionMode)
	parser.Interpreter = sim
	defer release()

	a.listener = &errorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(a.listener)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(a.listener)

	tree := parser.QueryStmt()
	if len(a.listener.errors) > 0 {
		return a, false
	}
	a.pr = &ParseResult{
		Tree:        tree,
		TokenStream: stream,
		Parser:      parser,
		Source:      sql,
	}
	return a, true
}

// Parse parses SQL using Grammar1 (full ClickHouse SELECT surface, no keywordForAlias).
// This is the parser used by all normalization passes.
//
// Both lexer and parser diagnostics are collected: input that fails to lex
// (stray control characters, unterminated strings) is rejected instead of
// being silently dropped from the token stream. The error message includes
// line:column positions for up to five diagnostics.
//
// Prediction is two-stage (ADR-0196): SLL first, then full-context LL if SLL
// reports anything. The reported diagnostics are always LL's, so a genuine
// syntax error reads exactly as it did before. See [antlr4utils.TwoStage] for
// why the fast path is worth ~80x on a WITH-heavy statement, and why the
// fallback is load-bearing rather than defensive.
//
// Input guards: [CheckInputGuards] runs first, rejecting inputs that would
// drive the recursive-descent parser into pathological regimes (CPU blowup
// on deep parenthesis nesting, stack exhaustion on deep CASE nesting). See
// MaxInputBytes / MaxNestingDepth for the limits and their rationale.
func Parse(sql string) (pr *ParseResult, err error) {
	if err = CheckInputGuards(sql); err != nil {
		return
	}
	syncDFALimits()

	a, ok, fellBack := antlr4utils.TwoStage(func(predictionMode int) (attempt, bool) {
		return parseGrammar1(sql, predictionMode)
	})
	recordTwoStage(&g1Hits, &g1Fallbacks, fellBack)
	if !ok {
		err = a.listener.buildError("syntax error")
		return
	}
	pr = a.pr
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
// Lexer diagnostics are collected like in [Parse], the same input guards
// apply (see [CheckInputGuards]), and prediction is two-stage on the same
// terms. Note that a rejection here is the point of the call rather than a
// failure, so this seam falls back to LL more often than [Parse] does by
// design — a non-canonical statement is refused by both stages.
//
// Used by the AST converter as its input parser.
func ParseCanonical(sql string) (pr *ParseResult, err error) {
	if err = CheckInputGuards(sql); err != nil {
		return
	}
	syncDFALimits()

	a, ok, fellBack := antlr4utils.TwoStage(func(predictionMode int) (attempt, bool) {
		return parseGrammar2(sql, predictionMode)
	})
	recordTwoStage(&g2Hits, &g2Fallbacks, fellBack)
	if !ok {
		err = a.listener.buildError("canonical parse failed, non-canonical SQL")
		return
	}
	pr = a.pr
	return
}
