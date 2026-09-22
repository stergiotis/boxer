package identsql

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestTagValue_Tables locks the chunk tables against the weights they are
// built from: an entry is the sum of the weights of its set bits, so the
// single-bit entries are the weights themselves and the all-ones entry is
// their sum; and the weights are the Fibonacci numbers from F(2).
func TestTagValue_Tables(t *testing.T) {
	require.Len(t, digitFibWeights, tagValueDigitPositions)
	require.Equal(t, uint64(1), digitFibWeights[0])
	require.Equal(t, uint64(2), digitFibWeights[1])
	for j := 2; j < len(digitFibWeights); j++ {
		require.Equal(t, digitFibWeights[j-1]+digitFibWeights[j-2], digitFibWeights[j])
	}
	require.Equal(t, uint64(2971215073), digitFibWeights[45], "the last digit of a width-47 code weighs F(47)")
	for _, chunkBits := range []int{macroTagValueChunkBits, udfTagValueChunkBits} {
		for p := 0; p < tagValueDigitPositions/chunkBits; p++ {
			tbl := strings.Split(strings.TrimSuffix(strings.TrimPrefix(chunkTableSql(chunkBits, p), "["), "]::Array(UInt64)"), ",")
			require.Len(t, tbl, 1<<chunkBits)
			require.Equal(t, "0", tbl[0])
			var all uint64
			for i := 0; i < chunkBits; i++ {
				w := digitFibWeights[chunkBits*p+i]
				all += w
				require.Equal(t, uint64String(w), tbl[1<<(chunkBits-1-i)], "chunk %d bit %d", p, i)
			}
			require.Equal(t, uint64String(all), tbl[len(tbl)-1])
		}
	}
}

// TestTagValue_Shapes pins what the two forms promise: neither carries a
// lambda or an array function beyond the constant-table lookups; the UDF
// form names its digit word and sum once; the macro form carries no alias
// because it lands in the query's scope; the macro form stays within a
// bounded size per call.
func TestTagValue_Shapes(t *testing.T) {
	udf := tagValueExpr("x", udfTagValueChunkBits, "_lw_d", "_lw_s")
	macro := expandTagValue("(id)")
	for name, body := range map[string]string{"udf": udf, "macro": macro} {
		require.NotContains(t, body, "->", name)
		require.NotContains(t, body, "arrayMap", name)
		require.NotContains(t, body, "arraySum", name)
		require.NotContains(t, body, "range(", name)
		require.Contains(t, body, "if(bitAnd(", name)
		require.Contains(t, body, " > 47, 0, ", name)
		require.Contains(t, body, " > 4294967295, 0, ", name)
	}
	require.Equal(t, 1, strings.Count(udf, " AS _lw_d)"))
	require.Equal(t, 1, strings.Count(udf, " AS _lw_s)"))
	require.Equal(t, tagValueDigitPositions/udfTagValueChunkBits, strings.Count(udf, "arrayElement("))
	require.NotContains(t, macro, " AS ")
	require.Equal(t, 2*tagValueDigitPositions/macroTagValueChunkBits, strings.Count(macro, "arrayElement("), "the macro splices the sum into its guard and its value")
	require.Less(t, len(macro), 12*1024, "macro expansion per call")
}

func uint64String(v uint64) string {
	return strconv.FormatUint(v, 10)
}
