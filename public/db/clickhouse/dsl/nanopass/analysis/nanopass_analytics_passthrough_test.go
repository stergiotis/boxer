package analysis

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stretchr/testify/require"
)

func formatRefs(refs []TableRef) (out []string) {
	out = make([]string, len(refs))
	for i, r := range refs {
		if r.Database != "" {
			out[i] = r.Database + "." + r.Table
			continue
		}
		out[i] = r.Table
	}
	return
}

func mustClassify(t *testing.T, sql string, defaultDB string) (got []string) {
	t.Helper()
	pr, err := nanopass.Parse(sql)
	require.NoError(t, err, "parse: %s", sql)
	refs, err := ExtractPassthroughTables(pr, defaultDB)
	require.NoError(t, err, "classify: %s", sql)
	got = formatRefs(refs)
	return
}

func TestExtractPassthroughTables(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected []string
	}{
		// The two motivating examples (example 1 uses the bare EXCEPT form,
		// since the parenthesised form does not parse — see the deferral test).
		{"example1_union", `SELECT c1,c2 FROM tbl1 UNION ALL SELECT * EXCEPT c3 FROM tbl2`, []string{"tbl1", "tbl2"}},
		{"example2_derived", `SELECT mycol1 as c1,mycol2+3 AS c2 FROM othertable`, []string{}},

		// Basic passthrough shapes.
		{"bare_columns", `SELECT c1, c2 FROM tbl1`, []string{"tbl1"}},
		{"star", `SELECT * FROM t`, []string{"t"}},
		{"qualified_star", `SELECT t.* FROM t`, []string{"t"}},
		{"bare_except", `SELECT * EXCEPT c3 FROM t`, []string{"t"}},
		{"qualified_columns", `SELECT t.a, t.b FROM t`, []string{"t"}},
		{"explicit_database", `SELECT a, b FROM mydb.mytable`, []string{"mydb.mytable"}},

		// Transformations taint the whole table out (strict rows).
		{"expression", `SELECT a+1 FROM t`, []string{}},
		{"rename", `SELECT a AS x FROM t`, []string{}},
		{"rename_same_name", `SELECT a AS a FROM t`, []string{}},
		{"mixed_verbatim_and_derived", `SELECT c1, c2+3 AS x FROM t`, []string{}},
		{"aggregate_sum", `SELECT sum(a) FROM t`, []string{}},
		{"aggregate_count_star", `SELECT count(*) FROM t`, []string{}},
		{"scalar_function", `SELECT lower(a) FROM t`, []string{}},
		{"window_function", `SELECT rank() OVER (ORDER BY a) FROM t`, []string{}},
		{"scalar_subquery_projection", `SELECT (SELECT max(x) FROM u) FROM t`, []string{}},

		// Row-collapsing / dedup clauses disqualify.
		{"group_by", `SELECT a FROM t GROUP BY a`, []string{}},
		{"group_by_having", `SELECT a FROM t GROUP BY a HAVING count() > 1`, []string{}},
		{"distinct", `SELECT DISTINCT a FROM t`, []string{}},
		{"array_join", `SELECT a FROM t ARRAY JOIN arr AS a`, []string{}},

		// Filters / reordering preserve 1:1.
		{"where", `SELECT a FROM t WHERE b > 5`, []string{"t"}},
		{"order_limit", `SELECT a FROM t ORDER BY b LIMIT 10`, []string{"t"}},
		{"star_where_order", `SELECT * FROM t WHERE b = 1 ORDER BY a`, []string{"t"}},

		// Joins disqualify (single-source only).
		{"inner_join", `SELECT t1.a, t2.b FROM t1 JOIN t2 ON t1.k = t2.k`, []string{}},
		{"comma_join", `SELECT t1.a, t2.b FROM t1, t2`, []string{}},

		// Table functions are not stored relations.
		{"table_function", `SELECT a FROM numbers(10)`, []string{}},

		// No source at all.
		{"no_from", `SELECT 1`, []string{}},
		{"no_from_alias", `SELECT 1 AS x`, []string{}},

		// UNION ALL unions the per-branch results; other set ops disqualify.
		{"union_all", `SELECT a FROM t1 UNION ALL SELECT b FROM t2`, []string{"t1", "t2"}},
		{"union_all_three", `SELECT a FROM t1 UNION ALL SELECT b FROM t2 UNION ALL SELECT c FROM t3`, []string{"t1", "t2", "t3"}},
		{"union_all_one_branch_derived", `SELECT a FROM t1 UNION ALL SELECT b+1 FROM t2`, []string{"t1"}},
		{"union_distinct", `SELECT a FROM t1 UNION DISTINCT SELECT b FROM t2`, []string{}},
		{"except_setop", `SELECT a FROM t1 EXCEPT SELECT b FROM t2`, []string{}},
		{"intersect_setop", `SELECT a FROM t1 INTERSECT SELECT b FROM t2`, []string{}},
		{"union_all_then_distinct_taints_all", `SELECT a FROM t1 UNION ALL SELECT b FROM t2 UNION DISTINCT SELECT c FROM t3`, []string{}},

		// Resolve through subqueries.
		{"subquery", `SELECT a FROM (SELECT a FROM t)`, []string{"t"}},
		{"subquery_aliased", `SELECT a FROM (SELECT a FROM t) s`, []string{"t"}},
		{"subquery_qualified_outer", `SELECT s.a FROM (SELECT a FROM t) s`, []string{"t"}},
		{"subquery_star_outer", `SELECT * FROM (SELECT a, b FROM t)`, []string{"t"}},
		{"subquery_inner_derived", `SELECT a FROM (SELECT a, b+1 AS c FROM t)`, []string{}},
		{"subquery_inner_rename", `SELECT x FROM (SELECT c1 AS x FROM t)`, []string{}},
		{"subquery_inner_join", `SELECT a FROM (SELECT t1.a FROM t1 JOIN t2 ON t1.k = t2.k)`, []string{}},
		{"subquery_inner_union_all", `SELECT a FROM (SELECT a FROM t1 UNION ALL SELECT a FROM t2)`, []string{"t1", "t2"}},
		{"subquery_inner_union_distinct", `SELECT a FROM (SELECT a FROM t1 UNION DISTINCT SELECT a FROM t2)`, []string{}},
		{"subquery_nested_twice", `SELECT a FROM (SELECT a FROM (SELECT a FROM t))`, []string{"t"}},

		// A set op buried in a WHERE/IN filter subquery must NOT taint the
		// outer table — that subquery is a filter, not resolved through.
		{"where_in_subquery_union_distinct", `SELECT a FROM t WHERE b IN (SELECT x FROM u UNION DISTINCT SELECT y FROM v)`, []string{"t"}},

		// Resolve through CTEs.
		{"cte", `WITH cte AS (SELECT a FROM t) SELECT a FROM cte`, []string{"t"}},
		{"cte_inner_derived", `WITH cte AS (SELECT a, b+1 AS c FROM t) SELECT a FROM cte`, []string{}},
		{"cte_chained", `WITH a AS (SELECT x FROM t), b AS (SELECT x FROM a) SELECT x FROM b`, []string{"t"}},
		{"cte_recursive", `WITH RECURSIVE r AS (SELECT a FROM t UNION ALL SELECT a FROM r) SELECT a FROM r`, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.expected, mustClassify(t, tt.sql, ""))
		})
	}
}

func TestExtractPassthroughTablesDefaultDatabase(t *testing.T) {
	// The default database is attached to unqualified sources; an explicit
	// qualifier wins over it.
	require.Equal(t, []string{"prod.t"}, mustClassify(t, `SELECT a FROM t`, "prod"))
	require.Equal(t, []string{"other.u"}, mustClassify(t, `SELECT a FROM other.u`, "prod"))
	require.Equal(t, []string{"prod.t1", "prod.t2"},
		mustClassify(t, `SELECT a FROM t1 UNION ALL SELECT b FROM t2`, "prod"))
}

// TestExtractPassthroughTablesParenthesizedExceptDeferred pins the known
// grammar gap: the parenthesised column-modifier form does not parse, so the
// motivating example must use the bare `* EXCEPT c` form. If Grammar1 ever
// gains the parenthesised form, this test fails and the classifier can be
// extended to cover it.
func TestExtractPassthroughTablesParenthesizedExceptDeferred(t *testing.T) {
	_, err := nanopass.Parse(`SELECT * EXCEPT (c3) FROM tbl2`)
	require.Error(t, err, "parenthesised EXCEPT unexpectedly parses now — revisit the deferral")
}

func TestExtractPassthroughTablesBadTreeErrors(t *testing.T) {
	// A nil/empty parse result cannot yield scopes; the classifier surfaces the
	// BuildScopes error rather than returning a silent empty set.
	_, err := ExtractPassthroughTables(&nanopass.ParseResult{}, "")
	require.Error(t, err)
}
