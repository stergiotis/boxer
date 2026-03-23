//go:build llm_generated_opus46

package nanopass_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			name:     "mixed_qualified",
			input:    "SELECT a FROM db.t1 UNION ALL SELECT b FROM t2",
			expected: "SELECT a FROM db.t1 UNION ALL SELECT b FROM mydb.t2",
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

func TestQualifyTablesSkipsCTEs(t *testing.T) {
	pass := passes.QualifyTables("mydb")

	sql := "WITH cte AS (SELECT a FROM t_real) SELECT x FROM cte"
	got, err := pass(sql)
	require.NoError(t, err)

	// cte reference should NOT be qualified
	assert.Contains(t, got, "FROM cte")
	// Real table inside CTE should be qualified
	assert.Contains(t, got, "FROM mydb.t_real")

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}
