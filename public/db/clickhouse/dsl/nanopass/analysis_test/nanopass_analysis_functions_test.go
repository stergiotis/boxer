//go:build llm_generated_opus46

package analysis_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFunctions(t *testing.T) {
	tests := []struct {
		name     string
		sql      string
		expected []analysis.FunctionRef
	}{
		{
			name:     "no_functions",
			sql:      "SELECT a FROM t",
			expected: nil,
		},
		{
			name: "simple_function",
			sql:  "SELECT count(*) FROM t",
			expected: []analysis.FunctionRef{
				{Name: "count"},
			},
		},
		{
			name: "nested_functions",
			sql:  "SELECT toStartOfHour(toDateTime(a)) FROM t",
			expected: []analysis.FunctionRef{
				{Name: "toStartOfHour"},
				{Name: "toDateTime"},
			},
		},
		{
			name: "parametric_aggregate",
			sql:  "SELECT quantile(0.5)(x) FROM t",
			expected: []analysis.FunctionRef{
				{Name: "quantile", IsParametric: true},
			},
		},
		{
			name: "window_function",
			sql:  "SELECT row_number() OVER (ORDER BY a) FROM t",
			expected: []analysis.FunctionRef{
				{Name: "row_number", IsWindow: true},
			},
		},
		{
			name: "multiple_functions",
			sql:  "SELECT sum(a), avg(b) FROM t",
			expected: []analysis.FunctionRef{
				{Name: "sum"},
				{Name: "avg"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pr, err := nanopass.Parse(tt.sql)
			require.NoError(t, err)
			refs := analysis.ExtractFunctions(pr)
			if tt.expected == nil {
				assert.Empty(t, refs)
			} else {
				assert.Equal(t, tt.expected, refs)
			}
		})
	}
}
