package aspectcodec

import (
	"math/bits"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWorkedExamples(t *testing.T) {
	cases := []struct {
		indices []uint8
		seg     string
	}{
		{[]uint8{}, ""},
		{[]uint8{0}, "1"},
		{[]uint8{1, 9, 55}, "2Au"},
		{[]uint8{59}, "y"},
		{[]uint8{60}, "z1"},
		{[]uint8{119}, "zy"},
		{[]uint8{120}, "zz1"},
		{[]uint8{9, 10}, "AB"},
	}
	for _, c := range cases {
		require.Equal(t, c.seg, Encode(c.indices), "indices %v", c.indices)
		known, unknown, err := Decode(c.seg, uint8(255))
		require.NoError(t, err, c.seg)
		require.Zero(t, unknown, c.seg)
		expected := slices.Clone(c.indices)
		slices.Sort(expected)
		if len(expected) == 0 {
			require.Empty(t, known, c.seg)
		} else {
			require.Equal(t, expected, known, c.seg)
		}
	}
}

func TestEncodeIsOrderAndDuplicateInsensitive(t *testing.T) {
	require.Equal(t, "2Au", Encode([]uint8{55, 1, 9, 1, 55}))
}

func TestLegacyEmptyMarker(t *testing.T) {
	require.True(t, IsEmpty(""))
	require.True(t, IsEmpty(legacyEmptySegment))
	require.False(t, IsEmpty("1"))

	known, unknown, err := Decode(legacyEmptySegment, uint8(60))
	require.NoError(t, err)
	require.Empty(t, known)
	require.Zero(t, unknown)

	n, err := Count(legacyEmptySegment)
	require.NoError(t, err)
	require.Zero(t, n)

	// "0" is accepted alone, never inside a segment, and never produced
	require.NoError(t, Validate(legacyEmptySegment))
	require.Error(t, Validate("10"))
	require.Error(t, Validate("01"))
	require.NotContains(t, Encode([]uint8{0, 1, 2}), "0")
}

func TestStructuralRejections(t *testing.T) {
	bad := []string{
		"A2",  // descending
		"22",  // duplicate
		"2A0", // reserved '0' inside
		"00",  // reserved '0' inside
		"z",   // dangling escape
		"2z",  // trailing escape
		"!",   // outside the alphabet
		" 2",  // outside the alphabet
		"z0",  // reserved '0' as escaped digit
	}
	for _, seg := range bad {
		require.Error(t, Validate(seg), "segment %q", seg)
		_, _, err := Decode(seg, uint8(255))
		require.Error(t, err, "segment %q", seg)
		require.False(t, Contains(seg, uint8(1)), "segment %q", seg)
	}
	// escaped duplicate of an unescaped index: 60 then 60 again
	require.Error(t, Validate("z1z1"))
}

func TestRoundTripAgainstBitmaskModel(t *testing.T) {
	seed1, seed2 := rand.Uint64(), rand.Uint64()
	t.Logf("randomized test seed: %d %d (rand.NewPCG)", seed1, seed2)
	rnd := rand.New(rand.NewPCG(seed1, seed2))
	for range 2000 {
		mask := rnd.Uint64()
		indices := make([]uint8, 0, 64)
		for i := range 64 {
			if mask&(uint64(1)<<i) != 0 {
				indices = append(indices, uint8(i))
			}
		}
		seg := Encode(indices)

		// exact length: one char per index below 60, two for 60..63
		expectedLen := 0
		for _, i := range indices {
			if i < indicesPerLevel {
				expectedLen++
			} else {
				expectedLen += 2
			}
		}
		require.Len(t, seg, expectedLen)

		require.NoError(t, Validate(seg))
		known, unknown, err := Decode(seg, uint8(64))
		require.NoError(t, err)
		require.Zero(t, unknown)
		if len(indices) == 0 {
			require.Empty(t, known)
		} else {
			require.Equal(t, indices, known)
		}

		n, err := Count(seg)
		require.NoError(t, err)
		require.Equal(t, bits.OnesCount64(mask), n)

		if mask != 0 {
			max, err := MaxIndex(seg)
			require.NoError(t, err)
			require.Equal(t, 63-bits.LeadingZeros64(mask), max)
		} else {
			_, err := MaxIndex(seg)
			require.ErrorIs(t, err, ErrEmptySet)
		}

		probe := uint8(rnd.IntN(64))
		require.Equal(t, mask&(uint64(1)<<probe) != 0, Contains(seg, probe))
	}
}

func TestUnionAgainstBitmaskModel(t *testing.T) {
	seed1, seed2 := rand.Uint64(), rand.Uint64()
	t.Logf("randomized test seed: %d %d (rand.NewPCG)", seed1, seed2)
	rnd := rand.New(rand.NewPCG(seed1, seed2))
	fromMask := func(mask uint64) string {
		indices := make([]uint8, 0, 64)
		for i := range 64 {
			if mask&(uint64(1)<<i) != 0 {
				indices = append(indices, uint8(i))
			}
		}
		return Encode(indices)
	}
	for range 2000 {
		a, b := rnd.Uint64(), rnd.Uint64()
		u, err := Union(fromMask(a), fromMask(b))
		require.NoError(t, err)
		require.Equal(t, fromMask(a|b), u)
	}
	_, err := Union("A2", "")
	require.Error(t, err)
}

func TestEscapeRangeRoundTrip(t *testing.T) {
	seed1, seed2 := rand.Uint64(), rand.Uint64()
	t.Logf("randomized test seed: %d %d (rand.NewPCG)", seed1, seed2)
	rnd := rand.New(rand.NewPCG(seed1, seed2))
	for range 500 {
		model := make(map[uint8]bool)
		indices := make([]uint8, 0, 8)
		for range 8 {
			i := uint8(rnd.IntN(256))
			if !model[i] {
				model[i] = true
				indices = append(indices, i)
			}
		}
		seg := Encode(indices)
		require.NoError(t, Validate(seg))
		known, unknown, err := Decode(seg, uint8(255))
		require.NoError(t, err)
		slices.Sort(indices)
		if model[255] {
			// 255 is the only index outside maxExcl=255
			require.Equal(t, 1, unknown)
			require.Equal(t, indices[:len(indices)-1], known)
		} else {
			require.Zero(t, unknown)
			require.Equal(t, indices, known)
		}
		for _, i := range indices {
			require.True(t, Contains(seg, i))
		}
	}
}

func TestDecodeSplitsUnknownIndices(t *testing.T) {
	// vocabulary of size 24 reading a segment carrying {1, 30, 61}
	seg := Encode([]uint8{1, 30, 61})
	known, unknown, err := Decode(seg, uint8(24))
	require.NoError(t, err)
	require.Equal(t, []uint8{1}, known)
	require.Equal(t, 2, unknown)
}
