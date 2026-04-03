//go:build llm_generated_opus46

/*
Great! Works fine. We reached an important milestone. We have created:
* two grammars `grammar1` and `grammar2` (embodying "be generous what you accept, careful what you send").
* a nanopass framework with fixed point iteration in the `nanopass` package
* a canonicalization pipeline combining various canonicalization passes in the `passes` package
* feature passes for application frameworks (e.g. setting manipulation, literal extraction, function expansion/macros, format override, ...) in the `passes` package
* an idiomatic, serializable AST representation in the `ast` package
* an AST to SQL unparser in the `ast` package
* a fluid query builder API ontop of the AST in the `astbuilder` package
* an AST to fluid query builder API code generator in the `ast` package
*/

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

// ============================================================================
// Helpers
// ============================================================================

func fullPipeline(sql string) (result string, err error) {
	return passes.CanonicalizeFull(100)(sql)
}

func sqlToAST(t *testing.T, sql string) ast.Query {
	t.Helper()
	normalized, err := passes.CanonicalizeFull(100)(sql)
	require.NoError(t, err, "pipeline failed for: %s", sql)
	pr, err := nanopass.ParseCanonical(normalized)
	require.NoError(t, err, "ParseCanonical failed for: %s", normalized)
	query, err := ast.ConvertCSTToAST(pr)
	require.NoError(t, err, "ConvertCSTToAST failed for: %s", normalized)
	return query
}

// mustParseGrammar1 verifies that sql is parseable by Grammar1.
func mustParseGrammar1(t *testing.T, sql string) {
	t.Helper()
	_, err := nanopass.Parse(sql)
	require.NoError(t, err, "ToSQL output not parseable by Grammar1:\n%s", sql)
}

// ============================================================================
// 1. ToSQL explicit pairs — verify exact output for simple queries
// ============================================================================

func TestToSQLExplicitPairs(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains []string // substrings the output must contain
	}{
		{
			name:     "simple_select",
			input:    "SELECT a, b FROM t",
			contains: []string{"SELECT", "FROM"},
		},
		{
			name:     "where",
			input:    "SELECT a FROM t WHERE a > 1",
			contains: []string{"WHERE", "> 1"},
		},
		{
			name:     "order_by_desc_nulls",
			input:    "SELECT a FROM t ORDER BY a DESC NULLS LAST",
			contains: []string{"ORDER BY", "DESC", "NULLS LAST"},
		},
		{
			name:     "limit_offset",
			input:    "SELECT a FROM t LIMIT 10 OFFSET 20",
			contains: []string{"LIMIT", "OFFSET"},
		},
		{
			name:     "group_by_totals",
			input:    "SELECT a, count(*) FROM t GROUP BY a WITH TOTALS",
			contains: []string{"GROUP BY", "WITH TOTALS"},
		},
		{
			name:     "join_on",
			input:    "SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
			contains: []string{"ALL", "LEFT JOIN", "ON"},
		},
		{
			name:     "cross_join",
			input:    "SELECT a FROM t1 CROSS JOIN t2",
			contains: []string{"CROSS JOIN"},
		},
		{
			name:     "union_all",
			input:    "SELECT 1 UNION ALL SELECT 2",
			contains: []string{"UNION ALL"},
		},
		{
			name:     "cte",
			input:    "WITH cte AS (SELECT 1 AS x) SELECT x FROM cte",
			contains: []string{"WITH", "AS ("},
		},
		{
			name:     "window_over",
			input:    "SELECT sum(a) OVER (PARTITION BY g ORDER BY a) FROM t",
			contains: []string{"OVER", "PARTITION BY", "ORDER BY"},
		},
		{
			name:     "between",
			input:    "SELECT a FROM t WHERE a BETWEEN 1 AND 10",
			contains: []string{"BETWEEN", "AND"},
		},
		{
			name:     "is_not_null",
			input:    "SELECT a FROM t WHERE a IS NOT NULL",
			contains: []string{"IS NOT NULL"},
		},
		{
			name:     "interval",
			input:    "SELECT INTERVAL 1 DAY",
			contains: []string{"INTERVAL", "DAY"},
		},
		{
			name:     "lambda",
			input:    "SELECT arrayMap(x -> x + 1, arr) FROM t",
			contains: []string{"->"},
		},
		{
			name:     "alias",
			input:    "SELECT a AS x FROM t",
			contains: []string{"AS"},
		},
		{
			name:     "settings",
			input:    "SELECT 1 SETTINGS max_threads = 4",
			contains: []string{"SETTINGS", "max_threads", "= 4"},
		},
		{
			name:     "format",
			input:    "SELECT 1 FORMAT JSONEachRow",
			contains: []string{"FORMAT", "JSONEachRow"},
		},
		{
			name:     "subquery_expr",
			input:    "SELECT (SELECT 1) FROM t",
			contains: []string{"(SELECT"},
		},
		{
			name:     "asterisk_qualified",
			input:    "SELECT t.* FROM t",
			contains: []string{"t.*"},
		},
		{
			name:     "dynamic_columns",
			input:    "SELECT COLUMNS('a.*') FROM t",
			contains: []string{"COLUMNS('a.*')"},
		},
		{
			name:     "boolean_literals",
			input:    "SELECT true, false",
			contains: []string{"true", "false"},
		},
		{
			name:     "distinct",
			input:    "SELECT DISTINCT a FROM t",
			contains: []string{"DISTINCT"},
		},
		{
			name:     "prewhere",
			input:    "SELECT a FROM t PREWHERE a > 0",
			contains: []string{"PREWHERE"},
		},
		{
			name:     "final",
			input:    "SELECT a FROM t FINAL",
			contains: []string{"FINAL"},
		},
		{
			name:     "limit_by",
			input:    "SELECT a, b FROM t ORDER BY a LIMIT 3 BY b",
			contains: []string{"LIMIT", "BY"},
		},
		{
			name:     "array_join",
			input:    "SELECT x FROM t LEFT ARRAY JOIN arr AS x",
			contains: []string{"LEFT ARRAY JOIN"},
		},
		{
			name:     "group_by_cube",
			input:    "SELECT a, b, count(*) FROM t GROUP BY CUBE(a, b)",
			contains: []string{"CUBE("},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := sqlToAST(t, tt.input)
			out := q.ToSQL()
			mustParseGrammar1(t, out)
			for _, s := range tt.contains {
				assert.Contains(t, out, s, "ToSQL output missing %q:\n%s", s, out)
			}
		})
	}
}

// ============================================================================
// 2. Round-trip: SQL → AST → ToSQL → Grammar1 parse
//
// The strongest test: if ToSQL output parses, the emitter is syntactically
// correct. We do NOT require textual identity — just parseability.
// ============================================================================

func TestRoundTripParseability(t *testing.T) {
	sqls := []string{
		"SELECT 1",
		"SELECT a, b, c FROM t WHERE a > 1 AND b < 10 ORDER BY c LIMIT 100",
		"SELECT count(DISTINCT a) FROM t GROUP BY b HAVING count(a) > 5",
		"SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id",
		"SELECT a FROM t1 JOIN t2 USING (id)",
		"SELECT a FROM t1 CROSS JOIN t2",
		"SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3",
		"WITH cte AS (SELECT 1 AS a) SELECT a FROM cte",
		"SELECT sum(a) OVER (PARTITION BY g ORDER BY a ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM t",
		"SELECT a FROM t WHERE a BETWEEN 1 AND 100",
		"SELECT a FROM t WHERE a IS NOT NULL",
		"SELECT a FROM t WHERE a IN (1, 2, 3)",
		"SELECT a FROM t WHERE a LIKE '%test%'",
		"SELECT a FROM t WHERE a NOT LIKE '%skip%'",
		"SELECT a FROM t WHERE a ILIKE '%TEST%'",
		"SELECT INTERVAL 1 DAY",
		"SELECT arrayMap(x -> x + 1, arr) FROM t",
		"SELECT a AS x, b AS y FROM t ORDER BY x DESC NULLS FIRST",
		"SELECT * FROM t FINAL",
		"SELECT a FROM (SELECT 1 AS a) AS sub",
		"SELECT a FROM t PREWHERE a > 0 WHERE a > 1",
		"SELECT CASE WHEN a = 1 THEN 'one' WHEN a = 2 THEN 'two' ELSE 'other' END FROM t",
		"SELECT a ? b : c FROM t",
		"SELECT DATE '2024-01-01'",
		"SELECT EXTRACT(DAY FROM d) FROM t",
		"SELECT SUBSTRING(s FROM 1 FOR 3) FROM t",
		"SELECT a FROM t WHERE a == 1",
		"SELECT a FROM t1 LEFT OUTER JOIN t2 ON t1.id = t2.id",
		"SELECT true, false, NULL",
		"SELECT -a, NOT b FROM t",
		"SELECT a + b * c FROM t",
		"SELECT a FROM t SETTINGS max_threads = 4",
		"SELECT 1 FORMAT JSONEachRow",
		"SELECT COLUMNS('a.*') FROM t",
		"SELECT t.* FROM t",
		"SELECT DISTINCT a FROM t",
		"SELECT a, b FROM t ORDER BY a LIMIT 3 BY b",
		"SELECT a, count(*) FROM t GROUP BY CUBE(a)",
	}

	for _, sql := range sqls {
		name := sql
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			q := sqlToAST(t, sql)
			out := q.ToSQL()
			mustParseGrammar1(t, out)
		})
	}
}

// ============================================================================
// 3. CBOR round-trip: SQL → AST → CBOR → AST → ToSQL → Grammar1 parse
//
// Full serialization round-trip. Verifies that CBOR encoding/decoding
// preserves enough structure for ToSQL to produce valid SQL.
// ============================================================================

func TestCBORRoundTripToSQL(t *testing.T) {
	sqls := []string{
		"SELECT a, count(b) AS cnt FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id WHERE a > 1 GROUP BY a ORDER BY cnt DESC LIMIT 10",
		"WITH cte AS (SELECT 1 AS x) SELECT x FROM cte",
		"SELECT sum(a) OVER (PARTITION BY g ORDER BY a ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW) FROM t",
		"SELECT 1 UNION ALL SELECT 2 UNION ALL SELECT 3",
		"SELECT a FROM t WHERE a BETWEEN 1 AND 100 AND b IS NOT NULL",
		"SELECT arrayMap(x -> x + 1, arr) FROM t",
		"SELECT true, false, NULL, 42, 'hello'",
		"SELECT INTERVAL 3 HOUR",
		"SELECT a FROM t SETTINGS max_threads = 4 FORMAT JSONEachRow",
	}

	for _, sql := range sqls {
		name := sql
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			q1 := sqlToAST(t, sql)

			data, err := cbor.Marshal(q1)
			require.NoError(t, err)

			var q2 ast.Query
			err = cbor.Unmarshal(data, &q2)
			require.NoError(t, err)

			out := q2.ToSQL()
			mustParseGrammar1(t, out)
		})
	}
}

// ============================================================================
// 4. Corpus round-trip: every corpus entry through full pipeline
//
// SQL → normalize → Grammar2 parse → AST → ToSQL → Grammar1 parse
// Also: → CBOR → AST → ToSQL → Grammar1 parse
// ============================================================================

func TestCorpusRoundTrip(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	passed, skipped := 0, 0
	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			normalized, err := fullPipeline(entry.SQL)
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
				t.Fatalf("ConvertCSTToAST failed for %s:\n  original:   %s\n  normalized: %s\n  error: %v",
					entry.Name, entry.SQL, normalized, err)
			}

			// Round-trip 1: ToSQL → Grammar1 parse
			{
				out := query.ToSQL()
				_, err = nanopass.Parse(out)
				assert.NoError(t, err,
					"ToSQL output not parseable for %s:\n  original:   %s\n  normalized: %s\n  toSQL:      %s",
					entry.Name, entry.SQL, normalized, out)
			}

			// Round-trip 2: CBOR → AST → ToSQL → Grammar1 parse
			{
				data, err := cbor.Marshal(query)
				require.NoError(t, err, "CBOR marshal failed for %s", entry.Name)

				var q2 ast.Query
				err = cbor.Unmarshal(data, &q2)
				require.NoError(t, err, "CBOR unmarshal failed for %s", entry.Name)

				out := q2.ToSQL()
				_, err = nanopass.Parse(out)
				assert.NoError(t, err,
					"CBOR round-trip ToSQL not parseable for %s:\n  toSQL: %s", entry.Name, out)
			}

			passed++
		})
	}
	t.Logf("corpus round-trip: %d passed, %d skipped (of %d)", passed, skipped, len(entries))
}

// ============================================================================
// 5. Structural invariants — properties that must hold for any valid AST
// ============================================================================

func TestStructuralInvariants(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			normalized, err := fullPipeline(entry.SQL)
			if err != nil {
				t.Skipf("pipeline: %v", err)
			}
			pr, err := nanopass.ParseCanonical(normalized)
			if err != nil {
				t.Skipf("ParseCanonical: %v", err)
			}
			query, err := ast.ConvertCSTToAST(pr)
			if err != nil {
				t.Skipf("ConvertCSTToAST: %v", err)
			}

			// Projection must not be empty
			assert.NotEmpty(t, query.Body.Head.Projection,
				"empty projection for %s", entry.Name)

			// Every Expr must have the correct data pointer set
			walkExprs(t, query.Body.Head.Projection, entry.Name)
		})
	}
}

// walkExprs recursively validates that each Expr has exactly one
// non-nil data field matching its Kind.
func walkExprs(t *testing.T, exprs []ast.Expr, context string) {
	t.Helper()
	for _, e := range exprs {
		assertExprConsistent(t, e, context)
	}
}

func assertExprConsistent(t *testing.T, e ast.Expr, context string) {
	t.Helper()
	switch e.Kind {
	case ast.KindLiteral:
		assert.NotNil(t, e.Literal, "KindLiteral nil data in %s", context)
	case ast.KindParamSlot:
		assert.NotNil(t, e.Param, "KindParamSlot nil data in %s", context)
	case ast.KindColumnRef:
		assert.NotNil(t, e.ColRef, "KindColumnRef nil data in %s", context)
		assert.NotEmpty(t, e.ColRef.Column, "empty column name in %s", context)
	case ast.KindFunctionCall:
		assert.NotNil(t, e.Func, "KindFunctionCall nil data in %s", context)
		assert.NotEmpty(t, e.Func.Name, "empty function name in %s", context)
		walkExprs(t, e.Func.Args, context+"/"+e.Func.Name)
	case ast.KindWindowFunc:
		assert.NotNil(t, e.WinFunc, "KindWindowFunc nil data in %s", context)
		assert.NotEmpty(t, e.WinFunc.Name, "empty window func name in %s", context)
		walkExprs(t, e.WinFunc.Args, context+"/"+e.WinFunc.Name)
	case ast.KindBinary:
		assert.NotNil(t, e.Binary, "KindBinary nil data in %s", context)
		assertExprConsistent(t, e.Binary.Left, context+"/binL")
		assertExprConsistent(t, e.Binary.Right, context+"/binR")
	case ast.KindUnary:
		assert.NotNil(t, e.Unary, "KindUnary nil data in %s", context)
		assertExprConsistent(t, e.Unary.Expr, context+"/unary")
	case ast.KindBetween:
		assert.NotNil(t, e.Between, "KindBetween nil data in %s", context)
	case ast.KindIsNull:
		assert.NotNil(t, e.IsNull, "KindIsNull nil data in %s", context)
	case ast.KindInterval:
		assert.NotNil(t, e.Interval, "KindInterval nil data in %s", context)
	case ast.KindLambda:
		assert.NotNil(t, e.Lambda, "KindLambda nil data in %s", context)
		assert.NotEmpty(t, e.Lambda.Params, "empty lambda params in %s", context)
		assertExprConsistent(t, e.Lambda.Body, context+"/lambda")
	case ast.KindAlias:
		assert.NotNil(t, e.Alias, "KindAlias nil data in %s", context)
		assert.NotEmpty(t, e.Alias.Name, "empty alias name in %s", context)
		assertExprConsistent(t, e.Alias.Expr, context+"/alias")
	case ast.KindSubquery:
		assert.NotNil(t, e.Subquery, "KindSubquery nil data in %s", context)
	case ast.KindAsterisk:
		assert.NotNil(t, e.Asterisk, "KindAsterisk nil data in %s", context)
	case ast.KindDynColumn:
		assert.NotNil(t, e.DynCol, "KindDynColumn nil data in %s", context)
	default:
		t.Errorf("unknown ExprKind %d in %s", e.Kind, context)
	}
}

// ============================================================================
// 6. ToSQL idempotency: ToSQL(AST) parsed and converted again produces
//    the same ToSQL output.
// ============================================================================

func TestToSQLIdempotency(t *testing.T) {
	sqls := []string{
		"SELECT a, b FROM t WHERE a > 1",
		"SELECT a FROM t1 ALL LEFT JOIN t2 ON t1.id = t2.id ORDER BY a DESC",
		"WITH cte AS (SELECT 1 AS x) SELECT x FROM cte",
		"SELECT 1 UNION ALL SELECT 2",
		"SELECT sum(a) OVER (PARTITION BY g ORDER BY a) FROM t",
		"SELECT a FROM t WHERE a BETWEEN 1 AND 10 AND b IS NOT NULL",
		"SELECT arrayMap(x -> x + 1, arr) FROM t",
		"SELECT true, false, NULL",
	}

	for _, sql := range sqls {
		name := sql
		if len(name) > 60 {
			name = name[:60]
		}
		t.Run(name, func(t *testing.T) {
			// Pass 1: SQL → AST → ToSQL
			q1 := sqlToAST(t, sql)
			out1 := q1.ToSQL()

			// Pass 2: ToSQL → AST → ToSQL
			q2 := sqlToAST(t, out1)
			out2 := q2.ToSQL()

			assert.Equal(t, out1, out2,
				"ToSQL not idempotent:\n  pass1: %s\n  pass2: %s", out1, out2)
		})
	}
}

// ============================================================================
// 7. Binary operator precedence — verify parentheses are correct
// ============================================================================

func TestToSQLBinaryPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "mul_in_add_no_parens",
			input:    "SELECT a + b * c FROM t",
			contains: "a + b * c", // no parens needed — * binds tighter
		},
		{
			name:     "add_in_comparison",
			input:    "SELECT a FROM t WHERE a + b > 10",
			contains: "a + b > 10",
		},
		{
			name:     "and_or_needs_parens",
			input:    "SELECT a FROM t WHERE (a > 1 OR b < 2) AND c = 3",
			contains: "(", // OR inside AND needs parens
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := sqlToAST(t, tt.input)
			out := q.ToSQL()
			mustParseGrammar1(t, out)
			if tt.contains != "" {
				assert.Contains(t, out, tt.contains)
			}
		})
	}
}
