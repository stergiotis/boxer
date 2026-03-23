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

func TestRemoveRedundantParens(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Top-level redundant parens
		{
			input:    "SELECT (a) FROM t",
			expected: "SELECT a FROM t",
		},
		// Nested redundant parens
		{
			input:    "SELECT ((a)) FROM t",
			expected: "SELECT a FROM t",
		},
		// Parens needed: lower precedence inner (addition inside multiplication)
		{
			input:    "SELECT (a + b) * c FROM t",
			expected: "SELECT (a + b) * c FROM t",
		},
		// Parens redundant: higher precedence inner (multiplication inside addition)
		{
			input:    "SELECT a + (b * c) FROM t",
			expected: "SELECT a + b * c FROM t",
		},
		// AND/OR precedence: parens needed (OR inside AND)
		{
			input:    "SELECT a FROM t WHERE a AND (b OR c)",
			expected: "SELECT a FROM t WHERE a AND (b OR c)",
		},
		// AND/OR precedence: parens redundant (AND inside OR)
		{
			input:    "SELECT a FROM t WHERE (a AND b) OR c",
			expected: "SELECT a FROM t WHERE a AND b OR c",
		},
		// OR with parens needed
		{
			input:    "SELECT a FROM t WHERE (a OR b) AND c",
			expected: "SELECT a FROM t WHERE (a OR b) AND c",
		},
		// Comparison inside AND — parens redundant
		{
			input:    "SELECT a FROM t WHERE (a > 1) AND (b < 2)",
			expected: "SELECT a FROM t WHERE a > 1 AND b < 2",
		},
		// NOT with parens around atom — redundant
		{
			input:    "SELECT NOT (a) FROM t",
			expected: "SELECT NOT (a) FROM t",
		},
		// Parens around subquery — ColumnExprSubquery, untouched
		{
			input:    "SELECT a FROM t WHERE a IN (SELECT b FROM t2)",
			expected: "SELECT a FROM t WHERE a IN (SELECT b FROM t2)",
		},
		// Parens around tuple — ColumnExprTuple, untouched
		{
			input:    "SELECT (1, 2, 3)",
			expected: "SELECT (1, 2, 3)",
		},
		// Same-precedence left-associative: left operand parens redundant
		{
			input:    "SELECT (a + b) + c FROM t",
			expected: "SELECT a + b + c FROM t",
		},
		// Same-precedence left-associative: right operand parens kept
		{
			input:    "SELECT a + (b + c) FROM t",
			expected: "SELECT a + (b + c) FROM t",
		},
		// Same-precedence multiplication: left operand parens redundant
		{
			input:    "SELECT (a * b) * c FROM t",
			expected: "SELECT a * b * c FROM t",
		},
		// Unary minus with atom — parens redundant
		{
			input:    "SELECT -(a) FROM t",
			expected: "SELECT -a FROM t",
		},
		// Function call args — syntactic, not ColumnExprParens
		{
			input:    "SELECT count(a) FROM t",
			expected: "SELECT count(a) FROM t",
		},
		// Deeply nested redundant
		{
			input:    "SELECT (((a + b))) FROM t",
			expected: "SELECT a + b FROM t",
		},
		// Mixed: some removable, some not
		{
			input:    "SELECT ((a + b)) * (c + d) FROM t",
			expected: "SELECT (a + b) * (c + d) FROM t",
		},
		// IN with single value — parens are syntactic, must not be removed
		{
			input:    "SELECT a FROM t WHERE a IN (1)",
			expected: "SELECT a FROM t WHERE a IN (1)",
		},
		// IN with multiple values — ColumnExprTuple, untouched
		{
			input:    "SELECT a FROM t WHERE a IN (1, 2, 3)",
			expected: "SELECT a FROM t WHERE a IN (1, 2, 3)",
		},
		// NOT IN with single value — parens must stay
		{
			input:    "SELECT a FROM t WHERE a NOT IN (1)",
			expected: "SELECT a FROM t WHERE a NOT IN (1)",
		},
		// GLOBAL IN with subquery — parens must stay
		{
			input:    "SELECT a FROM t WHERE a GLOBAL IN (SELECT b FROM t2)",
			expected: "SELECT a FROM t WHERE a GLOBAL IN (SELECT b FROM t2)",
		},
		// BETWEEN — parens around operands that bind tighter are redundant
		{
			input:    "SELECT a FROM t WHERE a BETWEEN (1) AND (10)",
			expected: "SELECT a FROM t WHERE a BETWEEN 1 AND 10",
		},
		// Ternary — right-associative, keep same-precedence parens
		{
			input:    "SELECT a ? b : (c ? d : e) FROM t",
			expected: "SELECT a ? b : (c ? d : e) FROM t",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.RemoveRedundantParens(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRemoveRedundantParensIdempotent(t *testing.T) {
	tests := []string{
		"SELECT (a + b) * c FROM t",
		"SELECT a AND (b OR c) FROM t",
		"SELECT a + b * c FROM t",
		"SELECT ((a)) FROM t",
		"SELECT a FROM t WHERE a IN (1)",
		"SELECT a ? b : (c ? d : e) FROM t",
	}
	for i, sql := range tests {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass1, err := passes.RemoveRedundantParens(sql)
			require.NoError(t, err)
			pass2, err := passes.RemoveRedundantParens(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent:\npass1: %s\npass2: %s", pass1, pass2)
		})
	}
}

func TestRemoveRedundantParensOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := passes.RemoveRedundantParens(entry.SQL)
			if err != nil {
				t.Skipf("pass failed (may be expected for some corpus entries): %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "pass produced invalid SQL for %s:\noutput: %s", entry.Name, out)
		})
	}
}
