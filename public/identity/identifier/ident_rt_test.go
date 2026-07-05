package identifier

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"testing"

	"github.com/stergiotis/boxer/public/identity/fibonaccicode"
	"github.com/stretchr/testify/require"
)

// TestGoldenLayout pins the ADR-0106 SD1 bit layout literally: the tag is the
// MSB-aligned fibonacci code of tagValue-1, the body sits in the low bits.
func TestGoldenLayout(t *testing.T) {
	for _, tc := range []struct {
		tv    TagValue
		tag   uint64
		width int
	}{
		{1, 0b11 << 62, 2},   // code "11"
		{2, 0b011 << 61, 3},  // code "011"
		{3, 0b0011 << 60, 4}, // code "0011"
		{4, 0b1011 << 60, 4}, // code "1011"
		{5, 0b00011 << 59, 5},
		{6, 0b10011 << 59, 5},
		{7, 0b01011 << 59, 5},
	} {
		tag := tc.tv.GetTag()
		require.Equal(t, tc.tag, tag.Value(), "tag value %d", tc.tv)
		require.Equal(t, tc.width, tag.GetTagWidth(), "tag value %d", tc.tv)
		require.True(t, tag.IsValid())

		id := tag.ComposeId(0x2A)
		require.Equal(t, tc.tag|0x2A, id.Value())
		gotTag, gotBody := id.Split()
		require.Equal(t, tag, gotTag)
		require.EqualValues(t, 0x2A, gotBody)
	}
	// The whole uint32 domain is encodable; its extremes need width 47.
	require.Equal(t, 47, TagValue(math.MaxUint32).GetTag().GetTagWidth())
	require.EqualValues(t, 1<<17-1, TagValue(math.MaxUint32).GetTag().GetMaxPossibleIdIncl())
}

// TestRoundtrip mirrors the old fixed-width randomized round-trip and adds
// the fibonaccicode oracle: the tag width derived by the split must equal the
// encoder's reported width, and tag bits must lead with exactly width many
// non-body bits.
func TestRoundtrip(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	n := 100000
	if testing.Short() {
		n = 1000
	}
	for i := 0; i < n; i++ {
		tv := TagValue(1 + rnd.Uint64N(uint64(MaxTagValue)))
		require.True(t, tv.IsValid())
		tg := tv.GetTag()
		require.True(t, tg.IsValid())
		require.Equal(t, tv, tg.GetValue())
		require.EqualValues(t, uint64(tg), tg.Value())

		_, wantWidth := fibonaccicode.EncodeFibonacciCode(uint64(tv) - 1)
		require.Equal(t, wantWidth, tg.GetTagWidth())

		maxId := tg.GetMaxPossibleIdIncl()
		require.Equal(t, 64, bits.OnesCount64(uint64(maxId))+tg.GetTagWidth())
		require.Equal(t, tg.GetTagWidth(), bits.LeadingZeros64(uint64(maxId)))

		u := UntaggedId(rnd.Uint64N(uint64(maxId) + 1))
		id := tg.ComposeId(u)
		require.EqualValues(t, u.AddTag(tg), id)
		require.Equal(t, tg, id.GetTag())
		require.Equal(t, tg.GetTagWidth(), id.GetTagWidth())
		require.Equal(t, u, id.RemoveTag())
		require.True(t, tg.SameTag(id))
		tg2, u2 := id.Split()
		require.Equal(t, tg, tg2)
		require.Equal(t, u, u2)
		require.True(t, id.IsValid())
		require.Equal(t, tv, id.GetTag().GetValue())
	}
}

// TestPrefixFreedomNoCrossTagCollision pins the SD1 uniqueness argument: two
// distinct tags can never compose the same id, whatever the bodies.
func TestPrefixFreedomNoCrossTagCollision(t *testing.T) {
	rnd := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < 20000; i++ {
		tvA := TagValue(1 + rnd.Uint64N(1<<20))
		tvB := TagValue(1 + rnd.Uint64N(1<<20))
		if tvA == tvB {
			continue
		}
		tagA, tagB := tvA.GetTag(), tvB.GetTag()
		idA := tagA.ComposeId(UntaggedId(rnd.Uint64N(uint64(tagA.GetMaxPossibleIdIncl()) + 1)))
		idB := tagB.ComposeId(UntaggedId(rnd.Uint64N(uint64(tagB.GetMaxPossibleIdIncl()) + 1)))
		require.NotEqual(t, idA, idB, "tags %d and %d", tvA, tvB)
		require.False(t, tagA.SameTag(idB))
		require.False(t, tagB.SameTag(idA))
	}
}

// TestCompressionFriendlyBitLayout: consecutive bodies under one tag are
// consecutive uint64s (the property the column compression relies on).
func TestCompressionFriendlyBitLayout(t *testing.T) {
	tag := TagValue(1 + rand.Uint32N(math.MaxUint32)).GetTag()
	id1 := tag.ComposeId(1)
	id2 := tag.ComposeId(2)
	require.Equal(t, uint64(1), uint64(id2-id1))
}

// TestAddTagGuards pins the always-on compose guard (ADR-0106 SD1): an
// oversized body or an invalid tag panics instead of silently corrupting the
// tag. The former ExtraChecks-only overlap test passed bodies whose bits
// cleared the tag's set bits; the mask guard must not.
func TestAddTagGuards(t *testing.T) {
	tag := TagValue(1).GetTag() // width 2: body must stay below bit 62
	require.Panics(t, func() { _ = UntaggedId(1 << 62).AddTag(tag) })
	// The historical false-negative: body bit inside the tag region but not
	// overlapping a set tag bit (tag "11" has both top bits set, so use a
	// wider tag with a zero bit: tag value 3 = "0011").
	tag3 := TagValue(3).GetTag()
	require.Panics(t, func() { _ = UntaggedId(1 << 63).AddTag(tag3) })
	require.Panics(t, func() { _ = UntaggedId(1).AddTag(IdTag(0)) })
	// A tag with stray bits below its comma is invalid.
	require.Panics(t, func() { _ = UntaggedId(1).AddTag(IdTag(0b11<<62 | 1)) })
	// In-range bodies compose fine, including the reserved 0.
	require.NotPanics(t, func() { _ = UntaggedId(0).AddTag(tag) })
	require.NotPanics(t, func() { _ = tag.ComposeId(tag.GetMaxPossibleIdIncl()) })
}

// TestInvalidInputsAreTotal: invalid values flow through as detectable
// invalids, not garbage.
func TestInvalidInputsAreTotal(t *testing.T) {
	require.False(t, TagValue(0).IsValid())
	require.EqualValues(t, 0, TagValue(0).GetTag())
	require.False(t, IdTag(0).IsValid())
	require.EqualValues(t, 0, IdTag(0).GetValue())
	require.EqualValues(t, 0, IdTag(0).GetMaxPossibleIdIncl())

	// No comma anywhere: structurally not a tagged id.
	for _, raw := range []uint64{0, 0b101, 0x5555555555555555, 1 << 63} {
		id := TaggedId(raw)
		require.False(t, id.IsValid(), "raw=%#x", raw)
		tg, u := id.Split()
		require.EqualValues(t, 0, tg, "raw=%#x", raw)
		require.EqualValues(t, 0, u, "raw=%#x", raw)
		require.Zero(t, id.GetTagWidth())
	}

	// A raw width-64 "tag" decodes beyond the uint32 tag-value domain and
	// must come back as the invalid TagValue, not a truncated one.
	huge := IdTag(1<<63 | 0b11)
	require.Equal(t, 64, huge.GetTagWidth())
	require.EqualValues(t, 0, huge.GetValue())
}
