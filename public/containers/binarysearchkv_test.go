package containers

import (
	"bytes"
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
