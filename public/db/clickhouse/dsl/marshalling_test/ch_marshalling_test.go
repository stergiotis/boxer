//go:build llm_generated_opus46

package marshalling_test

import (
	"math"
	"testing"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/marshalling"
	"github.com/stergiotis/boxer/public/semistructured/leeway/canonicaltypes/ctabb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- EscapeString / UnescapeString ---

func TestEscapeStringBasic(t *testing.T) {
	assert.Equal(t, "'hello'", marshalling.EscapeString("hello"))
}

func TestEscapeStringWithQuote(t *testing.T) {
	assert.Equal(t, "'it\\'s'", marshalling.EscapeString("it's"))
}

func TestEscapeStringWithBackslash(t *testing.T) {
	assert.Equal(t, "'back\\\\slash'", marshalling.EscapeString("back\\slash"))
}

func TestEscapeStringWithNewline(t *testing.T) {
	assert.Equal(t, "'line1\\nline2'", marshalling.EscapeString("line1\nline2"))
}

func TestEscapeStringWithTab(t *testing.T) {
	assert.Equal(t, "'col1\\tcol2'", marshalling.EscapeString("col1\tcol2"))
}

func TestEscapeStringWithNul(t *testing.T) {
	assert.Equal(t, "'a\\0b'", marshalling.EscapeString("a\x00b"))
}

func TestUnescapeStringBasic(t *testing.T) {
	result, err := marshalling.UnescapeString("'hello'")
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestUnescapeStringBackslashQuote(t *testing.T) {
	result, err := marshalling.UnescapeString("'it\\'s'")
	require.NoError(t, err)
	assert.Equal(t, "it's", result)
}

func TestUnescapeStringDoubledQuote(t *testing.T) {
	result, err := marshalling.UnescapeString("'it''s'")
	require.NoError(t, err)
	assert.Equal(t, "it's", result)
}

func TestUnescapeStringHex(t *testing.T) {
	result, err := marshalling.UnescapeString("'\\x41'")
	require.NoError(t, err)
	assert.Equal(t, "A", result)
}

func TestUnescapeStringUnicode(t *testing.T) {
	result, err := marshalling.UnescapeString("'\\u0041'")
	require.NoError(t, err)
	assert.Equal(t, "A", result)
}

func TestUnescapeStringFullUnicode(t *testing.T) {
	result, err := marshalling.UnescapeString("'\\U0001F600'")
	require.NoError(t, err)
	assert.Equal(t, "\U0001F600", result)
}

func TestEscapeUnescapeRoundTrip(t *testing.T) {
	inputs := []string{
		"hello",
		"it's a test",
		"back\\slash",
		"line1\nline2",
		"tab\there",
		"nul\x00byte",
		"",
		"a'b'c",
	}
	for _, input := range inputs {
		escaped := marshalling.EscapeString(input)
		unescaped, err := marshalling.UnescapeString(escaped)
		require.NoError(t, err)
		assert.Equal(t, input, unescaped, "round-trip failed for %q", input)
	}
}

func TestUnescapeStringInvalid(t *testing.T) {
	_, err := marshalling.UnescapeString("hello")
	assert.Error(t, err)

	_, err = marshalling.UnescapeString("'")
	assert.Error(t, err)
}

// --- UnmarshalScalarLiteral ---

func TestUnmarshalScalarString(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("'hello world'")
	require.NoError(t, err)
	assert.True(t, lit.IsScalar())
	assert.Equal(t, ctabb.S, lit.ScalarType)
	assert.Equal(t, "hello world", lit.StringVal)
}

func TestUnmarshalScalarUint(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("42")
	require.NoError(t, err)
	assert.Equal(t, ctabb.U64, lit.ScalarType)
	assert.Equal(t, uint64(42), lit.UintVal)
}

func TestUnmarshalScalarNegativeInt(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("-99")
	require.NoError(t, err)
	assert.Equal(t, ctabb.I64, lit.ScalarType)
	assert.Equal(t, int64(-99), lit.IntVal)
}

func TestUnmarshalScalarFloat(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("3.14")
	require.NoError(t, err)
	assert.Equal(t, ctabb.F64, lit.ScalarType)
	assert.Equal(t, 3.14, lit.FloatVal)
}

func TestUnmarshalScalarScientific(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("1e10")
	require.NoError(t, err)
	assert.Equal(t, ctabb.F64, lit.ScalarType)
	assert.Equal(t, 1e10, lit.FloatVal)
}

func TestUnmarshalScalarHex(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("0xFF")
	require.NoError(t, err)
	assert.Equal(t, ctabb.U64, lit.ScalarType)
	assert.Equal(t, uint64(255), lit.UintVal)
}

func TestUnmarshalScalarOctal(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("0777")
	require.NoError(t, err)
	assert.Equal(t, ctabb.U64, lit.ScalarType)
	assert.Equal(t, uint64(511), lit.UintVal)
}

func TestUnmarshalScalarTrue(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("true")
	require.NoError(t, err)
	assert.Equal(t, ctabb.B, lit.ScalarType)
	assert.True(t, lit.BoolVal)
}

func TestUnmarshalScalarFalse(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("false")
	require.NoError(t, err)
	assert.Equal(t, ctabb.B, lit.ScalarType)
	assert.False(t, lit.BoolVal)
}

func TestUnmarshalScalarNull(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("NULL")
	require.NoError(t, err)
	assert.True(t, lit.IsNull())
	assert.Nil(t, lit.ScalarType)
}

func TestUnmarshalScalarNullCaseInsensitive(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("null")
	require.NoError(t, err)
	assert.True(t, lit.IsNull())
}

func TestUnmarshalScalarInf(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("Inf")
	require.NoError(t, err)
	assert.Equal(t, ctabb.F64, lit.ScalarType)
	assert.True(t, math.IsInf(lit.FloatVal, 1))
}

func TestUnmarshalScalarNaN(t *testing.T) {
	lit, err := marshalling.UnmarshalScalarLiteral("NaN")
	require.NoError(t, err)
	assert.Equal(t, ctabb.F64, lit.ScalarType)
	assert.True(t, math.IsNaN(lit.FloatVal))
}

func TestUnmarshalScalarEmpty(t *testing.T) {
	_, err := marshalling.UnmarshalScalarLiteral("")
	assert.Error(t, err)
}

func TestUnmarshalScalarBareSign(t *testing.T) {
	_, err := marshalling.UnmarshalScalarLiteral("-")
	assert.Error(t, err)
}

// --- MarshalScalarToSQL ---

func TestMarshalScalarString(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarString("hello"))
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql)
}

func TestMarshalScalarUint(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarUint64(42))
	require.NoError(t, err)
	assert.Equal(t, "42", sql)
}

func TestMarshalScalarInt(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarInt64(-99))
	require.NoError(t, err)
	assert.Equal(t, "-99", sql)
}

func TestMarshalScalarFloat(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarFloat64(3.14))
	require.NoError(t, err)
	assert.Equal(t, "3.14", sql)
}

func TestMarshalScalarBoolTrue(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarBool(true))
	require.NoError(t, err)
	assert.Equal(t, "true", sql)
}

func TestMarshalScalarBoolFalse(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarBool(false))
	require.NoError(t, err)
	assert.Equal(t, "false", sql)
}

func TestMarshalScalarNull(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarNull())
	require.NoError(t, err)
	assert.Equal(t, "NULL", sql)
}

func TestMarshalScalarNilType(t *testing.T) {
	lit := marshalling.TypedLiteral{Kind: marshalling.KindScalar}
	_, err := marshalling.MarshalScalarToSQL(lit)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil ScalarType")
}

func TestMarshalScalarWrongKind(t *testing.T) {
	lit := marshalling.TypedLiteral{Kind: marshalling.KindTuple}
	_, err := marshalling.MarshalScalarToSQL(lit)
	assert.Error(t, err)
}

func TestMarshalScalarInf(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarFloat64(math.Inf(1)))
	require.NoError(t, err)
	assert.Equal(t, "Inf", sql)
}

func TestMarshalScalarNegInf(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarFloat64(math.Inf(-1)))
	require.NoError(t, err)
	assert.Equal(t, "-Inf", sql)
}

func TestMarshalScalarNaN(t *testing.T) {
	sql, err := marshalling.MarshalScalarToSQL(marshalling.NewScalarFloat64(math.NaN()))
	require.NoError(t, err)
	assert.Equal(t, "NaN", sql)
}

// --- Scalar round-trip ---

func TestScalarRoundTrip(t *testing.T) {
	inputs := []string{"'hello'", "42", "-99", "3.14", "true", "false", "NULL", "0xFF"}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			lit, err := marshalling.UnmarshalScalarLiteral(input)
			require.NoError(t, err)

			sql, err := marshalling.MarshalScalarToSQL(lit)
			require.NoError(t, err)

			lit2, err := marshalling.UnmarshalScalarLiteral(sql)
			require.NoError(t, err)

			assert.Equal(t, lit, lit2, "round-trip mismatch for %s → %s", input, sql)
		})
	}
}

// --- TypedLiteralKind ---

func TestTypedLiteralKindString(t *testing.T) {
	assert.Equal(t, "scalar", marshalling.KindScalar.String())
	assert.Equal(t, "homogeneous_array", marshalling.KindHomogeneousArray.String())
	assert.Equal(t, "heterogeneous_array", marshalling.KindHeterogeneousArray.String())
	assert.Equal(t, "tuple", marshalling.KindTuple.String())
}

// --- Predicates ---

func TestPredicatesScalar(t *testing.T) {
	lit := marshalling.NewScalarUint64(1)
	assert.True(t, lit.IsScalar())
	assert.False(t, lit.IsArray())
	assert.False(t, lit.IsTuple())
	assert.False(t, lit.IsNull())
	assert.False(t, lit.HasCast())
}

func TestPredicatesNull(t *testing.T) {
	lit := marshalling.NewScalarNull()
	assert.True(t, lit.IsScalar())
	assert.True(t, lit.IsNull())
}

func TestPredicatesHomArray(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{1, 2, 3})
	assert.True(t, lit.IsArray())
	assert.True(t, lit.IsHomogeneousArray())
	assert.False(t, lit.IsHeterogeneousArray())
	assert.False(t, lit.IsScalar())
	assert.False(t, lit.IsTuple())
	assert.Equal(t, 3, lit.ArrayLen())
}

func TestPredicatesHetArray(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(marshalling.NewScalarUint64(1), marshalling.NewScalarString("a"))
	assert.True(t, lit.IsArray())
	assert.True(t, lit.IsHeterogeneousArray())
	assert.False(t, lit.IsHomogeneousArray())
	assert.Equal(t, 2, lit.ArrayLen())
}

func TestPredicatesTuple(t *testing.T) {
	lit := marshalling.NewTupleTyped(marshalling.NewScalarUint64(1))
	assert.True(t, lit.IsTuple())
	assert.False(t, lit.IsArray())
}

func TestWithCast(t *testing.T) {
	lit := marshalling.NewScalarUint64(1).WithCast("u64")
	assert.True(t, lit.HasCast())
	assert.Equal(t, "u64", lit.CastTypeCanonical)
}

// --- HomogeneousArray ---

func TestHomogeneousArrayLen(t *testing.T) {
	a := marshalling.NewHomogeneousArray(ctabb.U64, 0)
	assert.Equal(t, 0, a.Len())

	a.AppendScalar(marshalling.NewScalarUint64(1))
	a.AppendScalar(marshalling.NewScalarUint64(2))
	assert.Equal(t, 2, a.Len())
}

func TestHomogeneousArrayGetScalar(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{10, 20, 30})
	elem, err := lit.HomArray.GetScalar(1)
	require.NoError(t, err)
	assert.Equal(t, uint64(20), elem.UintVal)
	assert.Equal(t, ctabb.U64, elem.ScalarType)
}

func TestHomogeneousArrayGetScalarOutOfRange(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{1})
	_, err := lit.HomArray.GetScalar(5)
	assert.Error(t, err)

	_, err = lit.HomArray.GetScalar(-1)
	assert.Error(t, err)
}

func TestHomogeneousArrayAppendTypeMismatch(t *testing.T) {
	a := marshalling.NewHomogeneousArray(ctabb.U64, 0)
	err := a.AppendScalar(marshalling.NewScalarString("wrong"))
	assert.Error(t, err)
}

func TestHomogeneousArrayAllTypes(t *testing.T) {
	tests := []struct {
		name string
		lit  marshalling.TypedLiteral
		len  int
	}{
		{"string", marshalling.NewHomogeneousStringArray([]string{"a", "b"}), 2},
		{"uint64", marshalling.NewHomogeneousUint64Array([]uint64{1, 2, 3}), 3},
		{"int64", marshalling.NewHomogeneousInt64Array([]int64{-1, 0, 1}), 3},
		{"float64", marshalling.NewHomogeneousFloat64Array([]float64{1.1, 2.2}), 2},
		{"bool", marshalling.NewHomogeneousBoolArray([]bool{true, false, true}), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.True(t, tt.lit.IsHomogeneousArray())
			assert.Equal(t, tt.len, tt.lit.ArrayLen())
		})
	}
}

// --- ToHeterogeneous ---

func TestToHeterogeneousUints(t *testing.T) {
	hom := marshalling.NewHomogeneousUint64Array([]uint64{10, 20, 30})
	het, err := hom.ToHeterogeneous()
	require.NoError(t, err)
	assert.True(t, het.IsHeterogeneousArray())
	assert.Equal(t, 3, het.ArrayLen())
	assert.Equal(t, uint64(10), het.Elements[0].UintVal)
	assert.Equal(t, uint64(30), het.Elements[2].UintVal)
}

func TestToHeterogeneousStrings(t *testing.T) {
	hom := marshalling.NewHomogeneousStringArray([]string{"a", "b"})
	het, err := hom.ToHeterogeneous()
	require.NoError(t, err)
	assert.Equal(t, 2, het.ArrayLen())
	assert.Equal(t, "a", het.Elements[0].StringVal)
}

func TestToHeterogeneousPreservesCast(t *testing.T) {
	hom := marshalling.NewHomogeneousUint64Array([]uint64{1}).WithCast("u64h")
	het, err := hom.ToHeterogeneous()
	require.NoError(t, err)
	assert.Equal(t, "u64h", het.CastTypeCanonical)
}

func TestToHeterogeneousNoopOnScalar(t *testing.T) {
	scalar := marshalling.NewScalarUint64(42)
	result, err := scalar.ToHeterogeneous()
	require.NoError(t, err)
	assert.Equal(t, scalar, result)
}

// --- TryHomogeneous ---

func TestTryHomogeneousSameType(t *testing.T) {
	het := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarUint64(2),
		marshalling.NewScalarUint64(3),
	)
	hom, ok := het.TryHomogeneous()
	assert.True(t, ok)
	assert.True(t, hom.IsHomogeneousArray())
	assert.Equal(t, 3, hom.ArrayLen())
	assert.Equal(t, ctabb.U64, hom.HomArray.ElementType)
}

func TestTryHomogeneousMixedTypes(t *testing.T) {
	het := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarString("a"),
	)
	_, ok := het.TryHomogeneous()
	assert.False(t, ok)
}

func TestTryHomogeneousWithCastElement(t *testing.T) {
	het := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1).WithCast("u64"),
		marshalling.NewScalarUint64(2),
	)
	_, ok := het.TryHomogeneous()
	assert.False(t, ok, "elements with casts should prevent homogeneous conversion")
}

func TestTryHomogeneousEmpty(t *testing.T) {
	het := marshalling.NewHeterogeneousArray()
	_, ok := het.TryHomogeneous()
	assert.False(t, ok, "empty array can't determine element type")
}

func TestTryHomogeneousWithNull(t *testing.T) {
	het := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarNull(),
	)
	_, ok := het.TryHomogeneous()
	assert.False(t, ok, "null element should prevent homogeneous conversion")
}

func TestTryHomogeneousPreservesCast(t *testing.T) {
	het := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarUint64(2),
	).WithCast("u64h")
	hom, ok := het.TryHomogeneous()
	assert.True(t, ok)
	assert.Equal(t, "u64h", hom.CastTypeCanonical)
}

// --- ToHeterogeneous → TryHomogeneous round-trip ---

func TestHomHetRoundTrip(t *testing.T) {
	original := marshalling.NewHomogeneousUint64Array([]uint64{10, 20, 30})
	het, err := original.ToHeterogeneous()
	require.NoError(t, err)
	hom, ok := het.TryHomogeneous()
	assert.True(t, ok)
	assert.Equal(t, original.HomArray.UintVals, hom.HomArray.UintVals)
}

// --- MarshalTypedLiteralToSQLEx ---

func TestMarshalTypedScalar(t *testing.T) {
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(marshalling.NewScalarString("hello"), nil)
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql)
}

func TestMarshalTypedScalarWithCast(t *testing.T) {
	lit := marshalling.NewScalarUint64(1).WithCast("u64")
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)
	assert.Equal(t, "CAST(1, 'UInt64')", sql)
}

func TestMarshalTypedScalarWithCastNilMapper(t *testing.T) {
	lit := marshalling.NewScalarUint64(1).WithCast("u64")
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "1", sql, "nil mapper should drop cast")
}

func TestMarshalTypedHomogeneousArray(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{1, 2, 3})
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)
	assert.Equal(t, "array(1, 2, 3)", sql)
}

func TestMarshalTypedHomogeneousArrayStrings(t *testing.T) {
	lit := marshalling.NewHomogeneousStringArray([]string{"a", "b", "c"})
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "array('a', 'b', 'c')", sql)
}

func TestMarshalTypedHomogeneousArrayEmpty(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array(nil)
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "array()", sql)
}

func TestMarshalTypedHomogeneousArrayWithCast(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{1, 2}).WithCast("u64h")
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)
	// Note: u64h doesn't map in our mock — cast is dropped
	// If it mapped, we'd get CAST([1, 2], 'Array(UInt64)')
	assert.Equal(t, "array(1, 2)", sql)
}

func TestMarshalTypedHeterogeneousArray(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarString("hello"),
	)
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "[1, 'hello']", sql)
}

func TestMarshalTypedHeterogeneousArrayWithElementCasts(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1).WithCast("u64"),
		marshalling.NewScalarUint64(2).WithCast("u64"),
	)
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)
	assert.Equal(t, "[CAST(1, 'UInt64'), CAST(2, 'UInt64')]", sql)
}

func TestMarshalTypedTuple(t *testing.T) {
	lit := marshalling.NewTupleTyped(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarString("hello"),
	)
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "tuple(1, 'hello')", sql)
}

func TestMarshalTypedTupleEmpty(t *testing.T) {
	lit := marshalling.NewTupleTyped()
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
	require.NoError(t, err)
	assert.Equal(t, "tuple()", sql)
}

func TestMarshalTypedTupleWithCasts(t *testing.T) {
	lit := marshalling.NewTupleTyped(
		marshalling.NewScalarUint64(1).WithCast("u64"),
		marshalling.NewScalarBool(true),
	)
	sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
	require.NoError(t, err)
	assert.Equal(t, "tuple(CAST(1, 'UInt64'), true)", sql)
}

// --- UnmarshalCompositeLiteral ---

func TestUnmarshalCompositeScalar(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("42")
	require.NoError(t, err)
	assert.True(t, lit.IsScalar())
	assert.Equal(t, uint64(42), lit.UintVal)
}

func TestUnmarshalCompositeString(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("'hello'")
	require.NoError(t, err)
	assert.Equal(t, "hello", lit.StringVal)
}

func TestUnmarshalCompositeBoolTrue(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("true")
	require.NoError(t, err)
	assert.True(t, lit.BoolVal)
}

func TestUnmarshalCompositeNull(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("NULL")
	require.NoError(t, err)
	assert.True(t, lit.IsNull())
}

func TestUnmarshalCompositeArrayHomogeneous(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("[1, 2, 3]")
	require.NoError(t, err)
	assert.True(t, lit.IsHomogeneousArray(), "uniform scalar array should be homogeneous")
	assert.Equal(t, 3, lit.ArrayLen())
	assert.Equal(t, ctabb.U64, lit.HomArray.ElementType)
	assert.Equal(t, []uint64{1, 2, 3}, lit.HomArray.UintVals)
}

func TestUnmarshalCompositeArrayHomogeneousStrings(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("['a', 'b', 'c']")
	require.NoError(t, err)
	assert.True(t, lit.IsHomogeneousArray())
	assert.Equal(t, []string{"a", "b", "c"}, lit.HomArray.StringVals)
}

func TestUnmarshalCompositeArrayHeterogeneous(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("[1, 'hello']")
	require.NoError(t, err)
	assert.True(t, lit.IsHeterogeneousArray(), "mixed-type array should be heterogeneous")
	assert.Equal(t, 2, lit.ArrayLen())
}

func TestUnmarshalCompositeArrayEmpty(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("[]")
	require.NoError(t, err)
	assert.True(t, lit.IsArray())
	assert.Equal(t, 0, lit.ArrayLen())
}

func TestUnmarshalCompositeArrayNested(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("[[1, 2], [3, 4]]")
	require.NoError(t, err)
	assert.True(t, lit.IsHeterogeneousArray(), "nested array should be heterogeneous")
	assert.Equal(t, 2, lit.ArrayLen())
	assert.True(t, lit.Elements[0].IsHomogeneousArray())
}

func TestUnmarshalCompositeArrayWithCasts(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("[CAST(1, 'UInt64'), CAST(2, 'UInt64')]")
	require.NoError(t, err)
	// Elements have casts → heterogeneous
	assert.True(t, lit.IsHeterogeneousArray())
	assert.Equal(t, "u64", lit.Elements[0].CastTypeCanonical)
}

func TestUnmarshalCompositeTuple(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("tuple(1, 'hello')")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
}

func TestUnmarshalCompositeTupleEmpty(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("tuple()")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Empty(t, lit.Elements)
}

func TestUnmarshalCompositeTupleWithCasts(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("tuple(CAST(1, 'UInt64'), true)")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, "u64", lit.Elements[0].CastTypeCanonical)
	assert.True(t, lit.Elements[1].BoolVal)
}

func TestUnmarshalCompositeCastDoubleColon(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("1::UInt64")
	require.NoError(t, err)
	assert.True(t, lit.IsScalar())
	assert.Equal(t, "u64", lit.CastTypeCanonical)
}

func TestUnmarshalCompositeCastFunction(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("CAST(1, 'UInt64')")
	require.NoError(t, err)
	assert.True(t, lit.IsScalar())
	assert.Equal(t, "u64", lit.CastTypeCanonical)
}

func TestUnmarshalCompositeCastNilMapper(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteralEx("CAST(1, 'UInt64')", nil)
	require.NoError(t, err)
	assert.Equal(t, "", lit.CastTypeCanonical, "nil mapper should not set cast")
}

func TestUnmarshalCompositeEmpty(t *testing.T) {
	_, err := marshalling.UnmarshalCompositeLiteral("")
	assert.Error(t, err)
}

// --- Composite round-trip: unmarshal → marshal → unmarshal ---

func TestCompositeRoundTrip(t *testing.T) {
	inputs := []struct {
		name string
		sql  string
	}{
		{"scalar_uint", "42"},
		{"scalar_string", "'hello'"},
		{"scalar_bool", "true"},
		{"scalar_null", "NULL"},
		{"scalar_float", "3.14"},
		{"array_ints", "array(1, 2, 3)"},
		{"array_strings", "array('a', 'b')"},
		{"array_empty", "array()"},
		{"tuple_mixed", "tuple(1, 'hello')"},
		{"tuple_empty", "tuple()"},
		{"cast_func", "CAST(1, 'UInt64')"},
		{"cast_array_elem", "array(CAST(1, 'UInt64'), CAST(2, 'UInt64'))"},
		{"tuple_with_cast", "tuple(CAST(1, 'UInt64'), true)"},
	}
	for _, tt := range inputs {
		t.Run(tt.name, func(t *testing.T) {
			lit, err := marshalling.UnmarshalCompositeLiteral(tt.sql)
			require.NoError(t, err)

			sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
			require.NoError(t, err)

			lit2, err := marshalling.UnmarshalCompositeLiteral(sql)
			require.NoError(t, err)

			assertTypedLiteralEqual(t, lit, lit2)
			t.Logf("%s → %s", tt.sql, sql)
		})
	}
}

// --- MarshalGoValueToSQL ---

func TestMarshalGoValueNil(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(nil)
	require.NoError(t, err)
	assert.Equal(t, "NULL", sql)
}

func TestMarshalGoValueString(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL("hello")
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql)
}

func TestMarshalGoValueBool(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(true)
	require.NoError(t, err)
	assert.Equal(t, "true", sql)
}

func TestMarshalGoValueInt64(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(int64(-42))
	require.NoError(t, err)
	assert.Equal(t, "-42", sql)
}

func TestMarshalGoValueUint64(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(uint64(42))
	require.NoError(t, err)
	assert.Equal(t, "42", sql)
}

func TestMarshalGoValueFloat64(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL(float64(3.14))
	require.NoError(t, err)
	assert.Equal(t, "3.14", sql)
}

func TestMarshalGoValueTypedLiteral(t *testing.T) {
	lit := marshalling.NewScalarString("hello")
	sql, err := marshalling.MarshalGoValueToSQL(lit)
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql)
}

func TestMarshalGoValueTypedLiteralPtr(t *testing.T) {
	lit := marshalling.NewScalarString("hello")
	sql, err := marshalling.MarshalGoValueToSQL(&lit)
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql)
}

func TestMarshalGoValueTypedLiteralPtrNil(t *testing.T) {
	var lit *marshalling.TypedLiteral
	sql, err := marshalling.MarshalGoValueToSQL(lit)
	require.NoError(t, err)
	assert.Equal(t, "NULL", sql)
}

func TestMarshalGoValueSliceAny(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL([]any{int64(1), int64(2)})
	require.NoError(t, err)
	assert.Equal(t, "array(1, 2)", sql)
}

func TestMarshalGoValueSliceString(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL([]string{"a", "b"})
	require.NoError(t, err)
	assert.Equal(t, "array('a', 'b')", sql)
}

func TestMarshalGoValueSliceEmpty(t *testing.T) {
	sql, err := marshalling.MarshalGoValueToSQL([]any{})
	require.NoError(t, err)
	assert.Equal(t, "array()", sql)
}

func TestMarshalGoValueTuple(t *testing.T) {
	tup := marshalling.NewUnnamedTuple(int64(1), "hello")
	sql, err := marshalling.MarshalGoValueToSQL(tup)
	require.NoError(t, err)
	assert.Contains(t, sql, "tuple(")
	assert.Contains(t, sql, "'hello'")
}

func TestMarshalGoValueSliceTypedLiteral(t *testing.T) {
	elems := []marshalling.TypedLiteral{
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarUint64(2),
	}
	sql, err := marshalling.MarshalGoValueToSQL(elems)
	require.NoError(t, err)
	assert.Equal(t, "[1, 2]", sql)
}

func TestMarshalGoValueUnsupported(t *testing.T) {
	_, err := marshalling.MarshalGoValueToSQL(struct{}{})
	assert.Error(t, err)
}

// --- MarshalGoValueToSQLWithOptions / PreserveCasts ---

func TestMarshalGoValueWithCastsInt64(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(int64(42), opts)
	require.NoError(t, err)
	assert.Equal(t, "CAST(42, 'Int64')", sql)
}

func TestMarshalGoValueWithCastsUint64(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(uint64(42), opts)
	require.NoError(t, err)
	assert.Equal(t, "CAST(42, 'UInt64')", sql)
}

func TestMarshalGoValueWithCastsFloat32(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(float32(1.0), opts)
	require.NoError(t, err)
	assert.Contains(t, sql, "CAST(")
	assert.Contains(t, sql, "'Float32'")
}

func TestMarshalGoValueWithCastsInt8(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(int8(5), opts)
	require.NoError(t, err)
	assert.Equal(t, "CAST(5, 'Int8')", sql)
}

func TestMarshalGoValueWithCastsUint16(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(uint16(256), opts)
	require.NoError(t, err)
	assert.Equal(t, "CAST(256, 'UInt16')", sql)
}

func TestMarshalGoValueWithCastsStringNoCast(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions("hello", opts)
	require.NoError(t, err)
	assert.Equal(t, "'hello'", sql, "string should not be wrapped in CAST")
}

func TestMarshalGoValueWithCastsBoolNoCast(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions(true, opts)
	require.NoError(t, err)
	assert.Equal(t, "true", sql, "bool should not be wrapped in CAST")
}

func TestMarshalGoValueWithCastsArrayInt32(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions([]int32{1, 2, 3}, opts)
	require.NoError(t, err)
	assert.Contains(t, sql, "CAST(")
	assert.Contains(t, sql, "'Array(Int32)'")
}

func TestMarshalGoValueWithCastsArrayFloat32(t *testing.T) {
	opts := marshalling.MarshalOptions{PreserveCasts: true}
	sql, err := marshalling.MarshalGoValueToSQLWithOptions([]float32{1.0, 2.0}, opts)
	require.NoError(t, err)
	assert.Contains(t, sql, "'Array(Float32)'")
}

func TestMarshalGoValueNoCastsDefault(t *testing.T) {
	sql1, err := marshalling.MarshalGoValueToSQL(int64(42))
	require.NoError(t, err)
	assert.Equal(t, "42", sql1, "default should not cast")
}

// --- Tuple ---

func TestTupleBasic(t *testing.T) {
	tup := marshalling.NewTuple([]string{"id", "name"})
	assert.Equal(t, 2, tup.Len())

	tup.SetByName("id", int64(1))
	tup.SetByName("name", "alice")

	val, found := tup.GetByName("id")
	assert.True(t, found)
	assert.Equal(t, int64(1), val)

	val, found = tup.GetByName("name")
	assert.True(t, found)
	assert.Equal(t, "alice", val)
}

func TestTupleByIndex(t *testing.T) {
	tup := marshalling.NewUnnamedTuple(int64(1), "hello", true)
	assert.Equal(t, 3, tup.Len())

	val, found := tup.GetByIndex(0)
	assert.True(t, found)
	assert.Equal(t, int64(1), val)

	val, found = tup.GetByIndex(2)
	assert.True(t, found)
	assert.Equal(t, true, val)
}

func TestTupleOutOfRange(t *testing.T) {
	tup := marshalling.NewUnnamedTuple(int64(1))
	_, found := tup.GetByIndex(5)
	assert.False(t, found)
	_, found = tup.GetByIndex(-1)
	assert.False(t, found)
}

func TestTupleIterate(t *testing.T) {
	tup := marshalling.NewUnnamedTuple(int64(1), "hello")
	count := 0
	for _, _ = range tup.IterateAll() {
		count++
	}
	assert.Equal(t, 2, count)
}

// --- Helpers ---

func assertTypedLiteralEqual(t *testing.T, a, b marshalling.TypedLiteral) {
	t.Helper()
	assert.Equal(t, a.Kind, b.Kind, "Kind mismatch")
	assert.Equal(t, a.CastTypeCanonical, b.CastTypeCanonical, "CastTypeCanonical mismatch")

	switch a.Kind {
	case marshalling.KindScalar:
		assert.Equal(t, a.Null, b.Null, "Null mismatch")
		if !a.Null {
			if a.ScalarType != nil && b.ScalarType != nil {
				assert.Equal(t, a.ScalarType.String(), b.ScalarType.String(), "ScalarType mismatch")
			}
			assert.Equal(t, a.StringVal, b.StringVal, "StringVal mismatch")
			assert.Equal(t, a.IntVal, b.IntVal, "IntVal mismatch")
			assert.Equal(t, a.UintVal, b.UintVal, "UintVal mismatch")
			assert.Equal(t, a.BoolVal, b.BoolVal, "BoolVal mismatch")
			if !math.IsNaN(a.FloatVal) {
				assert.Equal(t, a.FloatVal, b.FloatVal, "FloatVal mismatch")
			} else {
				assert.True(t, math.IsNaN(b.FloatVal), "expected NaN")
			}
		}

	case marshalling.KindHomogeneousArray:
		require.NotNil(t, a.HomArray)
		require.NotNil(t, b.HomArray)
		assert.Equal(t, a.HomArray.ElementType.String(), b.HomArray.ElementType.String())
		assert.Equal(t, a.HomArray.Len(), b.HomArray.Len())

	case marshalling.KindHeterogeneousArray, marshalling.KindTuple:
		require.Equal(t, len(a.Elements), len(b.Elements), "Elements length mismatch")
		for i := range a.Elements {
			assertTypedLiteralEqual(t, a.Elements[i], b.Elements[i])
		}
	}
}

// unmarshalTupleExprCST handles the (elem1, elem2, ...) parenthesized form.
// This is different from tuple(elem1, elem2) which goes through unmarshalTupleFunctionCST.
// The parser produces ColumnExprTupleContext for parenthesized comma-separated lists.

func TestUnmarshalTupleExprBasic(t *testing.T) {
	// (1, 2, 3) parses as ColumnExprTupleContext
	lit, err := marshalling.UnmarshalCompositeLiteral("(1, 2, 3)")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 3, len(lit.Elements))
	assert.Equal(t, uint64(1), lit.Elements[0].UintVal)
	assert.Equal(t, uint64(2), lit.Elements[1].UintVal)
	assert.Equal(t, uint64(3), lit.Elements[2].UintVal)
}

func TestUnmarshalTupleExprMixedTypes(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("(1, 'hello', true)")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 3, len(lit.Elements))
	assert.Equal(t, uint64(1), lit.Elements[0].UintVal)
	assert.Equal(t, "hello", lit.Elements[1].StringVal)
	assert.True(t, lit.Elements[2].BoolVal)
}

func TestUnmarshalTupleExprSingleElement(t *testing.T) {
	// Note: (1) in SQL is just parenthesization, not a tuple.
	// (1, 2) is the minimum tuple — but let's test what the parser produces for (1,2)
	lit, err := marshalling.UnmarshalCompositeLiteral("(1, 2)")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
}

func TestUnmarshalTupleExprWithCasts(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("(CAST(1, 'UInt64'), CAST(2, 'Int32'))")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
	assert.Equal(t, "u64", lit.Elements[0].CastTypeCanonical)
	assert.Equal(t, "i32", lit.Elements[1].CastTypeCanonical)
}

func TestUnmarshalTupleExprWithCastsNilMapper(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteralEx("(CAST(1, 'UInt64'), 'hello')", nil)
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, "", lit.Elements[0].CastTypeCanonical, "nil mapper should not set cast")
}

func TestUnmarshalTupleExprNestedArray(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("([1, 2], [3, 4])")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
	assert.True(t, lit.Elements[0].IsArray())
	assert.True(t, lit.Elements[1].IsArray())
}

func TestUnmarshalTupleExprNestedTuple(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("((1, 2), (3, 4))")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
	assert.True(t, lit.Elements[0].IsTuple())
	assert.True(t, lit.Elements[1].IsTuple())
}

func TestUnmarshalTupleExprWithNull(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("(1, NULL, 'hello')")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 3, len(lit.Elements))
	assert.True(t, lit.Elements[1].IsNull())
}

func TestUnmarshalTupleExprStringElements(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("('a', 'b', 'c')")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 3, len(lit.Elements))
	assert.Equal(t, "a", lit.Elements[0].StringVal)
	assert.Equal(t, "b", lit.Elements[1].StringVal)
	assert.Equal(t, "c", lit.Elements[2].StringVal)
}

func TestUnmarshalTupleExprFloats(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("(1.1, 2.2, 3.3)")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 3, len(lit.Elements))
	assert.Equal(t, ctabb.F64, lit.Elements[0].ScalarType)
	assert.Equal(t, 1.1, lit.Elements[0].FloatVal)
}

func TestUnmarshalTupleExprWithDoubleColonCast(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("(1::UInt64, 'hello')")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
	assert.Equal(t, "u64", lit.Elements[0].CastTypeCanonical)
	assert.Equal(t, "hello", lit.Elements[1].StringVal)
}

func TestUnmarshalTupleExprWithOuterCast(t *testing.T) {
	lit, err := marshalling.UnmarshalCompositeLiteral("CAST((1, 'hello'), 'Tuple(UInt64, String)')")
	require.NoError(t, err)
	assert.True(t, lit.IsTuple())
	assert.Equal(t, 2, len(lit.Elements))
	// Outer cast — may or may not be representable depending on mapper
	// Our mock doesn't handle "Tuple(UInt64, String)" so cast is empty
	t.Logf("outer cast: %q", lit.CastTypeCanonical)
}

// --- Round-trip for tuple expr forms ---

func TestTupleExprRoundTrip(t *testing.T) {
	inputs := []string{
		"(1, 2, 3)",
		"(1, 'hello', true)",
		"('a', 'b', 'c')",
		"(1.1, 2.2)",
		"([1, 2], [3, 4])",
		"(1, NULL, 'hello')",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			lit, err := marshalling.UnmarshalCompositeLiteral(input)
			require.NoError(t, err)

			// Marshal as tuple(...) form
			sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, nil)
			require.NoError(t, err)

			// Re-unmarshal (now as tuple() function form)
			lit2, err := marshalling.UnmarshalCompositeLiteral(sql)
			require.NoError(t, err)

			assert.Equal(t, lit.Kind, lit2.Kind)
			assert.Equal(t, len(lit.Elements), len(lit2.Elements))
			for i := range lit.Elements {
				assertTypedLiteralEqual(t, lit.Elements[i], lit2.Elements[i])
			}

			t.Logf("%s → %s", input, sql)
		})
	}
}

func TestTupleExprWithCastsRoundTrip(t *testing.T) {
	inputs := []string{
		"(CAST(1, 'UInt64'), CAST(2, 'Int32'))",
		"(CAST(1, 'UInt64'), true)",
	}
	for _, input := range inputs {
		t.Run(input, func(t *testing.T) {
			lit, err := marshalling.UnmarshalCompositeLiteral(input)
			require.NoError(t, err)

			sql, err := marshalling.MarshalTypedLiteralToSQLEx(lit, marshalling.MapCanonicalToClickHouseTypeStr)
			require.NoError(t, err)

			lit2, err := marshalling.UnmarshalCompositeLiteral(sql)
			require.NoError(t, err)

			assert.Equal(t, len(lit.Elements), len(lit2.Elements))
			for i := range lit.Elements {
				assert.Equal(t, lit.Elements[i].CastTypeCanonical, lit2.Elements[i].CastTypeCanonical,
					"cast mismatch at element %d", i)
			}

			t.Logf("%s → %s", input, sql)
		})
	}
}

// --- Scalar ToAny ---

func TestToAnyString(t *testing.T) {
	val, err := marshalling.NewScalarString("hello").ToAny()
	require.NoError(t, err)
	assert.Equal(t, "hello", val)
}

func TestToAnyUint64(t *testing.T) {
	val, err := marshalling.NewScalarUint64(42).ToAny()
	require.NoError(t, err)
	assert.Equal(t, uint64(42), val)
}

func TestToAnyInt64(t *testing.T) {
	val, err := marshalling.NewScalarInt64(-99).ToAny()
	require.NoError(t, err)
	assert.Equal(t, int64(-99), val)
}

func TestToAnyFloat64(t *testing.T) {
	val, err := marshalling.NewScalarFloat64(3.14).ToAny()
	require.NoError(t, err)
	assert.Equal(t, 3.14, val)
}

func TestToAnyBoolTrue(t *testing.T) {
	val, err := marshalling.NewScalarBool(true).ToAny()
	require.NoError(t, err)
	assert.Equal(t, true, val)
}

func TestToAnyBoolFalse(t *testing.T) {
	val, err := marshalling.NewScalarBool(false).ToAny()
	require.NoError(t, err)
	assert.Equal(t, false, val)
}

func TestToAnyNull(t *testing.T) {
	val, err := marshalling.NewScalarNull().ToAny()
	require.NoError(t, err)
	assert.Nil(t, val)
}

func TestToAnyNilScalarType(t *testing.T) {
	lit := marshalling.TypedLiteral{Kind: marshalling.KindScalar}
	_, err := lit.ToAny()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil ScalarType")
}

// --- Homogeneous array ToAny ---

func TestToAnyHomStringArray(t *testing.T) {
	val, err := marshalling.NewHomogeneousStringArray([]string{"a", "b", "c"}).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b", "c"}, arr)
}

func TestToAnyHomUint64Array(t *testing.T) {
	val, err := marshalling.NewHomogeneousUint64Array([]uint64{1, 2, 3}).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]uint64)
	require.True(t, ok)
	assert.Equal(t, []uint64{1, 2, 3}, arr)
}

func TestToAnyHomInt64Array(t *testing.T) {
	val, err := marshalling.NewHomogeneousInt64Array([]int64{-1, 0, 1}).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]int64)
	require.True(t, ok)
	assert.Equal(t, []int64{-1, 0, 1}, arr)
}

func TestToAnyHomFloat64Array(t *testing.T) {
	val, err := marshalling.NewHomogeneousFloat64Array([]float64{1.1, 2.2}).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]float64)
	require.True(t, ok)
	assert.Equal(t, []float64{1.1, 2.2}, arr)
}

func TestToAnyHomBoolArray(t *testing.T) {
	val, err := marshalling.NewHomogeneousBoolArray([]bool{true, false}).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]bool)
	require.True(t, ok)
	assert.Equal(t, []bool{true, false}, arr)
}

func TestToAnyHomEmptyArray(t *testing.T) {
	val, err := marshalling.NewHomogeneousUint64Array(nil).ToAny()
	require.NoError(t, err)
	arr, ok := val.([]uint64)
	require.True(t, ok)
	assert.Empty(t, arr)
}

func TestToAnyHomArrayNoCopyAliasing(t *testing.T) {
	orig := []uint64{1, 2, 3}
	lit := marshalling.NewHomogeneousUint64Array(orig)
	val, err := lit.ToAny()
	require.NoError(t, err)
	arr := val.([]uint64)

	// Mutating the output should not affect the original
	arr[0] = 999
	assert.Equal(t, uint64(1), orig[0], "ToAny should return a copy, not alias")
}

// --- Heterogeneous array ToAny ---

func TestToAnyHetArray(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarString("hello"),
		marshalling.NewScalarBool(true),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)
	arr, ok := val.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 3)
	assert.Equal(t, uint64(1), arr[0])
	assert.Equal(t, "hello", arr[1])
	assert.Equal(t, true, arr[2])
}

func TestToAnyHetEmptyArray(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray()
	val, err := lit.ToAny()
	require.NoError(t, err)
	arr, ok := val.([]any)
	require.True(t, ok)
	assert.Empty(t, arr)
}

func TestToAnyHetNestedArray(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(
		marshalling.NewHomogeneousUint64Array([]uint64{1, 2}),
		marshalling.NewHomogeneousUint64Array([]uint64{3, 4}),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)
	arr, ok := val.([]any)
	require.True(t, ok)
	assert.Len(t, arr, 2)

	inner0, ok := arr[0].([]uint64)
	require.True(t, ok)
	assert.Equal(t, []uint64{1, 2}, inner0)
}

func TestToAnyHetArrayWithNull(t *testing.T) {
	lit := marshalling.NewHeterogeneousArray(
		marshalling.NewScalarUint64(1),
		marshalling.NewScalarNull(),
		marshalling.NewScalarString("x"),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)
	arr, ok := val.([]any)
	require.True(t, ok)
	assert.Equal(t, uint64(1), arr[0])
	assert.Nil(t, arr[1])
	assert.Equal(t, "x", arr[2])
}

// --- Tuple ToAny ---

func TestToAnyTuple(t *testing.T) {
	lit := marshalling.NewTupleTyped(
		marshalling.NewScalarUint64(42),
		marshalling.NewScalarString("hello"),
		marshalling.NewScalarBool(true),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)

	tup, ok := val.(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 3, tup.Len())

	v0, found := tup.GetByIndex(0)
	assert.True(t, found)
	assert.Equal(t, uint64(42), v0)

	v1, found := tup.GetByIndex(1)
	assert.True(t, found)
	assert.Equal(t, "hello", v1)

	v2, found := tup.GetByIndex(2)
	assert.True(t, found)
	assert.Equal(t, true, v2)
}

func TestToAnyTupleEmpty(t *testing.T) {
	lit := marshalling.NewTupleTyped()
	val, err := lit.ToAny()
	require.NoError(t, err)

	tup, ok := val.(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 0, tup.Len())
}

func TestToAnyTupleNested(t *testing.T) {
	lit := marshalling.NewTupleTyped(
		marshalling.NewScalarUint64(1),
		marshalling.NewTupleTyped(
			marshalling.NewScalarString("inner"),
		),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)

	tup, ok := val.(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 2, tup.Len())

	v1, found := tup.GetByIndex(1)
	assert.True(t, found)
	innerTup, ok := v1.(*marshalling.Tuple)
	require.True(t, ok)
	assert.Equal(t, 1, innerTup.Len())
}

func TestToAnyTupleWithArray(t *testing.T) {
	lit := marshalling.NewTupleTyped(
		marshalling.NewScalarUint64(1),
		marshalling.NewHomogeneousStringArray([]string{"a", "b"}),
	)
	val, err := lit.ToAny()
	require.NoError(t, err)

	tup, ok := val.(*marshalling.Tuple)
	require.True(t, ok)

	v1, found := tup.GetByIndex(1)
	assert.True(t, found)
	arr, ok := v1.([]string)
	require.True(t, ok)
	assert.Equal(t, []string{"a", "b"}, arr)
}

// --- Cast info is dropped ---

func TestToAnyCastDropped(t *testing.T) {
	lit := marshalling.NewScalarUint64(42).WithCast("u64")
	val, err := lit.ToAny()
	require.NoError(t, err)
	// Cast is not preserved in the any value
	assert.Equal(t, uint64(42), val)
}

// --- Round-trip: TypedLiteral → ToAny → MarshalGoValueToSQL → UnmarshalScalarLiteral ---

func TestToAnyMarshalRoundTrip(t *testing.T) {
	scalars := []marshalling.TypedLiteral{
		marshalling.NewScalarString("hello"),
		marshalling.NewScalarUint64(42),
		marshalling.NewScalarInt64(-99),
		marshalling.NewScalarFloat64(3.14),
		marshalling.NewScalarBool(true),
		marshalling.NewScalarBool(false),
	}
	for _, lit := range scalars {
		t.Run(lit.Kind.String(), func(t *testing.T) {
			goVal, err := lit.ToAny()
			require.NoError(t, err)

			sql, err := marshalling.MarshalGoValueToSQL(goVal)
			require.NoError(t, err)

			lit2, err := marshalling.UnmarshalScalarLiteral(sql)
			require.NoError(t, err)

			goVal2, err := lit2.ToAny()
			require.NoError(t, err)

			assert.Equal(t, goVal, goVal2, "round-trip failed for %v → %s → %v", goVal, sql, goVal2)
		})
	}
}

func TestToAnyArrayMarshalRoundTrip(t *testing.T) {
	lit := marshalling.NewHomogeneousUint64Array([]uint64{1, 2, 3})
	goVal, err := lit.ToAny()
	require.NoError(t, err)

	sql, err := marshalling.MarshalGoValueToSQL(goVal)
	require.NoError(t, err)
	t.Logf("sql: %s", sql)

	assert.Contains(t, sql, "array(")
}
