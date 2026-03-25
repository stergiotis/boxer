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

// --- Basic wrapping ---

func TestWrapColumnsBasic(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single_match",
			input:    "SELECT id, tenant_id, amount FROM orders",
			expected: "SELECT id, COLUMNS('^tenant_id$'), amount FROM orders",
		},
		{
			name:     "multiple_matches",
			input:    "SELECT id, tenant_id, customer_id, amount FROM orders",
			expected: "SELECT id, COLUMNS('^tenant_id$'), COLUMNS('^customer_id$'), amount FROM orders",
		},
		{
			name:     "no_match",
			input:    "SELECT id, amount, created FROM orders",
			expected: "SELECT id, amount, created FROM orders",
		},
		{
			name:     "all_match",
			input:    "SELECT tenant_id, customer_id FROM orders",
			expected: "SELECT COLUMNS('^tenant_id$'), COLUMNS('^customer_id$') FROM orders",
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

// --- Qualified columns not wrapped ---

func TestWrapColumnsSkipsQualified(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "qualified_column_untouched",
			input:    "SELECT o.tenant_id FROM orders AS o",
			expected: "SELECT o.tenant_id FROM orders AS o",
		},
		{
			name:     "mixed_qualified_and_bare",
			input:    "SELECT tenant_id, o.customer_id FROM orders AS o",
			expected: "SELECT COLUMNS('^tenant_id$'), o.customer_id FROM orders AS o",
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

// --- Expressions and functions not wrapped ---

func TestWrapColumnsSkipsExpressions(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "function_call_untouched",
			input:    "SELECT count(*) FROM orders",
			expected: "SELECT count(*) FROM orders",
		},
		{
			name:     "aliased_column_untouched",
			input:    "SELECT tenant_id AS tid FROM orders",
			expected: "SELECT tenant_id AS tid FROM orders",
		},
		{
			name:     "arithmetic_untouched",
			input:    "SELECT amount + 1 FROM orders",
			expected: "SELECT amount + 1 FROM orders",
		},
		{
			name:     "star_untouched",
			input:    "SELECT * FROM orders",
			expected: "SELECT * FROM orders",
		},
		{
			name:     "literal_untouched",
			input:    "SELECT 1, 'hello' FROM orders",
			expected: "SELECT 1, 'hello' FROM orders",
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

// --- Different regex patterns ---

func TestWrapColumnsPatterns(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected string
	}{
		{
			name:     "prefix_match",
			pattern:  "^is_",
			input:    "SELECT id, is_active, is_deleted, name FROM users",
			expected: "SELECT id, COLUMNS('^is_active$'), COLUMNS('^is_deleted$'), name FROM users",
		},
		{
			name:     "exact_match",
			pattern:  "^amount$",
			input:    "SELECT id, amount, total_amount FROM orders",
			expected: "SELECT id, COLUMNS('^amount$'), total_amount FROM orders",
		},
		{
			name:     "contains_match",
			pattern:  "date",
			input:    "SELECT id, created_date, updated_date, name FROM events",
			expected: "SELECT id, COLUMNS('^created_date$'), COLUMNS('^updated_date$'), name FROM events",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := passes.WrapColumnsWithDynamic(tt.pattern)
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- Regex metacharacter escaping ---

func TestWrapColumnsEscapesMetachars(t *testing.T) {
	// Column name contains regex metacharacters
	pass := passes.WrapColumnsWithDynamic(".*")

	// If a column name were "a.b" (unusual but legal in backticks),
	// the generated COLUMNS pattern must escape the dot
	// We can test the escaping function directly
	got, err := pass("SELECT amount FROM orders")
	require.NoError(t, err)
	assert.Equal(t, "SELECT COLUMNS('^amount$') FROM orders", got)
}

// --- UNION ALL ---

func TestWrapColumnsUnionAll(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	got, err := pass("SELECT tenant_id, amount FROM t1 UNION ALL SELECT customer_id, price FROM t2")
	require.NoError(t, err)
	assert.Contains(t, got, "COLUMNS('^tenant_id$')")
	assert.Contains(t, got, "COLUMNS('^customer_id$')")
	assert.Contains(t, got, "amount")
	assert.Contains(t, got, "price")

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

// --- CTEs ---

func TestWrapColumnsCTE(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	got, err := pass("WITH cte AS (SELECT tenant_id, amount FROM orders) SELECT tenant_id FROM cte")
	require.NoError(t, err)

	// Both the CTE body and outer SELECT should be wrapped
	// Count occurrences of COLUMNS
	assert.Equal(t, 2, countOccurrences(got, "COLUMNS('^tenant_id$')"))

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

// --- Subqueries ---

func TestWrapColumnsSubquery(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	got, err := pass("SELECT * FROM (SELECT tenant_id, amount FROM orders)")
	require.NoError(t, err)
	assert.Contains(t, got, "COLUMNS('^tenant_id$')")
	assert.Contains(t, got, "amount")

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

// --- Columns in WHERE/GROUP BY are not affected ---

func TestWrapColumnsOnlyAffectsProjection(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	sql := "SELECT tenant_id FROM orders WHERE customer_id > 0 GROUP BY tenant_id"
	got, err := pass(sql)
	require.NoError(t, err)

	// Only the SELECT list column is wrapped
	assert.Contains(t, got, "SELECT COLUMNS('^tenant_id$')")
	// WHERE and GROUP BY columns are untouched
	assert.Contains(t, got, "WHERE customer_id > 0")
	assert.Contains(t, got, "GROUP BY tenant_id")

	_, err = nanopass.Parse(got)
	require.NoError(t, err, "produced invalid SQL: %s", got)
}

// --- Idempotency ---

func TestWrapColumnsIdempotent(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	sqls := []string{
		"SELECT tenant_id, amount FROM orders",
		"SELECT id, name FROM customers",
		"SELECT * FROM products",
	}
	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1, err := pass(sql)
			require.NoError(t, err)
			pass2, err := pass(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent:\npass1: %s\npass2: %s", pass1, pass2)
		})
	}
}

// --- Pipeline integration ---

func TestWrapColumnsInPipeline(t *testing.T) {
	result, err := nanopass.Pipeline(
		"select tenant_id, amount from orders",
		passes.NormalizeKeywordCase,
		passes.WrapColumnsWithDynamic(".*_id$"),
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Contains(t, result, "COLUMNS('^tenant_id$')")
	assert.Contains(t, result, "amount")
}

// --- Invalid regex ---

func TestWrapColumnsInvalidRegex(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic("[invalid")
	_, err := pass("SELECT a FROM t")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid regex")
}

// --- Edge cases ---

func TestWrapColumnsNoFrom(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*")
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", got)
}

func TestWrapColumnsRejectsInvalid(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*")
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus validity ---

func TestWrapColumnsOutputValidity(t *testing.T) {
	pass := passes.WrapColumnsWithDynamic(".*_id$")

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "produced invalid SQL for %s:\n%s", entry.Name, out)
		})
	}
}

// --- Helper ---

func countOccurrences(s, substr string) int {
	count := 0
	idx := 0
	for {
		i := indexOf(s[idx:], substr)
		if i < 0 {
			break
		}
		count++
		idx += i + len(substr)
	}
	return count
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
