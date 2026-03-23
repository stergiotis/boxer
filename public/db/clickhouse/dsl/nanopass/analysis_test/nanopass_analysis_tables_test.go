//go:build llm_generated_opus46

package analysis_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractTables(t *testing.T) {
	tests := []struct {
		sql      string
		expected []analysis.TableRef
	}{
		{
			sql:      "SELECT 1",
			expected: nil,
		},
		{
			sql: "SELECT a FROM t",
			expected: []analysis.TableRef{
				{Table: "t"},
			},
		},
		{
			sql: "SELECT a FROM db.t",
			expected: []analysis.TableRef{
				{Database: "db", Table: "t"},
			},
		},
		{
			sql: "SELECT * FROM t1 JOIN t2 ON t1.id = t2.id",
			expected: []analysis.TableRef{
				{Table: "t1"},
				{Table: "t2"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.sql, func(t *testing.T) {
			pr, err := nanopass.Parse(tt.sql)
			require.NoError(t, err)
			refs := analysis.ExtractTables(pr)
			if tt.expected == nil {
				assert.Empty(t, refs)
			} else {
				assert.Equal(t, tt.expected, refs)
			}
		})
	}
}
