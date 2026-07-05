package fibonacci

import (
	"math"
	"math/bits"
	"testing"

	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stergiotis/boxer/public/identity/identifier"
	"github.com/stretchr/testify/require"
)

// bruteForceTagValuesOfWidth enumerates tag values whose fibonacci code
// (of tagValue-1) has exactly the given full width, by encoding every
// candidate — a structural oracle independent of the Stats bounds.
func bruteForceTagValuesOfWidth(t *testing.T, width int, scanUpToExcl uint64) (vals []identifier.TagValue) {
	t.Helper()
	for i := uint64(1); i < scanUpToExcl; i++ {
		_, nBits := fibonaccicode.EncodeFibonacciCode(i - 1)
		if nBits == width {
			vals = append(vals, identifier.TagValue(i))
		}
	}
	return
}

// TestIterateTagValues_MatchesBruteForce is the regression test for the
// class-bounds off-by-one (ADR-0106 Context): the iterator used to warn on
// the first member of every width class and silently drop the last one.
func TestIterateTagValues_MatchesBruteForce(t *testing.T) {
	scanUpToExcl := fibonaccicode.MaxRepresentableExclByWidth(15) + 1
	for width := 2; width <= 14; width++ {
		var got []identifier.TagValue
		for tv := range IterateTagValuesWithGivenMinNumberOfLeadingZeros(uint8(width), 0) {
			got = append(got, tv)
		}
		want := bruteForceTagValuesOfWidth(t, width, scanUpToExcl)
		require.Equal(t, want, got, "width %d", width)
	}
}

func TestIterateTagValues_InvalidWidthsYieldNothing(t *testing.T) {
	for _, w := range []uint8{0, 1, Uint32TagValueTagWidth + 1, 200, 255} {
		count := 0
		for range IterateTagValuesWithGivenMinNumberOfLeadingZeros(w, 0) {
			count++
		}
		require.Zero(t, count, "width %d must yield nothing", w)
	}
}

func TestIterateTagValues_LeadingZerosFilter(t *testing.T) {
	const width = 8
	var all, filtered []identifier.TagValue
	for tv, lz := range IterateTagValuesWithGivenMinNumberOfLeadingZeros(width, 0) {
		code, _ := fibonaccicode.EncodeFibonacciCode(uint64(tv) - 1)
		require.EqualValues(t, bits.LeadingZeros64(code), lz)
		all = append(all, tv)
	}
	const minLZ = 2
	for tv, lz := range IterateTagValuesWithGivenMinNumberOfLeadingZeros(width, minLZ) {
		require.GreaterOrEqual(t, lz, uint8(minLZ))
		filtered = append(filtered, tv)
	}
	require.NotEmpty(t, all)
	require.Less(t, len(filtered), len(all), "filter must exclude codes with a leading one")
	require.Subset(t, all, filtered)
}

// TestSelectFittingTagValueRange_Properties pins the advisor contract: the
// returned class is exactly the widest tag width whose body still holds
// maxExpectedIds, clamped so every advised TagValue fits uint32. Regressions
// covered: maxExpectedIds==0 panicked (negative shift), and small
// maxExpectedIds silently truncated tag values through the uint32 conversion.
func TestSelectFittingTagValueRange_Properties(t *testing.T) {
	for _, maxIds := range []uint64{0, 1, 2, 100, 1 << 10, 1<<17 - 1, 1 << 17, 1 << 32, 1<<60 - 1} {
		lo, hiExcl, err := SelectFittingTagValueRange(maxIds)
		require.NoError(t, err, "maxIds=%d", maxIds)
		require.Less(t, uint64(lo), uint64(hiExcl), "class must be non-empty, maxIds=%d", maxIds)

		wantWidth := min(bits.LeadingZeros64(maxIds), maxAdvisedTagWidth)
		for _, tv := range []identifier.TagValue{lo, hiExcl - 1} {
			code, nBits := fibonaccicode.EncodeFibonacciCode(uint64(tv) - 1)
			require.Equal(t, wantWidth, nBits, "maxIds=%d tv=%d", maxIds, tv)
			bodyCapacityExcl := uint64(1) << (64 - nBits)
			require.Greater(t, bodyCapacityExcl, maxIds, "body must hold maxExpectedIds")
			_ = code
		}
		// The bound values did not wrap through the uint32 conversion.
		require.LessOrEqual(t, uint64(hiExcl), uint64(math.MaxUint32)+1)

		// Adjacent classes tile: the value below lo has a narrower code.
		if lo > 1 {
			_, nBitsBelow := fibonaccicode.EncodeFibonacciCode(uint64(lo) - 2)
			require.Equal(t, wantWidth-1, nBitsBelow, "maxIds=%d", maxIds)
		}
	}

	for _, tooLarge := range []uint64{1 << 60, math.MaxUint64} {
		_, _, err := SelectFittingTagValueRange(tooLarge)
		require.Error(t, err, "maxIds=%d", tooLarge)
	}
}

// TestStatsBoundsAgreeWithAdvisor guards against the two bound formulas
// drifting apart again (they disagreed by one before ADR-0106 SD9).
func TestStatsBoundsAgreeWithAdvisor(t *testing.T) {
	for width := 4; width <= Uint32TagValueTagWidth; width++ {
		maxIds := uint64(1)<<(64-width) - 1 // exactly `width` leading zeros
		lo, hiExcl, err := SelectFittingTagValueRange(maxIds)
		require.NoError(t, err)
		require.Equal(t, Stats.TagValueMinIncl[width], lo, "width %d", width)
		require.Equal(t, Stats.TagValueMaxIncl[width], hiExcl-1, "width %d", width)
	}
}
