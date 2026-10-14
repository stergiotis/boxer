package recordtable

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
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
		Sort: &sort, Order: sort.OrderFor(rec, rec.NumRows()), Selected: 0, HasSelection: true, State: &st,
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
	in.Refit = true
	frame(in)
	assert.Equal(t, uint32(3), st.applyGen, "and the host may ask for a fit")
	in.Record = nil
	frame(in)
	assert.Equal(t, uint32(4), st.applyGen, "even over no rows")
}

// Width identity is the Arrow field name and type, or a computed column's
// header, with the "#" gutter first so the read-back lines up by position.
func TestGridWidthColumns(t *testing.T) {
	rec := sortRec(t, []string{"a"}, []bool{false}, []int64{1})
	defer rec.Release()
	cols := gridColumns()
	cols[1].WidthType = "int64"
	in := Input{Record: rec, Columns: cols, WidthView: ";view=row"}
	assert.Equal(t, []colwidth.Column{
		{Name: "#", Type: "rownum;view=row"},
		{Name: "name", Type: arrow.BinaryTypes.String.String() + ";view=row"},
		{Name: "n", Type: "int64;view=row"},
		{Name: "twice", Type: "computed;view=row"},
	}, in.WidthColumns(), "the caption does not key the width; the field name does")
}

// The window and the order together pick the record rows drawn.
func TestGridDisplayRows(t *testing.T) {
	rec := sortRec(t, []string{"a", "b", "c", "d"}, []bool{false, false, false, false}, []int64{1, 2, 3, 4})
	defer rec.Release()
	in := Input{Record: rec}
	start, n := in.displayRows()
	assert.Equal(t, [2]int64{0, 4}, [2]int64{start, n})

	in.PageStart, in.PageEnd = 1, 3
	start, n = in.displayRows()
	assert.Equal(t, [2]int64{1, 2}, [2]int64{start, n})
	assert.Equal(t, int64(1), in.recordRow(start))

	in.Order = []int64{3, 2, 1, 0}
	assert.Equal(t, int64(2), in.recordRow(start), "display position 1 of a reversed order")

	in.PageStart, in.PageEnd = 2, 2
	_, n = in.displayRows()
	assert.Equal(t, int64(0), n, "an empty window draws nothing")
	in.PageStart, in.PageEnd = 10, 0
	_, n = in.displayRows()
	assert.Equal(t, int64(0), n, "a window past the end draws nothing")

	in.Order = []int64{9}
	in.PageStart = 0
	assert.Equal(t, int64(-1), in.recordRow(0), "a row the record lacks is skipped")
}

// With a CellsKey, an unchanged frame replays the capture; a changed key,
// a changed selection or a cell with a response rebuilds — and the rebuild
// is what reads a click.
func TestRenderGridCellsCache(t *testing.T) {
	t.Cleanup(scenetest.Install())
	sm := c.CurrentApplicationState.StateManager
	rec := sortRec(t, []string{"pear", "apple"}, []bool{false, false}, []int64{2, 1})
	defer rec.Release()
	var st State
	in := Input{Ids: c.NewWidgetIdStack(), ScopeKey: "t", Record: rec, Columns: gridColumns(),
		Selected: -1, State: &st, CellsKey: "result-1"}

	frame(in) // the fit frame
	frame(in) // the first frame with the visible range known
	hits, misses := st.cells.hits, st.cells.misses
	frame(in)
	assert.Equal(t, hits+1, st.cells.hits, "an unchanged frame replays")

	in.CellsKey = "result-2"
	frame(in)
	assert.Equal(t, misses+1, st.cells.misses, "a new key rebuilds")
	in.Selected, in.HasSelection = 1, true
	frame(in)
	assert.Equal(t, misses+2, st.cells.misses, "so does a new selection")

	require.NotEmpty(t, st.cells.ids, "the live path noted its buttons")
	for id := range st.cells.ids {
		sm.ScriptResponse(widgethandle.Make(id), c.PrimaryClickedResponseFlags)
	}
	res := RenderGrid(in)
	sm.ScriptReset()
	assert.Equal(t, misses+3, st.cells.misses, "a clicked cell rebuilds")
	assert.GreaterOrEqual(t, res.Clicked, int64(0), "and the rebuild reads the click")

	// A key that cannot be compared is refused rather than panicking.
	in.CellsKey = []int{1}
	assert.Error(t, frame(in).Err)

	// Without a key nothing is kept for a later frame to replay.
	in.CellsKey = nil
	frame(in)
	assert.False(t, st.cells.valid)
}

// The cells emitted are the rows on screen, on a fit frame too: a fit
// measures what the reader sees. Only with nothing on screen last frame does
// a fit fall back to the window's first rows.
func TestCellRows(t *testing.T) {
	type rows struct{ lo, hi int64 }
	at := func(lo, hi int64) rows { return rows{lo, hi} }
	got := func(n int64, rb, re uint64, ok, refit bool) rows {
		lo, hi := cellRows(n, rb, re, ok, refit)
		return rows{lo, hi}
	}
	assert.Equal(t, at(0, 1000), got(1000, 0, 0, false, false), "no range yet: every row")
	assert.Equal(t, at(600, 630), got(1000, 600, 630, true, false), "the visible rows")
	assert.Equal(t, at(600, 630), got(1000, 600, 630, true, true), "a fit keeps the visible rows")
	assert.Equal(t, at(0, fitMaxRows), got(1000, 0, 0, false, true), "a first-frame fit takes the first rows")
	assert.Equal(t, at(0, fitMaxRows), got(1000, 0, 0, true, true), "so does a fit after an empty frame")
	assert.Equal(t, at(100, 100+fitMaxRows), got(1000, 100, 900, true, true), "a fit is capped")
	assert.Equal(t, at(5, 10), got(10, 5, 40, true, false), "the range is clamped to the window")
}

// The zero Input selects nothing; a selection needs HasSelection and a row.
func TestGridSelection(t *testing.T) {
	_, ok := (&Input{}).selection()
	assert.False(t, ok, "the zero Input does not select row 0")
	_, ok = (&Input{HasSelection: true, Selected: -1}).selection()
	assert.False(t, ok, "a negative row is none")
	row, ok := (&Input{HasSelection: true}).selection()
	assert.Equal(t, int64(0), row)
	assert.True(t, ok, "row 0 when asked for")
}

// A missing id stack is refused like a missing State, not a panic.
func TestRenderGridRefusesNilIds(t *testing.T) {
	t.Cleanup(scenetest.Install())
	var st State
	assert.Error(t, frame(Input{State: &st}).Err)
}

// Without the "#" column the width identities and the table positions start
// at the first column, and a capped grid renders like a filling one.
func TestRenderGridNoRowNumbers(t *testing.T) {
	t.Cleanup(scenetest.Install())
	rec := sortRec(t, []string{"a", "b"}, []bool{false, false}, []int64{1, 2})
	defer rec.Release()
	in := Input{Ids: c.NewWidgetIdStack(), Record: rec, Columns: gridColumns(), NoRowNumbers: true, MaxHeight: 120}
	keys := in.WidthColumns()
	require.Len(t, keys, len(in.Columns))
	assert.Equal(t, "name", keys[0].Name)
	assert.Equal(t, 0, in.lead())

	var st State
	in.State = &st
	widths, err := colwidth.New(&mapStore{}, colwidth.Opts{AppId: "org.test.grid"})
	require.NoError(t, err)
	in.Widths = widths
	assert.NoError(t, frame(in).Err, "the reset menu renders on every resizable header")
	assert.Equal(t, uint32(1), st.applyGen, "and the grid fits as usual")
}
