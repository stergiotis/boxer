package identsql

import (
	"fmt"
	"math"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// hasTagLockTagValues is the tag-value corpus the UDF range arithmetic is
// locked against: every small value, both sides of every Fibonacci boundary
// up to the uint32 maximum, the maximum itself, and random draws.
func hasTagLockTagValues() (tvs []uint64) {
	tvs = make([]uint64, 0, 320000)
	for tv := uint64(1); tv <= 300000; tv++ {
		tvs = append(tvs, tv)
	}
	for _, f := range tagFibs {
		for _, d := range []int64{-1, 0, 1} {
			v := int64(f) + d
			if v > 300000 && v <= math.MaxUint32 {
				tvs = append(tvs, uint64(v))
			}
		}
	}
	tvs = append(tvs, math.MaxUint32-1, math.MaxUint32)
	rnd := rand.New(rand.NewPCG(0x0106, 5))
	for range 5000 {
		tvs = append(tvs, 1+rnd.Uint64N(math.MaxUint32))
	}
	return
}

// TestHasTagUdf_ArithmeticMatchesEncoder locks the greedy Zeckendorf twin
// (hasTagCodeDivisor) against the encoder the identifier package uses, on
// the Go side and without a server: code + divisor must be the tag's first
// id and divisor its body span, exactly as expandHasTag folds them.
func TestHasTagUdf_ArithmeticMatchesEncoder(t *testing.T) {
	require.Equal(t, uint64(2971215073), tagFibs[len(tagFibs)-1], "table must end at F(47)")
	require.Len(t, tagFibs, 46)
	for _, tv := range hasTagLockTagValues() {
		wantCode, nBits := fibonaccicode.EncodeFibonacciCode(tv - 1)
		wantDiv := uint64(1) << (64 - nBits)
		code, div := hasTagCodeDivisor(tv)
		require.Equal(t, wantDiv, div, "divisor of tag value %d", tv)
		require.Equal(t, wantCode, code+div, "first id of tag value %d", tv)
		require.Equal(t, uint64(identifier.TagValue(tv).GetTag()), code+div, "tag bits of tag value %d", tv)
	}
	code, div := hasTagCodeDivisor(0)
	require.Zero(t, code)
	require.Zero(t, div)
}

// TestHasTagUdf_BodyShape pins what makes the body foldable and prunable:
// scalar functions only (no lambda, no array function), every Fibonacci
// number of the table spelled once as a divisor, intDiv rather than
// intDivOrZero on the key, and the alias names that let the analyzer scope
// each remainder to the call.
func TestHasTagUdf_BodyShape(t *testing.T) {
	body := expandHasTagUdfBody("x", "tag_value")
	require.NotContains(t, body, "->", "a lambda would stop the analyzer from folding the constant subtree")
	require.NotContains(t, body, "array")
	require.NotContains(t, body, "intDivOrZero")
	require.True(t, strings.HasPrefix(body, "(toUInt64(tag_value) BETWEEN 1 AND 4294967295) AND intDiv(x, "), body[:80])
	for i, f := range tagFibs {
		require.Contains(t, body, fmt.Sprintf(", %d) * %d", f, uint64(1)<<(63-i)), "digit term for F(%d)", i+2)
		require.Contains(t, body, fmt.Sprintf(" AS _lw_r%d)", i+2))
	}
	require.Equal(t, len(tagFibs), strings.Count(body, "intDiv(("), "one digit term per Fibonacci number")
	require.Contains(t, body, "greatest(intDiv(bitAnd(")
	require.True(t, strings.HasSuffix(body, "= intDiv(_lw_code, _lw_div) + 1"))
}
