package containers

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewBinarySearchGrowingKV(t *testing.T) {
	dict := NewBinarySearchGrowingKV[[]byte, string](128, bytes.Compare)
	require.Equal(t, 0, dict.Len())
	require.False(t, dict.Has([]byte("k1")))
	dict.UpsertSingle([]byte("k1"), "v1")
	require.True(t, dict.Has([]byte("k1")))
	v, has := dict.Get([]byte("k1"))
	require.True(t, has)
	require.Equal(t, "v1", v)
	v, has = dict.Get([]byte("k2"))
	require.False(t, has)
	require.Equal(t, "", v)
	v = dict.GetDefault([]byte("k2"), "notfound")
	require.False(t, has)
	require.Equal(t, "notfound", v)

	dict.UpsertSingle([]byte("k0"), "v0")
	require.True(t, dict.Has([]byte("k0")))
	require.True(t, dict.Has([]byte("k1")))
	require.Equal(t, "v0", dict.GetDefault([]byte("k0"), ""))
	require.Equal(t, "v1", dict.GetDefault([]byte("k1"), ""))

	dict.UpsertBatch([]byte("k2"), "v2a")
	dict.UpsertBatch([]byte("k4"), "v4")
	dict.UpsertBatch([]byte("k3"), "v3")
	dict.UpsertBatch([]byte("k2"), "v2b")

	require.Equal(t, "v0", dict.GetDefault([]byte("k0"), ""))
	require.Equal(t, "v1", dict.GetDefault([]byte("k1"), ""))
	require.Equal(t, "v2b", dict.GetDefault([]byte("k2"), ""))
	require.Equal(t, "v3", dict.GetDefault([]byte("k3"), ""))
	require.Equal(t, "v4", dict.GetDefault([]byte("k4"), ""))

	require.EqualValues(t, slices.Collect(dict.IterateKeys()), [][]byte{[]byte("k0"), []byte("k1"), []byte("k2"), []byte("k3"), []byte("k4")})
	require.EqualValues(t, slices.Collect(dict.IterateValues()), []string{"v0", "v1", "v2b", "v3", "v4"})
}
func TestIterateSortedUniqueOrderedUnique(t *testing.T) {
	rnd := rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	var l1, l2 int
	var s1, s2, s3 []uint32
	for i := 0; i < 100; i++ {
		l1 = rnd.IntN(7)
		l2 = rnd.IntN(7)
		s1 = slices.Grow(s1, l1)
		s2 = slices.Grow(s2, l2)
		s3 = slices.Grow(s3, l1+l2)
		for j := 0; j < l1; j++ {
			v := rnd.Uint32N(32)
			if !slices.Contains(s1, v) {
				s1 = append(s1, v)
				s3 = append(s3, v)
			}
		}
		for j := 0; j < l2; j++ {
			v := rnd.Uint32N(32)
			if !slices.Contains(s2, v) {
				s2 = append(s2, v)
				s3 = append(s3, v)
			}
		}
		slices.Sort(s1)
		slices.Sort(s2)
		slices.Sort(s3)
		s3 = slices.Compact(s3)
		if len(s3) == 0 {
			s3 = nil
		}
		require.EqualValues(t, s3, slices.Collect(IterateSortedUniqueOrderedUnique(s1, s2)))

		s1 = s1[:0]
		s2 = s2[:0]
		s3 = s3[:0]
	}
}
