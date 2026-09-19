//go:build integration

package portolan_test

import (
	"math"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene"
	"github.com/stergiotis/boxer/public/thestack/imzero2/scene/scenetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// The portolan map's camera under synthetic gestures (ADR-0204 M5), asserted
// through the demo's readout. A scene of the computed kind (ADR-0248 §SD5):
// every gesture is specified in pixels and the readout is in degrees, so the
// oracle is a projection — which no declarative step should express.

// pixel projects a geographic point to web-mercator pixels at a zoom — the
// EPSG:3857 pyramid every raster tile server is cut on, 256 px at zoom 0.
//
// Deliberately a second implementation of what [portolan.EPSG3857] does, not a
// call into it: the scene asserts the widget's camera, and an oracle sharing
// the widget's projection would cancel a fault in it.
// [TestScenePixelMatchesEPSG3857] cross-checks the two so the copy cannot
// drift silently.
func pixel(lat, lon, zoom float64) (x, y float64) {
	s := 256 * math.Pow(2, zoom)
	rad := lat * math.Pi / 180
	x = (lon + 180) / 360 * s
	y = (1 - math.Log(math.Tan(rad)+1/math.Cos(rad))/math.Pi) / 2 * s
	return
}

func TestScenePixelMatchesEPSG3857(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lat := rapid.Float64Range(-portolan.SphericalMercatorMaxLatitude, portolan.SphericalMercatorMaxLatitude).Draw(t, "lat")
		lon := rapid.Float64Range(-180, 180).Draw(t, "lon")
		zoom := rapid.Float64Range(0, 19).Draw(t, "zoom")
		x, y := pixel(lat, lon, zoom)
		p := portolan.EPSG3857.LatLngToPoint(portolan.LatLng{Lat: lat, Lng: lon}, zoom)
		scale := 256 * math.Pow(2, zoom)
		assert.InDelta(t, p.X, x, scale*1e-9)
		assert.InDelta(t, p.Y, y, scale*1e-9)
	})
}

// camera is one reading of the demo's readout lines.
type camera struct {
	lat, lon, zoom float64
	ox, oy         float64
	w, h           float64
	requested      float64
	loaded, errors float64
	reships        float64
	loading        string
}

// readCamera takes a reading. The four `read` steps bind names suffixed with
// the reading's tag, so successive readings stay side by side in the session.
func readCamera(t *testing.T, s *scene.Session, tag string) (c camera) {
	t.Helper()
	scenetest.Run(t, s, `
{"do":"read","valueContains":"centre ","role":"label","pattern":"^centre (?P<lat`+tag+`>-?[\\d.]+), (?P<lon`+tag+`>-?[\\d.]+)\\s+zoom (?P<zoom`+tag+`>[\\d.]+)"}
{"do":"read","valueContains":"canvas at","role":"label","pattern":"^canvas at (?P<ox`+tag+`>-?[\\d.]+),(?P<oy`+tag+`>-?[\\d.]+) · (?P<w`+tag+`>\\d+) × (?P<h`+tag+`>\\d+) px"}
{"do":"read","valueContains":"requested ·","role":"label","pattern":"(?P<requested`+tag+`>\\d+) requested · (?P<loaded`+tag+`>\\d+) loaded · (?P<errors`+tag+`>\\d+) errors .* loading (?P<loading`+tag+`>\\w+)"}
{"do":"read","valueContains":"re-ships","role":"label","pattern":"re-ships (?P<reships`+tag+`>\\d+)"}`)
	n := func(name string) float64 { return scenetest.Number(t, s, name+tag) }
	c = camera{
		lat: n("lat"), lon: n("lon"), zoom: n("zoom"),
		ox: n("ox"), oy: n("oy"), w: n("w"), h: n("h"),
		requested: n("requested"), loaded: n("loaded"), errors: n("errors"), reships: n("reships"),
	}
	var err error
	c.loading, err = s.Vars.Text("loading" + tag)
	require.NoError(t, err)
	return c
}

// centreShift is how far the camera centre travelled between two readings, in
// pixels at the zoom of the first — the unit every gesture is specified in.
func centreShift(a, b camera) (dx, dy float64) {
	ax, ay := pixel(a.lat, a.lon, a.zoom)
	bx, by := pixel(b.lat, b.lon, a.zoom)
	return bx - ax, by - ay
}

// at is the screen point of a canvas-relative offset. Truncated rather than
// rounded: the driver wants a point inside the canvas, and the rect is given
// in whole pixels. Re-derived before every gesture, since the canvas can move.
func at(c camera, dx, dy float64) (x, y string) {
	return strconv.Itoa(int(c.ox + dx)), strconv.Itoa(int(c.oy + dy))
}

// minTilesLoaded is what a 720 × 460 canvas at zoom 12+ cannot show less of.
const minTilesLoaded = 12

func TestScenePortolanCamera(t *testing.T) {
	s := scenetest.Launch(t, scene.Spec{
		Launch:   "widgets",
		Size:     "1100x800",
		FPS:      60,
		Needs:    []string{scene.NeedRaster},
		Services: []string{scene.ServiceTileStub},
		Requires: []string{scene.RequireExePrefix + "python3"},
	})

	// 1. The demo up, every tile of the first view landed.
	scenetest.Run(t, s, `
{"do":"wait","role":"text_input","comment":"the gallery has mounted"}
{"do":"focus","role":"text_input"}
{"do":"type","role":"text_input","text":"portolan","comment":"narrow the gallery to the one demo"}
{"do":"wait","contains":"portolan (slippy","settleMs":400}
{"do":"click","contains":"portolan (slippy","comment":"expand the demo's section"}
{"do":"wait","valueContains":"bounds","role":"label","settleMs":500,"comment":"the readout is up"}
{"do":"wait","valueContains":"loading false","role":"label","settleMs":800,"comment":"every tile of the first view landed"}`)
	s0 := readCamera(t, s, "0")
	assert.InDelta(t, 51.0992, s0.lat, 1e-4, "baseline latitude")
	assert.InDelta(t, 17.0366, s0.lon, 1e-4, "baseline longitude")
	assert.InDelta(t, 12, s0.zoom, 0.006, "baseline zoom")
	assert.Equal(t, 720.0, s0.w)
	assert.Equal(t, 460.0, s0.h)
	assert.Equal(t, "false", s0.loading)
	assert.Zero(t, s0.errors)

	// 2. A slow 240 × 120 px drag from the canvas centre: the centre moves by
	//    exactly that, bar a few pixels of inertia at 200 px/s (a measured run
	//    coasted 3; the recipe this guards against lost 20 × 10).
	cx, cy := at(s0, 360, 230)
	tx, ty := at(s0, 360+240, 230+120)
	scenetest.Run(t, s, `
{"do":"drag","x":`+cx+`,"y":`+cy+`,"toX":`+tx+`,"toY":`+ty+`,"steps":24,"durationMs":1200,"settleMs":1500,"comment":"the drag verb, ADR-0204 §SD10"}
{"do":"wait","valueContains":"loading false","role":"label","settleMs":400}`)
	s1 := readCamera(t, s, "1")
	mx, my := centreShift(s0, s1)
	assert.InDelta(t, -240, mx, 6, "a drag by +d moves the centre by −d")
	assert.InDelta(t, -120, my, 6)
	assert.InDelta(t, s0.zoom, s1.zoom, 0.006, "a drag does not zoom")

	// 3. One wheel notch at the canvas centre: a zoom of Leaflet's sigmoid,
	//    chunked by egui's smoothing, about the centre.
	cx, cy = at(s1, 360, 230)
	scenetest.Run(t, s, `
{"do":"hover","x":`+cx+`,"y":`+cy+`,"settleMs":200}
{"do":"scroll","x":0,"y":60,"settleMs":1500,"comment":"one notch, 60 px: +0.6..0.8 levels through the sigmoid"}`)
	s2 := readCamera(t, s, "2")
	dz := s2.zoom - s1.zoom
	assert.GreaterOrEqual(t, dz, 0.55, "wheel zoom")
	assert.LessOrEqual(t, dz, 0.80, "wheel zoom")
	mx, my = centreShift(s1, s2)
	assert.InDelta(t, 0, mx, 3, "the notch is about the centre")
	assert.InDelta(t, 0, my, 3)

	// 4. A double click at the canvas centre: one level in, animated, anchored.
	cx, cy = at(s2, 360, 230)
	scenetest.Run(t, s, `
{"do":"click","x":`+cx+`,"y":`+cy+`,"settleMs":60}
{"do":"click","x":`+cx+`,"y":`+cy+`,"settleMs":1500,"comment":"two clicks within egui's double-click window"}`)
	s3 := readCamera(t, s, "3")
	assert.InDelta(t, 1, s3.zoom-s2.zoom, 0.011, "double click zooms one level")
	mx, my = centreShift(s2, s3)
	assert.InDelta(t, 0, mx, 3, "anchored at the centre")
	assert.InDelta(t, 0, my, 3)

	// 5. ArrowRight after the click left the map focused: 80 px, animated.
	scenetest.Run(t, s, `
{"do":"key","text":"ArrowRight","settleMs":1200,"comment":"the map took focus on the click; Leaflet pans 80 px per arrow"}`)
	s4 := readCamera(t, s, "4")
	mx, my = centreShift(s3, s4)
	assert.InDelta(t, 80, mx, 1.5, "ArrowRight pans 80 px")
	assert.InDelta(t, 0, my, 1.5)
	assert.InDelta(t, s3.zoom, s4.zoom, 0.006)

	// 6. The pipeline: no errors, no re-ships, and a capture of where we ended.
	scenetest.Run(t, s, `
{"do":"wait","valueContains":"loading false","role":"label","settleMs":600}
{"do":"capture","text":"portolan-scene","comment":"the end state: stub tiles, overlays, the readout"}`)
	s5 := readCamera(t, s, "5")
	assert.Zero(t, s5.errors, "tile errors")
	assert.Zero(t, s5.reships, "basemap re-ships")
	assert.Equal(t, "false", s5.loading)
	assert.GreaterOrEqual(t, s5.loaded, float64(minTilesLoaded))
	st, err := os.Stat(filepath.Join(s.OutDir(), "portolan-scene.png"))
	require.NoError(t, err, "the capture was written")
	assert.NotZero(t, st.Size())
}
