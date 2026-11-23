package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// The aggregate filter, `f(args) FILTER (WHERE c)`, as ClickHouse 26.9 reads
// it (checked with formatQuery and EXPLAIN SYNTAX against a server):
//
//   - it follows a call, parametric or not, with or without DISTINCT, and
//     comes before OVER, never after it; it does not follow a column, a
//     literal, an alias, a lambda call or another FILTER;
//   - WHERE is required and takes one expression (an alias inside it is
//     kept); the keywords are case-insensitive and need no whitespace;
//   - the parser rewrites it to the -If combinator, `fIf(args, c)`, for any
//     call name: `abs(x) FILTER (WHERE c)` parses and fails later as an
//     unknown function absIf, `avg(x) FILTER (WHERE avg(x) > 0)` as a nested
//     aggregate;
//   - `filter` stays a name: a column, a function, `SELECT 1 filter` and
//     `FROM t filter` as bare aliases.

// aggregateFilterAccepted are statements the server parses.
var aggregateFilterAccepted = []string{
	"SELECT quantile(0.05)(L) FILTER (WHERE L BETWEEN 177 AND 1502) AS p05 FROM t",
	"SELECT quantiles(0.05, 0.95)(L) FILTER (WHERE L BETWEEN 177 AND 1502), quantileExactWeighted(0.5)(L, w) FILTER (WHERE w > 0), topK(3, 2, 'counts')(L) FILTER (WHERE L > 1) FROM t",
	"SELECT sum(x) FILTER (WHERE y > 5), avg(x) filter (where y) FROM t",
	"SELECT sum(x)FILTER(WHERE y>5) FROM t",
	"SELECT sum(x) FILTER ( WHERE y ) s FROM t",
	"SELECT sum(x) FILTER /* c */ (WHERE y) FROM t",
	"SELECT count() FILTER (WHERE y), count(*) FILTER (WHERE y), count(DISTINCT x) FILTER (WHERE y) FROM t",
	"SELECT sum(x) FILTER (WHERE y) OVER (ORDER BY z) FROM t",
	"SELECT sum(x) FILTER (WHERE y) OVER w FROM t WINDOW w AS (ORDER BY z)",
	"SELECT sum(sum(x) FILTER (WHERE y)) OVER () FROM t",
	"SELECT sum(x) FILTER (WHERE y IN (SELECT a FROM u)) FROM t",
	"SELECT sum(x) FILTER (WHERE y > (SELECT avg(a) FROM u)) FROM t",
	"SELECT sum(x) FILTER (WHERE y > {p:UInt8}) FROM t",
	"SELECT sum(x) FILTER (WHERE y AS yy) FROM t",
	"SELECT sum(x) FILTER (WHERE y) + 1, -sum(x) FILTER (WHERE y), groupArray(x) FILTER (WHERE y)[1], sum(x) FILTER (WHERE y)::Int8 FROM t",
	"SELECT a FROM t GROUP BY a HAVING sum(x) FILTER (WHERE y) > 0 ORDER BY sum(x) FILTER (WHERE y)",
	"SELECT abs(x) FILTER (WHERE y) FROM t",
}

// aggregateFilterRefused are statements the server refuses as syntax errors.
var aggregateFilterRefused = []string{
	"SELECT sum(x) FILTER (y) FROM t",
	"SELECT sum(x) FILTER (WHERE) FROM t",
	"SELECT sum(x) FILTER (WHERE y, z) FROM t",
	"SELECT sum(x) FILTER (WHERE y) FILTER (WHERE z) FROM t",
	"SELECT sum(x) OVER () FILTER (WHERE y) FROM t",
	"SELECT x FILTER (WHERE y) FROM t",
	"SELECT [1] FILTER (WHERE y) FROM t",
	"SELECT sum(x) AS s FILTER (WHERE y) FROM t",
}

func TestAggregateFilterParsesWhatTheServerAccepts(t *testing.T) {
	for _, sql := range aggregateFilterAccepted {
		_, err := nanopass.Parse(sql)
		assert.NoError(t, err, sql)
	}
	for _, sql := range aggregateFilterRefused {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
	}
}

// TestAggregateFilterHangsOffTheCall: the clause is a child of the call it
// filters, in its own rule, so the call's own parentheses stay what they were.
func TestAggregateFilterHangsOffTheCall(t *testing.T) {
	pr, err := nanopass.Parse("SELECT quantile(0.05)(L) FILTER (WHERE L > 1) FROM t")
	require.NoError(t, err)
	fn, ok := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, isFn := ctx.(*grammar1.ColumnExprFunctionContext)
		return isFn
	}).(*grammar1.ColumnExprFunctionContext)
	require.True(t, ok)
	assert.Len(t, fn.AllLPAREN(), 2, "the parameter and argument lists, not the clause's")
	fc, ok := fn.FilterClause().(*grammar1.FilterClauseContext)
	require.True(t, ok)
	assert.Equal(t, "L>1", fc.ColumnExpr().GetText())

	pr, err = nanopass.Parse("SELECT sum(x) FILTER (WHERE y) OVER (ORDER BY z) FROM t")
	require.NoError(t, err)
	win, ok := nanopass.FindFirst(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, isWin := ctx.(*grammar1.ColumnExprWinFunctionContext)
		return isWin
	}).(*grammar1.ColumnExprWinFunctionContext)
	require.True(t, ok)
	assert.NotNil(t, win.FilterClause())
}

// TestFilterStaysAName: FILTER became a token, and is still a name wherever
// the server reads it as one, with its case kept by canonicalisation.
func TestFilterStaysAName(t *testing.T) {
	for sql, canonical := range map[string]string{
		"SELECT 1 filter":                                `SELECT 1 "filter"`,
		"SELECT 1 AS Filter":                             `SELECT 1 AS "Filter"`,
		"SELECT filter, t.filter FROM t filter":          `SELECT "filter", "t"."filter" FROM "t" "filter"`,
		"SELECT filter(x) FROM t":                        `SELECT "filter"("x") FROM "t"`,
		"WITH filter AS (SELECT 1) SELECT * FROM filter": `WITH "filter" AS (SELECT 1) SELECT * FROM "filter"`,
	} {
		got, err := passes.CanonicalizeFull(100).Run(sql)
		require.NoError(t, err, sql)
		assert.Equal(t, canonical, got, sql)
	}
}
