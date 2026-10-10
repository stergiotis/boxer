package nanopass_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/parsing/antlr4utils"
)

// FastSyncStrategy claims to change how Sync is computed and nothing else:
// the same tree for valid input, and the same diagnostics, in the same order,
// for invalid input — where Sync is what starts recovery. These tests hold it
// to that against antlr.DefaultErrorStrategy on otherwise identical parsers.

// msgListener records every diagnostic with its position.
type msgListener struct {
	antlr.DefaultErrorListener
	msgs []string
}

func (inst *msgListener) SyntaxError(_ antlr.Recognizer, _ any, line, col int, msg string, _ antlr.RecognitionException) {
	inst.msgs = append(inst.msgs, fmt.Sprintf("%d:%d %s", line, col, msg))
}

// parseWithStrategy parses under LL with a private DFA cache. fast selects the
// error strategy; the shared holder is bypassed so the two runs cannot share
// state through it.
func parseWithStrategy(sql string, fast bool, holder *antlr4utils.DFACache) (tree string, msgs []string) {
	lexer := grammar1.NewClickHouseLexer(antlr.NewInputStream(sql))
	parser := grammar1.NewClickHouseParserGrammar1(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	sim, release := holder.Acquire(parser)
	defer release()
	sim.SetPredictionMode(antlr.PredictionModeLL)
	parser.Interpreter = sim
	if !fast {
		parser.SetErrorHandler(antlr.NewDefaultErrorStrategy())
	}
	l := &msgListener{}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(l)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(l)
	t := parser.QueryStmt()
	return antlr.TreesStringTree(t, parser.GetRuleNames(), parser), l.msgs
}

// fastSyncInputs is the prediction corpus and the embedded corpus, each also
// cut short at every few bytes and with a token dropped, so recovery runs from
// many different ATN states.
func fastSyncInputs(t *testing.T) (inputs []string) {
	t.Helper()
	for _, tc := range predictionCorpus() {
		if len(tc.sql) < 2048 {
			inputs = append(inputs, tc.sql)
		}
	}
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)
	for _, e := range entries {
		inputs = append(inputs, e.SQL)
	}
	n := len(inputs)
	for _, sql := range inputs[:n] {
		for cut := 7; cut < len(sql); cut += 11 {
			inputs = append(inputs, sql[:cut])
		}
		fields := strings.Fields(sql)
		for i := 1; i < len(fields); i += 3 {
			inputs = append(inputs, strings.Join(append(append([]string{}, fields[:i]...), fields[i+1:]...), " "))
		}
	}
	inputs = append(inputs,
		"SELECT FROM WHERE",
		"SELECT a,, b FROM t",
		"SELECT a FROM t WHERE (x = 1",
		"SELECT a FROM t GROUP BY",
		"SELECT * FROM t1 JOIN t2 ON",
		"WITH c AS SELECT 1 SELECT a FROM c",
		"SELECT a b c d FROM t",
	)
	return
}

func TestFastSyncMatchesDefault(t *testing.T) {
	var fastHolder, defaultHolder antlr4utils.DFACache
	var invalid int
	for _, sql := range fastSyncInputs(t) {
		dTree, dMsgs := parseWithStrategy(sql, false, &defaultHolder)
		fTree, fMsgs := parseWithStrategy(sql, true, &fastHolder)
		if len(dMsgs) > 0 {
			invalid++
		}
		if !assert.Equal(t, dMsgs, fMsgs, "diagnostics differ for %q", sql) {
			continue
		}
		assert.Equal(t, dTree, fTree, "tree differs for %q", sql)
	}
	t.Logf("compared %d inputs, %d of them invalid", len(fastSyncInputs(t)), invalid)
	assert.Greater(t, invalid, 100, "too few invalid inputs to exercise recovery")
}
