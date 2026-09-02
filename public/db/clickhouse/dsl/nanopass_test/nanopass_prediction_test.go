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
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/observability/eh/eb/ebtest"
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

// TestSLLFallbackIsLoadBearing pins the reason the fallback cannot be dropped.
//
// Each statement below is rejected by SLL alone and parses cleanly under LL —
// all three are `x.y` qualified references, which is where SLL's missing call
// stack actually bites. With naive SLL and no fallback these were live test
// failures, not a hypothetical.
func TestSLLFallbackIsLoadBearing(t *testing.T) {
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"aliased_subquery_in_join", "SELECT * FROM t1 JOIN (SELECT b FROM t2) AS sub ON t1.id = sub.id"},
		{"in_subquery_correlated", "SELECT a FROM t1 WHERE a IN (SELECT 1 FROM t2 WHERE t2.id = t1.id)"},
		{"qualified_join_target", "SELECT * FROM t1 JOIN db2.t2 ON t1.id = t2.id"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, sllClean := parseAtMode(tc.sql, antlr.PredictionModeSLL)
			require.False(t, sllClean,
				"SLL now accepts this: the fixture has stopped witnessing the fallback, "+
					"so find another statement SLL rejects rather than deleting the case")

			llTree, llClean := parseAtMode(tc.sql, antlr.PredictionModeLL)
			require.True(t, llClean)

			before, _ := nanopass.PredictionStats()
			pr, err := nanopass.Parse(tc.sql)
			require.NoError(t, err, "the LL fallback did not rescue an input SLL rejects")
			after, _ := nanopass.PredictionStats()

			assert.Equal(t, llTree,
				antlr.TreesStringTree(pr.Tree, pr.Parser.GetRuleNames(), pr.Parser))
			assert.Greater(t, after.Fallbacks, before.Fallbacks,
				"a fallback happened but PredictionStats did not count it")
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
