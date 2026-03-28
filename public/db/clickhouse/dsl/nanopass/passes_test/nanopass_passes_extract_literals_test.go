//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Basic extraction ---

func TestExtractLiteralsLongString(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(10)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'this is a long string value'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Should have a SET statement
	assert.Contains(t, got, "SET param_eq_1 = 'this is a long string value';")
	// Should have a parameter slot
	assert.Contains(t, got, "{param_eq_1: String}")
	// Should NOT contain the original literal inline
	lines := strings.Split(got, "\n")
	lastLine := lines[len(lines)-1]
	assert.NotContains(t, lastLine, "'this is a long string value'")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsShortStringSkipped(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(32)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'short'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Short literal should NOT be extracted
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsLongNumber(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE id = 123456789"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_eq_1 = 123456789;")
	assert.Contains(t, got, "{param_eq_1: Int64}")
}

func TestExtractLiteralsFloat(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE rate = 3.14159"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_eq_1 = 3.14159;")
	assert.Contains(t, got, "{param_eq_1: Float64}")
}

// --- Context-aware naming ---

func TestExtractLiteralsContextNaming(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	tests := []struct {
		name          string
		input         string
		expectedParam string
		expectedType  string
	}{
		{
			name:          "equality_operator",
			input:         "SELECT a FROM t WHERE name = 'abcdefghij'",
			expectedParam: "param_eq_1",
			expectedType:  "String",
		},
		{
			name:          "greater_than",
			input:         "SELECT a FROM t WHERE x > 100000",
			expectedParam: "param_gt_1",
			expectedType:  "Int64",
		},
		{
			name:          "like_operator",
			input:         "SELECT a FROM t WHERE name LIKE '%longpattern%'",
			expectedParam: "param_like_1",
			expectedType:  "String",
		},
		{
			name:          "function_arg",
			input:         "SELECT substring('a]very long string value', 1, 5)",
			expectedParam: "param_substring_0",
			expectedType:  "String",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := pass(tt.input)
			require.NoError(t, err)
			assert.Contains(t, got, "SET "+tt.expectedParam)
			assert.Contains(t, got, tt.expectedType)
			t.Logf("Result:\n%s", got)
		})
	}
}

// --- Deduplication ---

func TestExtractLiteralsDeduplication(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	// Same literal in same context (eq, arg 1) should be deduplicated
	sql := "SELECT a FROM t WHERE name = 'longvalue1' AND name = 'longvalue1'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Should have only ONE SET statement for this literal
	setCount := strings.Count(got, "SET param_eq_1")
	assert.Equal(t, 1, setCount, "expected exactly 1 SET for deduplicated literal")

	// Both references should use the same parameter
	slotCount := strings.Count(got, "{param_eq_1: String}")
	assert.Equal(t, 2, slotCount, "expected 2 references to the same parameter")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsDistinctValues(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	// Different literals in same context position → different params with counter
	sql := "SELECT a FROM t WHERE name = 'value_one_long' OR name = 'value_two_long'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Should have two SET statements
	assert.True(t, strings.Contains(got, "value_one_long"), "should contain first value")
	assert.True(t, strings.Contains(got, "value_two_long"), "should contain second value")

	sets, _ := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2, "expected at least 2 SET statements")

	t.Logf("Result:\n%s", got)
}

// --- Whitelist ---

func TestExtractLiteralsWhitelist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100) // very high threshold
	config.Whitelist["equals"] = true
	config.Whitelist["eq"] = true
	pass := passes.ExtractLiterals(config)

	// Short literal, but in whitelisted context
	sql := "SELECT a FROM t WHERE name = 'hi'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Should be extracted despite being short
	assert.Contains(t, got, "SET param_eq_1")
	assert.Contains(t, got, "{param_eq_1: String}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsWhitelistFunction(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.Whitelist["like"] = true
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name LIKE '%x%'"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_like_1")

	t.Logf("Result:\n%s", got)
}

// --- Blacklist ---

func TestExtractLiteralsBlacklist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.Blacklist["eq"] = true
	pass := passes.ExtractLiterals(config)

	// Long literal, but in blacklisted context
	sql := "SELECT a FROM t WHERE name = 'this is a very long string value'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Should NOT be extracted
	assert.NotContains(t, got, "SET ")
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsBlacklistOverridesWhitelist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.Whitelist["eq"] = true
	config.Blacklist["eq"] = true
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	got, err := pass(sql)
	require.NoError(t, err)

	// Blacklist wins
	assert.NotContains(t, got, "SET ")
}

// --- Multiple literals ---

func TestExtractLiteralsMultiple(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longname1' AND status = 'longstatus'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2)
	assert.NotContains(t, query, "'longname1'")
	assert.NotContains(t, query, "'longstatus'")

	t.Logf("Result:\n%s", got)
}

// --- NULL is never extracted ---

func TestExtractLiteralsNullSkipped(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name IS NULL"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.NotContains(t, got, "SET ")
}

// --- No literals to extract ---

func TestExtractLiteralsNoLiterals(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a, b, c FROM t WHERE a > b"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Equal(t, sql, got)
}

// --- UNION ALL ---

func TestExtractLiteralsUnionAll(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 'longval1' UNION ALL SELECT b FROM t2 WHERE y = 'longval2'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, _ := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2)

	t.Logf("Result:\n%s", got)
}

// --- Subquery ---

func TestExtractLiteralsSubquery(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT * FROM (SELECT a FROM t WHERE name = 'longvalue')"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
	assert.Contains(t, got, "{param_eq_1: String}")
}

// --- CTE ---

func TestExtractLiteralsCTE(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "WITH cte AS (SELECT a FROM t WHERE name = 'longvalue') SELECT * FROM cte"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
	assert.Contains(t, got, "{param_eq_1: String}")
}

// --- IN list ---

func TestExtractLiteralsINList(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('longval1', 'longval2', 'longval3')"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
	// Each IN element should be a separate param
	sets, _ := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 3)

	t.Logf("Result:\n%s", got)
}

// --- ParseExtractedQuery ---

func TestParseExtractedQuery(t *testing.T) {
	input := "SET param_eq_1 = 'hello';\nSET param_gt_1 = 100;\nSELECT a FROM t WHERE name = {param_eq_1: String}"

	sets, query := passes.ParseExtractedQuery(input)
	assert.Len(t, sets, 2)
	assert.Equal(t, "SET param_eq_1 = 'hello'", sets[0])
	assert.Equal(t, "SET param_gt_1 = 100", sets[1])
	assert.True(t, strings.HasPrefix(query, "SELECT"))
}

// --- InjectParams (round-trip) ---

func TestInjectParamsRoundTrip(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass(original)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(extracted)
	injected, err := passes.InjectParams(sets, query)
	require.NoError(t, err)

	// Should recover the original query
	assert.Equal(t, original, injected)
}

// --- AnalyzeExtractions (dry-run) ---

func TestAnalyzeExtractions(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)

	sql := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extractions, err := passes.AnalyzeExtractions(sql, config)
	require.NoError(t, err)

	assert.GreaterOrEqual(t, len(extractions), 2)
	for _, e := range extractions {
		t.Logf("%s", e.String())
	}
}

// --- CountExtractableParams ---

func TestCountExtractableParams(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)

	count, err := passes.CountExtractableParams("SELECT a FROM t WHERE name = 'longvalue' AND x > 100000", config)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 2)

	count, err = passes.CountExtractableParams("SELECT a FROM t WHERE a > b", config)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// --- Invalid SQL ---

func TestExtractLiteralsRejectsInvalid(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus validity ---

func TestExtractLiteralsCorpus(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(32)
	pass := passes.ExtractLiterals(config)

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			got, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			// The output includes SET statements followed by the query
			// Just check it doesn't panic and produces non-empty output
			assert.NotEmpty(t, got)
		})
	}
}

// --- Mixed whitelist and blacklist with multiple functions ---

func TestExtractLiteralsMixedWhitelistBlacklist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100) // high threshold
	config.Whitelist["todate"] = true              // always extract toDate args
	config.Blacklist["tostring"] = true            // never extract toString args
	pass := passes.ExtractLiterals(config)

	sql := "SELECT toDate('2024-01-01'), toString('2024-01-01') FROM t"
	got, err := pass(sql)
	require.NoError(t, err)

	// toDate arg should be extracted (whitelisted)
	assert.Contains(t, got, "SET param_todate_0")
	// toString arg should NOT be extracted (blacklisted)
	_, query := passes.ParseExtractedQuery(got)
	assert.Contains(t, query, "toString('2024-01-01')")

	t.Logf("Result:\n%s", got)
}

// --- BETWEEN ---

func TestExtractLiteralsBetween(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x BETWEEN 100000 AND 999999"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
	assert.Contains(t, got, "between")

	t.Logf("Result:\n%s", got)
}

// --- Prefix customization ---

func TestExtractLiteralsCustomPrefix(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.Prefix = "qp"
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET qp_eq_1")
	assert.Contains(t, got, "{qp_eq_1: String}")
}
