package recordtable

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sortRec builds a record with a nullable string column and an int column, so
// one fixture covers the text, numeric and null orderings.
func sortRec(t *testing.T, names []string, nulls []bool, nums []int64) arrow.RecordBatch {
	t.Helper()
	mem := memory.NewGoAllocator()
	schema := arrow.NewSchema([]arrow.Field{
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "n", Type: arrow.PrimitiveTypes.Int64},
	}, nil)
	sb := array.NewStringBuilder(mem)
	nb := array.NewInt64Builder(mem)
	for i := range names {
		if nulls[i] {
			sb.AppendNull()
		} else {
			sb.Append(names[i])
		}
		nb.Append(nums[i])
	}
	sa, na := sb.NewArray(), nb.NewArray()
	rec := array.NewRecordBatch(schema, []arrow.Array{sa, na}, int64(len(names)))
	sb.Release()
	nb.Release()
	sa.Release()
	na.Release()
	return rec
}

// A fresh SortState draws rows in record order: the zero value must be
// "unsorted", not "sorted by column 0".
func TestSortZeroValueIsUnsorted(t *testing.T) {
	rec := sortRec(t, []string{"b", "a"}, []bool{false, false}, []int64{1, 2})
	defer rec.Release()
	var s SortState
	assert.Nil(t, s.OrderFor(rec, rec.NumRows()))
	assert.Equal(t, int64(3), s.RowAt(nil, 3), "identity without a permutation")
	assert.Equal(t, "", s.Glyph(0))
}

// Clicking cycles ascending → descending → unsorted; clicking a different
// column starts that column ascending.
func TestSortClickCycle(t *testing.T) {
	var s SortState
	s.Clicked(2)
	assert.True(t, s.Active)
	assert.Equal(t, 2, s.Col)
	assert.False(t, s.Desc)
	assert.Equal(t, " ▲", s.Glyph(2))

	s.Clicked(2)
	assert.True(t, s.Desc)
	assert.Equal(t, " ▼", s.Glyph(2))

	s.Clicked(2)
	assert.False(t, s.Active, "the third click restores the result's own order")
	assert.Equal(t, "", s.Glyph(2))

	s.Clicked(2)
	s.Clicked(5)
	assert.Equal(t, 5, s.Col)
	assert.False(t, s.Desc, "a new column starts ascending")
	assert.Equal(t, "", s.Glyph(2), "only the sorted column is marked")
}

func TestSortOrdersTextAndNumbers(t *testing.T) {
	rec := sortRec(t,
		[]string{"pear", "apple", "fig"}, []bool{false, false, false},
		[]int64{30, 10, 20})
	defer rec.Release()

	var s SortState
	s.Clicked(0)
	assert.Equal(t, []int64{1, 2, 0}, s.OrderFor(rec, 3), "apple, fig, pear")

	s.Clicked(0)
	assert.Equal(t, []int64{0, 2, 1}, s.OrderFor(rec, 3), "reversed")

	// Numeric columns compare numerically, not as rendered text.
	s.Clicked(1)
	assert.Equal(t, []int64{1, 2, 0}, s.OrderFor(rec, 3), "10, 20, 30")
}

// Numbers must not be compared as strings: "9" > "10" lexicographically.
func TestSortNumericNotLexicographic(t *testing.T) {
	rec := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{9, 10})
	defer rec.Release()
	var s SortState
	s.Clicked(1)
	assert.Equal(t, []int64{0, 1}, s.OrderFor(rec, 2))
}

// Ties keep the order the query produced — the sort refines the result's own
// ordering rather than replacing it.
func TestSortIsStable(t *testing.T) {
	rec := sortRec(t,
		[]string{"same", "same", "same"}, []bool{false, false, false},
		[]int64{7, 8, 9})
	defer rec.Release()
	var s SortState
	s.Clicked(0)
	assert.Equal(t, []int64{0, 1, 2}, s.OrderFor(rec, 3))
}

// Nulls sort last ascending; descending is the exact reverse, so re-reading
// the list top-to-bottom shows what the previous click showed, backwards.
func TestSortNullsLastThenReversed(t *testing.T) {
	rec := sortRec(t,
		[]string{"b", "", "a"}, []bool{false, true, false},
		[]int64{1, 2, 3})
	defer rec.Release()

	var s SortState
	s.Clicked(0)
	assert.Equal(t, []int64{2, 0, 1}, s.OrderFor(rec, 3), "a, b, null")
	s.Clicked(0)
	assert.Equal(t, []int64{1, 0, 2}, s.OrderFor(rec, 3), "null, b, a")
}

// The selection is a record row; under a sort it must be highlighted where
// that row is drawn, not on the line with the same index.
func TestSortSelectionFollowsTheRow(t *testing.T) {
	rec := sortRec(t,
		[]string{"pear", "apple", "fig"}, []bool{false, false, false},
		[]int64{30, 10, 20})
	defer rec.Release()

	var s SortState
	s.Clicked(0)
	order := s.OrderFor(rec, 3)
	require.NotNil(t, order)

	// Record row 0 ("pear") is drawn last after sorting by name.
	assert.Equal(t, int64(2), s.DisplayPos(order, 0))
	assert.Equal(t, int64(0), s.RowAt(order, 2))
	// Every position round-trips.
	for pos := range int64(3) {
		assert.Equal(t, pos, s.DisplayPos(order, s.RowAt(order, pos)))
	}
}

// A new result resets the permutation without discarding the chosen column.
func TestSortCacheKeyedOnRecord(t *testing.T) {
	first := sortRec(t, []string{"b", "a"}, []bool{false, false}, []int64{1, 2})
	defer first.Release()
	second := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{1, 2})
	defer second.Release()

	var s SortState
	s.Clicked(0)
	assert.Equal(t, []int64{1, 0}, s.OrderFor(first, 2))
	assert.Equal(t, []int64{0, 1}, s.OrderFor(second, 2), "recomputed for the new record")
	assert.True(t, s.Active, "the chosen column survives a new result")
}

// A descending-first column starts at its interesting end; the cycle then
// runs through the other direction back to unsorted.
func TestSortDescFirst(t *testing.T) {
	var s SortState
	s.ClickedFrom(2, true)
	assert.True(t, s.Active)
	assert.True(t, s.Desc, "the first click sorts descending")
	s.ClickedFrom(2, true)
	assert.False(t, s.Desc)
	s.ClickedFrom(2, true)
	assert.False(t, s.Active, "the third click restores record order")
}

// NoUnsorted flips between the two directions and never leaves the column.
func TestSortNoUnsorted(t *testing.T) {
	s := SortState{NoUnsorted: true}
	s.ClickedFrom(1, true)
	for range 3 {
		was := s.Desc
		s.ClickedFrom(1, true)
		assert.True(t, s.Active)
		assert.Equal(t, !was, s.Desc)
	}
	s.Clicked(4)
	assert.Equal(t, 4, s.Col)
	assert.False(t, s.Desc, "a new column starts in its own first direction")
}

// Apply takes the clicked column's first direction from the grid's result,
// and does nothing on a frame without a click.
func TestSortApply(t *testing.T) {
	var s SortState
	s.Apply(Result{SortClicked: -1})
	assert.False(t, s.Active)
	s.Apply(Result{SortClicked: 3, sortDescFirst: true})
	assert.Equal(t, 3, s.Col)
	assert.True(t, s.Desc)
}
