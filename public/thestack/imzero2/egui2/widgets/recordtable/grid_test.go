package recordtable

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapStore is a colwidth.StoreI over a slice, enough for a resolver to load
// and flush against.
type mapStore struct {
	rows []statestore.ColumnWidthRow
}

func (inst *mapStore) ListColumnWidths(appId app.AppIdT) (rows []statestore.ColumnWidthRow, err error) {
	return inst.rows, nil
}

func (inst *mapStore) WriteColumnWidth(row statestore.ColumnWidthRow) (err error) {
	inst.rows = append(inst.rows, row)
	return
}

func (inst *mapStore) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	return
}

// frame renders one grid frame and closes it, as a host's frame boundary
// would, so the next frame may reuse the ids.
func frame(in Input) (res Result) {
	defer c.CurrentApplicationState.StateManager.ScriptReset()
	return RenderGrid(in)
}

func gridColumns() []Column {
	return []Column{
		{Arrow: 0},
		{Arrow: 1, Header: "count", Sub: []string{"i64"}},
		{Arrow: -1, Header: "twice", Cell: func(row int64) Cell { return Cell{Text: "x"} }},
	}
}

// One frame renders under the discard channel from a zero State, with and
// without a resolver, and a click on nothing reports nothing.
func TestRenderGridHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	rec := sortRec(t, []string{"pear", "apple"}, []bool{false, true}, []int64{2, 1})
	defer rec.Release()
	var sort SortState
	sort.Clicked(0)
	var st State
	in := Input{
		Ids: c.NewWidgetIdStack(), ScopeKey: "t", Record: rec, Columns: gridColumns(),
		Sort: &sort, Order: sort.OrderFor(rec, rec.NumRows()), Selected: 0, State: &st,
	}
	res := frame(in)
	require.NoError(t, res.Err)
	assert.Equal(t, int64(-1), res.Clicked)
	assert.Equal(t, -1, res.SortClicked)

	widths, err := colwidth.New(&mapStore{}, colwidth.Opts{AppId: "org.test.grid", MinPoints: float64(DragMinWidth(0)), MaxPoints: float64(MaxColumnWidth)})
	require.NoError(t, err)
	require.NoError(t, widths.Load())
	in.Widths = widths
	res = frame(in)
	require.NoError(t, res.Err)
	assert.NoError(t, res.WidthsErr)

	// A nil record draws the headers over no rows.
	in.Record, in.Order = nil, nil
	assert.NoError(t, frame(in).Err)
}

// An unusable input draws a message and says why, rather than panicking.
func TestRenderGridRefusesBrokenInput(t *testing.T) {
	t.Cleanup(scenetest.Install())
	rec := sortRec(t, []string{"a"}, []bool{false}, []int64{1})
	defer rec.Release()
	ids := c.NewWidgetIdStack()
	assert.Error(t, frame(Input{Ids: ids, Record: rec, Columns: gridColumns()}).Err, "no State")
	var st State
	assert.Error(t, frame(Input{Ids: ids, Record: rec, State: &st,
		Columns: []Column{{Arrow: -1, Header: "computed"}}}).Err, "a computed column needs a Cell")
	assert.Error(t, frame(Input{Ids: ids, Record: rec, State: &st,
		Columns: []Column{{Arrow: 7}}}).Err, "an Arrow column the record lacks")
}

// The grid fits its columns once per column set, and only once it has rows:
// fitted on an empty grid a column would be as wide as its header.
func TestRenderGridFitsOncePerColumnSet(t *testing.T) {
	t.Cleanup(scenetest.Install())
	rec := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{1, 2})
	defer rec.Release()
	var st State
	in := Input{Ids: c.NewWidgetIdStack(), Columns: gridColumns(), State: &st, Selected: -1}

	frame(in)
	assert.Equal(t, uint32(0), st.applyGen, "no rows, no fit")
	in.Record = rec
	frame(in)
	assert.Equal(t, uint32(1), st.applyGen, "the first frame with rows fits")
	frame(in)
	assert.Equal(t, uint32(1), st.applyGen, "and only that one")
	in.Columns = in.Columns[:2]
	frame(in)
	assert.Equal(t, uint32(2), st.applyGen, "a changed column set fits again")
}

// Width identity is the Arrow field name and type, or a computed column's
// header, with the "#" gutter first so the read-back lines up by position.
func TestGridWidthColumns(t *testing.T) {
	rec := sortRec(t, []string{"a"}, []bool{false}, []int64{1})
	defer rec.Release()
	cols := gridColumns()
	cols[1].WidthType = "int64;view=row"
	in := Input{Record: rec, Columns: cols}
	assert.Equal(t, []colwidth.Column{
		{Name: "#", Type: "rownum"},
		{Name: "name", Type: arrow.BinaryTypes.String.String()},
		{Name: "n", Type: "int64;view=row"},
		{Name: "twice", Type: "computed"},
	}, in.widthColumns(), "the caption does not key the width; the field name does")
}

// The window and the order together pick the record rows drawn.
func TestGridDisplayRows(t *testing.T) {
	rec := sortRec(t, []string{"a", "b", "c", "d"}, []bool{false, false, false, false}, []int64{1, 2, 3, 4})
	defer rec.Release()
	in := Input{Record: rec}
	start, n := in.displayRows()
	assert.Equal(t, [2]int64{0, 4}, [2]int64{start, n})

	in.PageStart, in.PageLen = 1, 2
	start, n = in.displayRows()
	assert.Equal(t, [2]int64{1, 2}, [2]int64{start, n})
	assert.Equal(t, int64(1), in.recordRow(start))

	in.Order = []int64{3, 2, 1, 0}
	assert.Equal(t, int64(2), in.recordRow(start), "display position 1 of a reversed order")
	in.Order = []int64{9}
	in.PageStart, in.PageLen = 0, 0
	assert.Equal(t, int64(-1), in.recordRow(0), "a row the record lacks is skipped")

	in.PageStart = 10
	_, n = in.displayRows()
	assert.Equal(t, int64(0), n, "a window past the end draws nothing")
}
