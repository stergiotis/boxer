//go:build llm_generated_opus46

package ast_test

import (
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/ast"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// normalizeAndConvert runs the full pipeline then converts to AST.
func normalizeAndConvert(t *testing.T, sql string) ast.Query {
	t.Helper()
	normalize := passes.CanonicalizeFull(10)
	normalized, err := normalize(sql)
	require.NoError(t, err, "pipeline failed for: %s", sql)
	pr, err := nanopass.ParseCanonical(normalized)
	require.NoError(t, err, "ParseCanonical failed for: %s", normalized)
	query, err := ast.ConvertCSTToAST(pr)
	require.NoError(t, err, "ConvertCSTToAST failed for: %s", normalized)
	return query
}

// ============================================================================
// Basic round-trip: SQL → normalize → Grammar2 parse → AST → no error
// ============================================================================

func TestConvertBasicSelect(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a, b FROM t WHERE a > 1")
	assert.Len(t, q.Body.Head.Projection, 2)
	require.NotNil(t, q.Body.Head.From)
	require.NotNil(t, q.Body.Head.Where)
}

func TestConvertSelectStar(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT * FROM t")
	require.Len(t, q.Body.Head.Projection, 1)
	assert.Equal(t, ast.KindAsterisk, q.Body.Head.Projection[0].Kind)
}

func TestConvertSelectLiterals(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1, 'hello', NULL, true, false")
	require.Len(t, q.Body.Head.Projection, 5)
	for _, expr := range q.Body.Head.Projection {
		assert.Equal(t, ast.KindLiteral, expr.Kind)
		assert.NotNil(t, expr.Literal)
	}
}

// ============================================================================
// Expression kinds
// ============================================================================

func TestConvertFunctionCall(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT count(a) FROM t")
	require.Len(t, q.Body.Head.Projection, 1)
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindFunctionCall, e.Kind)
	assert.Equal(t, "count", e.Func.Name)
	assert.Len(t, e.Func.Args, 1)
}

func TestConvertFunctionDistinct(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT count(DISTINCT a) FROM t")
	e := q.Body.Head.Projection[0]
	assert.True(t, e.Func.Distinct)
}

func TestConvertBinaryOps(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a + b, a * b, a = b, a AND b FROM t")
	ops := []ast.BinaryOpE{ast.BinOpPlus, ast.BinOpMultiply, ast.BinOpEq, ast.BinOpAnd}
	for i, op := range ops {
		assert.Equal(t, ast.KindBinary, q.Body.Head.Projection[i].Kind)
		assert.Equal(t, op, q.Body.Head.Projection[i].Binary.Op)
	}
}

func TestConvertUnaryOps(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT -a, NOT b FROM t")
	assert.Equal(t, ast.UnaryOpNegate, q.Body.Head.Projection[0].Unary.Op)
	assert.Equal(t, ast.UnaryOpNot, q.Body.Head.Projection[1].Unary.Op)
}

func TestConvertIsNull(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a IS NULL, b IS NOT NULL FROM t")
	assert.False(t, q.Body.Head.Projection[0].IsNull.Negate)
	assert.True(t, q.Body.Head.Projection[1].IsNull.Negate)
}

func TestConvertBetween(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a BETWEEN 1 AND 10, b NOT BETWEEN 5 AND 20 FROM t")
	assert.False(t, q.Body.Head.Projection[0].Between.Negate)
	assert.True(t, q.Body.Head.Projection[1].Between.Negate)
}

func TestConvertInOperator(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t WHERE a IN (1, 2, 3)")
	assert.Equal(t, ast.BinOpIn, q.Body.Head.Where.Binary.Op)
}

func TestConvertLike(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t WHERE a LIKE '%test%'")
	assert.Equal(t, ast.BinOpLike, q.Body.Head.Where.Binary.Op)
}

func TestConvertColumnRef(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT t.col FROM db.t")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindColumnRef, e.Kind)
	assert.Equal(t, "t", e.ColRef.Table)
	assert.Equal(t, "col", e.ColRef.Column)
}

func TestConvertAlias(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a AS x FROM t")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindAlias, e.Kind)
	assert.Equal(t, "x", e.Alias.Name)
}

func TestConvertSubqueryExpr(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT (SELECT 1) FROM t")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindSubquery, e.Kind)
	assert.NotNil(t, e.Subquery)
}

func TestConvertInterval(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT INTERVAL 1 DAY")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindInterval, e.Kind)
	assert.Equal(t, ast.IntervalDay, e.Interval.Unit)
}

func TestConvertLambda(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT arrayMap(x -> x + 1, arr) FROM t")
	e := q.Body.Head.Projection[0]
	require.Equal(t, ast.KindFunctionCall, e.Kind)
	require.Len(t, e.Func.Args, 2)
	lam := e.Func.Args[0]
	assert.Equal(t, ast.KindLambda, lam.Kind)
	assert.Equal(t, []string{"x"}, lam.Lambda.Params)
}

// ============================================================================
// Clauses
// ============================================================================

func TestConvertAllClauses(t *testing.T) {
	q := normalizeAndConvert(t, `
		SELECT DISTINCT a, count(b) AS cnt
		FROM t
		PREWHERE a > 0
		WHERE a > 1
		GROUP BY a WITH TOTALS
		HAVING cnt > 5
		ORDER BY a DESC NULLS LAST
		LIMIT 10 OFFSET 20
		SETTINGS max_threads = 4
	`)
	sel := q.Body.Head
	assert.True(t, sel.Distinct)
	assert.Len(t, sel.Projection, 2)
	require.NotNil(t, sel.Prewhere)
	require.NotNil(t, sel.Where)
	require.NotNil(t, sel.GroupBy)
	assert.True(t, sel.GroupBy.WithTotals)
	require.NotNil(t, sel.Having)
	require.NotNil(t, sel.OrderBy)
	assert.Equal(t, ast.OrderDirDesc, sel.OrderBy.Items[0].Dir)
	assert.Equal(t, ast.OrderNullsLast, sel.OrderBy.Items[0].Nulls)
	require.NotNil(t, sel.Limit)
	require.NotNil(t, sel.Limit.Limit.Offset)
	assert.Len(t, sel.Settings, 1)
	assert.Equal(t, "max_threads", sel.Settings[0].Key)
}

func TestConvertLimitBy(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a, b FROM t ORDER BY a LIMIT 3 BY b")
	require.NotNil(t, q.Body.Head.LimitBy)
	assert.Len(t, q.Body.Head.LimitBy.Columns, 1)
}

func TestConvertGroupByCube(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a, b, count(*) FROM t GROUP BY CUBE(a, b)")
	assert.Equal(t, ast.GroupByModCube, q.Body.Head.GroupBy.Modifier)
}

// ============================================================================
// FROM / JOIN
// ============================================================================

func TestConvertSimpleFrom(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM db.t")
	require.NotNil(t, q.Body.Head.From)
	assert.Equal(t, ast.JoinExprTable, q.Body.Head.From.Kind)
	assert.Equal(t, ast.TableKindRef, q.Body.Head.From.Table.TableKind)
	assert.Equal(t, "db", q.Body.Head.From.Table.Database)
	assert.Equal(t, "t", q.Body.Head.From.Table.Table)
}

func TestConvertJoinOp(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id")
	require.NotNil(t, q.Body.Head.From)
	assert.Equal(t, ast.JoinExprOp, q.Body.Head.From.Kind)
	op := q.Body.Head.From.Op
	assert.Equal(t, ast.JoinKindLeft, op.Kind)
	assert.Equal(t, ast.JoinStrictnessAll, op.Strictness)
	assert.Equal(t, ast.JoinConstraintOn, op.Constraint.Kind)
}

func TestConvertJoinUsing(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t1 JOIN t2 USING (id)")
	op := q.Body.Head.From.Op
	assert.Equal(t, ast.JoinConstraintUsing, op.Constraint.Kind)
}

func TestConvertCrossJoin(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t1 CROSS JOIN t2")
	assert.Equal(t, ast.JoinExprCross, q.Body.Head.From.Kind)
	assert.NotNil(t, q.Body.Head.From.Cross)
}

func TestConvertTableAlias(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT x.a FROM t AS x")
	assert.Equal(t, "x", q.Body.Head.From.Table.Alias)
}

func TestConvertFromSubquery(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM (SELECT 1 AS a) AS sub")
	assert.Equal(t, ast.TableKindSubquery, q.Body.Head.From.Table.TableKind)
	assert.NotNil(t, q.Body.Head.From.Table.Subquery)
	assert.Equal(t, "sub", q.Body.Head.From.Table.Alias)
}

func TestConvertFinal(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t FINAL")
	assert.True(t, q.Body.Head.From.Table.Final)
}

// ============================================================================
// UNION / EXCEPT / INTERSECT
// ============================================================================

func TestConvertUnionAll(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3")
	assert.Len(t, q.Body.Items, 2)
	assert.Equal(t, ast.UnionOpUnion, q.Body.Items[0].Op)
	assert.Equal(t, ast.UnionModAll, q.Body.Items[0].Modifier)
}

func TestConvertExcept(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1 EXCEPT SELECT 2")
	require.Len(t, q.Body.Items, 1)
	assert.Equal(t, ast.UnionOpExcept, q.Body.Items[0].Op)
}

// ============================================================================
// CTEs
// ============================================================================

func TestConvertCTE(t *testing.T) {
	q := normalizeAndConvert(t, "WITH cte AS (SELECT 1 AS a) SELECT a FROM cte")
	require.Len(t, q.CTEs, 1)
	assert.Equal(t, "cte", q.CTEs[0].Name)
}

func TestConvertCTEWithColumnAliases(t *testing.T) {
	q := normalizeAndConvert(t, "WITH cte(x) AS (SELECT 1) SELECT x FROM cte")
	require.Len(t, q.CTEs, 1)
	assert.Equal(t, []string{"x"}, q.CTEs[0].ColumnAliases)
}

// ============================================================================
// Window functions
// ============================================================================

func TestConvertWindowFunction(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT sum(a) OVER (PARTITION BY g ORDER BY a) FROM t")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindWindowFunc, e.Kind)
	assert.Equal(t, "sum", e.WinFunc.Name)
	assert.Len(t, e.WinFunc.Window.PartitionBy, 1)
	assert.Len(t, e.WinFunc.Window.OrderBy, 1)
}

func TestConvertWindowFrame(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT sum(a) OVER (ORDER BY a ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM t")
	w := q.Body.Head.Projection[0].WinFunc.Window
	require.NotNil(t, w.Frame)
	assert.Equal(t, ast.FrameUnitRows, w.Frame.Unit)
	assert.Equal(t, ast.FrameBoundUnboundedPreceding, w.Frame.Start.Kind)
	require.NotNil(t, w.Frame.End)
	assert.Equal(t, ast.FrameBoundCurrentRow, w.Frame.End.Kind)
}

func TestConvertWindowRef(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT sum(a) OVER w FROM t WINDOW w AS (ORDER BY a)")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, "w", e.WinFunc.WindowRef)
	require.NotNil(t, q.Body.Head.WindowDef)
	assert.Equal(t, "w", q.Body.Head.WindowDef.Name)
}

// ============================================================================
// Canonicalized sugar → function calls
// ============================================================================

func TestConvertCanonicalizedCase(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT CASE WHEN a = 1 THEN 'one' ELSE 'other' END FROM t")
	e := q.Body.Head.Projection[0]
	// Single-branch searched CASE → if()
	assert.Equal(t, ast.KindFunctionCall, e.Kind)
	assert.Equal(t, "IF", e.Func.Name)
}

func TestConvertCanonicalizedTernary(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a > 0 ? a : -a FROM t")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindFunctionCall, e.Kind)
	assert.Equal(t, "IF", e.Func.Name)
	assert.Len(t, e.Func.Args, 3)
}

func TestConvertCanonicalizedDateSugar(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT DATE '2024-01-01'")
	e := q.Body.Head.Projection[0]
	assert.Equal(t, ast.KindFunctionCall, e.Kind)
	assert.Equal(t, "toDate", e.Func.Name)
}

// ============================================================================
// CBOR round-trip
// ============================================================================

func TestCBORRoundTrip(t *testing.T) {
	q := normalizeAndConvert(t, `
		SELECT a, count(b) AS cnt
		FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id
		WHERE a > 1 AND b IS NOT NULL
		GROUP BY a
		ORDER BY cnt DESC
		LIMIT 10
	`)

	data, err := cbor.Marshal(q)
	require.NoError(t, err)
	assert.Greater(t, len(data), 0)

	var q2 ast.Query
	err = cbor.Unmarshal(data, &q2)
	require.NoError(t, err)

	// Verify key structural properties survive round-trip
	assert.Len(t, q2.Body.Head.Projection, 2)
	assert.Equal(t, ast.JoinExprOp, q2.Body.Head.From.Kind)
	assert.Equal(t, ast.JoinKindLeft, q2.Body.Head.From.Op.Kind)
	assert.Equal(t, ast.JoinStrictnessAll, q2.Body.Head.From.Op.Strictness)
	assert.NotNil(t, q2.Body.Head.Where)
	assert.NotNil(t, q2.Body.Head.GroupBy)
	assert.NotNil(t, q2.Body.Head.OrderBy)
	assert.Equal(t, ast.OrderDirDesc, q2.Body.Head.OrderBy.Items[0].Dir)
	assert.NotNil(t, q2.Body.Head.Limit)
}

func TestCBOREnumValues(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t1 SEMI LEFT JOIN t2 ON t1.id = t2.id ORDER BY a DESC NULLS FIRST")

	data, err := cbor.Marshal(q)
	require.NoError(t, err)

	var q2 ast.Query
	err = cbor.Unmarshal(data, &q2)
	require.NoError(t, err)

	assert.Equal(t, ast.JoinKindLeft, q2.Body.Head.From.Op.Kind)
	assert.Equal(t, ast.JoinStrictnessSemi, q2.Body.Head.From.Op.Strictness)
	assert.Equal(t, ast.OrderDirDesc, q2.Body.Head.OrderBy.Items[0].Dir)
	assert.Equal(t, ast.OrderNullsFirst, q2.Body.Head.OrderBy.Items[0].Nulls)
}

// ============================================================================
// Corpus — end-to-end: SQL → normalize → Grammar2 parse → AST → CBOR
// ============================================================================

func TestCorpusEndToEnd(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	passed, skipped := 0, 0
	normalize := passes.CanonicalizeFull(100)
	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			normalized, err := normalize(entry.SQL)
			if err != nil {
				skipped++
				t.Skipf("pipeline: %v", err)
			}

			pr, err := nanopass.ParseCanonical(normalized)
			if err != nil {
				skipped++
				t.Skipf("ParseCanonical: %v", err)
			}

			query, err := ast.ConvertCSTToAST(pr)
			if err != nil {
				t.Fatalf("ConvertCSTToAST failed:\n  original:   %s\n  normalized: %s\n  error: %v", entry.SQL, normalized, err)
			}

			// Verify CBOR serialization
			data, err := cbor.Marshal(query)
			require.NoError(t, err, "CBOR marshal failed for %s", entry.Name)
			assert.Greater(t, len(data), 0)

			var q2 ast.Query
			err = cbor.Unmarshal(data, &q2)
			require.NoError(t, err, "CBOR unmarshal failed for %s", entry.Name)

			// Basic structural invariant: projection must not be empty
			assert.NotEmpty(t, q2.Body.Head.Projection, "empty projection for %s", entry.Name)
			passed++
		})
	}
	t.Logf("corpus: %d passed, %d skipped (of %d)", passed, skipped, len(entries))
}

// ============================================================================
// Edge cases
// ============================================================================

func TestConvertMinimalQuery(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1")
	assert.Len(t, q.Body.Head.Projection, 1)
	assert.Nil(t, q.Body.Head.From)
	assert.Nil(t, q.Body.Head.Where)
}

func TestConvertNestedSubquery(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM (SELECT b FROM (SELECT 1 AS b) AS inner) AS outer")
	assert.Equal(t, ast.TableKindSubquery, q.Body.Head.From.Table.TableKind)
	inner := q.Body.Head.From.Table.Subquery.Head
	assert.Equal(t, ast.TableKindSubquery, inner.From.Table.TableKind)
}

func TestConvertMultipleJoins(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT a FROM t1 JOIN t2 ON t1.id = t2.id JOIN t3 ON t2.id = t3.id")
	// Left-associative: ((t1 JOIN t2) JOIN t3)
	assert.Equal(t, ast.JoinExprOp, q.Body.Head.From.Kind)
	assert.Equal(t, ast.JoinExprOp, q.Body.Head.From.Op.Left.Kind)
}

func TestConvertDynamicColumns(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT COLUMNS('a.*') FROM t")
	require.Len(t, q.Body.Head.Projection, 1)
	assert.Equal(t, ast.KindDynColumn, q.Body.Head.Projection[0].Kind)
	assert.Equal(t, "a.*", q.Body.Head.Projection[0].DynCol.Pattern)
}

func TestConvertSettings(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1 SETTINGS max_threads = 4, optimize_read_in_order = 1")
	assert.Len(t, q.Body.Head.Settings, 2)
}

func TestConvertFormat(t *testing.T) {
	q := normalizeAndConvert(t, "SELECT 1 FORMAT JSONEachRow")
	assert.Equal(t, "JSONEachRow", q.Format)
}
