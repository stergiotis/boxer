package analysis_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractColumns(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected []analysis.ColumnRef
	}{
		{
			name:     "literal_only",
			sql:      "SELECT 1",
			expected: nil,
		},
		{
			name: "simple_columns",
			sql:  "SELECT a, b FROM t",
			expected: []analysis.ColumnRef{
				{Column: "a"},
				{Column: "b"},
			},
		},
		{
			name: "qualified_column",
			sql:  "SELECT t.a FROM t",
			expected: []analysis.ColumnRef{
				{Table: "t", Column: "a"},
			},
		},
		{
			name: "multiple_tables",
			sql:  "SELECT t1.x, t2.y FROM t1 JOIN t2 ON t1.id = t2.id",
			expected: []analysis.ColumnRef{
				{Table: "t1", Column: "x"},
				{Table: "t2", Column: "y"},
				{Table: "t1", Column: "id"},
				{Table: "t2", Column: "id"},
			},
		},
		{
			name: "where_clause_columns",
			sql:  "SELECT a FROM t WHERE b > 1",
			expected: []analysis.ColumnRef{
				{Column: "a"},
				{Column: "b"},
			},
		},
		{
			// A name that can only be written quoted must come back
			// unquoted, or a caller matching it against a schema misses
			// it silently. keelson.adrcontent's content column is exactly
			// this shape (ADR-0123 `label@mime`).
			name: "quoted_column",
			sql:  "SELECT num, `content@text/markdown` FROM adrcontent",
			expected: []analysis.ColumnRef{
				{Column: "num"},
				{Column: "content@text/markdown"},
			},
		},
		{
			name: "quoted_table_and_column",
			sql:  "SELECT `my tbl`.`odd col` FROM `my tbl`",
			expected: []analysis.ColumnRef{
				{Table: "my tbl", Column: "odd col"},
			},
		},
		{
			// Each segment decodes on its own; decoding the whole text
			// would read the outer backticks as one pair.
			name: "quoted_nested_path",
			sql:  "SELECT `a b`.`c d` FROM t",
			expected: []analysis.ColumnRef{
				{Table: "a b", Column: "c d"},
			},
		},
		{
			// Three parts read as database.table.column: the grammar's
			// tableIdentifier absorbs the first two, so Table carries the
			// qualified name and Column is the bare leaf.
			name: "qualified_three_part",
			sql:  "SELECT `my db`.t.`odd col` FROM `my db`.t",
			expected: []analysis.ColumnRef{
				{Table: "my db.t", Column: "odd col"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := nanopass.Parse(tt.sql)
			require.NoError(t, err)
			refs := analysis.ExtractColumns(pr)
			if tt.expected == nil {
				assert.Empty(t, refs)
			} else {
				assert.Equal(t, tt.expected, refs)
			}
		})
	}
}
