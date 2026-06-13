package passes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers ---

func newSeqConfig(minLength int) *passes.ExtractLiteralsConfig {
	config := passes.NewExtractLiteralsConfig(minLength)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(0)
	return config
}

// --- Basic extraction with sequential names ---

func TestExtractLiteralsSeqLongString(t *testing.T) {
	config := newSeqConfig(10)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'this is a long string value'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "'this is a long string value'")
	assert.NotContains(t, query, "'this is a long string value'")
	assert.Contains(t, query, "String}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsSeqShortStringSkipped(t *testing.T) {
	config := newSeqConfig(32)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'short'"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsSeqNumber(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE id = 123456789"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "123456789")
	assert.NotContains(t, query, "123456789")

	t.Logf("Result:\n%s", got)
}

// --- Stable naming (hash-based) ---

func TestExtractLiteralsStableNames(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"

	got1, err := pass.Run(sql)
	require.NoError(t, err)
	got2, err := pass.Run(sql)
	require.NoError(t, err)

	sets1, _, _ := passes.ParseExtractedQuery(got1, "")
	sets2, _, _ := passes.ParseExtractedQuery(got2, "")
	require.Len(t, sets1, 1)
	require.Len(t, sets2, 1)
	assert.Equal(t, sets1[0], sets2[0], "parameter names should be stable across runs")
}

// --- CBOR encoding determinism ---

func TestParamMetadataEncodingDeterministic(t *testing.T) {
	meta := passes.ParamMetadata{
		ArgIndex:          1,
		ContentHash:       0xdeadbeef,
		CastTypeCanonical: "u64",
	}

	encoded1, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)
	encoded2, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	assert.Equal(t, encoded1, encoded2, "CBOR encoding must be deterministic")
}

// --- Deduplication ---

func TestExtractLiteralsSeqDedup(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue1' AND name = 'longvalue1'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, _ := passes.ParseExtractedQuery(got, "")
	assert.Len(t, sets, 1, "expected exactly 1 SET for deduplicated literal")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsSeqDistinctValues(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'value_one_long' OR name = 'value_two_long'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, _ := passes.ParseExtractedQuery(got, "")
	assert.GreaterOrEqual(t, len(sets), 2)

	t.Logf("Result:\n%s", got)
}

// --- Whitelist ---

func TestExtractLiteralsWhitelist(t *testing.T) {
	config := newSeqConfig(100)
	config.Whitelist("eq")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hi'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_x_eq_")
	assert.Contains(t, got, "String}")
}

// --- Blacklist ---

func TestExtractLiteralsBlacklist(t *testing.T) {
	config := newSeqConfig(5)
	config.Blacklist("eq")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'this is a very long string value'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	assert.NotContains(t, got, "SET ")
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsBlacklistOverridesWhitelist(t *testing.T) {
	config := newSeqConfig(5)
	config.Whitelist("eq")
	config.Blacklist("eq")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	assert.NotContains(t, got, "SET ")
}

// --- IN-list collapsing ---

func TestExtractLiteralsINListCollapse(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('longval1', 'longval2', 'longval3')"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "['longval1', 'longval2', 'longval3']")
	assert.Contains(t, query, "Array(String)")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsINListCollapseIntegers(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE id IN (1, 2, 3, 4, 5)"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "[1, 2, 3, 4, 5]")
	assert.Contains(t, query, "Array(")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsINListTooSmall(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetMinINListSize(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('a', 'b', 'c')"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsINListDisabled(t *testing.T) {
	config := newSeqConfig(100)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('longval1', 'longval2', 'longval3')"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Array function-call collapsing ---
//
// ExtractLiterals collapses literal-only `array(...)` and `tuple(...)` calls
// into a single composite parameter. Syntactic `[...]` / `(...)` literals are
// expected to be lowered to the function form by
// CanonicalizeConstructors(ConstructorFormFunction) before this pass runs.

func TestExtractLiteralsArrayCollapse(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array(1, 2, 3, 4, 5) FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1, "expected single SET for array() call")
	assert.Contains(t, sets[0], "[1, 2, 3, 4, 5]")
	assert.Contains(t, query, "Array(")
	assert.NotContains(t, query, "array(1", "array() call should be replaced by param slot")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsArrayCollapseStrings(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array('a', 'b', 'c') FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "['a', 'b', 'c']")
	assert.Contains(t, query, "Array(String)")
}

func TestExtractLiteralsArrayTooSmall(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetMinINListSize(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array(1, 2, 3) FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got, "small array below threshold should be untouched")
}

func TestExtractLiteralsArrayDisabled(t *testing.T) {
	config := newSeqConfig(100)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array(1, 2, 3, 4, 5) FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsArrayWithExpressionsSkipped(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array(1, 2, x) FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	sets, _, _ := passes.ParseExtractedQuery(got, "")
	assert.Len(t, sets, 0, "non-literal element should disqualify the array() call")
}

func TestExtractLiteralsArraySyntacticFormSkipped(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	// `[1, 2, 3]` is the syntactic form. It is intentionally not handled here;
	// callers are expected to run CanonicalizeConstructors(ConstructorFormFunction)
	// first to lower it to `array(1, 2, 3)`.
	sql := "SELECT [1, 2, 3, 4, 5] FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	sets, _, _ := passes.ParseExtractedQuery(got, "")
	assert.Len(t, sets, 0, "syntactic [..] is not collapsed; canonicalize first")
}

// --- Tuple function-call collapsing ---

func TestExtractLiteralsTupleCollapse(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT tuple(1, 2, 3) AS x FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1, "expected single SET for tuple() call")
	assert.Contains(t, sets[0], "(1, 2, 3)")
	assert.Contains(t, query, "Tuple(")
	assert.NotContains(t, query, "tuple(1", "tuple() call should be replaced by param slot")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsTupleHeterogeneous(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT tuple(1, 'two', 3) AS x FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "(1, 'two', 3)")
	assert.Contains(t, query, "Tuple(")
	assert.Contains(t, query, "String")
}

func TestExtractLiteralsTupleInINStillArray(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	// IN-tuple syntactic form is preserved for callers that have not yet run
	// the constructor canonicalization. It still collapses to an Array param.
	sql := "SELECT a FROM t WHERE id IN (1, 2, 3)"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "[1, 2, 3]", "IN-tuples must remain Array-shaped values")
	assert.Contains(t, query, "Array(")
	assert.NotContains(t, query, "Tuple(")
}

func TestExtractLiteralsTupleBlacklist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	config.Blacklist("tuple")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT tuple(1, 2, 3) AS x FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got, "blacklisted tuple kind should leave the call alone")
}

func TestExtractLiteralsArrayBlacklist(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	config.Blacklist("array")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT array(1, 2, 3) FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsArrayRoundTrip(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	original := "SELECT array(1, 2, 3) FROM t"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")
	injected, err := passes.InjectParams(sets, "", query)
	require.NoError(t, err)
	// After injection, the param slot is replaced by the original SQL value `[1,2,3]`,
	// not the array(...) call. That is the expected output and parses identically.
	assert.Equal(t, "SELECT [1, 2, 3] FROM t", injected)
}

func TestExtractLiteralsTupleRoundTrip(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	original := "SELECT tuple(1, 2, 3) AS x FROM t"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")
	injected, err := passes.InjectParams(sets, "", query)
	require.NoError(t, err)
	assert.Equal(t, "SELECT (1, 2, 3) AS x FROM t", injected)
}

// --- Canonicalize + extract integration ---

func TestExtractLiteralsCanonicalizeThenExtractArray(t *testing.T) {
	canon := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	extract := passes.ExtractLiterals(config)

	canonicalized, err := canon.Run("SELECT [1, 2, 3, 4, 5] FROM t")
	require.NoError(t, err)
	got, err := extract.Run(canonicalized)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1, "syntactic array should be captured after canonicalization")
	assert.Contains(t, query, "Array(")
}

func TestExtractLiteralsCanonicalizeThenExtractTuple(t *testing.T) {
	canon := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	extract := passes.ExtractLiterals(config)

	canonicalized, err := canon.Run("SELECT (1, 2, 3) AS x FROM t")
	require.NoError(t, err)
	got, err := extract.Run(canonicalized)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1, "syntactic tuple should be captured after canonicalization")
	assert.Contains(t, query, "Tuple(")
}

func TestExtractLiteralsCanonicalizeThenExtractIN(t *testing.T) {
	canon := passes.CanonicalizeConstructors(passes.ConstructorFormFunction)
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	extract := passes.ExtractLiterals(config)

	canonicalized, err := canon.Run("SELECT a FROM t WHERE id IN (1, 2, 3)")
	require.NoError(t, err)
	assert.Contains(t, canonicalized, "IN array(", "IN-tuples should be lowered to array() form")

	got, err := extract.Run(canonicalized)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.Len(t, sets, 1, "IN-list should be captured as a single composite param")
	assert.Contains(t, query, "Array(", "IN's RHS should be Array-typed, not Tuple")
	assert.NotContains(t, query, "Tuple(")
}

// --- Cast-aware type inference ---

func TestExtractLiteralsCastDoubleColon(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.GreaterOrEqual(t, len(sets), 1)
	assert.Contains(t, sets[0], " = 1")
	assert.NotContains(t, sets[0], "::")
	assert.Contains(t, query, "UInt64}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastAS(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT CAST(1 AS UInt64)"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	require.GreaterOrEqual(t, len(sets), 1)
	assert.Contains(t, sets[0], " = 1")
	assert.Contains(t, query, "UInt64}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastUnknownType(t *testing.T) {
	config := newSeqConfig(5)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::Decimal128"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastNilMapper(t *testing.T) {
	config := newSeqConfig(1)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	t.Logf("Result:\n%s", got)
}

// --- Cast type in metadata round-trip ---

func TestExtractLiteralsCastMetadataRoundTrip(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(got, "")
	require.GreaterOrEqual(t, len(params), 1)

	var castParam *passes.ExtractedParamInfo
	for i := range params {
		if params[i].HasCast() {
			castParam = &params[i]
			break
		}
	}
	require.NotNil(t, castParam, "expected at least one param with cast type")

	assert.Equal(t, "u64", castParam.Metadata.CastTypeCanonical)
	assert.Equal(t, "1", castParam.LiteralSQL)

	t.Logf("Cast param: %s", castParam.String())
}

// --- Cast arg index precision ---

func TestExtractLiteralsCastArgIndexRight(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	extracted, err := pass.Run(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	require.GreaterOrEqual(t, len(params), 1)

	for _, p := range params {
		if p.HasCast() {
			assert.Equal(t, uint32(1), p.Metadata.ArgIndex, "right operand of = should be arg 1")
		}
	}
}

// --- InjectParamsWithCasts ---

func TestInjectParamsWithCastsBasic(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE x = 1::UInt64"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")

	injected, err := passes.InjectParamsWithCasts(sets, query, "", marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)

	assert.Equal(t, original, injected, "round-trip should reconstruct original SQL")
	t.Logf("Original:  %s", original)
	t.Logf("Injected:  %s", injected)
}

func TestInjectParamsWithCastsNoCast(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE name = 'longvalue'"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")

	injected, err := passes.InjectParamsWithCasts(sets, query, "", nil)
	require.NoError(t, err)

	assert.Equal(t, original, injected, "no-cast round-trip should work")
}

func TestInjectParamsWithCastsMixed(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE x = 1::UInt64 AND name = 'hello'"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")

	injected, err := passes.InjectParamsWithCasts(sets, query, "", marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)

	assert.Contains(t, injected, "1::UInt64")
	assert.Contains(t, injected, "'hello'")
}

func TestInjectParamsWithCastsNilMapper(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	extracted, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")

	injected, err := passes.InjectParamsWithCasts(sets, query, "", nil)
	require.NoError(t, err)

	assert.Contains(t, injected, "1")
	assert.NotContains(t, injected, "::UInt64")
}

func TestInjectParamsWithCastsCustomPrefix(t *testing.T) {
	config := newSeqConfig(5)
	config.SetPrefix("qp")
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE name = 'longvalue'"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "qp")

	injected, err := passes.InjectParamsWithCasts(sets, query, "qp", nil)
	require.NoError(t, err)

	assert.Equal(t, original, injected)
}

// --- Multiple literals ---

func TestExtractLiteralsSeqMultiple(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longname1' AND status = 'longstatus'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(got, "")
	assert.GreaterOrEqual(t, len(sets), 2)
	assert.NotContains(t, query, "'longname1'")
	assert.NotContains(t, query, "'longstatus'")
}

// --- NULL skipped ---

func TestExtractLiteralsNullSkipped(t *testing.T) {
	config := newSeqConfig(1)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name IS NULL"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.NotContains(t, got, "SET ")
}

// --- No literals ---

func TestExtractLiteralsNoLiterals(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a, b, c FROM t WHERE a > b"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Config accessors ---

func TestExtractLiteralsConfigAccessors(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(32)

	assert.Equal(t, 32, config.MinLength())
	assert.Equal(t, passes.ParamPrefixExtracted, config.Prefix())
	assert.Equal(t, 3, config.MinINListSize())
	assert.False(t, config.UseSequentialNames())

	config.SetMinLength(64)
	assert.Equal(t, 64, config.MinLength())

	config.SetPrefix("qp")
	assert.Equal(t, "qp", config.Prefix())

	config.SetMinINListSize(10)
	assert.Equal(t, 10, config.MinINListSize())

	config.SetUseSequentialNames(true)
	assert.True(t, config.UseSequentialNames())

	config.Whitelist("eq")
	assert.True(t, config.IsWhitelisted("eq"))
	assert.True(t, config.IsWhitelisted("EQ"))

	config.Blacklist("eq")
	assert.True(t, config.IsBlacklisted("eq"))

	config.RemovePolicy("eq")
	assert.False(t, config.IsBlacklisted("eq"))
	assert.False(t, config.IsWhitelisted("eq"))
}

// --- UNION ALL ---

func TestExtractLiteralsUnionAll(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 'longval1' UNION ALL SELECT b FROM t2 WHERE y = 'longval2'"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	sets, _, _ := passes.ParseExtractedQuery(got, "")
	assert.GreaterOrEqual(t, len(sets), 2)
}

// --- Subquery ---

func TestExtractLiteralsSubquery(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT * FROM (SELECT a FROM t WHERE name = 'longvalue')"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "SET ")
}

// --- CTE ---

func TestExtractLiteralsCTE(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "WITH cte AS (SELECT a FROM t WHERE name = 'longvalue') SELECT * FROM cte"
	got, err := pass.Run(sql)
	require.NoError(t, err)
	assert.Contains(t, got, "SET ")
}

// --- Mixed whitelist and blacklist ---

func TestExtractLiteralsMixedWhitelistBlacklist(t *testing.T) {
	config := newSeqConfig(100)
	config.Whitelist("todate")
	config.Blacklist("tostring")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT toDate('2024-01-01'), toString('2024-01-01') FROM t"
	got, err := pass.Run(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_x_todate_")
	_, _, query := passes.ParseExtractedQuery(got, "")
	assert.Contains(t, query, "toString('2024-01-01')")
}

// --- ParseExtractedQuery ---

func TestParseExtractedQuery(t *testing.T) {
	input := "SET param_eq_abcd = 'hello';\nSET param_gt_ef01 = 100;\nSELECT a FROM t"

	sets, _, query := passes.ParseExtractedQuery(input, "")
	assert.Len(t, sets, 0)
	assert.True(t, strings.HasPrefix(query, "SELECT"))
}

// --- InjectParams (simple) ---

func TestInjectParamsRoundTrip(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass.Run(original)
	require.NoError(t, err)

	sets, _, query := passes.ParseExtractedQuery(extracted, "")
	injected, err := passes.InjectParams(sets, "", query)
	require.NoError(t, err)

	assert.Equal(t, original, injected)
}

// --- CountExtractableParams ---

func TestCountExtractableParams(t *testing.T) {
	config := newSeqConfig(5)

	count, err := passes.CountExtractableParams("SELECT a FROM t WHERE name = 'longvalue' AND x > 100000", config)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, count, 2)

	count, err = passes.CountExtractableParams("SELECT a FROM t WHERE a > b", config)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

// --- Invalid SQL ---

func TestExtractLiteralsRejectsInvalid(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass.Run(sql)
			assert.Error(t, err)
		})
	}
}

// --- Corpus ---

func TestExtractLiteralsCorpus(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(32)
	pass := passes.ExtractLiterals(config)

	entries, err := testdata.LoadCorpus()
	require.NoError(t, err)

	for _, entry := range entries {
		t.Run(entry.Name, func(t *testing.T) {
			got, err := pass.Run(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			assert.NotEmpty(t, got)
		})
	}
}

// --- End-to-end: extract → iterate → deserialize with casts ---

func TestExtractIterateWithCast(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64 AND y = 'hello'"
	extracted, err := pass.Run(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	assert.GreaterOrEqual(t, len(params), 2)

	var foundCast bool
	for _, p := range params {
		t.Logf("%s", p.String())
		val, valErr := p.Value()
		require.NoError(t, valErr)
		t.Logf("  value: %v (%T)", val, val)

		if p.HasCast() {
			foundCast = true
			assert.Equal(t, "u64", p.Metadata.CastTypeCanonical)
			assert.Equal(t, ctabb.U64, p.CastType)
		}
	}
	assert.True(t, foundCast, "expected at least one param with cast")
}
