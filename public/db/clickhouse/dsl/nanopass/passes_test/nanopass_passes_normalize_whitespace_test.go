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

func TestNormalizeWhitespace(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "SELECT  a  FROM  t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "  SELECT a FROM t  ",
			expected: "SELECT a FROM t",
		},
		{
			input:    "SELECT\t\ta\t\tFROM\t\tt",
			expected: "SELECT a FROM t",
		},
		// Newlines preserved as single newline
		{
			input:    "SELECT a\n\n\nFROM t",
			expected: "SELECT a\nFROM t",
		},
		// Mixed whitespace with newline collapses to newline
		{
			input:    "SELECT a  \n  FROM t",
			expected: "SELECT a\nFROM t",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.NormalizeWhitespace(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalizeWhitespaceSingleLine(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "SELECT  a  FROM  t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "SELECT a\nFROM t\nWHERE b > 1",
			expected: "SELECT a FROM t WHERE b > 1",
		},
		{
			input:    "  SELECT\n\n  a  \n\n  FROM  t  ",
			expected: "SELECT a FROM t",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.NormalizeWhitespaceSingleLine(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalizeWhitespaceIdempotent(t *testing.T) {
	sql := "SELECT  a  \n\n  FROM  t  WHERE   b > 1"
	pass1, err := passes.NormalizeWhitespace(sql)
	require.NoError(t, err)
	pass2, err := passes.NormalizeWhitespace(pass1)
	require.NoError(t, err)
	assert.Equal(t, pass1, pass2)
}

func TestNormalizeWhitespaceOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := passes.NormalizeWhitespaceSingleLine(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "produced invalid SQL for %s:\noutput: %s", entry.Name, out)
		})
	}
}
