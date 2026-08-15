package nanopass_test

// The ADR-0181 §SD8 (Update 2026-08-15) parse corpus: the INSERT wrapper the
// M0 port admits, the out-of-scope forms it must keep rejecting, and the
// pipeline entry's view of the wrapper (M1). The corpus is the pin against
// the upstream utils/antlr lineage: a regeneration or a later grammar edit
// that widens or narrows the admitted set fails here first, with the
// offending statement in the failure.

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

// countingErrorListener collects nothing but the number of syntax errors —
// these tests assert acceptance, not messages.
type countingErrorListener struct {
	antlr.DefaultErrorListener
	n int
}

func (inst *countingErrorListener) SyntaxError(_ antlr.Recognizer, _ any, _, _ int, _ string, _ antlr.RecognitionException) {
	inst.n++
}

func parseGrammar1Stmt(sql string) (tree grammar1.IQueryStmtContext, syntaxErrors int) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)
	el := &countingErrorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(el)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(el)
	tree = parser.QueryStmt()
	syntaxErrors = el.n
	return
}

func parseGrammar2Stmt(sql string) (syntaxErrors int) {
	lexer := grammar2.NewClickHouseLexer(antlr.NewInputStream(sql))
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar2.NewClickHouseParserGrammar2(stream)
	el := &countingErrorListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(el)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(el)
	parser.QueryStmt()
	return el.n
}

// TestInsertWrapperGrammar1Accepts pins the admitted set: a SELECT source,
// an optional column list (physical leeway names included — they are just
// backquoted identifiers), the upstream TABLE noise word, qualified and
// parametrised targets, WITH and UNION inside the source.
func TestInsertWrapperGrammar1Accepts(t *testing.T) {
	corpus := []string{
		"INSERT INTO t SELECT 1",
		"INSERT INTO t SELECT 1;",
		"INSERT INTO db.t SELECT * FROM src",
		"INSERT INTO TABLE t SELECT 1",
		"INSERT INTO t (a, b) SELECT a, b FROM src",
		"INSERT INTO t (`tv:symbol:value:val:s:124::I:0::data`) SELECT `symbol:value` FROM src",
		"INSERT INTO t SELECT x FROM src WHERE y > 0 ORDER BY x LIMIT 10",
		"INSERT INTO t SELECT 1 UNION ALL SELECT 2",
		"INSERT INTO t WITH c AS (SELECT 1) SELECT * FROM c",
		"INSERT INTO {target:Identifier} SELECT 1",
	}
	for _, sql := range corpus {
		tree, errs := parseGrammar1Stmt(sql)
		require.Zerof(t, errs, "grammar1 must accept: %s", sql)
		// The wrapper is the unlabeled second alternative: same context
		// type, Query nil, InsertStmt set — the shape the M1 pass work and
		// the Parse guard both key on.
		require.NotNilf(t, tree.InsertStmt(), "InsertStmt child expected: %s", sql)
		require.Nilf(t, tree.Query(), "Query child must be absent under the wrapper: %s", sql)
	}
}

// TestInsertWrapperGrammar1Rejects pins the out-of-scope forms of the §SD8
// decision: data-carrying clauses (VALUES, FORMAT), the FUNCTION target arm,
// CTAS, and a trailing FORMAT on the SELECT source (the upstream lineage has
// none there either — a host appends FORMAT after the pipeline, and M3 makes
// that appending statement-kind-aware).
func TestInsertWrapperGrammar1Rejects(t *testing.T) {
	corpus := []string{
		"INSERT INTO t VALUES (1)",
		"INSERT INTO t FORMAT TSV",
		"INSERT INTO t (a) FORMAT JSONEachRow",
		"INSERT INTO FUNCTION remote('h', d.t) SELECT 1",
		"CREATE TABLE t ENGINE = Memory AS SELECT 1",
		"INSERT INTO t SELECT 1 FORMAT TSV",
		"INSERT INTO t",
	}
	for _, sql := range corpus {
		_, errs := parseGrammar1Stmt(sql)
		require.Positivef(t, errs, "grammar1 must reject: %s", sql)
	}
}

// TestInsertWrapperGrammar2Mirror pins the canonical mirror: quoted
// identifiers, no TABLE noise word (canonical form keeps one spelling per
// statement).
func TestInsertWrapperGrammar2Mirror(t *testing.T) {
	accepted := []string{
		`INSERT INTO "t" SELECT 1`,
		`INSERT INTO "db"."t" ("a", "b") SELECT "a", "b" FROM "db"."src"`,
		`INSERT INTO "t" ("tv:symbol:value:val:s:124::I:0::data") SELECT "x" FROM "src"`,
	}
	for _, sql := range accepted {
		require.Zerof(t, parseGrammar2Stmt(sql), "grammar2 must accept: %s", sql)
	}
	rejected := []string{
		`INSERT INTO TABLE "t" SELECT 1`,
		`INSERT INTO "t" VALUES (1)`,
	}
	for _, sql := range rejected {
		require.Positivef(t, parseGrammar2Stmt(sql), "grammar2 must reject: %s", sql)
	}
}

// TestInsertWrapperParsesAtEntry pins M1's flip of the M0 guard: the
// pipeline entry accepts the wrapper and exposes it via
// ParseResult.InsertStmt — the accessor passes key their refusals on and
// hosts key statement-kind-aware behaviour on.
func TestInsertWrapperParsesAtEntry(t *testing.T) {
	pr, err := nanopass.Parse("INSERT INTO t SELECT 1")
	require.NoError(t, err)
	require.NotNil(t, pr.InsertStmt())

	pr, err = nanopass.Parse("SELECT 1")
	require.NoError(t, err)
	require.Nil(t, pr.InsertStmt())
}
