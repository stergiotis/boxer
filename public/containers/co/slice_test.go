package co

import (
	"slices"
	"testing"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stretchr/testify/require"
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
	for k, vs := range IterateSliceGroupedFunc([]int{1}, []int{-1}, cmpInt) {
		require.Equal(t, 1, k)
		require.EqualValues(t, []int{-1}, vs)
	}

	{
		i := 1
		for k, vs := range IterateSliceGroupedFunc([]int{1, 2}, []int{-1, -2}, cmpInt) {
			require.Equal(t, i, k)
			require.EqualValues(t, []int{-i}, vs)
			i++
		}
	}
	{
		i := 1
		for k, vs := range IterateSliceGroupedFunc([]int{1, 2, 3}, []int{-1, -2, -3}, cmpInt) {
			require.Equal(t, i, k)
			require.EqualValues(t, []int{-i}, vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGroupedFunc([]int{1, 1, 2, 2, 3, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, i+1, k)
			require.EqualValues(t, [][]int{{-1, -2}, {-3, -4}, {-5, -6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGroupedFunc([]int{1, 1, 1, 1, 1, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 3}[i], k)
			require.EqualValues(t, [][]int{{-1, -2, -3, -4, -5}, {-6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGroupedFunc([]int{1, 2, 2, 2, 2, 2}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 2}[i], k)
			require.EqualValues(t, [][]int{{-1}, {-2, -3, -4, -5, -6}}[i], vs)
			i++
		}
	}
	{
		i := 0
		for k, vs := range IterateSliceGroupedFunc([]int{1, 2, 2, 2, 2, 3}, []int{-1, -2, -3, -4, -5, -6}, cmpInt) {
			require.Equal(t, []int{1, 2, 3}[i], k)
			require.EqualValues(t, [][]int{{-1}, {-2, -3, -4, -5}, {-6}}[i], vs)
			i++
		}
	}
}

func TestCoSortSlices(t *testing.T) {
	a := []int{4, 1, 2, 3}
	b := []string{"4", "1", "2", "3"}
	CoSortSlices(a, func(i int, j int) {
		b[j], b[i] = b[i], b[j]
	})
	require.Equal(t, []int{1, 2, 3, 4}, a)
	require.Equal(t, []string{"1", "2", "3", "4"}, b)
	CoSortSlices(a[:0], func(i int, j int) {
		require.Fail(t, "should never get here")
	})
	CoSortSlices(a[:1], func(i int, j int) {
		b[j], b[i] = b[i], b[j]
	})
	require.Equal(t, []int{1}, a[:1])
	require.Equal(t, []string{"1"}, b[:1])
}

func TestCoSortSlices_NilSwap(t *testing.T) {
	a := []int{4, 1, 2, 3}
	CoSortSlices(a, nil)
	require.Equal(t, []int{1, 2, 3, 4}, a)
}

func TestCoSortSlicesReverse_NilSwap(t *testing.T) {
	// Regression (containers review 2026-07-05): the reverse variant
	// dereferenced the nil swap while the forward variant fell back to a
	// plain sort.
	a := []int{4, 1, 2, 3}
	CoSortSlicesReverse(a, nil)
	require.Equal(t, []int{4, 3, 2, 1}, a)

	CoSortSlicesReverse([]int{}, nil)
	CoSortSlicesReverse([]int{7}, nil)
}
func TestCoSortSlicesReverse(t *testing.T) {
	a := []int{4, 1, 2, 3}
	b := []string{"4", "1", "2", "3"}
	CoSortSlicesReverse(a, func(i int, j int) {
		b[j], b[i] = b[i], b[j]
	})
	require.Equal(t, []int{4, 3, 2, 1}, a)
	require.Equal(t, []string{"4", "3", "2", "1"}, b)

	CoSortSlicesReverse(a[:0], func(i int, j int) {
		require.Fail(t, "should never get here")
	})
	CoSortSlicesReverse(a[:1], func(i int, j int) {
		b[j], b[i] = b[i], b[j]
	})
	require.Equal(t, []int{4}, a[:1])
	require.Equal(t, []string{"4"}, b[:1])
}

func TestCoIterateFilter(t *testing.T) {
	a := []int{0, 1, 2, 3, 4, 5, 5}
	b := []string{"zero", "one", "two", "three", "four", "five", "five-too"}
	for i := range a[:5] {
		require.EqualValues(t, []string{b[i]}, slices.Collect(functional.IterRightOnly(CoIterateFilter(a, a[i], b))))
	}
	require.EqualValues(t, []string{"five", "five-too"}, slices.Collect(functional.IterRightOnly(CoIterateFilter(a, 5, b))))
}
func TestCoIterateFilterFunc(t *testing.T) {
	a := []int{0, 1, 2, 3, 4, 5, 5}
	b := []string{"zero", "one", "two", "three", "four", "five", "five-too"}
	require.EqualValues(t, []string{"zero", "two", "four"}, slices.Collect(functional.IterRightOnly(CoIterateFilterFunc(a, func(a int) (keep bool) {
		keep = a%2 == 0
		return
	}, b))))
}

// The int component is the SOURCE index into s1/s2, not a match ordinal
// (containers review 2026-07-05).
func TestCoIterateFilter_YieldsSourceIndex(t *testing.T) {
	keys := []int{7, 0, 7, 1, 7}
	vals := []string{"a", "b", "c", "d", "e"}
	var idxs []int
	var vs []string
	for i, v := range CoIterateFilter(keys, 7, vals) {
		idxs = append(idxs, i)
		vs = append(vs, v)
	}
	require.Equal(t, []int{0, 2, 4}, idxs)
	require.Equal(t, []string{"a", "c", "e"}, vs)
}

func TestCoIterateFilterFunc_YieldsSourceIndex(t *testing.T) {
	keys := []int{0, 1, 2, 3}
	vals := []string{"zero", "one", "two", "three"}
	var idxs []int
	for i := range CoIterateFilterFunc(keys, func(a int) (keep bool) { return a%2 == 1 }, vals) {
		idxs = append(idxs, i)
	}
	require.Equal(t, []int{1, 3}, idxs)
}
