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

// --- SetFormat: add FORMAT ---

func TestSetFormatAdd(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		format   string
		expected string
	}{
		{
			name:     "add_json",
			input:    "SELECT 1",
			format:   "JSON",
			expected: "SELECT 1 FORMAT JSON",
		},
		{
			name:     "add_tab_separated",
			input:    "SELECT a FROM t",
			format:   "TabSeparated",
			expected: "SELECT a FROM t FORMAT TabSeparated",
		},
		{
			name:     "add_json_each_row",
			input:    "SELECT a FROM t WHERE x > 1",
			format:   "JSONEachRow",
			expected: "SELECT a FROM t WHERE x > 1 FORMAT JSONEachRow",
		},
		{
			name:     "add_to_query_with_order_by",
			input:    "SELECT a FROM t ORDER BY a LIMIT 10",
			format:   "CSV",
			expected: "SELECT a FROM t ORDER BY a LIMIT 10 FORMAT CSV",
		},
		{
			name:     "add_to_query_with_settings",
			input:    "SELECT a FROM t SETTINGS max_threads = 1",
			format:   "JSON",
			expected: "SELECT a FROM t SETTINGS max_threads = 1 FORMAT JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := passes.SetFormat(tt.format)
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- SetFormat: replace existing FORMAT ---

func TestSetFormatReplace(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		format   string
		expected string
	}{
		{
			name:     "replace_json_with_csv",
			input:    "SELECT 1 FORMAT JSON",
			format:   "CSV",
			expected: "SELECT 1 FORMAT CSV",
		},
		{
			name:     "replace_tab_with_json_each_row",
			input:    "SELECT a FROM t FORMAT TabSeparated",
			format:   "JSONEachRow",
			expected: "SELECT a FROM t FORMAT JSONEachRow",
		},
		{
			name:     "replace_with_same",
			input:    "SELECT 1 FORMAT JSON",
			format:   "JSON",
			expected: "SELECT 1 FORMAT JSON",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := passes.SetFormat(tt.format)
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- SetFormat: remove FORMAT ---

func TestSetFormatRemove(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "remove_json",
			input:    "SELECT 1 FORMAT JSON",
			expected: "SELECT 1",
		},
		{
			name:     "remove_tab_separated",
			input:    "SELECT a FROM t FORMAT TabSeparated",
			expected: "SELECT a FROM t",
		},
		{
			name:     "remove_when_none_exists",
			input:    "SELECT 1",
			expected: "SELECT 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pass := passes.SetFormat("")
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)

			_, err = nanopass.Parse(got)
			require.NoError(t, err, "produced invalid SQL: %s", got)
		})
	}
}

// --- RemoveFormat convenience ---

func TestRemoveFormat(t *testing.T) {
	got, err := passes.RemoveFormat("SELECT 1 FORMAT JSON")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1", got)
}

// --- GetFormat ---

func TestGetFormat(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "json",
			input:    "SELECT 1 FORMAT JSON",
			expected: "JSON",
		},
		{
			name:     "tab_separated",
			input:    "SELECT a FROM t FORMAT TabSeparated",
			expected: "TabSeparated",
		},
		{
			name:     "json_each_row",
			input:    "SELECT a FROM t FORMAT JSONEachRow",
			expected: "JSONEachRow",
		},
		{
			name:     "no_format",
			input:    "SELECT 1",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := passes.GetFormat(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// --- Idempotency ---

func TestSetFormatIdempotent(t *testing.T) {
	sqls := []struct {
		input  string
		format string
	}{
		{"SELECT 1", "JSON"},
		{"SELECT 1 FORMAT CSV", "JSON"},
		{"SELECT 1 FORMAT JSON", ""},
		{"SELECT a FROM t ORDER BY a", "TabSeparated"},
	}
	for i, tt := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			pass := passes.SetFormat(tt.format)
			pass1, err := pass(tt.input)
			require.NoError(t, err)
			pass2, err := pass(pass1)
			require.NoError(t, err)
			assert.Equal(t, pass1, pass2, "not idempotent")
		})
	}
}

// --- Pipeline integration ---

func TestSetFormatInPipeline(t *testing.T) {
	result, err := nanopass.Pipeline(
		"select a from t",
		passes.CanonicalizeKeywordCase,
		passes.SetFormat("JSON"),
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t FORMAT JSON", result)
}

func TestSetFormatWithRemoveAndAdd(t *testing.T) {
	// Remove existing format, then add a new one
	result, err := nanopass.Pipeline(
		"SELECT a FROM t FORMAT CSV",
		passes.SetFormat(""),     // remove
		passes.SetFormat("JSON"), // add
		nanopass.Validate,
	)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t FORMAT JSON", result)
}

// --- UNION ALL ---

func TestSetFormatUnionAll(t *testing.T) {
	// FORMAT applies to the whole query, not per-branch
	pass := passes.SetFormat("JSON")
	got, err := pass("SELECT a FROM t1 UNION ALL SELECT b FROM t2")
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t1 UNION ALL SELECT b FROM t2 FORMAT JSON", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

// --- Edge cases ---

func TestSetFormatNoFrom(t *testing.T) {
	pass := passes.SetFormat("JSON")
	got, err := pass("SELECT 1")
	require.NoError(t, err)
	assert.Equal(t, "SELECT 1 FORMAT JSON", got)

	_, err = nanopass.Parse(got)
	require.NoError(t, err)
}

func TestSetFormatRejectsInvalid(t *testing.T) {
	pass := passes.SetFormat("JSON")
	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus validity ---

func TestSetFormatOutputValidity(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	formats := []string{"JSON", "TabSeparated", "CSV", "JSONEachRow", ""}
	for _, format := range formats {
		pass := passes.SetFormat(format)
		for _, entry := range entries {
			t.Run(entry.Name+"/"+format, func(t *testing.T) {
				out, err := pass(entry.SQL)
				if err != nil {
					t.Skipf("pass failed: %v", err)
				}
				_, err = nanopass.Parse(out)
				require.NoError(t, err, "produced invalid SQL for %s with format %q:\n%s", entry.Name, format, out)
			})
		}
	}
}
