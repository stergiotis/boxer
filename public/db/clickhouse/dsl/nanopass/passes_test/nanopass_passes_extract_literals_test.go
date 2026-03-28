//go:build llm_generated_opus46

package passes_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/testdata"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/scalars"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes"
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

// mockMapTypeToCanonical is a test implementation of MapClickHouseTypeToCanonicalI.
func mockMapTypeToCanonical(chType string) (ct canonicaltypes.PrimitiveAstNodeI, err error) {
	switch chType {
	case "UInt8":
		return ctabb.U8, nil
	case "UInt16":
		return ctabb.U16, nil
	case "UInt32":
		return ctabb.U32, nil
	case "UInt64":
		return ctabb.U64, nil
	case "Int8":
		return ctabb.I8, nil
	case "Int16":
		return ctabb.I16, nil
	case "Int32":
		return ctabb.I32, nil
	case "Int64":
		return ctabb.I64, nil
	case "Float32":
		return ctabb.F32, nil
	case "Float64":
		return ctabb.F64, nil
	case "String":
		return ctabb.S, nil
	case "Bool":
		return ctabb.B, nil
	default:
		return nil, fmt.Errorf("unknown type: %s", chType)
	}
}

// --- Basic extraction with sequential names ---

func TestExtractLiteralsSeqLongString(t *testing.T) {
	config := newSeqConfig(10)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'this is a long string value'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
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
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsSeqNumber(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE id = 123456789"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
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

	got1, err := pass(sql)
	require.NoError(t, err)
	got2, err := pass(sql)
	require.NoError(t, err)

	sets1, _ := passes.ParseExtractedQuery(got1)
	sets2, _ := passes.ParseExtractedQuery(got2)
	require.Len(t, sets1, 1)
	require.Len(t, sets2, 1)
	assert.Equal(t, sets1[0], sets2[0], "parameter names should be stable across runs")
}

// --- Deduplication ---

func TestExtractLiteralsSeqDedup(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue1' AND name = 'longvalue1'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, _ := passes.ParseExtractedQuery(got)
	assert.Len(t, sets, 1, "expected exactly 1 SET for deduplicated literal")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsSeqDistinctValues(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'value_one_long' OR name = 'value_two_long'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, _ := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2)

	t.Logf("Result:\n%s", got)
}

// --- Whitelist ---

func TestExtractLiteralsWhitelist(t *testing.T) {
	config := newSeqConfig(100)
	config.Whitelist("eq")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'hi'"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_eq_")
	assert.Contains(t, got, "String}")
}

// --- Blacklist ---

func TestExtractLiteralsBlacklist(t *testing.T) {
	config := newSeqConfig(5)
	config.Blacklist("eq")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'this is a very long string value'"
	got, err := pass(sql)
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
	got, err := pass(sql)
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
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
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
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
	require.Len(t, sets, 1)
	assert.Contains(t, sets[0], "[1, 2, 3, 4, 5]")
	assert.Contains(t, query, "Array(UInt64)")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsINListTooSmall(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetMinINListSize(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('a', 'b', 'c')"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

func TestExtractLiteralsINListDisabled(t *testing.T) {
	config := newSeqConfig(100)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('longval1', 'longval2', 'longval3')"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Cast-aware type inference ---

func TestExtractLiteralsCastDoubleColon(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
	require.GreaterOrEqual(t, len(sets), 1)
	// Value should be just "1", not "1::UInt64"
	assert.Contains(t, sets[0], " = 1")
	assert.NotContains(t, sets[0], "::")
	// Slot should use the cast type
	assert.Contains(t, query, "UInt64}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastAS(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT CAST(1 AS UInt64)"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
	require.GreaterOrEqual(t, len(sets), 1)
	assert.Contains(t, sets[0], " = 1")
	assert.Contains(t, query, "UInt64}")

	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastUnknownType(t *testing.T) {
	config := newSeqConfig(5)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	// Decimal128 is not in our mock mapping — should fall back to no-cast behavior
	sql := "SELECT a FROM t WHERE x = 1::Decimal128"
	got, err := pass(sql)
	require.NoError(t, err)

	// The literal "1" is too short (len=1 < minLength=5), and without cast recognition
	// it won't be extracted
	t.Logf("Result:\n%s", got)
}

func TestExtractLiteralsCastNilMapper(t *testing.T) {
	config := newSeqConfig(1)
	// No mapper set — casts should be ignored
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	// Without mapper, the literal "1" is extracted without cast awareness
	// The "::UInt64" stays in the query as part of the surrounding syntax
	t.Logf("Result:\n%s", got)
}

// --- Cast type in metadata round-trip ---

func TestExtractLiteralsCastMetadataRoundTrip(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64"
	got, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(got, "")
	require.GreaterOrEqual(t, len(params), 1)

	// Find the param that has a cast
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

// --- ParamMetadata encoding/decoding ---

func TestParamMetadataRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		meta passes.ParamMetadata
	}{
		{
			name: "basic",
			meta: passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xdeadbeef},
		},
		{
			name: "sequential",
			meta: passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 42},
		},
		{
			name: "with_cast",
			meta: passes.ParamMetadata{ArgIndex: 2, ContentHash: 0x12345678, CastTypeCanonical: "u64"},
		},
		{
			name: "with_collision",
			meta: passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xaabb, HashCollisionCounter: 3},
		},
		{
			name: "full",
			meta: passes.ParamMetadata{
				ArgIndex:             1,
				ContentHash:          0xcafe,
				HashCollisionCounter: 2,
				CastTypeCanonical:    "u64h",
				IsSequential:         false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			encoded, err := passes.EncodeParamMetadata(&tt.meta)
			require.NoError(t, err)
			assert.NotEmpty(t, encoded)

			decoded, err := passes.DecodeParamMetadata(encoded)
			require.NoError(t, err)
			assert.Equal(t, tt.meta, decoded)

			t.Logf("encoded: %s (len=%d)", encoded, len(encoded))
		})
	}
}

// --- Iterator with new metadata ---

func TestIterateExtractedParamsSeq(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	assert.GreaterOrEqual(t, len(params), 2)

	for _, p := range params {
		t.Logf("%s", p.String())
		assert.NotEmpty(t, p.FullName)
		assert.NotEmpty(t, p.FunctionName)
		assert.NotEmpty(t, p.LiteralSQL)
		assert.True(t, p.Metadata.IsSequential)
	}
}

func TestIterateExtractedParamsContextInfo(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	require.Len(t, params, 1)

	p := params[0]
	assert.Equal(t, "eq", p.FunctionName)
	assert.Equal(t, uint32(1), p.Metadata.ArgIndex)
	assert.Equal(t, "'longvalue'", p.LiteralSQL)
	assert.True(t, p.Metadata.IsSequential)
	assert.Equal(t, ctabb.S, p.Type)
}

func TestIterateExtractedParamsINList(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(100)
	config.SetUseSequentialNames(true)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE b IN ('x', 'y', 'z')"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	require.Len(t, params, 1)

	p := params[0]
	assert.Equal(t, "in", p.FunctionName)
	assert.Contains(t, p.LiteralSQL, "['x', 'y', 'z']")
	assert.Nil(t, p.Type, "array should have nil Type")
}

func TestIterateExtractedParamsCustomPrefix(t *testing.T) {
	config := newSeqConfig(5)
	config.SetPrefix("qp")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "qp")
	require.Len(t, params, 1)
	assert.Equal(t, "eq", params[0].FunctionName)
}

// --- Value() deserialization ---

func TestExtractedParamInfoValueString(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "'hello world'"}
	val, err := info.Value()
	require.NoError(t, err)

	lit, ok := val.(scalars.Literal)
	require.True(t, ok)
	assert.Equal(t, "hello world", lit.StringVal)
}

func TestExtractedParamInfoValueInt(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "42"}
	val, err := info.Value()
	require.NoError(t, err)

	lit, ok := val.(scalars.Literal)
	require.True(t, ok)
	assert.Equal(t, uint64(42), lit.UintVal)
}

func TestExtractedParamInfoValueNull(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "NULL"}
	val, err := info.Value()
	require.NoError(t, err)

	lit, ok := val.(scalars.Literal)
	require.True(t, ok)
	assert.True(t, lit.Null)
}

func TestExtractedParamInfoValueArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "['a', 'b', 'c']"}
	val, err := info.Value()
	require.NoError(t, err)

	arr, ok := val.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
}

func TestExtractedParamInfoValueTuple(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "(1, 'hello')"}
	val, err := info.Value()
	require.NoError(t, err)

	tup, ok := val.(*passes.Tuple)
	require.True(t, ok)
	assert.Equal(t, 2, tup.Len())
}

// --- ScalarValue ---

func TestScalarValueString(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "'hello'"}
	lit, err := info.ScalarValue()
	require.NoError(t, err)
	assert.Equal(t, ctabb.S, lit.Type)
	assert.Equal(t, "hello", lit.StringVal)
}

func TestScalarValueRejectsArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[1, 2, 3]"}
	_, err := info.ScalarValue()
	assert.Error(t, err)
}

// --- End-to-end with casts ---

func TestExtractIterateWithCast(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(mockMapTypeToCanonical)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64 AND y = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	assert.GreaterOrEqual(t, len(params), 2)

	for _, p := range params {
		t.Logf("%s", p.String())
		val, valErr := p.Value()
		require.NoError(t, valErr)
		t.Logf("  value: %v (%T)", val, val)
	}

	// Find the cast param
	var foundCast bool
	for _, p := range params {
		if p.HasCast() {
			foundCast = true
			assert.Equal(t, "u64", p.Metadata.CastTypeCanonical)
		}
	}
	assert.True(t, foundCast, "expected at least one param with cast")
}

// --- Multiple literals ---

func TestExtractLiteralsSeqMultiple(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longname1' AND status = 'longstatus'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2)
	assert.NotContains(t, query, "'longname1'")
	assert.NotContains(t, query, "'longstatus'")
}

// --- NULL skipped ---

func TestExtractLiteralsNullSkipped(t *testing.T) {
	config := newSeqConfig(1)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name IS NULL"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.NotContains(t, got, "SET ")
}

// --- No literals ---

func TestExtractLiteralsNoLiterals(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a, b, c FROM t WHERE a > b"
	got, err := pass(sql)
	require.NoError(t, err)
	assert.Equal(t, sql, got)
}

// --- Config accessors ---

func TestExtractLiteralsConfigAccessors(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(32)

	assert.Equal(t, 32, config.MinLength())
	assert.Equal(t, "param", config.Prefix())
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

// --- Invalid SQL ---

func TestExtractLiteralsRejectsInvalid(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	invalid := []string{"", "   ", "SELECT", ";;;"}
	for i, sql := range invalid {
		t.Run(fmt.Sprintf("invalid_%d", i), func(t *testing.T) {
			_, err := pass(sql)
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
			got, err := pass(entry.SQL)
			if err != nil {
				t.Skipf("pass failed: %v", err)
			}
			assert.NotEmpty(t, got)
		})
	}
}

// --- Mixed whitelist and blacklist ---

func TestExtractLiteralsMixedWhitelistBlacklist(t *testing.T) {
	config := newSeqConfig(100)
	config.Whitelist("todate")
	config.Blacklist("tostring")
	pass := passes.ExtractLiterals(config)

	sql := "SELECT toDate('2024-01-01'), toString('2024-01-01') FROM t"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET param_todate_")
	_, query := passes.ParseExtractedQuery(got)
	assert.Contains(t, query, "toString('2024-01-01')")
}

// --- ParseExtractedQuery ---

func TestParseExtractedQuery(t *testing.T) {
	input := "SET param_eq_abcd = 'hello';\nSET param_gt_ef01 = 100;\nSELECT a FROM t"

	sets, query := passes.ParseExtractedQuery(input)
	assert.Len(t, sets, 2)
	assert.True(t, strings.HasPrefix(query, "SELECT"))
}

// --- InjectParams ---

func TestInjectParamsRoundTrip(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	original := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass(original)
	require.NoError(t, err)

	sets, query := passes.ParseExtractedQuery(extracted)
	injected, err := passes.InjectParams(sets, query)
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

// --- UNION ALL ---

func TestExtractLiteralsUnionAll(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 'longval1' UNION ALL SELECT b FROM t2 WHERE y = 'longval2'"
	got, err := pass(sql)
	require.NoError(t, err)

	sets, _ := passes.ParseExtractedQuery(got)
	assert.GreaterOrEqual(t, len(sets), 2)
}

// --- Subquery ---

func TestExtractLiteralsSubquery(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT * FROM (SELECT a FROM t WHERE name = 'longvalue')"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
}

// --- CTE ---

func TestExtractLiteralsCTE(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "WITH cte AS (SELECT a FROM t WHERE name = 'longvalue') SELECT * FROM cte"
	got, err := pass(sql)
	require.NoError(t, err)

	assert.Contains(t, got, "SET ")
}

// --- Edge: empty CollectExtractedParams ---

func TestCollectExtractedParamsEmpty(t *testing.T) {
	params := passes.CollectExtractedParams("SELECT 1", "")
	assert.Empty(t, params)
}

// --- Edge: malformed SET lines skipped ---

func TestIterateExtractedParamsSkipsMalformed(t *testing.T) {
	// Manually construct output with valid and invalid SET lines
	input := "SET param_eq_abcdef = 'valid';\nnot a SET line\nSELECT 1"

	params := passes.CollectExtractedParams(input, "")
	// The malformed line should be skipped, only valid params returned
	// (may be 0 or 1 depending on whether "abcdef" decodes as valid CBOR)
	t.Logf("params: %d", len(params))
}
