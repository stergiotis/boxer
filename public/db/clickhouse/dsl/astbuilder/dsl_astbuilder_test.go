//go:build llm_generated_opus46

package astbuilder

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func toSQL(t *testing.T, sb SelectBuilder) string {
	t.Helper()
	sql, err := sb.ToSQL()
	require.NoError(t, err)
	_, err = nanopass.Parse(sql)
	require.NoError(t, err, "builder SQL not parseable:\n%s", sql)
	return sql
}

// ============================================================================
// Basic queries
// ============================================================================

func TestSelectLiteral(t *testing.T) {
	sql := toSQL(t, Select(Lit(1)))
	assert.Contains(t, sql, "SELECT 1")
}

func TestSelectColumns(t *testing.T) {
	sql := toSQL(t, Select(Col("a"), Col("b")).From("t"))
	assert.Contains(t, sql, "FROM")
}

func TestSelectStar(t *testing.T) {
	sql := toSQL(t, Select(Star()).From("t"))
	assert.Contains(t, sql, "*")
}

func TestSelectDistinct(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Distinct())
	assert.Contains(t, sql, "DISTINCT")
}

// ============================================================================
// Expressions
// ============================================================================

func TestLiterals(t *testing.T) {
	tests := []struct {
		val      interface{}
		contains string
	}{
		{42, "42"},
		{int64(100), "100"},
		{3.14, "3.14"},
		{"hello", "'hello'"},
		{true, "true"},
		{false, "false"},
		{nil, "NULL"},
	}
	for _, tt := range tests {
		sql := toSQL(t, Select(Lit(tt.val)))
		assert.Contains(t, sql, tt.contains)
	}
}

func TestBinaryOps(t *testing.T) {
	tests := []struct {
		name string
		expr E
		op   string
	}{
		{"eq", Col("a").Eq(Lit(1)), "="},
		{"gt", Col("a").Gt(Lit(1)), ">"},
		{"and", Col("a").And(Col("b")), "AND"},
		{"or", Col("a").Or(Col("b")), "OR"},
		{"plus", Col("a").Plus(Lit(1)), "+"},
		{"like", Col("a").Like(Lit("%x%")), "LIKE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql := toSQL(t, Select(tt.expr))
			assert.Contains(t, sql, tt.op)
		})
	}
}

func TestUnaryOps(t *testing.T) {
	sql := toSQL(t, Select(Col("a").Not(), Col("b").Neg()))
	assert.Contains(t, sql, "NOT")
	assert.Contains(t, sql, "-")
}

func TestIsNull(t *testing.T) {
	sql := toSQL(t, Select(Col("a").IsNull(), Col("b").IsNotNull()))
	assert.Contains(t, sql, "IS NULL")
	assert.Contains(t, sql, "IS NOT NULL")
}

func TestBetween(t *testing.T) {
	sql := toSQL(t, Select(Col("a").Between(Lit(1), Lit(10))))
	assert.Contains(t, sql, "BETWEEN")
}

func TestAlias(t *testing.T) {
	sql := toSQL(t, Select(Col("a").As("x")).From("t"))
	assert.Contains(t, sql, "AS x")
}

func TestFunctionCall(t *testing.T) {
	sql := toSQL(t, Select(Func("count", Col("a"))).From("t"))
	assert.Contains(t, sql, "count")
}

func TestFunctionDistinct(t *testing.T) {
	sql := toSQL(t, Select(FuncDistinct("count", Col("a"))).From("t"))
	assert.Contains(t, sql, "DISTINCT")
}

func TestSubqueryExpr(t *testing.T) {
	sql := toSQL(t, Select(Sub(Select(Lit(1)))))
	assert.Contains(t, sql, "(SELECT")
}

func TestIn(t *testing.T) {
	sub := Select(Col("id")).From("t2")
	sql := toSQL(t, Select(Col("a")).From("t").Where(Col("a").In(Sub(sub))))
	assert.Contains(t, sql, "IN")
}

func TestRawExpr(t *testing.T) {
	sql := toSQL(t, Select(Raw("1 + 2")))
	assert.Contains(t, sql, "1 + 2")
}

// ============================================================================
// Clauses
// ============================================================================

func TestWhere(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Where(Col("a").Gt(Lit(1))))
	assert.Contains(t, sql, "WHERE")
}

func TestWhereAccumulates(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").
		Where(Col("a").Gt(Lit(1))).
		Where(Col("b").Lt(Lit(10))))
	assert.Contains(t, sql, "AND")
}

func TestPrewhere(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Prewhere(Col("a").Gt(Lit(0))))
	assert.Contains(t, sql, "PREWHERE")
}

func TestGroupByWithTotals(t *testing.T) {
	sql := toSQL(t, Select(Col("a"), Func("count", Star())).From("t").
		GroupBy(Col("a")).WithTotals())
	assert.Contains(t, sql, "GROUP BY")
	assert.Contains(t, sql, "WITH TOTALS")
}

func TestHaving(t *testing.T) {
	sql := toSQL(t, Select(Col("a"), Func("count", Star()).As("cnt")).From("t").
		GroupBy(Col("a")).Having(Col("cnt").Gt(Lit(5))))
	assert.Contains(t, sql, "HAVING")
}

func TestOrderBy(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").
		OrderBy(Col("a").Desc(), Col("b").Asc()))
	assert.Contains(t, sql, "ORDER BY")
	assert.Contains(t, sql, "DESC")
}

func TestOrderByNulls(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").
		OrderBy(NullsLast(Col("a").Desc())))
	assert.Contains(t, sql, "NULLS LAST")
}

func TestLimit(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Limit(10))
	assert.Contains(t, sql, "LIMIT 10")
}

func TestLimitOffset(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Limit(10).Offset(20))
	assert.Contains(t, sql, "OFFSET")
}

func TestLimitBy(t *testing.T) {
	sql := toSQL(t, Select(Col("a"), Col("b")).From("t").
		OrderBy(Col("a").Asc()).LimitBy(3, Col("b")))
	assert.Contains(t, sql, "BY")
}

func TestSettings(t *testing.T) {
	sql := toSQL(t, Select(Lit(1)).Settings("max_threads", "4"))
	assert.Contains(t, sql, "SETTINGS")
}

func TestFormat(t *testing.T) {
	sql := toSQL(t, Select(Lit(1)).Format("JSONEachRow"))
	assert.Contains(t, sql, "FORMAT JSONEachRow")
}

func TestFinal(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("t").Final())
	assert.Contains(t, sql, "FINAL")
}

func TestArrayJoin(t *testing.T) {
	sql := toSQL(t, Select(Col("x")).From("t").LeftArrayJoin(Col("arr").As("x")))
	assert.Contains(t, sql, "LEFT ARRAY JOIN")
}

// ============================================================================
// FROM variants
// ============================================================================

func TestFromQualified(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).From("db", "t"))
	assert.Contains(t, sql, "db.t")
}

func TestFromAlias(t *testing.T) {
	sql := toSQL(t, Select(Col("x", "a")).FromAlias("t", "x"))
	assert.Contains(t, sql, "AS x")
}

func TestFromSubquery(t *testing.T) {
	sql := toSQL(t, Select(Col("a")).FromSubquery(Select(Lit(1).As("a")), "s"))
	assert.Contains(t, sql, "(SELECT")
	assert.Contains(t, sql, "AS s")
}

// ============================================================================
// UNION / CTE
// ============================================================================

func TestUnionAll(t *testing.T) {
	sql := toSQL(t, Select(Lit(1)).UnionAll(Select(Lit(2))))
	assert.Contains(t, sql, "UNION ALL")
}

func TestCTE(t *testing.T) {
	sql := toSQL(t, Select(Col("x")).From("cte").With("cte", Select(Lit(1).As("x"))))
	assert.Contains(t, sql, "WITH")
}

// ============================================================================
// Composability
// ============================================================================

func TestComposability(t *testing.T) {
	base := Select(Col("a"), Col("b")).From("t")
	sql1 := toSQL(t, base.Where(Col("a").Gt(Lit(1))))
	sql2 := toSQL(t, base.Where(Col("b").Lt(Lit(10))))

	assert.Contains(t, sql1, "> 1")
	assert.NotContains(t, sql1, "< 10")
	assert.Contains(t, sql2, "< 10")
	assert.NotContains(t, sql2, "> 1")
}

func TestConditionalBuilding(t *testing.T) {
	q := Select(Col("a")).From("t")
	cond := true
	if cond {
		q = q.Where(Col("a").Gt(Lit(0)))
	}
	sql := toSQL(t, q)
	assert.Contains(t, sql, "WHERE")
}

// ============================================================================
// Immutability — forking must not corrupt siblings
// ============================================================================

func TestImmutabilityWithTotals(t *testing.T) {
	base := Select(Col("a"), Func("count", Star())).From("t").GroupBy(Col("a"))
	q1, err := base.WithTotals().Build()
	require.NoError(t, err)
	q2, err := base.Build()
	require.NoError(t, err)
	assert.True(t, q1.Body.Head.GroupBy.WithTotals)
	assert.False(t, q2.Body.Head.GroupBy.WithTotals)
}

func TestImmutabilityFinal(t *testing.T) {
	base := Select(Col("a")).From("t")
	q1, err := base.Final().Build()
	require.NoError(t, err)
	q2, err := base.Build()
	require.NoError(t, err)
	assert.True(t, q1.Body.Head.From.Table.Final)
	assert.False(t, q2.Body.Head.From.Table.Final)
}

func TestImmutabilityOffset(t *testing.T) {
	base := Select(Col("a")).From("t").Limit(10)
	q1, err := base.Offset(20).Build()
	require.NoError(t, err)
	q2, err := base.Build()
	require.NoError(t, err)
	assert.NotNil(t, q1.Body.Head.Limit.Limit.Offset)
	assert.Nil(t, q2.Body.Head.Limit.Limit.Offset)
}

func TestImmutabilitySettings(t *testing.T) {
	base := Select(Lit(1)).Settings("k1", "v1")
	q1, err := base.Settings("k2", "v2").Build()
	require.NoError(t, err)
	q2, err := base.Settings("k3", "v3").Build()
	require.NoError(t, err)
	assert.Equal(t, "k2", q1.Body.Head.Settings[1].Key)
	assert.Equal(t, "k3", q2.Body.Head.Settings[1].Key)
}

func TestImmutabilityUnion(t *testing.T) {
	base := Select(Lit(1))
	q1, err := base.UnionAll(Select(Lit(2))).Build()
	require.NoError(t, err)
	q2, err := base.UnionAll(Select(Lit(3))).Build()
	require.NoError(t, err)
	assert.Len(t, q1.Body.Items, 1)
	assert.Len(t, q2.Body.Items, 1)
}

// ============================================================================
// Deferred error handling
// ============================================================================

func TestDeferredErrorFromLit(t *testing.T) {
	// Pass an unsupported type to Lit
	ch := make(chan int)
	_, err := Select(Lit(ch)).ToSQL()
	assert.Error(t, err, "Lit with chan should produce deferred error")
	assert.Contains(t, err.Error(), "Lit")
}

func TestDeferredErrorPropagatesThroughOperators(t *testing.T) {
	ch := make(chan int)
	badExpr := Lit(ch)
	// Error should propagate through .Eq and .And
	expr := Col("a").Eq(badExpr).And(Col("b").Gt(Lit(1)))
	assert.Error(t, expr.Err(), "error should propagate through operators")
}

func TestDeferredErrorPropagatesThroughWhere(t *testing.T) {
	ch := make(chan int)
	_, err := Select(Col("a")).From("t").Where(Col("a").Eq(Lit(ch))).Build()
	assert.Error(t, err)
}

func TestDeferredErrorPropagatesThroughFunc(t *testing.T) {
	ch := make(chan int)
	_, err := Select(Func("f", Lit(ch))).Build()
	assert.Error(t, err)
}

func TestDeferredErrorPropagatesThroughSub(t *testing.T) {
	ch := make(chan int)
	sub := Select(Lit(ch))
	_, err := Select(Col("a")).From("t").Where(Col("a").In(Sub(sub))).Build()
	assert.Error(t, err)
}

func TestDeferredErrorPropagatesThroughUnion(t *testing.T) {
	ch := make(chan int)
	_, err := Select(Lit(1)).UnionAll(Select(Lit(ch))).Build()
	assert.Error(t, err)
}

func TestDeferredErrorPropagatesThroughCTE(t *testing.T) {
	ch := make(chan int)
	_, err := Select(Col("x")).From("cte").With("cte", Select(Lit(ch))).Build()
	assert.Error(t, err)
}

func TestDeferredErrorPropagatesThroughFromSubquery(t *testing.T) {
	ch := make(chan int)
	_, err := Select(Col("a")).FromSubquery(Select(Lit(ch)), "s").Build()
	assert.Error(t, err)
}

func TestNoDeferredErrorOnValidQuery(t *testing.T) {
	_, err := Select(Col("a"), Func("count", Col("b")).As("cnt")).
		From("t").
		Where(Col("a").Gt(Lit(1))).
		GroupBy(Col("a")).
		OrderBy(Col("cnt").Desc()).
		Limit(10).
		Build()
	assert.NoError(t, err)
}

// ============================================================================
// Build() structure
// ============================================================================

func TestBuildStructure(t *testing.T) {
	q, err := Select(Col("a"), Func("count", Col("b")).As("cnt")).
		From("db", "t").
		Where(Col("a").Gt(Lit(1))).
		GroupBy(Col("a")).WithTotals().
		OrderBy(Col("cnt").Desc()).
		Limit(10).Offset(20).
		Settings("max_threads", "4").
		Build()
	require.NoError(t, err)

	assert.Len(t, q.Body.Head.Projection, 2)
	assert.Equal(t, ast.TableKindRef, q.Body.Head.From.Table.TableKind)
	assert.Equal(t, "db", q.Body.Head.From.Table.Database)
	assert.NotNil(t, q.Body.Head.Where)
	assert.True(t, q.Body.Head.GroupBy.WithTotals)
	assert.NotNil(t, q.Body.Head.OrderBy)
	assert.NotNil(t, q.Body.Head.Limit.Limit.Offset)
	assert.Len(t, q.Body.Head.Settings, 1)
}

// ============================================================================
// Complex query
// ============================================================================

func TestComplexQuery(t *testing.T) {
	sql := toSQL(t, Select(
		Col("tenant_id"),
		Func("count", Star()).As("total"),
		Func("sum", Col("amount")).As("revenue"),
	).
		From("db", "orders").
		Prewhere(Col("created").Ge(Raw("toDate('2024-01-01')"))).
		Where(Col("status").Eq(Lit("completed"))).
		GroupBy(Col("tenant_id")).WithTotals().
		Having(Func("count", Star()).Gt(Lit(100))).
		OrderBy(NullsLast(Col("revenue").Desc())).
		Limit(50).
		Settings("max_threads", "8").
		Format("JSONEachRow"))

	assert.Contains(t, sql, "PREWHERE")
	assert.Contains(t, sql, "WITH TOTALS")
	assert.Contains(t, sql, "SETTINGS")
	assert.Contains(t, sql, "FORMAT JSONEachRow")
}

func TestDeferredErrorFromLitCast(t *testing.T) {
	ch := make(chan int)
	_, err := Select(LitCast(ch)).ToSQL()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "LitCast")
}
func TestLitCastStructure(t *testing.T) {
	q, err := Select(LitCast(42)).Build()
	require.NoError(t, err)
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindFunctionCall, e.Kind)
	assert.Equal(t, "CAST", e.Func.Name)
	assert.Len(t, e.Func.Args, 2)
}

func TestLitCastSQL(t *testing.T) {
	sql := toSQL(t, Select(LitCast(42)))
	assert.Contains(t, sql, "CAST")
}
