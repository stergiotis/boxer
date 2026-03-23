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

func TestNormalizeKeywordCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "select a from t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "Select A, b From T Where X > 1",
			expected: "SELECT A, b FROM T WHERE X > 1",
		},
		{
			input:    "SELECT a FROM t",
			expected: "SELECT a FROM t",
		},
		{
			input:    "select distinct a from t order by a desc limit 10",
			expected: "SELECT DISTINCT a FROM t ORDER BY a DESC LIMIT 10",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.NormalizeKeywordCase(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalizeKeywordCaseIdempotent(t *testing.T) {
	sql := "select A, b from T where X > 1"
	pass1, err := passes.NormalizeKeywordCase(sql)
	require.NoError(t, err)
	pass2, err := passes.NormalizeKeywordCase(pass1)
	require.NoError(t, err)
	assert.Equal(t, pass1, pass2, "NormalizeKeywordCase is not idempotent")
}

func TestNormalizeKeywordCaseOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := passes.NormalizeKeywordCase(entry.SQL)
			require.NoError(t, err, "pass failed on %s", entry.Name)
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "pass produced invalid SQL for %s:\noutput: %s", entry.Name, out)
		})
	}
}
