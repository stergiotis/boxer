package fsbrowser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/statestore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// memWidthStore is the smallest colwidth.StoreI: rows in a slice, latest
// wins per key, delete tombstones by removing.
type memWidthStore struct {
	rows []statestore.ColumnWidthRow
}

func (m *memWidthStore) ListColumnWidths(appId app.AppIdT) (rows []statestore.ColumnWidthRow, err error) {
	for _, r := range m.rows {
		if r.AppId == appId {
			rows = append(rows, r)
		}
	}
	return
}

func (m *memWidthStore) WriteColumnWidth(row statestore.ColumnWidthRow) (err error) {
	m.rows = append(m.rows, row)
	return nil
}

func (m *memWidthStore) DeleteColumnWidth(appId app.AppIdT, tier string, scope string, columnKey string) (err error) {
	kept := m.rows[:0]
	for _, r := range m.rows {
		if r.AppId == appId && r.Tier == tier && r.Scope == scope && r.ColumnKey == columnKey {
			continue
		}
		kept = append(kept, r)
	}
	m.rows = kept
	return nil
}

func TestWidthColumnsAndDefaults(t *testing.T) {
	in := Input{ScopeKey: "pane-A", Columns: []Column{{Header: "kind", Width: 110}, {Header: "hash", WidthType: "hex"}}}
	cols := in.widthColumns(widthViewList)
	require.Len(t, cols, 5)
	assert.Equal(t, colwidth.Column{Name: "name", Type: "fsname;view=list"}, cols[0])
	assert.Equal(t, colwidth.Column{Name: "size", Type: "bytes;view=list"}, cols[1])
	assert.Equal(t, colwidth.Column{Name: "modified", Type: "time;view=list"}, cols[2])
	assert.Equal(t, colwidth.Column{Name: "kind", Type: "host;view=list"}, cols[3], "a host column defaults to the host type")
	assert.Equal(t, colwidth.Column{Name: "hash", Type: "hex;view=list"}, cols[4], "or names its own")
	assert.NotEqual(t, in.widthColumns(widthViewList)[0], in.widthColumns(widthViewOutline)[0], "the view discriminates the column tier")
	d := in.widthDefaults()
	assert.Equal(t, []float64{float64(defaultNameWidth), float64(defaultSizeWidth), float64(defaultTimeWidth), 110, float64(defaultColumnWidth)}, d)
	assert.NotEqual(t, widthSignature(cols), widthSignature(cols[:4]))
	assert.Greater(t, MinColumnWidth(styletokens.ActiveDensity()), widthContentMin)
}

func TestPlanWidthsWithoutAResolverIsTheDefaults(t *testing.T) {
	var st State
	in := Input{ScopeKey: "p"}
	plan := in.planWidths(&st, widthViewList, styletokens.ActiveDensity())
	assert.False(t, plan.on)
	assert.Equal(t, in.widthDefaults(), plan.widths)
	assert.Equal(t, seedWidthEpoch, plan.epoch, "the defaults are applied, or the crate fits the columns to truncating cells")

	plan = in.planWidths(&st, widthViewList, styletokens.ActiveDensity())
	assert.Equal(t, seedWidthEpoch+1, plan.epoch, "once more on the second frame, past a window's sizing pass")
	plan = in.planWidths(&st, widthViewList, styletokens.ActiveDensity())
	assert.Equal(t, seedWidthEpoch+1, plan.epoch, "and never again, so a drag stays")

	plan = in.planWidths(&st, widthViewOutline, styletokens.ActiveDensity())
	assert.Equal(t, seedWidthEpoch, plan.epoch, "the outline is its own table and starts over")
}

func TestPlanWidthsResolvesAndObserves(t *testing.T) {
	store := &memWidthStore{}
	res, err := colwidth.New(store, colwidth.Opts{AppId: "test/app", MinPoints: 30, MaxPoints: 1200})
	require.NoError(t, err)
	require.NoError(t, res.Load())
	var st State
	in := Input{ScopeKey: "pane-A", Widths: res}
	plan := in.planWidths(&st, widthViewList, styletokens.ActiveDensity())
	require.True(t, plan.on)
	assert.Equal(t, "pane-A", plan.tag)
	assert.Equal(t, in.widthDefaults(), plan.widths, "nothing stored: the defaults")
	epoch0 := plan.epoch

	// Two settle reports (the baseline), then a drag on the size column.
	seed := []float32{float32(defaultNameWidth), float32(defaultSizeWidth), float32(defaultTimeWidth)}
	in.observeWidths(&st, plan, seed, widthViewList)
	in.observeWidths(&st, plan, seed, widthViewList)
	dragged := []float32{float32(defaultNameWidth), 150, float32(defaultTimeWidth)}
	in.observeWidths(&st, plan, dragged, widthViewList)
	in.observeWidths(&st, plan, dragged, widthViewList)
	// The resolver debounces; force the write by flushing well after.
	for i := 0; i < 3; i++ {
		_, _ = res.Flush(farFuture())
	}
	plan2 := in.planWidths(&st, widthViewList, styletokens.ActiveDensity())
	assert.Equal(t, 150.0, plan2.widths[1], "the dragged width resolves back")
	assert.Equal(t, epoch0, plan2.epoch, "the table already shows the reader's drag, so no re-apply is due")

	// A fresh resolver over the same store sees it too: that is persistence.
	res2, err := colwidth.New(store, colwidth.Opts{AppId: "test/app", MinPoints: 30, MaxPoints: 1200})
	require.NoError(t, err)
	require.NoError(t, res2.Load())
	var st2 State
	in2 := Input{ScopeKey: "pane-A", Widths: res2}
	assert.Equal(t, 150.0, in2.planWidths(&st2, widthViewList, styletokens.ActiveDensity()).widths[1])
	// The outline view is keyed apart.
	assert.Equal(t, float64(defaultSizeWidth), in2.planWidths(&st2, widthViewOutline, styletokens.ActiveDensity()).widths[1])
	// The column tier reaches another table in the app with the same column.
	in3 := Input{ScopeKey: "pane-B", Widths: res2}
	var st3 State
	assert.Equal(t, 150.0, in3.planWidths(&st3, widthViewList, styletokens.ActiveDensity()).widths[1], "the column tier crosses panes")
}

// farFuture is a flush instant past any debounce: observeWidths stamps
// captures with the wall clock, so an hour later is settled by any measure.
func farFuture() time.Time { return time.Now().Add(time.Hour) }

func TestFillWidthGivesTheNameColumnWhatTheOthersLeave(t *testing.T) {
	density := styletokens.ActiveDensity()
	store := &memWidthStore{}
	res, err := colwidth.New(store, colwidth.Opts{AppId: "test/app", MinPoints: 30, MaxPoints: 1200})
	require.NoError(t, err)
	require.NoError(t, res.Load())
	var st State
	in := Input{ScopeKey: "dlg", Widths: res, FillWidth: true}

	plan := in.planWidths(&st, widthViewList, density)
	assert.Equal(t, float64(defaultNameWidth), plan.widths[0], "until the probe has reported, the plan stands")
	epoch0 := plan.epoch

	st.tableW = 800
	plan = in.planWidths(&st, widthViewList, density)
	want := float64(800 - defaultSizeWidth - defaultTimeWidth - fillSlack)
	assert.Equal(t, want, plan.widths[0], "the pane's width less the other columns")
	assert.Equal(t, float64(defaultNameWidth), plan.resolvedName)
	assert.Greater(t, plan.epoch, epoch0, "a moved name width is re-applied")
	epoch1 := plan.epoch

	// The table reports the layout back; the resolver must not read the name
	// column as dragged, or every resize of the pane would be written.
	report := func() []float32 {
		return []float32{float32(plan.widths[0]), float32(plan.widths[1]), float32(plan.widths[2])}
	}
	for i := 0; i < 4; i++ {
		in.observeWidths(&st, plan, report(), widthViewList)
		plan = in.planWidths(&st, widthViewList, density)
	}
	assert.Equal(t, epoch1, plan.epoch, "an unmoved layout is not re-applied")
	assert.Zero(t, res.PendingCount(), "the name column is never captured")

	// The reader drags the name|size edge 40 to the left: the name column
	// narrows and the size column, to its right, takes it.
	dragged := report()
	dragged[0] -= 40
	in.observeWidths(&st, plan, dragged, widthViewList)
	plan = in.planWidths(&st, widthViewList, density)
	assert.Equal(t, want-40, plan.widths[0])
	assert.Equal(t, float64(defaultSizeWidth+40), plan.widths[1], "the right-hand neighbour gives or takes")
	assert.Equal(t, float64(defaultTimeWidth), plan.widths[2])
	assert.Greater(t, plan.epoch, epoch1)
	assert.NotZero(t, res.PendingCount(), "and the size column's new width is what persists")

	st.tableW = 100
	plan = in.planWidths(&st, widthViewList, density)
	assert.Equal(t, float64(MinColumnWidth(density)), plan.widths[0], "a pane too narrow leaves the floor, and the table scrolls")
}

func TestFillLayoutIsASplitter(t *testing.T) {
	const floor = 30
	// Frames until the layout has settled: plan, then the table's report.
	settled := func(f *fillT) {
		f.plan([]float64{0, 90, 140}, 1, 802, floor)
		for f.settle > 0 {
			f.dragged(f.w, floor)
			f.plan([]float64{0, 90, 140}, 1, 802, floor)
		}
	}
	sum := func(f *fillT) (s float32) {
		for _, w := range f.w {
			s += w
		}
		return
	}

	var f fillT
	settled(&f)
	require.Equal(t, []float32{570, 90, 140}, f.w)

	// A drag on size's right edge, read over three frames. The report is a
	// frame late and the layout already holds the previous reading, so each
	// step is taken against the layout — never counted twice — and what size
	// gains, modified gives: name, left of the edge, stands still, which is
	// what lets the edge follow the pointer.
	for _, size := range []float32{100, 115, 115} {
		f.dragged([]float32{570, size, f.prev[2]}, floor)
		f.plan([]float64{0, 90, 140}, 1, 802, floor)
	}
	assert.Equal(t, []float32{570, 115, 115}, f.w)
	assert.Equal(t, float32(800), sum(&f), "the total is the pane's")

	// The giver keeps its floor, and the dragged column stops there.
	epoch := f.epoch
	f.dragged([]float32{570, 300, 115}, floor)
	assert.Equal(t, []float32{570, 200, 30}, f.w)
	assert.Greater(t, f.epoch, epoch, "re-applied, which puts the dragged column back to what it got")

	// A wider pane is the name column's.
	f.plan([]float64{0, 90, 140}, 1, 902, floor)
	assert.Equal(t, []float32{670, 200, 30}, f.w)

	// Reports are ignored while that settles: they describe the old layout.
	f.dragged([]float32{570, 200, 30}, floor)
	assert.Equal(t, []float32{670, 200, 30}, f.w, "a stale report is not a drag")

	// The resolver's answer moving — a reset — retakes the layout from it.
	f.plan([]float64{0, 90, 140}, 2, 902, floor)
	assert.Equal(t, []float32{670, 90, 140}, f.w)
}
