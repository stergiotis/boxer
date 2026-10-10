package nanopass_test

// Two-stage prediction (ADR-0196) is an optimisation that changes how a parse
// is computed, not what it produces — and the way it could silently stop being
// true is the reason these tests exist.
//
// SLL prediction ignores the parser call stack. It is therefore *weaker* than
// the full-context LL prediction ANTLR uses by default: it can report a syntax
// error on input LL accepts, and — the case that would actually be dangerous —
// it can in principle resolve an ambiguous decision to a different alternative,
// yielding a clean parse with a different tree. The first is handled by the LL
// fallback. The second would not be, so it is asserted here rather than
// reasoned about.
//
// Measured over the repo's whole SQL corpus when ADR-0196 was written: 270
// statements, 228 identical trees, 42 SLL-rejected (which fall back), and zero
// disagreements. These tests keep a curated slice of that hermetic, with the
// WITH forms that carry the ambiguity over-represented on purpose.

import (
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// dirtyListener records whether anything was reported.
type dirtyListener struct {
	antlr.DefaultErrorListener
	dirty bool
}

func (inst *dirtyListener) SyntaxError(_ antlr.Recognizer, _ any, _, _ int, _ string, _ antlr.RecognitionException) {
	inst.dirty = true
}

// parseAtMode parses sql at one prediction mode and renders the tree as an
// s-expression.
//
// It builds its own parser and its own private DFA cache rather than going
// through nanopass, so it stays an independent oracle: a bug in the shared
// holder or in the two-stage driver cannot make this agree with itself.
func parseAtMode(sql string, predictionMode int) (sexpr string, clean bool) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar1.NewClickHouseParserGrammar1(stream)

	atn := parser.GetATN()
	dfa := make([]*antlr.DFA, len(atn.DecisionToState))
	for i, ds := range atn.DecisionToState {
		dfa[i] = antlr.NewDFA(ds, i)
	}
	sim := antlr.NewParserATNSimulator(parser, atn, dfa, antlr.NewPredictionContextCache())
	sim.SetPredictionMode(predictionMode)
	parser.Interpreter = sim

	l := &dirtyListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(l)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(l)

	tree := parser.QueryStmt()
	return antlr.TreesStringTree(tree, parser.GetRuleNames(), parser), !l.dirty
}

// predictionCorpus is weighted towards the WITH clause, because that is where
// grammar1's ambiguity lives: `ctes` and `withClause` have byte-identical
// right-hand sides, so a leading WITH is what drives ANTLR into the
// full-context simulation ADR-0196 exists to skip.
func predictionCorpus() []struct {
	name string
	sql  string
} {
	return []struct {
		name string
		sql  string
	}{
		{"no_with", "SELECT a, b FROM t WHERE x = 1"},
		{"cte_single", "WITH c AS (SELECT a FROM t) SELECT a FROM c"},
		{"cte_multi", "WITH c1 AS (SELECT a FROM t), c2 AS (SELECT b FROM u) SELECT c1.a, c2.b FROM c1, c2"},
		{"with_scalar", "WITH (SELECT max(a) FROM t) AS m SELECT m"},
		{"with_scalar_expr", "WITH 1 + 2 AS n SELECT n"},
		// The mixed form is why withItem was unified, and so why the two rules
		// became identical in the first place.
		{"with_mixed", "WITH c AS (SELECT a FROM t), 1 AS n SELECT a, n FROM c"},
		{"with_recursive", "WITH RECURSIVE r AS (SELECT 1 AS n UNION ALL SELECT n + 1 FROM r WHERE n < 10) SELECT n FROM r"},
		{"with_nested", "WITH outer_c AS (WITH inner_c AS (SELECT a FROM t) SELECT a FROM inner_c) SELECT a FROM outer_c"},
		// A leading WITH scopes over the whole union — the constraint that
		// keeps ctes at query level and so keeps the ambiguity reachable.
		{"with_over_union", "WITH c AS (SELECT a FROM t) SELECT a FROM c UNION ALL SELECT b FROM u"},
		{"with_in_paren_arm", "(WITH c AS (SELECT a FROM t) SELECT a FROM c) UNION ALL (SELECT b FROM u)"},
		{"with_settings", "WITH c AS (SELECT a FROM t) SELECT a FROM c SETTINGS max_threads = 4"},
		{"bench_small", benchSmallSQL},
		{"bench_medium", benchMediumSQL},
		{"bench_large", benchLargeSQL},
		{"applet_9kb", runtimeTimelineAppletSQL},
	}
}

// TestTwoStagePreservesTheTree is the contract: whatever nanopass.Parse returns
// must be what a full-context LL parse would have returned.
func TestTwoStagePreservesTheTree(t *testing.T) {
	for _, tc := range predictionCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			llTree, clean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, clean, "fixture does not parse under LL; fix the fixture, not the parser")

			pr, err := nanopass.Parse(tc.sql)
			require.NoError(t, err)

			got := antlr.TreesStringTree(pr.Tree, pr.Parser.GetRuleNames(), pr.Parser)
			assert.Equal(t, llTree, got,
				"two-stage parse produced a different tree than full-context LL")
		})
	}
}

// TestSLLNeverDisagreesWhenItSucceeds is the narrower, sharper claim. The LL
// fallback rescues inputs SLL *rejects*; nothing rescues an input SLL accepts
// with the wrong tree, so that case must simply not occur.
func TestSLLNeverDisagreesWhenItSucceeds(t *testing.T) {
	var accepted, rejected int
	for _, tc := range predictionCorpus() {
		t.Run(tc.name, func(t *testing.T) {
			llTree, llClean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, llClean)

			sllTree, sllClean := parseAtMode(tc.sql, antlr.PredictionModeSLL)
			if !sllClean {
				rejected++
				t.Skip("SLL rejects this input; the LL fallback covers it")
			}
			accepted++
			assert.Equal(t, llTree, sllTree,
				"SLL accepted this input but built a different tree than LL — "+
					"the LL fallback cannot catch this, see ADR-0196 §Consequences")
		})
	}
	t.Logf("SLL accepted %d, rejected %d", accepted, rejected)
}

// llIslandFixtures are statements plain SLL rejects and LL accepts — one or
// more per island (ADR-0305). Before the islands, each of them sent
// its statement through the LL fallback.
var llIslandFixtures = []struct {
	name string
	sql  string
}{
	{"cast_as_type", "SELECT CAST(a AS UInt64) FROM t"},
	{"cast_nested", "SELECT CAST(CAST(1 AS UInt32) AS UInt64)"},
	{"between_and", "SELECT a FROM t WHERE a BETWEEN 1 AND 10"},
	{"between_then_format", "SELECT a FROM t WHERE a BETWEEN 1 AND 10 FORMAT CSV"},
	{"between_parenthesised_low", "SELECT a BETWEEN (b AND c) AND d FROM t"},
	{"view_of_select", "SELECT * FROM view(SELECT * FROM numbers(3))"},
	{"bench_medium", benchMediumSQL},
	{"bench_large", benchLargeSQL},
}

// parseWithIslands parses under SLL with the grammar's LL islands, through a
// private DFA cache, so it stays independent of the shared holder.
func parseWithIslands(sql string) (sexpr string, clean bool) {
	input := antlr.NewInputStream(sql)
	lexer := grammar1.NewClickHouseLexer(input)
	parser := grammar1.NewClickHouseParserGrammar1(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	var holder antlr4utils.DFACache
	holder.SetLLIslands(grammar1.LLIsland)
	sim, release := holder.AcquireMode(parser, antlr.PredictionModeSLL)
	defer release()
	parser.Interpreter = sim
	l := &dirtyListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(l)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(l)
	tree := parser.QueryStmt()
	return antlr.TreesStringTree(tree, parser.GetRuleNames(), parser), !l.dirty
}

// TestLLIslandsAreLoadBearing: each fixture is rejected by plain SLL, so
// without the islands it would fall back; with them, nanopass.Parse accepts it
// in stage one and returns LL's tree.
func TestLLIslandsAreLoadBearing(t *testing.T) {
	// A memoised repeat parses nothing and counts nothing (ADR-0306), which
	// would pass the fallback assertion below without testing it.
	nanopass.SetMemoBudget(0)
	t.Cleanup(func() { nanopass.SetMemoBudget(nanopass.DefaultMemoBudget) })
	for _, tc := range llIslandFixtures {
		t.Run(tc.name, func(t *testing.T) {
			_, sllClean := parseAtMode(tc.sql, antlr.PredictionModeSLL)
			require.False(t, sllClean,
				"plain SLL now accepts this: the fixture has stopped witnessing an island, "+
					"so find another statement plain SLL rejects rather than deleting the case")

			llTree, llClean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, llClean)

			before, _ := nanopass.PredictionStats()
			pr, err := nanopass.Parse(tc.sql)
			require.NoError(t, err)
			after, _ := nanopass.PredictionStats()

			assert.Equal(t, llTree,
				antlr.TreesStringTree(pr.Tree, pr.Parser.GetRuleNames(), pr.Parser))
			assert.Equal(t, before.Fallbacks, after.Fallbacks,
				"the statement fell back to LL: an island no longer covers it")
			assert.Equal(t, before.Hits+1, after.Hits, "the statement was not parsed")
		})
	}
}

// TestLLIslandsMatchLL is the contract that makes the islands safe: wherever
// LL accepts, SLL with islands must accept too and build the same tree. Over
// every statement the test suite parses this held without exception when the
// islands were introduced; the fixtures here keep a hermetic slice of it.
func TestLLIslandsMatchLL(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	var sqls []string
	for _, e := range entries {
		sqls = append(sqls, e.SQL)
	}
	for _, tc := range predictionCorpus() {
		sqls = append(sqls, tc.sql)
	}
	for _, tc := range goldenExtraInputs {
		sqls = append(sqls, tc.sql)
	}
	for _, tc := range llIslandFixtures {
		sqls = append(sqls, tc.sql)
	}
	var compared int
	for _, sql := range sqls {
		llTree, llClean := parseAtMode(sql, antlr.PredictionModeLL)
		if !llClean {
			continue
		}
		compared++
		islTree, islClean := parseWithIslands(sql)
		if assert.True(t, islClean, "SLL with islands rejects input LL accepts: %q", sql) {
			assert.Equal(t, llTree, islTree, "SLL with islands built a different tree: %q", sql)
		}
	}
	t.Logf("compared %d statements", compared)
}

// TestSLLAcceptsQualifiedNames pins the columnQualifier repair (ADR-0304).
//
// When the column qualifier was a tableIdentifier, SLL left that rule through
// every place it is invoked — FROM lists, JOIN targets, INSERT targets — so
// `t.c` followed by anything that can follow a table (`,` `)` WHERE JOIN EOF …)
// was read as `db.table` and the parse fell back to LL. That was 97% of all
// SLL rejections over the test-suite corpus. Each statement here must be
// accepted by SLL and give LL's tree.
func TestSLLAcceptsQualifiedNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"aliased_subquery_in_join", "SELECT * FROM t1 JOIN (SELECT b FROM t2) AS sub ON t1.id = sub.id"},
		{"in_subquery_correlated", "SELECT a FROM t1 WHERE a IN (SELECT 1 FROM t2 WHERE t2.id = t1.id)"},
		{"qualified_join_target", "SELECT * FROM t1 JOIN db2.t2 ON t1.id = t2.id"},
		{"qualified_before_comma", "SELECT t.a, t.b FROM t"},
		{"qualified_before_paren", "SELECT f(t.a) FROM t"},
		{"qualified_at_eof", "SELECT a FROM t WHERE t.a = u.b"},
		{"qualified_before_group", "SELECT t.a FROM t WHERE x = t.a GROUP BY t.a ORDER BY t.a LIMIT 1"},
		{"three_part", "SELECT db.t.c, db.t.c AS d FROM db.t"},
		{"nested_field", "SELECT t.n.f, n.f FROM t"},
		{"columns_qualifier", "SELECT columns.name FROM system.columns"},
		{"param_qualifier", "SELECT {t:Identifier}.a FROM {t:Identifier}"},
		{"qualified_star", "SELECT t.*, db.t.* FROM db.t AS t"},
		// benchMediumSQL without its BETWEEN, which SLL rejects for an
		// unrelated reason (TestSLLFallbackIsLoadBearing).
		{"medium_without_between", strings.Replace(benchMediumSQL, "o.amount BETWEEN 10 AND 1000", "o.amount >= 10", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			llTree, llClean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, llClean, "fixture does not parse under LL; fix the fixture, not the parser")

			sllTree, sllClean := parseAtMode(tc.sql, antlr.PredictionModeSLL)
			require.True(t, sllClean, "SLL rejects a qualified name again: the column qualifier "+
				"must stay a rule only DOT can follow (ADR-0304)")
			assert.Equal(t, llTree, sllTree)
		})
	}
}

// TestSyntaxErrorsStillReport checks that two-stage parsing did not turn a
// genuine syntax error into a silent success, and that the message is LL's.
func TestSyntaxErrorsStillReport(t *testing.T) {
	_, err := nanopass.Parse("SELECT FROM WHERE")
	require.Error(t, err)
	assert.Contains(t, ebtest.Text(t, err), "syntax error")
}
