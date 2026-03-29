//go:build llm_generated_opus46

package passes_test

import (
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/passes"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helpers ---

func buildTestSET(context string, meta *passes.ParamMetadata, value string) string {
	name, err := passes.BuildParamName(passes.ParamPrefixExtracted, context, meta)
	if err != nil {
		panic(err)
	}
	return "SET " + name + " = " + value
}

// --- ParamMetadata encode/decode ---

func TestParamMetadataRoundTripBasic(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xdeadbeef}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, decoded)
}

func TestParamMetadataRoundTripSequential(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 42}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, decoded)
}

func TestParamMetadataRoundTripWithCast(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 2, ContentHash: 0x12345678, CastTypeCanonical: "u64"}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, decoded)
}

func TestParamMetadataRoundTripWithCollision(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xaabb, HashCollisionCounter: 3}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, decoded)
}

func TestParamMetadataRoundTripFull(t *testing.T) {
	meta := passes.ParamMetadata{
		ArgIndex:             1,
		ContentHash:          0xcafe,
		HashCollisionCounter: 2,
		CastTypeCanonical:    "u64h",
	}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, meta, decoded)

	t.Logf("encoded: %s (len=%d)", encoded, len(encoded))
}

func TestParamMetadataRoundTripTupleCast(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0, CastTypeCanonical: "u8-s"}
	encoded, err := passes.EncodeParamMetadata(&meta)
	require.NoError(t, err)

	decoded, err := passes.DecodeParamMetadata(encoded)
	require.NoError(t, err)
	assert.Equal(t, "u8-s", decoded.CastTypeCanonical)
}

func TestDecodeParamMetadataInvalidHex(t *testing.T) {
	_, err := passes.DecodeParamMetadata("zzzz")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hex")
}

func TestDecodeParamMetadataInvalidCBOR(t *testing.T) {
	_, err := passes.DecodeParamMetadata("ffff")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cbor")
}

// --- BuildParamName / ParseParamName ---

func TestBuildParseParamNameRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		context string
		meta    passes.ParamMetadata
	}{
		{"hash_based", "eq", passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xdeadbeef}},
		{"sequential", "like", passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 5}},
		{"with_cast", "eq", passes.ParamMetadata{ArgIndex: 1, ContentHash: 0x1234, CastTypeCanonical: "u64"}},
		{"in_list", "in", passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0}},
		{"collision", "eq", passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xabcd, HashCollisionCounter: 2}},
		{"long_func", "substring", passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0}},
		{"cast_array", "eq", passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xff, CastTypeCanonical: "u64h"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, err := passes.BuildParamName(passes.ParamPrefixExtracted, tt.context, &tt.meta)
			require.NoError(t, err)

			parsedCtx, parsedMeta, err := passes.ParseParamName(name, passes.ParamPrefixExtracted)
			require.NoError(t, err)

			assert.Equal(t, tt.context, parsedCtx)
			assert.Equal(t, tt.meta, parsedMeta)

			t.Logf("name: %s", name)
		})
	}
}

func TestParseParamNameWrongPrefix(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 1}
	name, err := passes.BuildParamName(passes.ParamPrefixExtracted, "eq", &meta)
	require.NoError(t, err)

	_, _, err = passes.ParseParamName(name, "wrongprefix")
	assert.Error(t, err)
}

func TestParseParamNameMalformed(t *testing.T) {
	_, _, err := passes.ParseParamName("param_x_eq", passes.ParamPrefixExtracted)
	assert.Error(t, err)
}

func TestBuildParamNameCustomPrefix(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0}
	name, err := passes.BuildParamName("qp", "eq", &meta)
	require.NoError(t, err)
	assert.Contains(t, name, "qp_eq_")

	parsedCtx, parsedMeta, err := passes.ParseParamName(name, "qp")
	require.NoError(t, err)
	assert.Equal(t, "eq", parsedCtx)
	assert.Equal(t, meta, parsedMeta)
}

// --- IterateExtractedParamsFromSets ---

func TestIterateFromSetsBasic(t *testing.T) {
	sets := []string{
		buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xaabb}, "'hello'"),
		buildTestSET("gt", &passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xccdd}, "100000"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 2)

	assert.Equal(t, "eq", params[0].FunctionName)
	assert.Equal(t, uint32(1), params[0].Metadata.ArgIndex)
	assert.Equal(t, uint64(0xaabb), params[0].Metadata.ContentHash)
	assert.Equal(t, "'hello'", params[0].LiteralSQL)
	assert.Equal(t, ctabb.S, params[0].Type)

	assert.Equal(t, "gt", params[1].FunctionName)
	assert.Equal(t, uint32(1), params[1].Metadata.ArgIndex)
	assert.Equal(t, "100000", params[1].LiteralSQL)
}

func TestIterateFromSetsSequential(t *testing.T) {
	sets := []string{
		buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, IsSequential: true, SequentialIndex: 0}, "'value1'"),
		buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, IsSequential: true, SequentialIndex: 1}, "'value2'"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 2)
	assert.True(t, params[0].Metadata.IsSequential)
	assert.Equal(t, uint32(0), params[0].Metadata.SequentialIndex)
	assert.True(t, params[1].Metadata.IsSequential)
	assert.Equal(t, uint32(1), params[1].Metadata.SequentialIndex)
}

func TestIterateFromSetsWithCast(t *testing.T) {
	sets := []string{
		buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, IsSequential: true, SequentialIndex: 0, CastTypeCanonical: "u64"}, "42"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, "u64", params[0].Metadata.CastTypeCanonical)
	assert.True(t, params[0].HasCast())
	assert.Equal(t, ctabb.U64, params[0].CastType)
}

func TestIterateFromSetsWithCastArray(t *testing.T) {
	sets := []string{
		buildTestSET("in", &passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0, CastTypeCanonical: "u64h"}, "[1, 2, 3]"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, "u64h", params[0].Metadata.CastTypeCanonical)
	assert.True(t, params[0].HasCast())
	assert.Nil(t, params[0].Type, "array value should have nil scalar Type")
}

func TestIterateFromSetsWithCollision(t *testing.T) {
	sets := []string{
		buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xabcd, HashCollisionCounter: 3}, "'collision'"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, uint8(3), params[0].Metadata.HashCollisionCounter)
}

func TestIterateFromSetsINList(t *testing.T) {
	sets := []string{
		buildTestSET("in", &passes.ParamMetadata{ArgIndex: 0, ContentHash: 0x1234}, "['x', 'y', 'z']"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, "in", params[0].FunctionName)
	assert.Equal(t, "['x', 'y', 'z']", params[0].LiteralSQL)
	assert.Nil(t, params[0].Type)
}

func TestIterateFromSetsCustomPrefix(t *testing.T) {
	meta := passes.ParamMetadata{ArgIndex: 0, IsSequential: true, SequentialIndex: 0}
	name, err := passes.BuildParamName("qp", "eq", &meta)
	require.NoError(t, err)

	sets := []string{"SET " + name + " = 'hello'"}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "qp") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, "eq", params[0].FunctionName)
}

func TestIterateFromSetsSkipsMalformed(t *testing.T) {
	validSET := buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 1, ContentHash: 0xaa}, "'valid'")

	sets := []string{
		validSET,
		"not a SET line",
		"SET missing_equals",
		"SET param_eq_invalidhex = 'bad'",
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	assert.Equal(t, 1, len(params))
	assert.Equal(t, "eq", params[0].FunctionName)
}

func TestIterateFromSetsEmpty(t *testing.T) {
	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(nil, "") {
		params = append(params, info)
	}
	assert.Empty(t, params)
}

func TestIterateFromSetsSingleParam(t *testing.T) {
	sets := []string{
		buildTestSET("like", &passes.ParamMetadata{ArgIndex: 1, IsSequential: true, SequentialIndex: 0}, "'%pattern%'"),
	}

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets(sets, "") {
		params = append(params, info)
	}

	require.Len(t, params, 1)
	assert.Equal(t, "like", params[0].FunctionName)
	assert.Equal(t, "'%pattern%'", params[0].LiteralSQL)
}

// --- IterateExtractedParams (from full output) ---

func TestIterateFromExtractedOutput(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass(sql)
	require.NoError(t, err)

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParams(extracted, "") {
		params = append(params, info)
	}

	assert.GreaterOrEqual(t, len(params), 2)
	for _, p := range params {
		assert.NotEmpty(t, p.FullName)
		assert.NotEmpty(t, p.FunctionName)
		assert.NotEmpty(t, p.LiteralSQL)
		assert.True(t, p.Metadata.IsSequential)
		t.Logf("%s", p.String())
	}
}

func TestIterateFromExtractedOutputContextInfo(t *testing.T) {
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
	assert.False(t, p.HasCast())
}

func TestIterateFromExtractedOutputINList(t *testing.T) {
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
	assert.Nil(t, p.Type)
}

func TestIterateFromExtractedOutputEmpty(t *testing.T) {
	params := passes.CollectExtractedParams("SELECT 1", "")
	assert.Empty(t, params)
}

func TestIterateFromExtractedOutputHashBased(t *testing.T) {
	config := passes.NewExtractLiteralsConfig(5)
	config.SetMinINListSize(0)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	require.Len(t, params, 1)

	p := params[0]
	assert.False(t, p.Metadata.IsSequential)
	assert.NotZero(t, p.Metadata.ContentHash)
}

// --- Type inference ---

func TestIteratorTypeInference(t *testing.T) {
	tests := []struct {
		name         string
		value        string
		expectedType interface{}
	}{
		{"string", "'hello'", ctabb.S},
		{"unsigned_int", "42", ctabb.U64},
		{"negative_int", "-5", ctabb.I64},
		{"float", "3.14", ctabb.F64},
		{"bool_true", "true", ctabb.B},
		{"bool_false", "false", ctabb.B},
		{"null", "NULL", nil},
		{"array", "[1, 2, 3]", nil},
		{"hex", "0xFF", ctabb.U64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := buildTestSET("eq", &passes.ParamMetadata{ArgIndex: 0, ContentHash: 0xaa}, tt.value)
			var params []passes.ExtractedParamInfo
			for _, info := range passes.IterateExtractedParamsFromSets([]string{set}, "") {
				params = append(params, info)
			}
			require.Len(t, params, 1)
			if tt.expectedType == nil {
				assert.Nil(t, params[0].Type)
			} else {
				assert.Equal(t, tt.expectedType, params[0].Type)
			}
		})
	}
}

// --- CastType reconstruction ---

func TestIteratorCastTypeReconstruction(t *testing.T) {
	tests := []struct {
		name          string
		castCanonical string
		expectedCast  interface{}
	}{
		{"u8", "u8", ctabb.U8},
		{"u16", "u16", ctabb.U16},
		{"u32", "u32", ctabb.U32},
		{"u64", "u64", ctabb.U64},
		{"i8", "i8", ctabb.I8},
		{"i16", "i16", ctabb.I16},
		{"i32", "i32", ctabb.I32},
		{"i64", "i64", ctabb.I64},
		{"f32", "f32", ctabb.F32},
		{"f64", "f64", ctabb.F64},
		{"string", "s", ctabb.S},
		{"bool", "b", ctabb.B},
		{"no_cast", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := buildTestSET("eq", &passes.ParamMetadata{
				ArgIndex:          0,
				ContentHash:       0xaa,
				CastTypeCanonical: tt.castCanonical,
			}, "42")

			var params []passes.ExtractedParamInfo
			for _, info := range passes.IterateExtractedParamsFromSets([]string{set}, "") {
				params = append(params, info)
			}
			require.Len(t, params, 1)

			if tt.expectedCast == nil {
				assert.Nil(t, params[0].CastType)
				assert.False(t, params[0].HasCast())
			} else {
				require.NotNil(t, params[0].CastType)
				assert.Equal(t, tt.expectedCast, params[0].CastType)
				assert.True(t, params[0].HasCast())
			}
		})
	}
}

func TestIteratorCastTypeTupleGroup(t *testing.T) {
	set := buildTestSET("eq", &passes.ParamMetadata{
		ArgIndex:          0,
		ContentHash:       0xaa,
		CastTypeCanonical: "u8-s",
	}, "(1, 'hello')")

	var params []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParamsFromSets([]string{set}, "") {
		params = append(params, info)
	}
	require.Len(t, params, 1)

	assert.Nil(t, params[0].CastType)
	assert.Equal(t, "u8-s", params[0].Metadata.CastTypeCanonical)
	assert.False(t, params[0].HasCast())
}

// --- Value() deserialization ---

func TestIteratorValueString(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "'hello world'"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.S, val.ScalarType)
	assert.Equal(t, "hello world", val.StringVal)
}

func TestIteratorValueStringEscaped(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "'it\\'s a test'"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, "it's a test", val.StringVal)
}

func TestIteratorValueInt(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "42"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.U64, val.ScalarType)
	assert.Equal(t, uint64(42), val.UintVal)
}

func TestIteratorValueNegativeInt(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "-99"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.I64, val.ScalarType)
	assert.Equal(t, int64(-99), val.IntVal)
}

func TestIteratorValueFloat(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "3.14"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.F64, val.ScalarType)
	assert.Equal(t, 3.14, val.FloatVal)
}

func TestIteratorValueNull(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "NULL"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsNull())
}

func TestIteratorValueBool(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "true"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.B, val.ScalarType)
	assert.True(t, val.BoolVal)
}

func TestIteratorValueHex(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "0xFF"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.U64, val.ScalarType)
	assert.Equal(t, uint64(255), val.UintVal)
}

func TestIteratorValueArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "['a', 'b', 'c']"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsArray())

	// Homogeneous string array
	if val.IsHomogeneousArray() {
		assert.Equal(t, 3, val.HomArray.Len())
		assert.Equal(t, ctabb.S, val.HomArray.ElementType)
		assert.Equal(t, []string{"a", "b", "c"}, val.HomArray.StringVals)
	} else {
		assert.Equal(t, 3, len(val.Elements))
	}
}

func TestIteratorValueIntArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[1, 2, 3]"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsArray())

	if val.IsHomogeneousArray() {
		assert.Equal(t, 3, val.HomArray.Len())
		assert.Equal(t, ctabb.U64, val.HomArray.ElementType)
		assert.Equal(t, []uint64{1, 2, 3}, val.HomArray.UintVals)
	} else {
		assert.Equal(t, 3, len(val.Elements))
	}
}

func TestIteratorValueEmptyArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[]"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsArray())
	assert.Equal(t, 0, val.ArrayLen())
}

func TestIteratorValueNestedArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[[1, 2], [3, 4]]"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsHeterogeneousArray(), "nested arrays should be heterogeneous")
	assert.Equal(t, 2, len(val.Elements))
	assert.True(t, val.Elements[0].IsArray())
	assert.True(t, val.Elements[1].IsArray())
}

func TestIteratorValueTuple(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "(1, 'hello')"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsTuple())
	assert.Equal(t, 2, len(val.Elements))

	assert.True(t, val.Elements[0].IsScalar())
	assert.Equal(t, uint64(1), val.Elements[0].UintVal)

	assert.True(t, val.Elements[1].IsScalar())
	assert.Equal(t, "hello", val.Elements[1].StringVal)
}

func TestIteratorValueStringWithComma(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "['a,b', 'c,d']"}
	val, err := info.Value()
	require.NoError(t, err)
	assert.True(t, val.IsArray())

	if val.IsHomogeneousArray() {
		assert.Equal(t, 2, val.HomArray.Len())
		assert.Equal(t, "a,b", val.HomArray.StringVals[0])
		assert.Equal(t, "c,d", val.HomArray.StringVals[1])
	} else {
		assert.Equal(t, 2, len(val.Elements))
		assert.Equal(t, "a,b", val.Elements[0].StringVal)
	}
}

// --- ScalarValue ---

func TestIteratorScalarValueString(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "'hello'"}
	val, err := info.ScalarValue()
	require.NoError(t, err)
	assert.True(t, val.IsScalar())
	assert.Equal(t, ctabb.S, val.ScalarType)
	assert.Equal(t, "hello", val.StringVal)
}

func TestIteratorScalarValueRejectsArray(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[1, 2, 3]"}
	_, err := info.ScalarValue()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "composite")
}

func TestIteratorScalarValueRejectsTuple(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "(1, 2)"}
	_, err := info.ScalarValue()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "composite")
}

// --- End-to-end ---

func TestExtractIterateDeserializeEndToEnd(t *testing.T) {
	config := newSeqConfig(5)
	config.SetMinINListSize(3)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue' AND id IN (100, 200, 300)"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	assert.GreaterOrEqual(t, len(params), 2)

	for _, p := range params {
		val, valErr := p.Value()
		require.NoError(t, valErr, "failed to deserialize %q", p.LiteralSQL)
		t.Logf("%s → %s (%T)", p.String(), val.Kind, val)
	}
}

func TestExtractIterateDeserializeWithCast(t *testing.T) {
	config := newSeqConfig(1)
	config.SetMapTypeToCanonical(marshalling.MapClickHouseToCanonicalType)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE x = 1::UInt64 AND y = 'hello'"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")
	assert.GreaterOrEqual(t, len(params), 2)

	var foundCast bool
	for _, p := range params {
		val, valErr := p.Value()
		require.NoError(t, valErr)
		t.Logf("%s → %s (%T)", p.String(), val.Kind, val)

		if p.HasCast() {
			foundCast = true
			assert.Equal(t, "u64", p.Metadata.CastTypeCanonical)
			assert.Equal(t, ctabb.U64, p.CastType)
		}
	}
	assert.True(t, foundCast, "expected at least one param with cast")
}

// --- Iterator early termination ---

func TestIteratorEarlyTermination(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longname1' AND status = 'longstatus' AND x > 100000"
	extracted, err := pass(sql)
	require.NoError(t, err)

	count := 0
	for _, _ = range passes.IterateExtractedParams(extracted, "") {
		count++
		if count >= 1 {
			break
		}
	}
	assert.Equal(t, 1, count)
}

// --- CollectExtractedParams ---

func TestCollectExtractedParams(t *testing.T) {
	config := newSeqConfig(5)
	pass := passes.ExtractLiterals(config)

	sql := "SELECT a FROM t WHERE name = 'longvalue' AND x > 100000"
	extracted, err := pass(sql)
	require.NoError(t, err)

	params := passes.CollectExtractedParams(extracted, "")

	var iterParams []passes.ExtractedParamInfo
	for _, info := range passes.IterateExtractedParams(extracted, "") {
		iterParams = append(iterParams, info)
	}
	assert.Equal(t, len(params), len(iterParams))
}

// --- Value() serialization round-trip via marshalling ---

func TestIteratorValueMarshalRoundTrip(t *testing.T) {
	inputs := []string{
		"'hello'",
		"42",
		"-99",
		"3.14",
		"true",
		"NULL",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			info := passes.ExtractedParamInfo{LiteralSQL: input}
			val, err := info.Value()
			require.NoError(t, err)

			sql, err := marshalling.MarshalScalarToSQL(val)
			require.NoError(t, err)

			val2, err := marshalling.UnmarshalScalarLiteral(sql)
			require.NoError(t, err)

			assert.Equal(t, val.ScalarType, val2.ScalarType, "type mismatch for %s → %s", input, sql)
		})
	}
}

// --- Value() array marshal round-trip ---

func TestIteratorValueArrayMarshalRoundTrip(t *testing.T) {
	info := passes.ExtractedParamInfo{LiteralSQL: "[1, 2, 3]"}
	val, err := info.Value()
	require.NoError(t, err)

	sql, err := marshalling.MarshalTypedLiteralToSQL(val, nil)
	require.NoError(t, err)
	t.Logf("[1, 2, 3] → %s", sql)

	assert.Contains(t, sql, "[")
	assert.Contains(t, sql, "]")
}
