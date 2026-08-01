package play

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

// A fresh PlayApp draws rows in result order: the zero value must be
// "unsorted", not "sorted by column 0".
func TestTableSortZeroValueIsUnsorted(t *testing.T) {
	rec := sortRec(t, []string{"b", "a"}, []bool{false, false}, []int64{1, 2})
	defer rec.Release()
	var s tableSortState
	assert.Nil(t, s.orderFor(rec, rec.NumRows()))
	assert.Equal(t, int64(3), s.rowAt(nil, 3), "identity without a permutation")
	assert.Equal(t, "", s.glyph(0))
}

// Clicking cycles ascending → descending → unsorted; clicking a different
// column starts that column ascending.
func TestTableSortClickCycle(t *testing.T) {
	var s tableSortState
	s.clicked(2)
	assert.True(t, s.active)
	assert.Equal(t, 2, s.col)
	assert.False(t, s.desc)
	assert.Equal(t, " ▲", s.glyph(2))

	s.clicked(2)
	assert.True(t, s.desc)
	assert.Equal(t, " ▼", s.glyph(2))

	s.clicked(2)
	assert.False(t, s.active, "the third click restores the result's own order")
	assert.Equal(t, "", s.glyph(2))

	s.clicked(2)
	s.clicked(5)
	assert.Equal(t, 5, s.col)
	assert.False(t, s.desc, "a new column starts ascending")
	assert.Equal(t, "", s.glyph(2), "only the sorted column is marked")
}

func TestTableSortOrdersTextAndNumbers(t *testing.T) {
	rec := sortRec(t,
		[]string{"pear", "apple", "fig"}, []bool{false, false, false},
		[]int64{30, 10, 20})
	defer rec.Release()

	var s tableSortState
	s.clicked(0)
	assert.Equal(t, []int64{1, 2, 0}, s.orderFor(rec, 3), "apple, fig, pear")

	s.clicked(0)
	assert.Equal(t, []int64{0, 2, 1}, s.orderFor(rec, 3), "reversed")

	// Numeric columns compare numerically, not as rendered text.
	s.clicked(1)
	assert.Equal(t, []int64{1, 2, 0}, s.orderFor(rec, 3), "10, 20, 30")
}

// Numbers must not be compared as strings: "9" > "10" lexicographically.
func TestTableSortNumericNotLexicographic(t *testing.T) {
	rec := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{9, 10})
	defer rec.Release()
	var s tableSortState
	s.clicked(1)
	assert.Equal(t, []int64{0, 1}, s.orderFor(rec, 2))
}

// Ties keep the order the query produced — the sort refines the result's own
// ordering rather than replacing it.
func TestTableSortIsStable(t *testing.T) {
	rec := sortRec(t,
		[]string{"same", "same", "same"}, []bool{false, false, false},
		[]int64{7, 8, 9})
	defer rec.Release()
	var s tableSortState
	s.clicked(0)
	assert.Equal(t, []int64{0, 1, 2}, s.orderFor(rec, 3))
}

// Nulls sort last ascending; descending is the exact reverse, so re-reading
// the list top-to-bottom shows what the previous click showed, backwards.
func TestTableSortNullsLastThenReversed(t *testing.T) {
	rec := sortRec(t,
		[]string{"b", "", "a"}, []bool{false, true, false},
		[]int64{1, 2, 3})
	defer rec.Release()

	var s tableSortState
	s.clicked(0)
	assert.Equal(t, []int64{2, 0, 1}, s.orderFor(rec, 3), "a, b, null")
	s.clicked(0)
	assert.Equal(t, []int64{1, 0, 2}, s.orderFor(rec, 3), "null, b, a")
}

// The selection is a record row; under a sort it must be highlighted where
// that row is drawn, not on the line with the same index.
func TestTableSortSelectionFollowsTheRow(t *testing.T) {
	rec := sortRec(t,
		[]string{"pear", "apple", "fig"}, []bool{false, false, false},
		[]int64{30, 10, 20})
	defer rec.Release()

	var s tableSortState
	s.clicked(0)
	order := s.orderFor(rec, 3)
	require.NotNil(t, order)

	// Record row 0 ("pear") is drawn last after sorting by name.
	assert.Equal(t, int64(2), s.displayPos(order, 0))
	assert.Equal(t, int64(0), s.rowAt(order, 2))
	// Every position round-trips.
	for pos := range int64(3) {
		assert.Equal(t, pos, s.displayPos(order, s.rowAt(order, pos)))
	}
}

// A new result resets the permutation without discarding the chosen column.
func TestTableSortCacheKeyedOnRecord(t *testing.T) {
	first := sortRec(t, []string{"b", "a"}, []bool{false, false}, []int64{1, 2})
	defer first.Release()
	second := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{1, 2})
	defer second.Release()

	var s tableSortState
	s.clicked(0)
	assert.Equal(t, []int64{1, 0}, s.orderFor(first, 2))
	assert.Equal(t, []int64{0, 1}, s.orderFor(second, 2), "recomputed for the new record")
	assert.True(t, s.active, "the chosen column survives a new result")
}
