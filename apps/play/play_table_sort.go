package play

import (
	"sort"
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// play_table_sort.go sorts the Table tab by a clicked column header.
//
// The sort is a **view permutation over the record play already holds**, not a
// re-issued query: it costs no round trip, it works on a lane whose SQL cannot
// be re-run, and it leaves the result itself — and therefore every other
// panel's idea of what row 7 is — untouched. Row identity stays the record's
// own index, so `selection` (a cursor into the record) and the Detail pane
// keep agreeing with the grid after a sort; only the drawing order moves.
//
// Clicking cycles ascending → descending → unsorted. The third state matters
// for a SQL surface: a result usually arrives in a deliberate ORDER BY, and
// without a way back the user cannot recover it short of re-running.

// tableSortIDSalt namespaces the header buttons' widget ids. Cell ids are
// built as cellIdBase + absRow*cellColStride + col, which stays below 2^48 for
// any result play can hold, so a salt in the top bit cannot collide with them.
const tableSortIDSalt uint64 = 1 << 62

// tableSortState is the Table pane's sort: which Arrow column, which
// direction, and the cached permutation for the record it was computed over.
// The cache is keyed on the record pointer, so a new result resets the order
// without resetting the user's chosen column.
//
// The zero value is "unsorted" — hence `active` rather than a sentinel column
// index, so a PlayApp that never initialises the field draws its rows in
// result order.
type tableSortState struct {
	active bool
	col    int // Arrow column index, meaningful only while active
	desc   bool

	forRec    arrow.RecordBatch
	forActive bool
	forCol    int
	forDesc   bool
	// order maps display position → record row; inv is its inverse, so the
	// selected record row can be highlighted at its displayed position.
	order []int64
	inv   []int64
}

// clicked advances the three-state cycle for col: a new column starts
// ascending, the sorted column goes descending, then back to unsorted.
func (inst *tableSortState) clicked(col int) {
	switch {
	case !inst.active || inst.col != col:
		inst.active, inst.col, inst.desc = true, col, false
	case !inst.desc:
		inst.desc = true
	default:
		inst.active, inst.desc = false, false
	}
}

// glyph is the header suffix marking the sorted column.
func (inst *tableSortState) glyph(col int) (s string) {
	if !inst.active || inst.col != col {
		return
	}
	if inst.desc {
		return " ▼"
	}
	return " ▲"
}

// orderFor returns the display→record permutation for the first n rows of rec,
// or nil when the rows are drawn in record order (no sort, or a column the
// record does not have). Recomputed only when the record or the sort changes.
func (inst *tableSortState) orderFor(rec arrow.RecordBatch, n int64) (order []int64) {
	if rec == nil || !inst.active || inst.col < 0 || inst.col >= int(rec.NumCols()) || n <= 0 {
		return nil
	}
	if inst.forRec == rec && inst.forActive && inst.forCol == inst.col &&
		inst.forDesc == inst.desc && int64(len(inst.order)) == n {
		return inst.order
	}
	order = make([]int64, n)
	for i := range order {
		order[i] = int64(i)
	}
	col := rec.Column(inst.col)
	// Stable, so rows that tie keep the order the query gave them — the
	// sort refines the result's order rather than replacing it.
	sort.SliceStable(order, func(a, b int) bool {
		cmp := compareCells(col, order[a], order[b])
		if inst.desc {
			return cmp > 0
		}
		return cmp < 0
	})
	inv := make([]int64, n)
	for pos, row := range order {
		inv[row] = int64(pos)
	}
	inst.forRec, inst.forActive, inst.forCol, inst.forDesc = rec, true, inst.col, inst.desc
	inst.order, inst.inv = order, inv
	return order
}

// rowAt maps a display position to the record row drawn there.
func (inst *tableSortState) rowAt(order []int64, pos int64) (row int64) {
	if order == nil || pos < 0 || pos >= int64(len(order)) {
		return pos
	}
	return order[pos]
}

// displayPos maps a record row to where it is drawn, so a selection made
// before a sort stays highlighted on the right line after it.
func (inst *tableSortState) displayPos(order []int64, row int64) (pos int64) {
	if order == nil || row < 0 || row >= int64(len(inst.inv)) {
		return row
	}
	return inst.inv[row]
}

// compareCells orders two rows of one Arrow column: -1, 0 or +1.
//
// Nulls sort last in ascending order (and, since descending negates the
// comparison, first in descending) — the simple rule, chosen over ClickHouse's
// NULLS LAST default matching in both directions because this is a view
// permutation, not a query, and a user re-reading the same list top-to-bottom
// should see the reverse of what they just saw.
//
// Numeric columns compare numerically and strings lexicographically; anything
// else falls back to its rendered text, which is what the cell shows.
func compareCells(col arrow.Array, a, b int64) (cmp int) {
	ia, ib := int(a), int(b)
	an, bn := col.IsNull(ia), col.IsNull(ib)
	switch {
	case an && bn:
		return 0
	case an:
		return 1
	case bn:
		return -1
	}
	switch v := col.(type) {
	case *array.String:
		return strings.Compare(v.Value(ia), v.Value(ib))
	case *array.LargeString:
		return strings.Compare(v.Value(ia), v.Value(ib))
	case *array.Boolean:
		return compareOrdered(boolOrd(v.Value(ia)), boolOrd(v.Value(ib)))
	case *array.Int8:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Int16:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Int32:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Int64:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Uint8:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Uint16:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Uint32:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Uint64:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Float32:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Float64:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Date32:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Timestamp:
		return compareOrdered(v.Value(ia), v.Value(ib))
	case *array.Duration:
		return compareOrdered(v.Value(ia), v.Value(ib))
	}
	return strings.Compare(col.ValueStr(ia), col.ValueStr(ib))
}

type ordered interface {
	~int8 | ~int16 | ~int32 | ~int64 | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~float32 | ~float64
}

func compareOrdered[T ordered](a, b T) (cmp int) {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func boolOrd(b bool) (n uint8) {
	if b {
		return 1
	}
	return 0
}
