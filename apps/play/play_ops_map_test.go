package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// mapPaneLaidOut gives the Map a camera, as its first frame does.
func mapPaneLaidOut(t *testing.T, p *PlayApp) {
	t.Helper()
	d := p.mapDriver
	d.pm = portolan.New(d.ids, "play-map-test", portolan.Options{Center: portolan.LL(40, 0), Zoom: 4, NoTiles: true})
	t.Cleanup(d.pm.Close)
	d.pm.View().SetSize(portolan.Point{X: 800, Y: 600})
	d.pm.View().SetView(portolan.LL(40, 0), 4)
	require.True(t, d.pm.View().Loaded())
}

func TestGetMapAndSetMapView(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	drawnPane(p, mapPaneId, 0, nil)
	r := queryOp[MapReading](t, h, opGetMap, nil)
	assert.Zero(t, r.Drawn.ResultId, "a tool pane draws no result")
	assert.Equal(t, p.mapDriver.table, r.Table)
	assert.Equal(t, builtinRenders[0].name, r.Render)
	assert.Len(t, r.Renders, len(builtinRenders))
	assert.Nil(t, r.Camera, "no camera before the map has laid out")

	err := applyOp(t, h, opSetMapView, SetMapViewArgs{Center: &MapPoint{Lat: 50, Lon: 8}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "show_pane map")

	mapPaneLaidOut(t, p)
	zoom := 6.0
	require.NoError(t, applyOp(t, h, opSetMapView, SetMapViewArgs{Center: &MapPoint{Lat: 50, Lon: 8}, Zoom: &zoom}))
	v := p.mapDriver.pm.View()
	assert.InDelta(t, 50, v.Center().Lat, 1e-6)
	assert.InDelta(t, 6, v.Zoom(), 1e-9)
	assert.True(t, p.mapDriver.forceRefresh, "one fetch for the new view, whatever live says")
	assert.Equal(t, "task:t", p.mapDriver.emitWriter(), "the viewport signals of that fetch carry the task")
	r = queryOp[MapReading](t, h, opGetMap, nil)
	require.NotNil(t, r.Camera)
	assert.InDelta(t, 8, r.Camera.Center.Lon, 1e-6)

	require.NoError(t, applyOp(t, h, opSetMapView, SetMapViewArgs{Bounds: &MapBox{South: 45, West: 5, North: 55, East: 15}}))
	assert.InDelta(t, 50, v.Center().Lat, 3)
	for _, in := range []SetMapViewArgs{{}, {Bounds: &MapBox{South: 50, West: 0, North: 40, East: 10}}, {Bounds: &MapBox{South: 40, North: 50, West: 0, East: 10}, Zoom: &zoom}} {
		require.Error(t, applyOp(t, h, opSetMapView, in), "%+v", in)
	}
	huge := 99.0
	require.Error(t, applyOp(t, h, opSetMapView, SetMapViewArgs{Zoom: &huge}))
}

func TestSetMapOptionsSetsTheRasterAndTheArea(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	d := p.mapDriver
	before := d.table
	for _, in := range []SetMapOptionsArgs{
		{},
		{Table: strp("t; DROP TABLE x")},
		{Table: strp("t -- c")},
		{TimeColumn: strp("a b")},
		{Render: strp("Nope")},
		{ColorSql: strp("1 AS red; SELECT 2")},
		{Sampling: float64p(0)},
		{Area: &MapBox{South: 10, West: 20, North: 30, East: 40}, ClearArea: true},
		{Area: &MapBox{South: 30, West: 20, North: 10, East: 40}},
		{Table: strp("ok"), Sampling: float64p(500)},
	} {
		require.Error(t, applyOp(t, h, opSetMapOptions, in), "%+v", in)
	}
	assert.Equal(t, before, d.table, "a refused call applies nothing")

	require.NoError(t, applyOp(t, h, opSetMapOptions, SetMapOptionsArgs{Table: strp("hits"), Render: strp("density"), TimeColumn: strp(""), Sampling: float64p(10)}))
	assert.Equal(t, "hits", d.table)
	assert.Equal(t, 1, d.renderIdx)
	assert.Empty(t, d.timeCol)
	assert.Equal(t, 10.0, d.sampling)
	assert.Equal(t, "task:t", d.emitWriter())
	assert.Contains(t, mapOptionsDigest(p), "hits|")

	require.NoError(t, applyOp(t, h, opSetMapOptions, SetMapOptionsArgs{Area: &MapBox{South: 10, West: 20, North: 30, East: 40}}))
	assert.True(t, d.hasArea)
	raw, writer := signalOf(t, p, signalAreaMinLat)
	assert.Equal(t, "10", raw)
	assert.Equal(t, "task:t", writer)
	drawnPane(p, mapPaneId, 0, nil)
	r := queryOp[MapReading](t, h, opGetMap, nil)
	require.NotNil(t, r.Area)
	assert.Equal(t, 30.0, r.Area.North)
	assert.Equal(t, "Density", r.Render)

	require.NoError(t, applyOp(t, h, opSetMapOptions, SetMapOptionsArgs{ClearArea: true}))
	assert.False(t, d.hasArea)
	raw, _ = signalOf(t, p, signalAreaMinLat)
	assert.Equal(t, "-90", raw)
}

// The render combo and a drawn box go through set_map_options as the
// person's gestures; the area keeps the pane as writer.
func TestMapGesturesGoThroughSetMapOptions(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	name := builtinRenders[2].name
	p.mapDriver.requestOptions(SetMapOptionsArgs{Render: &name}, nil)
	e := lastEntry(t, eng)
	assert.Equal(t, opSetMapOptions, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, 2, p.mapDriver.renderIdx)
	assert.Equal(t, signalWriterMap, p.mapDriver.emitWriter(), "the person's change is the pane's own")

	p.mapDriver.onSelected(portolan.Events{SelectedOk: true,
		Selected: portolan.LatLngBoundsOf(portolan.LL(1, 2), portolan.LL(3, 4))}, p.sigEmit.as(signalWriterMap))
	e = lastEntry(t, eng)
	assert.Equal(t, opSetMapOptions, e.Op)
	_, writer := signalOf(t, p, signalAreaMaxLat)
	assert.Equal(t, signalWriterMap, writer)
	assert.True(t, p.mapDriver.hasArea)
}

func float64p(v float64) *float64 { return &v }

// The table source and the colour expression are SQL the raster runs on
// every pan, after the task's mark is gone: an agent's call naming either
// is refused and applies nothing (review finding).
func TestSetMapOptionsKeepsTheSQLFragmentsThePersons(t *testing.T) {
	l, h := opsLauncher(t)
	d := l.inner.mapDriver
	before, beforeColor := d.table, d.customColorSQL
	agent := app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}
	for _, in := range []SetMapOptionsArgs{
		{Table: strp("url('https://host/x', CSV)")},
		{ColorSql: strp("255 AS red, 0 AS green, 0 AS blue")},
		{Table: strp("hits"), Render: strp("density")},
	} {
		_, err := h.ApplyCommand(agent, opSetMapOptions, mustEncode(t, in))
		require.Error(t, err, "%+v", in)
	}
	assert.Equal(t, before, d.table)
	assert.Equal(t, beforeColor, d.customColorSQL)

	_, err := h.ApplyCommand(agent, opSetMapOptions, mustEncode(t, SetMapOptionsArgs{Render: strp("density")}))
	require.NoError(t, err, "the other settings stay an agent's")
}
