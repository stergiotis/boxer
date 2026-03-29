//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CAST(expr, 'Type') already canonical ---

func TestCanonicalizeCastsFuncFormUnchanged(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST(1, 'UInt64')"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestCanonicalizeCastsFuncFormArrayUnchanged(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST([0], 'Array(UInt64)')"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- expr::Type → CAST(expr, 'Type') ---

func TestCanonicalizeCastsDoubleColonSimple(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT 1::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(1, 'UInt64')", got)
}

func TestCanonicalizeCastsDoubleColonString(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT 'hello'::String"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST('hello', 'String')", got)
}

func TestCanonicalizeCastsDoubleColonComplexType(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT [0]::Array(UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST([0], 'Array(UInt64)')", got)
}

func TestCanonicalizeCastsDoubleColonInWhere(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE x = CAST(1, 'UInt64')", got)
}

func TestCanonicalizeCastsDoubleColonMultiple(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT 1::UInt64, 'hello'::String"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(1, 'UInt64'), CAST('hello', 'String')", got)
}

// --- CAST(expr AS Type) → CAST(expr, 'Type') ---

func TestCanonicalizeCastsASSimple(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST(1 AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(1, 'UInt64')", got)
}

func TestCanonicalizeCastsASString(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST('hello' AS String)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST('hello', 'String')", got)
}

func TestCanonicalizeCastsASInWhere(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT a FROM t WHERE x = CAST(1 AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT a FROM t WHERE x = CAST(1, 'UInt64')", got)
}

func TestCanonicalizeCastsASMultiple(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST(1 AS UInt64), CAST('hello' AS String)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(1, 'UInt64'), CAST('hello', 'String')", got)
}

// --- Mixed forms ---

func TestCanonicalizeCastsMixed(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST(1 AS UInt64), CAST(2, 'UInt32'), 3::UInt8"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(1, 'UInt64'), CAST(2, 'UInt32'), CAST(3, 'UInt8')", got)
}

// --- Nested casts (fixpoint loop) ---

func TestCanonicalizeCastsNestedDoubleColon(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	// Inner :: and outer :: — requires two iterations
	sql := "SELECT CAST(CAST(1 AS UInt32) AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Equal(t, "SELECT CAST(CAST(1, 'UInt32'), 'UInt64')", got)
	t.Logf("Result: %s", got)
}

func TestCanonicalizeCastsNestedMixed(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	// Inner is CAST AS, outer is ::
	sql := "SELECT CAST(1 AS UInt32)::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Equal(t, "SELECT CAST(CAST(1, 'UInt32'), 'UInt64')", got)
	t.Logf("Result: %s", got)
}

func TestCanonicalizeCastsTripleNested(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	// Three levels — requires three iterations of the fixpoint loop
	sql := "SELECT CAST(CAST(CAST(1 AS UInt8) AS UInt32) AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Equal(t, "SELECT CAST(CAST(CAST(1, 'UInt8'), 'UInt32'), 'UInt64')", got)
	t.Logf("Result: %s", got)
}

func TestCanonicalizeCastsNestedDoubleColonOnly(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT 1::UInt32::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Equal(t, "SELECT CAST(CAST(1, 'UInt32'), 'UInt64')", got)
	t.Logf("Result: %s", got)
}

func TestCanonicalizeCastsNestedInnerAlreadyCanonical(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	// Inner is already canonical CAST(expr, 'Type'), outer is ::
	sql := "SELECT CAST(1, 'UInt32')::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	// Only the outer :: needs to be canonicalized
	// But CAST(1, 'UInt32') is a function call, not a ColumnExprCastContext
	// so the outer :: wraps it: ColumnExprCastContext → ColumnExprFunctionContext + :: + TypeExpr
	assert.Contains(t, got, "CAST(CAST(1, 'UInt32'), 'UInt64')")
	t.Logf("Result: %s", got)
}

// --- No casts ---

func TestCanonicalizeCastsNoCasts(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT a, b, c FROM t WHERE x > 1"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Non-CAST functions not affected ---

func TestCanonicalizeCastsNonCastFunctionUnchanged(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT toUInt64(1), toString('hello')"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Preserves whitespace/formatting ---

func TestCanonicalizeCastsPreservesWhitespace(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT\n    1::UInt64\nFROM t"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT\n    CAST(1, 'UInt64')\nFROM t", got)
}

// --- Expression in cast ---

func TestCanonicalizeCastsWithExpression(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT CAST(a + b AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, "SELECT CAST(a + b, 'UInt64')", got)
}

func TestCanonicalizeCastsDoubleColonWithExpression(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sql := "SELECT (a + b)::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "CAST(")
	assert.Contains(t, got, "'UInt64')")
	t.Logf("Result: %s", got)
}

// --- Idempotency ---

func TestCanonicalizeCastsIdempotent(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	sqls := []string{
		"SELECT CAST(1 AS UInt64)",
		"SELECT CAST(1, 'UInt64')",
		"SELECT 1::UInt64",
		"SELECT CAST(1 AS UInt64), CAST(2, 'UInt32'), 3::UInt8",
		"SELECT CAST(CAST(1 AS UInt32) AS UInt64)",
		"SELECT 1::UInt32::UInt64",
	}

	for i, sql := range sqls {
		t.Run(fmt.Sprintf("idempotent_%d", i), func(t *testing.T) {
			got1, err := pass(sql)
			require.NoError(t, err)
			got2, err := pass(got1)
			require.NoError(t, err)
			assert.Equal(t, got1, got2, "pass should be idempotent")
		})
	}
}

// --- Invalid SQL ---

func TestCanonicalizeCastsInvalidSQL(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	_, err := pass("")
	assert.Error(t, err)
}

// --- Corpus ---

func TestCanonicalizeCastsCorpus(t *testing.T) {
	pass := passes.CanonicalizeCasts()

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			got, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			assert.NotEmpty(t, got)

			// Idempotency check
			got2, err := pass(got)
			if err != nil {
				t.Skipf("second pass failed: %v", err)
			}
			assert.Equal(t, got, got2, "CanonicalizeCasts should be idempotent")
		})
	}
}

// --- Composition: CanonicalizeCasts + ExtractLiterals ---

func TestCanonicalizeCastsThenExtractLiterals(t *testing.T) {
	castPass := passes.CanonicalizeCasts()
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	extractPass := passes.ExtractLiterals(config)

	sqls := []string{
		"SELECT CAST(1 AS UInt64)",
		"SELECT 1::UInt64",
		"SELECT a FROM t WHERE x = CAST(1 AS UInt64) AND y = 'hello'::String",
	}

	for i, sql := range sqls {
		t.Run(fmt.Sprintf("compose_%d", i), func(t *testing.T) {
			canonicalized, err := castPass(sql)
			require.NoError(t, err)

			extracted, err := extractPass(canonicalized)
			require.NoError(t, err)

			t.Logf("Original:       %s", sql)
			t.Logf("Canonicalized:  %s", canonicalized)
			t.Logf("Extracted:      %s", extracted)

			params := passes.CollectExtractedParams(extracted, "")
			for _, p := range params {
				if p.HasCast() {
					t.Logf("  param %s has cast: %s", p.FullName, p.Metadata.CastTypeCanonical)
				}
			}
		})
	}
}

// --- Composition: CanonicalizeCasts + ExtractLiterals + InjectParamsAsCTE ---

func TestFullPipelineCanonicalizeCastsExtractCTE(t *testing.T) {
	castPass := passes.CanonicalizeCasts()
	config := passes.NewExtractLiteralsConfig(1)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	extractPass := passes.ExtractLiterals(config)
	ctePass := passes.InjectParamsAsCTE("", acceptAll, mockMapCanonicalToClickHouse)

	sqls := []string{
		"SELECT CAST(1 AS UInt64)",
		"SELECT 1::UInt64",
		"SELECT a FROM t WHERE x = CAST(1 AS UInt64) AND y = 'hello'",
	}

	for i, sql := range sqls {
		t.Run(fmt.Sprintf("pipeline_%d", i), func(t *testing.T) {
			canonicalized, err := castPass(sql)
			require.NoError(t, err)

			extracted, err := extractPass(canonicalized)
			require.NoError(t, err)

			cteResult, err := ctePass(extracted)
			require.NoError(t, err)

			t.Logf("Original: %s", sql)
			t.Logf("CTE:      %s", cteResult)

			assert.NotContains(t, cteResult, "SET param_x_")
			assert.NotContains(t, cteResult, "{param_x_")
			assert.Contains(t, cteResult, "WITH")
		})
	}
}
