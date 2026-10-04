package providersgui

import (
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/introspect"
	"github.com/stergiotis/boxer/public/keelson/runtime/windowhost"
)

// TestWindowsTableRendersLaunchProvenance drives the table with fixed rows
// (no window host, no GUI): the three windows differ only in where their
// content came from, which is exactly what the launch_reason column exists
// to make queryable (ADR-0148 §SD5).
func TestWindowsTableRendersLaunchProvenance(t *testing.T) {
	ws := []windowhost.WindowInfo{
		{Key: 1, AppId: "test.a", LaunchReason: app.LaunchReasonPlain},
		{
			Key: 2, AppId: "test.b", LaunchReason: app.LaunchReasonCaller,
			ConfigKind: "testLaunch", ConfigBytes: 12,
		},
		{
			Key: 3, AppId: "test.b", LaunchReason: app.LaunchReasonRestore,
			ConfigKind: "testLaunch", ConfigBytes: 34, SharesInstance: true,
		},
	}
	rec := windowsTable(ws).Build(introspect.AllColumns(), len(ws))
	defer rec.Release()
	require.EqualValues(t, 3, rec.NumRows())

	reasons := stringColumn(t, rec, "launch_reason")
	assert.Equal(t, []string{"plain", "caller", "restore"}, reasons)

	kinds := stringColumn(t, rec, "config_kind")
	assert.Empty(t, kinds[0], "a plain open carries no config kind")
	assert.Equal(t, "testLaunch", kinds[1])

	sizes := rec.Column(colIndex(t, rec, "config_bytes")).(*array.Int64)
	assert.EqualValues(t, 0, sizes.Value(0))
	assert.EqualValues(t, 34, sizes.Value(2))

	shared := rec.Column(colIndex(t, rec, "shares_instance")).(*array.Boolean)
	assert.False(t, shared.Value(0))
	assert.True(t, shared.Value(2))
}

func TestWindowsProviderEmptyHost(t *testing.T) {
	// A nil host is the headless/no-window case: an empty table, not an
	// error and not an absent one.
	p := windowsProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Zero(t, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols())
}

func colIndex(t *testing.T, rec arrow.RecordBatch, col string) int {
	t.Helper()
	idx := rec.Schema().FieldIndices(col)
	require.NotEmpty(t, idx, "column %q not found", col)
	return idx[0]
}

func stringColumn(t *testing.T, rec arrow.RecordBatch, col string) (out []string) {
	t.Helper()
	c := rec.Column(colIndex(t, rec, col)).(*array.String)
	out = make([]string, c.Len())
	for i := range out {
		out[i] = c.Value(i)
	}
	return
}

func TestFrameTimesTable(t *testing.T) {
	rows := []windowhost.FrameTimeInfo{
		{Scope: windowhost.FrameScopeLoop, Frames: 10, Samples: 10, P95: 16 * time.Millisecond},
		{Scope: windowhost.FrameScopeWindow, Key: 3, AppId: "test.a", P95: 1500 * time.Microsecond, MessagesMean: 42.5, Mount: 2 * time.Millisecond},
	}
	rec := frameTimesTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	require.EqualValues(t, 2, rec.NumRows())
	assert.Equal(t, []string{"loop", "window"}, stringColumn(t, rec, "scope"))
	assert.Equal(t, []string{"", "test.a"}, stringColumn(t, rec, "app_id"))
	p95 := rec.Column(colIndex(t, rec, "p95_us")).(*array.Int64)
	assert.EqualValues(t, 16000, p95.Value(0))
	assert.EqualValues(t, 1500, p95.Value(1))
	mount := rec.Column(colIndex(t, rec, "mount_us")).(*array.Int64)
	assert.EqualValues(t, 2000, mount.Value(1))
}

func TestFrameTimesProviderNilHost(t *testing.T) {
	p := frameTimesProvider{}
	rec, err := p.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer rec.Release()
	assert.Zero(t, rec.NumRows())
	assert.EqualValues(t, p.Schema().NumFields(), rec.NumCols())
}

// The windows table carries each window's geometry and shell state
// (ADR-0276 §SD1): a drawn window's outer rect as x, y, w, h, and a window
// that has not drawn yet reads as not shown with zero geometry.
func TestWindowsTableRendersGeometry(t *testing.T) {
	ws := []windowhost.WindowInfo{
		{Key: 1, AppId: "test.a", Geom: windowhost.WindowGeom{
			Shown: true,
			Rect:  windowhost.Rect{MinX: 10, MinY: 30, MaxX: 410, MaxY: 330},
			NeedW: 400, NeedH: 520, Stack: 7, Active: true,
		}},
		{Key: 2, AppId: "test.b"},
	}
	rec := windowsTable(ws).Build(introspect.AllColumns(), len(ws))
	defer rec.Release()
	f := func(col string, row int) float64 {
		return rec.Column(colIndex(t, rec, col)).(*array.Float64).Value(row)
	}
	b := func(col string, row int) bool {
		return rec.Column(colIndex(t, rec, col)).(*array.Boolean).Value(row)
	}
	assert.True(t, b("shown", 0))
	assert.Equal(t, []float64{10, 30, 400, 300}, []float64{f("x", 0), f("y", 0), f("w", 0), f("h", 0)})
	assert.InDelta(t, 520, f("need_h", 0), 1e-9, "need exceeds h where the content overflowed")
	assert.EqualValues(t, 7, rec.Column(colIndex(t, rec, "stack")).(*array.Uint64).Value(0))
	assert.True(t, b("active", 0))
	assert.False(t, b("maximized", 0))

	assert.False(t, b("shown", 1))
	assert.Zero(t, f("w", 1))
	assert.False(t, b("active", 1))
}

// The desktop table is one row from a host and none without one.
func TestDesktopTable(t *testing.T) {
	rows := []windowhost.DesktopInfo{{
		WorkShown: true,
		Work:      windowhost.Rect{MinX: 0, MinY: 22, MaxX: 1400, MaxY: 870},
		ActiveKey: 3,
		Arranging: windowhost.ArrangeTile,
	}}
	rec := desktopTable(rows).Build(introspect.AllColumns(), len(rows))
	defer rec.Release()
	require.EqualValues(t, 1, rec.NumRows())
	f := func(col string) float64 { return rec.Column(colIndex(t, rec, col)).(*array.Float64).Value(0) }
	assert.Equal(t, []float64{0, 22, 1400, 848}, []float64{f("work_x"), f("work_y"), f("work_w"), f("work_h")})
	assert.EqualValues(t, 3, rec.Column(colIndex(t, rec, "active_key")).(*array.Int64).Value(0))
	assert.Equal(t, []string{"Tile"}, stringColumn(t, rec, "arranging"))

	idle := desktopTable([]windowhost.DesktopInfo{{}}).Build(introspect.AllColumns(), 1)
	defer idle.Release()
	assert.Equal(t, []string{""}, stringColumn(t, idle, "arranging"), "no arrangement reads as empty")

	empty, err := desktopProvider{}.Snapshot(introspect.AllColumns())
	require.NoError(t, err)
	defer empty.Release()
	assert.Zero(t, empty.NumRows())
}
