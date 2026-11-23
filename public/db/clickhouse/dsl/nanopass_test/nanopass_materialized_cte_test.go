package nanopass_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/grammar1"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// A common table expression marked MATERIALIZED, as ClickHouse 26.9 reads
// it (checked with `clickhouse format` and against a server):
//
//   - the keyword stands after AS and before the parenthesis of the query,
//     `name [(columns)] AS MATERIALIZED (query)`, in any case, per
//     definition, in any WITH that takes a named query;
//   - nowhere else: not ahead of the name or of AS, not twice, not ahead of
//     a scalar, and there is no NOT MATERIALIZED;
//   - `materialized` stays a name: a common table expression, a column and
//     the alias of a scalar WITH item (`WITH 1 AS materialized SELECT …`);
//   - with the setting enable_materialized_cte off, the default, the server
//     reads the keyword and inlines the definition as it does any other;
//     under WITH RECURSIVE it parses and is refused when analysed.

// materializedCTEAccepted are statements the server parses.
var materializedCTEAccepted = []string{
	"WITH a AS MATERIALIZED (SELECT k FROM t), b AS MATERIALIZED (SELECT k, k * 2 AS d FROM a) SELECT count() AS n, sum(d) AS s FROM b",
	"WITH a AS materialized (SELECT 1 AS k) SELECT k FROM a",
	"WITH a AS Materialized(SELECT 1 AS k) SELECT k FROM a",
	"WITH a(k) AS MATERIALIZED (SELECT 1) SELECT k FROM a",
	"WITH a AS MATERIALIZED ((SELECT 1 AS k)) SELECT k FROM a",
	"WITH a AS MATERIALIZED (SELECT 1 AS k UNION ALL SELECT 2) SELECT sum(k) FROM a",
	"WITH a AS MATERIALIZED (SELECT 1 AS k), b AS (SELECT 2 AS k), 3 AS c SELECT a.k + b.k + c FROM a, b",
	"WITH a AS MATERIALIZED (WITH i AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM i) SELECT k FROM a",
	"SELECT * FROM (WITH a AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM a)",
	"SELECT 1 UNION ALL WITH a AS MATERIALIZED (SELECT 2 AS k) SELECT k FROM a",
	"SELECT x IN (WITH a AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM a) FROM t",
	"WITH RECURSIVE a AS MATERIALIZED (SELECT 1 AS k UNION ALL SELECT k + 1 FROM a WHERE k < 3) SELECT sum(k) FROM a",
	"WITH materialized AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM materialized",
	"WITH materialized AS (SELECT 1 AS k) SELECT k FROM materialized",
	"WITH 1 AS MATERIALIZED SELECT materialized",
	"WITH (SELECT 1) AS MATERIALIZED SELECT 1",
	"WITH 1 AS materialized, 2 AS b SELECT materialized + b",
	"INSERT INTO t WITH a AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM a",
}

// materializedCTERefused are statements the server refuses as syntax errors.
var materializedCTERefused = []string{
	"WITH a AS NOT MATERIALIZED (SELECT 1) SELECT * FROM a",
	"WITH a MATERIALIZED AS (SELECT 1) SELECT * FROM a",
	"WITH MATERIALIZED a AS (SELECT 1) SELECT * FROM a",
	"WITH a AS MATERIALIZED MATERIALIZED (SELECT 1) SELECT * FROM a",
	"WITH a AS MATERIALIZED SELECT 1 AS k SELECT k FROM a",
	"WITH a AS MATERIALIZED (1) SELECT a",
	"WITH a AS MATERIALIZED (SELECT 1) AS b SELECT 1",
	"SELECT * FROM (SELECT 1) AS MATERIALIZED (SELECT 2)",
}

func TestMaterializedCTEParsesWhatTheServerAccepts(t *testing.T) {
	for _, sql := range materializedCTEAccepted {
		_, err := nanopass.Parse(sql)
		assert.NoError(t, err, sql)
	}
	for _, sql := range materializedCTERefused {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
	}
}

// TestMaterializedCTEIsADefinition: the marked item is a named query like an
// unmarked one — a definition of the scope with its own body scopes — and
// the keyword is a token of the definition, not a name.
func TestMaterializedCTEIsADefinition(t *testing.T) {
	pr, err := nanopass.Parse("WITH a AS MATERIALIZED (SELECT k FROM db.t), b AS (SELECT k FROM a) SELECT k FROM b")
	require.NoError(t, err)
	named := nanopass.FindAll(pr.Tree, func(ctx antlr.ParserRuleContext) bool {
		_, is := ctx.(*grammar1.NamedQueryContext)
		return is
	})
	require.Len(t, named, 2)
	assert.NotNil(t, named[0].(*grammar1.NamedQueryContext).MATERIALIZED())
	assert.Nil(t, named[1].(*grammar1.NamedQueryContext).MATERIALIZED())

	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	require.Len(t, scopes[0].CTEDefs, 2)
	def, found := scopes[0].ResolveCTE("a")
	require.True(t, found)
	require.Len(t, def.Scopes, 1)
	require.Len(t, def.Scopes[0].Tables, 1)
	assert.Equal(t, "db", def.Scopes[0].Tables[0].Database)
	assert.Equal(t, "t", def.Scopes[0].Tables[0].Table)
	require.Len(t, scopes[0].Tables, 1)
	assert.True(t, scopes[0].Tables[0].IsCTE)
}

// TestMaterializedCTEIsKept: the marker is part of the definition, so the
// canonical form, grammar2 and the canonical AST keep it — upper-cased like
// any keyword — and an alias of that name keeps its spelling.
func TestMaterializedCTEIsKept(t *testing.T) {
	for sql, canonical := range map[string]string{
		"with a as materialized (select k from t), b as (select k from a) select k from b": `WITH "a" AS MATERIALIZED (SELECT "k" FROM "t"), "b" AS (SELECT "k" FROM "a") SELECT "k" FROM "b"`,
		"WITH a(k) AS Materialized (SELECT 1) SELECT k FROM a":                             `WITH "a"("k") AS MATERIALIZED (SELECT 1) SELECT "k" FROM "a"`,
		"WITH materialized AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM materialized":     `WITH "materialized" AS MATERIALIZED (SELECT 1 AS "k") SELECT "k" FROM "materialized"`,
		"WITH 1 AS materialized SELECT materialized":                                       `WITH 1 AS "materialized" SELECT "materialized"`,
		"SELECT * FROM (WITH a AS MATERIALIZED (SELECT 1 AS k) SELECT k FROM a)":           `SELECT * FROM (WITH "a" AS MATERIALIZED (SELECT 1 AS "k") SELECT "k" FROM "a")`,
	} {
		got, err := passes.CanonicalizeFull(100).Run(sql)
		require.NoError(t, err, sql)
		assert.Equal(t, canonical, got, sql)
		again, err := passes.CanonicalizeFull(100).Run(got)
		require.NoError(t, err, sql)
		assert.Equal(t, got, again, "not idempotent: %s", sql)

		pr, err := nanopass.ParseCanonical(got)
		require.NoError(t, err, "canonical form outside grammar2: %s", got)
		query, err := ast.ConvertCSTToAST(pr)
		require.NoError(t, err, got)
		rendered, err := passes.CanonicalizeFull(100).Run(query.ToSQL())
		require.NoError(t, err, query.ToSQL())
		assert.Equal(t, got, rendered, "the AST's rendering reads differently: %s", query.ToSQL())
	}
}

// TestMaterializedCTEInTheAST: each definition carries its own marker.
func TestMaterializedCTEInTheAST(t *testing.T) {
	canonical, err := passes.CanonicalizeFull(100).Run(
		"WITH a AS MATERIALIZED (SELECT 1 AS k), b AS (SELECT k FROM a) SELECT k FROM (WITH c AS MATERIALIZED (SELECT k FROM b) SELECT k FROM c)")
	require.NoError(t, err)
	pr, err := nanopass.ParseCanonical(canonical)
	require.NoError(t, err)
	query, err := ast.ConvertCSTToAST(pr)
	require.NoError(t, err)
	require.Len(t, query.CTEs, 2)
	assert.True(t, query.CTEs[0].Materialized)
	assert.False(t, query.CTEs[1].Materialized)
	assert.Contains(t, query.ToSQL(), "WITH a AS MATERIALIZED (SELECT 1 AS k), b AS (SELECT k FROM a)")
	assert.Contains(t, query.ToSQL(), "(WITH c AS MATERIALIZED (SELECT k FROM b) SELECT k FROM c)")
	assert.Contains(t, query.ToGoCode(), `WithMaterialized("a", `)
	assert.Contains(t, query.ToGoCode(), `With("b", `)
	assert.Contains(t, query.ToGoCode(), `WithMaterialized("c", `)
}
