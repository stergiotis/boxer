package portolancam

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// TestPixelMatchesEPSG3857 is what licenses the second implementation in
// pixel(): the oracle keeps its own projection so a fault in the widget's
// cannot cancel itself out of a gesture check, and this pins the two together
// so the copy cannot drift unnoticed.
func TestPixelMatchesEPSG3857(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		lat := rapid.Float64Range(-portolan.SphericalMercatorMaxLatitude, portolan.SphericalMercatorMaxLatitude).Draw(t, "lat")
		lon := rapid.Float64Range(-180, 180).Draw(t, "lon")
		zoom := rapid.Float64Range(0, 22).Draw(t, "zoom")

		x, y := pixel(lat, lon, zoom)
		p := portolan.EPSG3857.LatLngToPoint(portolan.LatLng{Lat: lat, Lng: lon}, zoom)
		// Both reach the same pixel through different arithmetic, so they
		// agree to float64 rounding, not to the bit — and the world is
		// 2^30 px across at zoom 22, where one ulp is already 1e-7 px.
		assert.InDelta(t, p.X, x, ulps(p.X))
		assert.InDelta(t, p.Y, y, ulps(p.Y))
	})
}

// ulps is the slack two orderings of the same float64 arithmetic earn at a
// magnitude: a few units in the last place, never less than the subpixel
// agreement the checks actually need.
func ulps(v float64) float64 { return max(1e-9, math.Abs(v)*1e-12) }

func TestCentreShiftIsPixelsAtTheFirstZoom(t *testing.T) {
	// One tile east at zoom 12 is 256 px, whatever the latitude.
	a := Readout{Lat: 51.0992, Lon: 17.0366, Zoom: 12}
	step := 360.0 / math.Pow(2, 12)
	b := Readout{Lat: a.Lat, Lon: a.Lon + step, Zoom: 12}
	dx, dy := centreShift(a, b)
	assert.InDelta(t, 256.0, dx, 1e-6)
	assert.InDelta(t, 0.0, dy, 1e-6)
}

// TestReadTree reads the four readout lines out of a dump shaped like the
// scene's: interleaved log lines, non-label nodes, and the labels in the order
// the demo paints them.
func TestReadTree(t *testing.T) {
	dump := `2026-09-17 warn something the host logged
{"role":"text_input","value":"portolan"}
{"role":"label","value":"centre 51.09920, 17.03660   zoom 12.00 bounds …"}
not json at all
{"role":"label","value":"canvas at 190.5,230.0 · 720 × 460 px"}
{"role":"label","value":"tiles: 20 requested · 18 loaded · 0 errors · loading false"}
{"role":"label","value":"basemap re-ships 0 this frame"}
`
	path := filepath.Join(t.TempDir(), "tree.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(dump), 0o600))

	r, seen, err := ReadTree(path)
	require.NoError(t, err)
	missing, ok := seen.Complete()
	require.True(t, ok, "incomplete: %s", missing)

	assert.InDelta(t, 51.0992, r.Lat, 1e-9)
	assert.InDelta(t, 17.0366, r.Lon, 1e-9)
	assert.InDelta(t, 12.0, r.Zoom, 1e-9)
	assert.InDelta(t, 190.5, r.OX, 1e-9)
	assert.InDelta(t, 230.0, r.OY, 1e-9)
	assert.Equal(t, 720, r.W)
	assert.Equal(t, 460, r.H)
	assert.Equal(t, 20, r.Requested)
	assert.Equal(t, 18, r.Loaded)
	assert.Equal(t, 0, r.Errors)
	assert.Equal(t, "false", r.Loading)
	assert.Equal(t, 0, r.Reships)
}

func TestReadTreeNamesTheMissingLine(t *testing.T) {
	dump := `{"role":"label","value":"centre 51.09920, 17.03660   zoom 12.00"}` + "\n"
	path := filepath.Join(t.TempDir(), "tree.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(dump), 0o600))

	_, seen, err := ReadTree(path)
	require.NoError(t, err)
	missing, ok := seen.Complete()
	assert.False(t, ok)
	assert.Equal(t, "canvas rect", missing)
}
