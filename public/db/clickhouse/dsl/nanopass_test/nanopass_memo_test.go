package nanopass_test

import (
	"maps"
	"sync"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
)

// Tests of the text-keyed memo behind Parse, ParseCanonical and Pass.Run
// (ADR-0306). Each test parses text no other test does, since the memo is
// process-wide. Its equivalence with unmemoised parsing is TestPassGolden's:
// the golden file predates the memo and every pass output still matches it.

func TestParseMemoSharesTheResult(t *testing.T) {
	const sql = "SELECT a FROM t -- memo: shares"
	p1, _, _ := nanopass.MemoStats()
	first, err := nanopass.Parse(sql)
	require.NoError(t, err)
	second, err := nanopass.Parse(sql)
	require.NoError(t, err)
	p2, _, _ := nanopass.MemoStats()
	assert.Same(t, first, second)
	assert.Equal(t, p1.Hits+1, p2.Hits)
}

func TestParseMemoKeepsGrammarsApart(t *testing.T) {
	const sql = `SELECT "a" FROM "t" -- memo: grammars`
	g1, err := nanopass.Parse(sql)
	require.NoError(t, err)
	g2, err := nanopass.ParseCanonical(sql)
	require.NoError(t, err)
	assert.NotSame(t, g1, g2)
	assert.IsType(t, &grammar2.QueryStmtContext{}, g2.Tree, "a grammar2 parse was served from the grammar1 memo")
}

func TestParseMemoDoesNotKeepFailures(t *testing.T) {
	const sql = "SELECT FROM WHERE -- memo: failure"
	before, _, _ := nanopass.MemoStats()
	for range 2 {
		_, err := nanopass.Parse(sql)
		require.Error(t, err)
	}
	after, _, _ := nanopass.MemoStats()
	assert.Equal(t, before.Entries, after.Entries)
	assert.Equal(t, before.Hits, after.Hits)
}

func TestParseMemoBudgetZeroDisables(t *testing.T) {
	const sql = "SELECT b FROM t -- memo: disabled"
	nanopass.SetMemoBudget(0)
	t.Cleanup(func() { nanopass.SetMemoBudget(nanopass.DefaultMemoBudget) })
	first, err := nanopass.Parse(sql)
	require.NoError(t, err)
	second, err := nanopass.Parse(sql)
	require.NoError(t, err)
	assert.NotSame(t, first, second)
	p, _, x := nanopass.MemoStats()
	assert.Zero(t, p.Entries)
	assert.Zero(t, x.Entries)
}

// A cached result must not pin the shared DFA (ADR-0084): the parser keeps
// the ATN, so rule names and GetATN still work, and nothing else.
func TestParseResultIsDetachedFromPrediction(t *testing.T) {
	pr, err := nanopass.Parse("SELECT a BETWEEN 1 AND 2 FROM t -- memo: detached")
	require.NoError(t, err)
	assert.NotNil(t, pr.Parser.GetATN())
	assert.Empty(t, pr.Parser.GetParseListeners(), "the LL-island listener holds the simulator")
	assert.NotEmpty(t, antlr.TreesStringTree(pr.Tree, pr.Parser.GetRuleNames(), pr.Parser))
}

func TestParseMemoIsSafeToShareAcrossGoroutines(t *testing.T) {
	const sql = "SELECT x, y + 1 FROM t WHERE z IN (1, 2) -- memo: goroutines"
	want, err := nanopass.Parse(sql)
	require.NoError(t, err)
	wantText := want.TokenStream.GetAllText()
	var wg sync.WaitGroup
	got := make([]string, 16)
	for i := range got {
		wg.Go(func() {
			pr, err := nanopass.Parse(sql)
			if err == nil {
				got[i] = pr.TokenStream.GetAllText() + pr.Tree.GetText()
			}
		})
	}
	wg.Wait()
	for _, g := range got {
		assert.Equal(t, wantText+want.Tree.GetText(), g)
	}
}

// Pass.Run memoises extraction, but a pass writes to the environment it is
// given; the next run of the same text must not see that write.
func TestExtractMemoHandsEachRunItsOwnEnvironment(t *testing.T) {
	const sql = "SET max_threads = 4;\nSELECT {p: UInt8} -- memo: env"
	var seen []map[string]env.Setting
	pass := nanopass.Pass{
		Name: "writesEnv",
		Apply: func(e *env.Environment, body string) (string, error) {
			seen = append(seen, maps.Clone(e.SessionSettings))
			e.SessionSettings["leak"] = env.Setting{Name: "leak", Raw: "1"}
			return body, nil
		},
	}
	// Three runs: the first is a miss, so only the later ones are served from
	// the memo and could see a write an earlier one made.
	for range 3 {
		_, err := pass.Run(sql)
		require.NoError(t, err)
	}
	require.Len(t, seen, 3)
	for i, s := range seen {
		assert.Equal(t, "4", s["max_threads"].Raw)
		_, leaked := s["leak"]
		assert.False(t, leaked, "run %d saw an earlier run's write", i)
	}
}

func TestExtractMemoMatchesExtract(t *testing.T) {
	const sql = "-- header\nSET param_p = 3;\nSET max_threads = 4;\nSELECT {p: UInt8} SETTINGS a = 1 -- memo: extract"
	wantEnv, wantBody, err := env.Extract(sql)
	require.NoError(t, err)
	for range 2 {
		var gotEnv *env.Environment
		var gotBody string
		pass := nanopass.Pass{
			Name: "observesEnv",
			Apply: func(e *env.Environment, body string) (string, error) {
				gotEnv, gotBody = e, body
				return body, nil
			},
		}
		_, err := pass.Run(sql)
		require.NoError(t, err)
		assert.Equal(t, wantEnv, gotEnv)
		assert.Equal(t, wantBody, gotBody)
	}
}
