package fsbrowser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/colwidth"
)

// memWidthStore is the smallest colwidth.StoreI: rows in a slice, latest
// wins per key, delete tombstones by removing.
type memWidthStore struct {
	rows []factsstore.ColumnWidthRow
}

func (m *memWidthStore) ListColumnWidths(appId app.AppIdT) (rows []factsstore.ColumnWidthRow, err error) {
	for _, r := range m.rows {
		if r.AppId == appId {
			rows = append(rows, r)
		}
	}
	return
}

func (m *memWidthStore) WriteColumnWidth(row factsstore.ColumnWidthRow) (id uint64, err error) {
	m.rows = append(m.rows, row)
	return uint64(len(m.rows)), nil
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
	assert.Greater(t, MinColumnWidth(styletokens.DensityFromEnv()), widthContentMin)
}

func TestPlanWidthsWithoutAResolverIsTheDefaults(t *testing.T) {
	var st State
	in := Input{ScopeKey: "p"}
	plan := in.planWidths(&st, widthViewList)
	assert.False(t, plan.on)
	assert.Equal(t, in.widthDefaults(), plan.widths)
	assert.Equal(t, uint32(0), plan.epoch)
}

func TestPlanWidthsResolvesAndObserves(t *testing.T) {
	store := &memWidthStore{}
	res, err := colwidth.New(store, colwidth.Opts{AppId: "test/app", MinPoints: 30, MaxPoints: 1200})
	require.NoError(t, err)
	require.NoError(t, res.Load())
	var st State
	in := Input{ScopeKey: "pane-A", Widths: res}
	plan := in.planWidths(&st, widthViewList)
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
	plan2 := in.planWidths(&st, widthViewList)
	assert.Equal(t, 150.0, plan2.widths[1], "the dragged width resolves back")
	assert.Equal(t, epoch0, plan2.epoch, "the table already shows the reader's drag, so no re-apply is due")

	// A fresh resolver over the same store sees it too: that is persistence.
	res2, err := colwidth.New(store, colwidth.Opts{AppId: "test/app", MinPoints: 30, MaxPoints: 1200})
	require.NoError(t, err)
	require.NoError(t, res2.Load())
	var st2 State
	in2 := Input{ScopeKey: "pane-A", Widths: res2}
	assert.Equal(t, 150.0, in2.planWidths(&st2, widthViewList).widths[1])
	// The outline view is keyed apart.
	assert.Equal(t, float64(defaultSizeWidth), in2.planWidths(&st2, widthViewOutline).widths[1])
	// The column tier reaches another table in the app with the same column.
	in3 := Input{ScopeKey: "pane-B", Widths: res2}
	var st3 State
	assert.Equal(t, 150.0, in3.planWidths(&st3, widthViewList).widths[1], "the column tier crosses panes")
}

// farFuture is a flush instant past any debounce: observeWidths stamps
// captures with the wall clock, so an hour later is settled by any measure.
func farFuture() time.Time { return time.Now().Add(time.Hour) }
