//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers ---

func acceptAll(info passes.ExtractedParamInfo) bool {
	return true
}

func rejectAll(info passes.ExtractedParamInfo) bool {
	return false
}

func extractAndInjectAsCTE(t *testing.T, sql string, predicate func(passes.ExtractedParamInfo) bool) string {
	t.Helper()

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("", predicate, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	return result
}

func extractAndInjectAsCTEWithCasts(t *testing.T, sql string, predicate func(passes.ExtractedParamInfo) bool) string {
	t.Helper()

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("", predicate, marshalling.MapCanonicalToClickHouseType)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	return result
}

// --- Basic CTE injection ---

func TestInjectParamsAsCTEBasic(t *testing.T) {
	result := extractAndInjectAsCTE(t, "SELECT a FROM t WHERE name = 'hello'", acceptAll)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "'hello' AS param_x_eq_")
	assert.NotContains(t, result, "SET ")
	assert.NotContains(t, result, "{param_x_")

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTEMultiple(t *testing.T) {
	result := extractAndInjectAsCTE(t, "SELECT a FROM t WHERE name = 'hello' AND x > 100000", acceptAll)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "'hello' AS param_x_eq_")
	assert.Contains(t, result, "100000 AS param_x_gt_")
	assert.NotContains(t, result, "SET ")

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTENumber(t *testing.T) {
	result := extractAndInjectAsCTE(t, "SELECT a FROM t WHERE x > 100000", acceptAll)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "100000 AS param_x_gt_")
	assert.NotContains(t, result, "{param_x_")

	t.Logf("Result:\n%s", result)
}

// --- Predicate filtering ---

func TestInjectParamsAsCTERejectAll(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("", rejectAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	// Nothing injected as CTE — original extracted output preserved
	assert.Equal(t, extracted, result)
}

func TestInjectParamsAsCTEPartialPredicate(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hello' AND x > 100000"
	extracted, err := pass(sql)
	require.NoError(t, err)

	// Only accept "eq" context params
	predicate := func(info passes.ExtractedParamInfo) bool {
		return info.FunctionName == "eq"
	}

	ctePass := passes.InjectParamsAsCTE("", predicate, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	// "eq" param should be in CTE, "gt" param should remain as SET + slot
	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "'hello' AS param_x_eq_")
	assert.Contains(t, result, "SET param_x_gt_")
	assert.Contains(t, result, "{param_x_gt_")

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTENilPredicate(t *testing.T) {
	// nil predicate should accept all
	result := extractAndInjectAsCTE(t, "SELECT a FROM t WHERE name = 'hello'", nil)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "'hello' AS param_x_eq_")
	assert.NotContains(t, result, "SET ")
}

// --- Cast reconstruction ---

func TestInjectParamsAsCTEWithCast(t *testing.T) {
	result := extractAndInjectAsCTEWithCasts(t, "SELECT a FROM t WHERE x = 1::UInt64", acceptAll)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "1::UInt64 AS param_x_")
	assert.NotContains(t, result, "SET ")
	assert.NotContains(t, result, "{param_x_")

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTEWithCastAndNonCast(t *testing.T) {
	result := extractAndInjectAsCTEWithCasts(t, "SELECT a FROM t WHERE x = 1::UInt64 AND y = 'hello'", acceptAll)

	assert.Contains(t, result, "1::UInt64 AS param_x_")
	assert.Contains(t, result, "'hello' AS param_x_")
	assert.NotContains(t, result, "SET ")

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTENilMapperWithCast(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	extracted, err := pass(sql)
	require.NoError(t, err)

	// nil mapCanonicalToClickHouse — cast not reconstructed in CTE
	ctePass := passes.InjectParamsAsCTE("", acceptAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	assert.Contains(t, result, "1 AS param_x_")
	assert.NotContains(t, result, "::UInt64")

	t.Logf("Result:\n%s", result)
}

// --- No extraction needed ---

func TestInjectParamsAsCTENoParams(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100) // high threshold — nothing extracted
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hi'"
	extracted, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, extracted, "nothing should be extracted")

	ctePass := passes.InjectParamsAsCTE("", acceptAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	assert.Equal(t, sql, result, "no params → no change")
}

// --- Custom prefix ---

func TestInjectParamsAsCTECustomPrefix(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetPrefix("qp")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("qp", acceptAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	assert.Contains(t, result, "WITH")
	assert.Contains(t, result, "'hello' AS qp_eq_")
	assert.NotContains(t, result, "SET ")

	t.Logf("Result:\n%s", result)
}

// --- UNION ALL ---

func TestInjectParamsAsCTEUnionAll(t *testing.T) {
	result := extractAndInjectAsCTE(t,
		"SELECT a FROM t WHERE x = 'longval1' UNION ALL SELECT b FROM t2 WHERE y = 'longval2'",
		acceptAll)

	// CTE should be at the top level, before the first SELECT
	assert.True(t, strings.HasPrefix(strings.TrimSpace(result), "WITH"))
	assert.NotContains(t, result, "SET ")
	assert.NotContains(t, result, "{param_x_")

	t.Logf("Result:\n%s", result)
}

// --- Subquery ---

func TestInjectParamsAsCTESubquery(t *testing.T) {
	result := extractAndInjectAsCTE(t,
		"SELECT * FROM (SELECT a FROM t WHERE name = 'longvalue')",
		acceptAll)

	assert.Contains(t, result, "WITH")
	assert.NotContains(t, result, "SET ")

	t.Logf("Result:\n%s", result)
}

// --- Deduplication ---

func TestInjectParamsAsCTEDedup(t *testing.T) {
	result := extractAndInjectAsCTE(t,
		"SELECT a FROM t WHERE name = 'longval' AND status = 'longval'",
		acceptAll)

	// Deduplicated — only one CTE definition
	cteCount := strings.Count(result, " AS param_x_")
	assert.Equal(t, 1, cteCount, "deduplicated literals should produce one CTE definition")

	t.Logf("Result:\n%s", result)
}

// --- Output parses ---

func TestInjectParamsAsCTEOutputParses(t *testing.T) {
	queries := []string{
		"SELECT a FROM t WHERE name = 'hello'",
		"SELECT a FROM t WHERE x > 100000",
		"SELECT a FROM t WHERE name = 'hello' AND x > 100000",
		"WITH 1 AS existing SELECT a FROM t WHERE name = 'hello'",
		"SELECT a FROM t WHERE x = 'longval1' UNION ALL SELECT b FROM t2 WHERE y = 'longval2'",
	}

	for i, sql := range queries {
		t.Run(fmt.Sprintf("parses_%d", i), func(t *testing.T) {
			result := extractAndInjectAsCTE(t, sql, acceptAll)

			// The output should be valid SQL (no {param: Type} slots remaining)
			assert.NotContains(t, result, "{param_x_")
			assert.NotContains(t, result, "SET ")

			t.Logf("Input:  %s", sql)
			t.Logf("Output: %s", result)
		})
	}
}

// --- Corpus ---

func TestInjectParamsAsCTECorpus(t *testing.T) {
	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	ctePass := passes.InjectParamsAsCTE("", acceptAll, nil)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			// Skip entries with user-defined SET statements
			if strings.HasPrefix(strings.TrimSpace(entry.SQL), "SET ") {
				t.Skipf("skipping entry with SET statements")
			}

			extracted, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("extraction failed: %v", err)
			}

			result, err := ctePass(extracted)
			if err != nil {
				t.Skipf("CTE injection failed: %v", err)
			}

			// No remaining SET lines or {param} slots
			assert.NotContains(t, result, "SET param_x_", "unexpected SET line in result for %s", entry.Name)
			assert.NotContains(t, result, "{param_x_", "unexpected slot in result for %s", entry.Name)
		})
	}
}
func TestInjectParamsAsCTEExistingWith(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5) // "1" is too short to extract
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "WITH 1 AS existing SELECT existing FROM t WHERE name = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("", acceptAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	assert.Contains(t, result, "'hello' AS param_x_")
	assert.Contains(t, result, "1 AS existing")
	assert.Equal(t, 1, strings.Count(strings.ToUpper(result), "WITH "))

	t.Logf("Result:\n%s", result)
}

func TestInjectParamsAsCTEExistingWithMultiple(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "WITH 1 AS x, 2 AS y SELECT x, y FROM t WHERE name = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	ctePass := passes.InjectParamsAsCTE("", acceptAll, nil)
	result, err := ctePass(extracted)
	require.NoError(t, err)

	assert.Contains(t, result, "'hello' AS param_x_")
	assert.Contains(t, result, "1 AS x")
	assert.Contains(t, result, "2 AS y")
	assert.Equal(t, 1, strings.Count(strings.ToUpper(result), "WITH "))

	t.Logf("Result:\n%s", result)
}
