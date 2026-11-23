package nanopass_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
)

// Several ARRAY JOIN clauses on one FROM, as ClickHouse 26.9 reads them
// (checked with `clickhouse format` and `clickhouse local`):
//
//   - a FROM takes any number of them, each plain, LEFT or INNER, each with
//     its own list of expressions; an alias in the list needs AS;
//   - each unfolds over the rows the one before it made, so two clauses
//     over arrays of 3 and 2 elements give 6 rows, where one clause listing
//     both walks them in step and wants equal lengths;
//   - there is no RIGHT or GLOBAL ARRAY JOIN and none without a FROM.
//
// The server also takes a JOIN after an ARRAY JOIN
// (`FROM t ARRAY JOIN a AS x JOIN u ON …`). This grammar does not: its
// JOINs all stand in the fromClause, ahead of the first ARRAY JOIN.

// arrayJoinsAccepted are statements the server parses.
var arrayJoinsAccepted = []string{
	"SELECT x, T, tau FROM t ARRAY JOIN [1.0, 1.5, 2.0] AS T ARRAY JOIN [0.5, 0.7] AS tau",
	"SELECT x FROM t LEFT ARRAY JOIN a AS x ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x LEFT ARRAY JOIN b AS y",
	"SELECT x FROM t INNER ARRAY JOIN a AS x INNER ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x, b AS y ARRAY JOIN c AS z",
	"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN b AS y ARRAY JOIN c AS z",
	"SELECT x FROM t JOIN u ON u.k = t.k ARRAY JOIN a AS x ARRAY JOIN b AS y",
	"SELECT x FROM t, u ARRAY JOIN a AS x ARRAY JOIN b AS y",
	"SELECT x FROM t FINAL ARRAY JOIN a AS x ARRAY JOIN b AS y",
	"SELECT x FROM (SELECT 1) ARRAY JOIN [1] AS x ARRAY JOIN [2] AS y",
	"SELECT x FROM t AS tt ARRAY JOIN a AS x ARRAY JOIN (SELECT [1]) AS y",
	"SELECT x FROM t ARRAY JOIN arrayMap(i -> i IN (SELECT k FROM u), a) AS x ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN b AS y PREWHERE p WHERE x > 1 GROUP BY x",
	"SELECT x FROM t array join a AS x left array join b AS y",
}

// arrayJoinsRefused are statements the server refuses as syntax errors.
var arrayJoinsRefused = []string{
	"SELECT x FROM t ARRAY JOIN a AS x ARRAY JOIN",
	"SELECT x FROM t ARRAY JOIN a AS x RIGHT ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x GLOBAL ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x LEFT INNER ARRAY JOIN b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x ARRAY b AS y",
	"SELECT x FROM t ARRAY JOIN a AS x WHERE x > 1 ARRAY JOIN b AS y",
	"SELECT x FROM t WINDOW w AS () ARRAY JOIN a AS x ARRAY JOIN b AS y",
}

// arrayJoinThenJoin are statements the server parses and this grammar
// refuses: a JOIN after an ARRAY JOIN.
var arrayJoinThenJoin = []string{
	"SELECT x FROM t ARRAY JOIN a AS x JOIN u ON u.k = t.k",
	"SELECT x FROM t ARRAY JOIN a AS x CROSS JOIN u",
	"SELECT x FROM t JOIN u ON u.k = t.k ARRAY JOIN a AS x JOIN v ON v.k = t.k",
}

func TestArrayJoinsParseWhatTheServerAccepts(t *testing.T) {
	for _, sql := range arrayJoinsAccepted {
		_, err := nanopass.Parse(sql)
		assert.NoError(t, err, sql)
	}
	for _, sql := range arrayJoinsRefused {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
	}
	for _, sql := range arrayJoinThenJoin {
		_, err := nanopass.Parse(sql)
		assert.Error(t, err, sql)
	}
}

// TestArrayJoinsAreKeptApart: the clauses stay separate clauses in their
// order through the canonical form, grammar2 and the canonical AST, because
// two clauses and one clause with both lists are different statements.
func TestArrayJoinsAreKeptApart(t *testing.T) {
	sql := "select x, y, z from t left array join a as x, b as y array join [1, 2] as z inner array join c as w where x > 1"
	canonical, err := passes.CanonicalizeFull(100).Run(sql)
	require.NoError(t, err)
	assert.Equal(t, `SELECT "x", "y", "z" FROM "t" LEFT ARRAY JOIN "a" AS "x", "b" AS "y" ARRAY JOIN "array"(1, 2) AS "z" INNER ARRAY JOIN "c" AS "w" WHERE "x" > 1`, canonical)
	again, err := passes.CanonicalizeFull(100).Run(canonical)
	require.NoError(t, err)
	assert.Equal(t, canonical, again)

	pr, err := nanopass.Parse(sql)
	require.NoError(t, err)
	scopes, err := nanopass.BuildScopes(pr, "")
	require.NoError(t, err)
	require.Len(t, scopes, 1)
	assert.Len(t, scopes[0].Node.AllArrayJoinClause(), 3)

	cpr, err := nanopass.ParseCanonical(canonical)
	require.NoError(t, err, "canonical form outside grammar2")
	query, err := ast.ConvertCSTToAST(cpr)
	require.NoError(t, err)
	clauses := query.Body.Head.ArrayJoins()
	require.Len(t, clauses, 3)
	assert.Equal(t, ast.ArrayJoinLeft, clauses[0].Kind)
	assert.Len(t, clauses[0].Exprs, 2)
	assert.Len(t, clauses[1].Exprs, 1)
	assert.Equal(t, ast.ArrayJoinInner, clauses[2].Kind)
	rendered, err := passes.CanonicalizeFull(100).Run(query.ToSQL())
	require.NoError(t, err, query.ToSQL())
	assert.Equal(t, canonical, rendered, "the AST's rendering reads differently: %s", query.ToSQL())
	code := query.ToGoCode()
	assert.Contains(t, code, "LeftArrayJoin(")
	assert.Regexp(t, `(?s)LeftArrayJoin\(.*ArrayJoin\(.*ArrayJoin\(`, code)
}
