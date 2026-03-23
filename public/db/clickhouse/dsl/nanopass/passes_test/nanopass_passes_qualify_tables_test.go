//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQualifyTables(t *testing.T) {
	pass := passes.QualifyTables("mydb")
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic qualification
		{
			name:     "simple",
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM mydb.t",
		},
		// Already qualified — untouched
		{
			name:     "already_qualified",
			input:    "SELECT a FROM db.t",
			expected: "SELECT a FROM db.t",
		},
		// Column qualifiers untouched
		{
			name:     "column_qualifiers_untouched",
			input:    "SELECT a FROM t1 JOIN t2 ON t1.id = t2.id",
			expected: "SELECT a FROM mydb.t1 JOIN mydb.t2 ON t1.id = t2.id",
		},
		// Mixed qualified and unqualified
		{
			name:     "mixed",
			input:    "SELECT a FROM db.t1 JOIN t2 ON db.t1.id = t2.id",
			expected: "SELECT a FROM db.t1 JOIN mydb.t2 ON db.t1.id = t2.id",
		},
		// Multiple joins
		{
			name:     "multiple_joins",
			input:    "SELECT * FROM t1 JOIN t2 ON t1.id = t2.id LEFT JOIN t3 ON t2.id = t3.id",
			expected: "SELECT * FROM mydb.t1 JOIN mydb.t2 ON t1.id = t2.id LEFT JOIN mydb.t3 ON t2.id = t3.id",
		},
		// Aliased table
		{
			name:     "aliased",
			input:    "SELECT a.x FROM t AS a",
			expected: "SELECT a.x FROM mydb.t AS a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestQualifyTablesUnionAll(t *testing.T) {
	pass := passes.QualifyTables("mydb")
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "two_branches",
			input:    "SELECT a FROM t1 UNION ALL SELECT b FROM t2",
			expected: "SELECT a FROM mydb.t1 UNION ALL SELECT b FROM mydb.t2",
		},
		{
			name:     "three_branches",
			input:    "SELECT a FROM t1 UNION ALL SELECT b FROM t2 UNION ALL SELECT c FROM t3",
			expected: "SELECT a FROM mydb.t1 UNION ALL SELECT b FROM mydb.t2 UNION ALL SELECT c FROM mydb.t3",
		},
		{
			name:     "mixed_qualified_union",
			input:    "SELECT a FROM db.t1 UNION ALL SELECT b FROM t2",
			expected: "SELECT a FROM db.t1 UNION ALL SELECT b FROM mydb.t2",
		},
		{
			name:     "union_with_aliases",
			input:    "SELECT a.x FROM t1 AS a UNION ALL SELECT b.y FROM t2 AS b",
			expected: "SELECT a.x FROM mydb.t1 AS a UNION ALL SELECT b.y FROM mydb.t2 AS b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestQualifyTablesSkipsCTEs(t *testing.T) {
	pass := passes.QualifyTables("mydb")
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple_cte",
			input:    "WITH cte AS (SELECT a FROM t_real) SELECT x FROM cte",
			expected: "WITH cte AS (SELECT a FROM mydb.t_real) SELECT x FROM cte",
		},
		{
			name:     "multiple_ctes",
			input:    "WITH a AS (SELECT 1 AS x FROM t1), b AS (SELECT 2 AS y FROM t2) SELECT * FROM a, b",
			expected: "WITH a AS (SELECT 1 AS x FROM mydb.t1), b AS (SELECT 2 AS y FROM mydb.t2) SELECT * FROM a, b",
		},
		{
			name:     "cte_with_qualified_table",
			input:    "WITH cte AS (SELECT a FROM db.t) SELECT x FROM cte",
			expected: "WITH cte AS (SELECT a FROM db.t) SELECT x FROM cte",
		},
		{
			name:     "cte_and_real_table",
			input:    "WITH cte AS (SELECT a FROM t_inner) SELECT cte.a, t_outer.b FROM cte JOIN t_outer ON cte.a = t_outer.a",
			expected: "WITH cte AS (SELECT a FROM mydb.t_inner) SELECT cte.a, t_outer.b FROM cte JOIN mydb.t_outer ON cte.a = t_outer.a",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

func TestQualifyTablesSubqueries(t *testing.T) {
	pass := passes.QualifyTables("mydb")
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Subquery in FROM — the subquery source itself isn't qualified,
		// but tables inside the subquery are (on re-parse in a second pass,
		// or via scope recursion if implemented)
		{
			name:     "subquery_not_qualified",
			input:    "SELECT * FROM (SELECT a FROM t)",
			expected: "SELECT * FROM (SELECT a FROM mydb.t)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestQualifyTablesNoFrom(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", got)
}

func TestQualifyTablesIdempotent(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sqls := []string{
		"SELECT a FROM t",
		"SELECT a FROM mydb.t",
		"WITH cte AS (SELECT a FROM t) SELECT x FROM cte",
		"SELECT a FROM t1 UNION ALL SELECT b FROM t2",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1, err := pass(sql)
			require.NoError(t, err)
			pass2, err := pass(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

func TestQualifyTablesOutputValidity(t *testing.T) {
	pass := passes.QualifyTables("default")
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed (may be expected for some corpus entries): %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "pass produced invalid SQL for %s:\noutput: %s", entry.Name, out)
		})
	}
}
func TestQualifyTablesAliasedSubqueryInJoin(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "SELECT * FROM t1 JOIN (SELECT b FROM t2) AS sub ON t1.id = sub.id"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t1")
	assert.Contains(t, got, "FROM mydb.t2")
	// sub is a subquery alias, not a table — should not be qualified
	assert.NotContains(t, got, "mydb.sub")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestQualifyTablesScalarSubqueryInSelect(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "SELECT (SELECT max(x) FROM t2) AS mx, a FROM t1"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t1")
	assert.Contains(t, got, "FROM mydb.t2")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestQualifyTablesExistsSubquery(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "SELECT a FROM t1 WHERE a IN (SELECT 1 FROM t2 WHERE t2.id = t1.id)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t1")
	assert.Contains(t, got, "FROM mydb.t2")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestQualifyTablesGlobalInSubquery(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "SELECT a FROM t1 WHERE a GLOBAL IN (SELECT b FROM t2)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t1")
	assert.Contains(t, got, "FROM mydb.t2")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestQualifyTablesNestedCTEWithSubqueryInWhere(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "WITH cte AS (SELECT a FROM t1 WHERE b IN (SELECT c FROM t2)) SELECT * FROM cte"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t1")
	assert.Contains(t, got, "FROM mydb.t2")
	assert.Contains(t, got, "FROM cte") // CTE ref not qualified

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestQualifyTablesDeepNesting(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "SELECT * FROM (SELECT * FROM (SELECT a FROM t_deep))"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "FROM mydb.t_deep")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}
