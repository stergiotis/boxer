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
		{
			// ClickHouse identifiers are case-sensitive: keyword tokens used as
			// database / table / column names (e.g. system, tables, events, id)
			// must keep their original case.
			input:    "SELECT * FROM system.tables",
			expected: "SELECT * FROM system.tables",
		},
		{
			input:    "select id, name from events where day > 1",
			expected: "SELECT id, name FROM events WHERE day > 1",
		},
		{
			// Real keyword positions stay uppercased even when adjacent to
			// keywords-as-identifiers.
			input:    "select extract(day from ts), interval 1 day from system.events",
			expected: "SELECT EXTRACT(DAY FROM ts), INTERVAL 1 DAY FROM system.events",
		},
	}
	for i, tt := range tests {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			got, err := passes.CanonicalizeKeywordCase.Run(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNormalizeKeywordCaseIdempotent(t *testing.T) {
	sql := "select A, b from T where X > 1"
	pass1, err := passes.CanonicalizeKeywordCase.Run(sql)
	require.NoError(t, err)
	pass2, err := passes.CanonicalizeKeywordCase.Run(pass1)
	require.NoError(t, err)
	assert.Equal(t, pass1, pass2, "CanonicalizeKeywordCase is not idempotent")
}

func TestNormalizeKeywordCaseOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			out, err := passes.CanonicalizeKeywordCase.Run(entry.SQL)
			require.NoError(t, err, "pass failed on %s", entry.Name)
			_, err = nanopass.Parse(out)
			require.NoError(t, err, "pass produced invalid SQL for %s:\noutput: %s", entry.Name, out)
		})
	}
}
