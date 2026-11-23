package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
)

// EXISTS as ClickHouse 26.9 reads it (checked with `clickhouse format` and
// against a server):
//
//   - `EXISTS (query)` and `exists(query)` are one form, in any case, where
//     an expression may stand; the parentheses hold a query — with its own
//     WITH, set operations and SETTINGS — and never a FORMAT clause, an
//     argument list or DISTINCT;
//   - a query in parentheses of its own, `exists((SELECT 1))`, is the same
//     predicate, which is how the server prints every one of them;
//   - it is not a call that takes FILTER or OVER;
//   - `exists` not followed by a parenthesis stays a name.

// existsAccepted are statements the server parses.
var existsAccepted = []string{
	"SELECT exists(SELECT 1 FROM t WHERE x > 5) AS a, EXISTS (SELECT 1 FROM t) AS b",
	"SELECT a FROM t WHERE NOT EXISTS (SELECT 1 FROM u WHERE u.k = t.k)",
	"SELECT a FROM t WHERE exists(SELECT 1 FROM u WHERE u.k = t.k) AND a IN (SELECT a FROM v)",
	"SELECT a FROM t WHERE EXISTS (SELECT 1 FROM u) = 1",
	"SELECT exists(SELECT 1 UNION ALL SELECT 2)",
	"SELECT exists(WITH 1 AS x SELECT x)",
	"SELECT exists(SELECT 1, 2)",
	"SELECT exists(SELECT * FROM t)",
	"SELECT exists(SELECT x)",
	"SELECT exists((SELECT 1)), EXISTS ((SELECT 1)), exists(((SELECT 1)))",
	"SELECT exists((SELECT 1) UNION ALL (SELECT 2))",
	"SELECT exists(SELECT 1 SETTINGS max_threads = 1)",
	"SELECT Exists (SELECT 1), eXiStS(SELECT 1)",
	"SELECT EXISTS/* c */(SELECT 1)",
	"SELECT a FROM t GROUP BY a HAVING EXISTS (SELECT 1 FROM u) ORDER BY exists(SELECT 1 FROM v)",
	"SELECT if(EXISTS (SELECT 1), 1, 0), EXISTS (SELECT 1) + 1, -exists(SELECT 1), exists(SELECT 1)::UInt8, [exists(SELECT 1)]",
	"SELECT exists(SELECT 1 WHERE exists(SELECT 2))",
	"SELECT sum(x) FILTER (WHERE EXISTS (SELECT 1 FROM u)) FROM t",
	"SELECT EXISTS(SELECT 1) AS exists, exists AS e2",
	"SELECT 1 AS exists, exists + 1",
}

// existsRefused are statements the server refuses as syntax errors.
var existsRefused = []string{
	"SELECT exists(SELECT 1) FILTER (WHERE 1)",
	"SELECT exists (SELECT 1) OVER ()",
	"SELECT EXISTS SELECT 1",
	"SELECT exists(SELECT 1 FORMAT TSV)",
	"SELECT exists(SELECT 1, SELECT 2)",
	"SELECT exists(DISTINCT SELECT 1)",
	"SELECT exists(SELECT 1; SELECT 2)",
	"SELECT exists(SELECT 1) (2)",
}

func TestExistsParsesWhatTheServerAccepts(t *testing.T) {
	for _, sql := range existsAccepted {
		_, err := nanopass.Parse(sql)
		assert.NoError(t, err, sql)
	}
	for _, sql := range existsRefused {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
	}
}

// TestExistsHoldsAQuery: the parentheses hold a query, so what it selects
// from is a table. `exists(SELECT * FROM t)` also reads as a call on the
// product of two columns named SELECT and FROM with the alias t, and
// `exists(SELECT * FROM db.t)` as a call on a field of it; the query reading
// wins, in whatever case the word is written.
func TestExistsHoldsAQuery(t *testing.T) {
	for sql, want := range map[string]analysis.TableRef{
		"SELECT exists(SELECT * FROM t)":                        {Table: "t"},
		"SELECT exists(SELECT * FROM db.t)":                     {Database: "db", Table: "t"},
		"SELECT EXISTS (SELECT * FROM db.t)":                    {Database: "db", Table: "t"},
		"SELECT Exists((SELECT * FROM db.t))":                   {Database: "db", Table: "t"},
		"SELECT 1 WHERE NOT exists(SELECT 1 FROM db.t WHERE a)": {Database: "db", Table: "t"},
	} {
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err, sql)
		exists := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			_, is := ctx.(*grammar1.ColumnExprExistsContext)
			return is
		})
		require.Len(t, exists, 1, sql)
		assert.NotNil(t, exists[0].(*grammar1.ColumnExprExistsContext).SelectUnionStmt(), sql)
		assert.Equal(t, []analysis.TableRef{want}, analysis.ExtractTables(pr), sql)

		scopes, err := nanopass.BuildScopes(pr, "")
		require.NoError(t, err, sql)
		require.Len(t, scopes, 1, sql)
		require.Len(t, scopes[0].Subqueries, 1, "the query of an EXISTS is a scope of the statement: %s", sql)
		sub := scopes[0].Subqueries[0]
		require.Len(t, sub.Tables, 1, sql)
		assert.Equal(t, want.Database, sub.Tables[0].Database, sql)
		assert.Equal(t, want.Table, sub.Tables[0].Table, sql)
		assert.Same(t, scopes[0], sub.Parent, sql)
	}
}

// TestExistsStaysAName: without a query in its parentheses the word is the
// name it was — a column, an alias, and a call whose arguments are
// expressions (which the server refuses).
func TestExistsStaysAName(t *testing.T) {
	for _, sql := range []string{
		"SELECT exists, t.exists, 1 AS exists FROM t",
		"SELECT exists(1), exists(a, b), exists()",
		"SELECT exists((SELECT 1), 2)",
	} {
		pr, err := nanopass.Parse(sql)
		require.NoError(t, err, sql)
		assert.Empty(t, nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
			_, is := ctx.(*grammar1.ColumnExprExistsContext)
			return is
		}), sql)
	}
}

// TestExistsSeesTheEnclosingNames: a correlated EXISTS resolves a common
// table expression of the statement around it, as any subquery does.
func TestExistsSeesTheEnclosingNames(t *testing.T) {
	pr, err := nanopass.Parse("WITH c AS (SELECT k FROM db.u) SELECT a FROM t WHERE EXISTS (SELECT 1 FROM c WHERE c.k = t.k)")
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].Subqueries, 1)
	sub := scopes[0].Subqueries[0]
	require.Len(t, sub.Tables, 1)
	assert.True(t, sub.Tables[0].IsCTE, "c inside the EXISTS is the statement's common table expression")
}
