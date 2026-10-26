package recordtable

import (
	"testing"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A grouped grid renders under the discard channel — two levels, a column
// and a path, aggregates on, the cells cache on — from a zero State, and
// reports no gesture; a toggle applied between frames re-flattens; a
// broken level is drawn as a message, not a panic.
func TestRenderGridGroupedHeadless(t *testing.T) {
	t.Cleanup(scenetest.Install())
	rec := groupRec(t, []int64{2, 1, 2, 1}, []string{"/m/a", "/m/a", "/m/b", "/x"}, []float64{1, 2, 3, 4})
	defer rec.Release()
	var st State
	var groups GroupState
	groups.SetLevels([]GroupLevel{{Arrow: colEnt, KeyIsRow: true}, {Segments: segments(rec)}})
	cols := aggCols(AggSum)
	cols[colVal].Agg = AggMean
	in := Input{
		Ids: c.NewWidgetIdStack(), ScopeKey: "g", Record: rec, Columns: cols,
		Groups: &groups, Selected: 2, HasSelection: true,
		SelectedKeyRow: 1, HasSelectedKeyRow: true,
		CellsKey: "same", State: &st,
	}
	res := frame(in)
	require.NoError(t, res.Err)
	assert.Equal(t, int64(-1), res.Clicked)
	assert.Equal(t, int32(-1), res.GroupToggled)
	assert.Equal(t, int64(-1), res.GroupClicked)
	assert.Equal(t, -1, res.GroupOrderClicked)
	lay := groups.Layout()
	assert.Equal(t, "v1 .vm ..1 .3 v2 .vm ..0 ..2", draw(lay))

	// A second frame with nothing changed; then a toggle, which the grid
	// does not apply itself.
	res = frame(in)
	require.NoError(t, res.Err)
	groups.Apply(Result{GroupToggled: lay.GroupAt(0), GroupOrderClicked: -1})
	res = frame(in)
	require.NoError(t, res.Err)
	assert.Equal(t, ">1 v2 .vm ..0 ..2", draw(groups.Layout()))

	// Paging windows the lines, group lines included.
	in.PageStart, in.PageEnd = 1, 3
	require.NoError(t, frame(in).Err)
	in.PageStart, in.PageEnd = 0, 0

	// A nil record draws the headers over no rows, grouped or not.
	in.Record = nil
	require.NoError(t, frame(in).Err)
	in.Record = rec

	// A level on a column the record lacks is drawn as a message.
	groups.SetLevels([]GroupLevel{{Arrow: 9}})
	assert.Error(t, frame(in).Err)
}

// Apply folds the toggled group and reverses the clicked level; a Result
// with nothing in it changes nothing.
func TestGroupStateApply(t *testing.T) {
	rec := groupRec(t, []int64{1, 2}, []string{"/a", "/b"}, []float64{1, 2})
	defer rec.Release()
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: colEnt}})
	lay, err := st.LayoutFor(rec, nil, 2, aggCols(AggNone))
	require.NoError(t, err)
	gen := st.Gen()
	st.Apply(Result{GroupToggled: -1, GroupOrderClicked: -1})
	assert.Equal(t, gen, st.Gen())
	st.Apply(Result{GroupToggled: lay.GroupAt(0), GroupOrderClicked: -1})
	assert.False(t, st.IsOpen(lay.Groups[lay.GroupAt(0)]))
	assert.Equal(t, " ▲", st.Glyph(0))
	st.Apply(Result{GroupToggled: -1, GroupOrderClicked: 0})
	assert.True(t, st.Levels()[0].Order.Desc)
	assert.Equal(t, " ▼", st.Glyph(0))
	assert.Equal(t, "", st.Glyph(3), "no such level")
	lay, err = st.LayoutFor(rec, nil, 2, aggCols(AggNone))
	require.NoError(t, err)
	assert.Equal(t, "v2 .1 >1", draw(lay), "reversed, with the folded group still folded")
}

// The header of a grouped column reverses its level; a level with its own
// Key does not claim the column it reads.
func TestGroupLevelOf(t *testing.T) {
	var st GroupState
	st.SetLevels([]GroupLevel{{Arrow: 1}, {Arrow: 2, Key: func(int64) Key { return Key{} }}})
	in := Input{Groups: &st}
	assert.Equal(t, 0, in.groupLevelOf(1))
	assert.Equal(t, -1, in.groupLevelOf(2), "a Key of its own")
	assert.Equal(t, -1, in.groupLevelOf(0))
	assert.Equal(t, -1, (&Input{}).groupLevelOf(1))
}

func TestFormatAgg(t *testing.T) {
	var v AggValue
	v.addLeaf(1, true)
	v.addLeaf(2.5, true)
	v.addLeaf(0, false)
	assert.Equal(t, "3", FormatAgg(AggCount, v))
	assert.Equal(t, "3.5", FormatAgg(AggSum, v))
	assert.Equal(t, "1", FormatAgg(AggMin, v))
	assert.Equal(t, "2.5", FormatAgg(AggMax, v))
	assert.Equal(t, "1.75", FormatAgg(AggMean, v))
	assert.Equal(t, "", FormatAgg(AggNone, v))
	assert.Equal(t, "", FormatAgg(AggSum, AggValue{Count: 2}), "no numeric value")
	var third AggValue
	third.addLeaf(1, true)
	third.addLeaf(0, true)
	third.addLeaf(0, true)
	assert.Equal(t, "0.333", FormatAgg(AggMean, third))
}
