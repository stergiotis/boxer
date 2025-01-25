package co

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func cmpInt(i int, i2 int) int {
	if i == i2 {
		return 0
	}
	if i < i2 {
		return -1
	}
	return 1
}

func TestIterateSliceGrouped(t *testing.T) {
	for k, vs := range IterateSliceGrouped([]int{1}, []int{-1}, cmpInt) {
		require.Equal(t, 1, k)
		require.EqualValues(t, []int{-1}, vs)
	}

	{
		i := 1
		for k, vs := range IterateSliceGrouped([]int{1, 2}, []int{-1, -2}, cmpInt) {
			require.Equal(t, i, k)
			require.EqualValues(t, []int{-i}, vs)
			i++
		}
	}
	{
		i := 1
		for k, vs := range IterateSliceGrouped([]int{1, 2, 3}, []int{-1, -2, -3}, cmpInt) {
			require.Equal(t, i, k)
			require.EqualValues(t, []int{-i}, vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGrouped([]int{1, 1, 2, 2, 3, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, i+1, k)
			require.EqualValues(t, [][]int{{-1, -2}, {-3, -4}, {-5, -6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGrouped([]int{1, 1, 1, 1, 1, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 3}[i], k)
			require.EqualValues(t, [][]int{{-1, -2, -3, -4, -5}, {-6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGrouped([]int{1, 2, 2, 2, 2, 2}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 2}[i], k)
			require.EqualValues(t, [][]int{{-1}, {-2, -3, -4, -5, -6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGrouped([]int{1, 2, 2, 2, 2, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 2, 3}[i], k)
			require.EqualValues(t, [][]int{{-1}, {-2, -3, -4, -5}, {-6}}[i], vs)
			i++
		}
	}
}
