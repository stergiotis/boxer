package csr

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

func TestRadixSortMatchesSort(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		a := rapid.SliceOf(rapid.OneOf(rapid.Uint64(), rapid.Uint64Range(0, 1000), rapid.Uint64Range(1<<40, 1<<40+7))).Draw(t, "a")
		want := slices.Clone(a)
		slices.Sort(want)
		radixSortUint64(a, nil)
		require.Equal(t, want, a)
	})
}
