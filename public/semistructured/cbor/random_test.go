package cbor

import (
	"bytes"
	"testing"
	"unicode/utf8"

	cbor "github.com/fxamacker/cbor/v2"
	"github.com/stretchr/testify/require"
)

func TestGeneratorWellformed(t *testing.T) {
	for _, seed := range []int64{1, 7, 42, -3, 1 << 40} {
		buf := &bytes.Buffer{}
		gen := NewGenerator(buf, seed)
		n, err := gen.GenerateRandomCbor()
		require.NoError(t, err)
		require.Equal(t, n, buf.Len())
		require.NoError(t, cbor.Wellformed(buf.Bytes()), "seed %d", seed)
	}
}

func TestGeneratorDeterministic(t *testing.T) {
	run := func() ([]byte, uint64) {
		buf := &bytes.Buffer{}
		gen := NewGenerator(buf, 99)
		_, err := gen.GenerateRandomCbor()
		require.NoError(t, err)
		return buf.Bytes(), gen.Hasher.Sum64()
	}
	a, ha := run()
	b, hb := run()
	require.Equal(t, a, b)
	require.Equal(t, ha, hb)
}

func TestGeneratorStringLengthBound(t *testing.T) {
	for _, maxLen := range []int{0, 1, 23, 512, 4096} {
		gen := NewGenerator(&bytes.Buffer{}, 5)
		gen.SetMaxStringLength(maxLen)
		for range 2000 {
			s := gen.generateString()
			require.LessOrEqual(t, len(s), maxLen)
			require.True(t, utf8.ValidString(s), "invalid utf-8 at max %d", maxLen)
		}
	}
}

// TestGeneratorReachesWideHeads guards the length spread: strings have to grow
// past the 8-bit head class, otherwise only the two smallest encodeHead
// branches are ever taken.
func TestGeneratorReachesWideHeads(t *testing.T) {
	gen := NewGenerator(&bytes.Buffer{}, 11)
	longest := 0
	for range 2000 {
		longest = max(longest, len(gen.generateString()))
	}
	require.Greater(t, longest, 255)
}
