package fibonaccicode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaxRepresentableExclByWidth_BruteForceOracle cross-checks the width
// bound against a structural oracle: count values by encoding each one and
// measuring its full width, never consulting the function under test.
func TestMaxRepresentableExclByWidth_BruteForceOracle(t *testing.T) {
	const maxW = 24
	// countByWidth[w] = number of values whose full code width is exactly w.
	countByWidth := make([]uint64, maxW+2)
	limit := MaxRepresentableExclByWidth(maxW + 1)
	for n := range limit {
		_, nBits := EncodeFibonacciCode(n)
		if nBits <= maxW+1 {
			countByWidth[nBits]++
		}
	}
	cum := uint64(0)
	for w := 0; w <= maxW; w++ {
		cum += countByWidth[w]
		require.Equal(t, cum, MaxRepresentableExclByWidth(w), "width %d", w)
	}
	// The per-width class intervals tile the value space contiguously.
	for w := 3; w <= maxW; w++ {
		lo := MaxRepresentableExclByWidth(w - 1)
		hiExcl := MaxRepresentableExclByWidth(w)
		require.Less(t, lo, hiExcl, "width %d class must be non-empty", w)
		for _, n := range []uint64{lo, hiExcl - 1} {
			_, nBits := EncodeFibonacciCode(n)
			require.Equal(t, w, nBits, "value %d must have width %d", n, w)
		}
	}
}

// TestMaxRepresentableExclByWidth_Guards pins the domain edges: no panics for
// any int input, zero below the smallest code, saturation at and beyond 64.
func TestMaxRepresentableExclByWidth_Guards(t *testing.T) {
	for _, nBits := range []int{-100, -1, 0, 1} {
		require.Zero(t, MaxRepresentableExclByWidth(nBits), "nBits=%d", nBits)
	}
	require.Equal(t, uint64(1), MaxRepresentableExclByWidth(2))
	for _, nBits := range []int{64, 65, 200} {
		require.Equal(t, MaxRepresentableExcl, MaxRepresentableExclByWidth(nBits), "nBits=%d", nBits)
	}
	// Continuity at the saturation point: width 63 stays below width 64.
	require.Less(t, MaxRepresentableExclByWidth(63), MaxRepresentableExclByWidth(64))
}

// TestDecodeFibonacciCode_Invalid pins the explicit invalid result for inputs
// without a comma (previously these decoded to 2^64-1 via unsigned underflow).
func TestDecodeFibonacciCode_Invalid(t *testing.T) {
	for _, f := range []uint64{0, 0b101, 1 << 63, 0x5555555555555555, 0xAAAAAAAAAAAAAAAA} {
		n, ok := DecodeFibonacciCode(f)
		require.False(t, ok, "f=%#x", f)
		require.Zero(t, n, "f=%#x", f)
	}
}

// TestDecodeFibonacciCode_IgnoresBitsBelowComma pins the tagged-id use case:
// payload bits below the code must not change the decoded value.
func TestDecodeFibonacciCode_IgnoresBitsBelowComma(t *testing.T) {
	for _, n := range []uint64{0, 1, 2, 3, 12, 20, 33, 54, 12345, 4294967295} {
		code, nBits := EncodeFibonacciCode(n)
		bodyBits := 64 - nBits
		for _, body := range []uint64{0, 1, (uint64(1) << bodyBits) - 1} {
			dec, ok := DecodeFibonacciCode(code | body)
			require.True(t, ok)
			require.Equal(t, n, dec, "n=%d body=%#x", n, body)
		}
	}
}

// TestEncodeFibonacciCode_RangeBound pins the exclusive bound: the largest
// encodable value fills all 64 bits, the bound itself panics.
func TestEncodeFibonacciCode_RangeBound(t *testing.T) {
	f, nBits := EncodeFibonacciCode(MaxRepresentableExcl - 1)
	require.Equal(t, 64, nBits)
	dec, ok := DecodeFibonacciCode(f)
	require.True(t, ok)
	require.Equal(t, MaxRepresentableExcl-1, dec)

	require.Panics(t, func() { _, _ = EncodeFibonacciCode(MaxRepresentableExcl) })
}
