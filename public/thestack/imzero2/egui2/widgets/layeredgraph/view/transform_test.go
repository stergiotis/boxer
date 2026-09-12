package view

import (
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"

	cam "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/camera"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
)

// The transform this view composes had no test when it was arithmetic inline
// in Render; these pin it against that arithmetic, so the move to the shared
// camera (ADR-0228 §SD5) is verifiably a refactor.

// legacyTf is the expression Render carried before the camera was shared.
func legacyTf(scale, offX, offY, zoom, panX, panY float64, canvasW, canvasH float32) func(layeredgraph.Point) (float32, float32) {
	ccx, ccy := float64(canvasW)/2, float64(canvasH)/2
	escale := scale * zoom
	ox := (offX-ccx)*zoom + ccx + panX
	oy := (offY-ccy)*zoom + ccy + panY
	return func(p layeredgraph.Point) (float32, float32) {
		return float32(p.X*escale + ox), float32(p.Y*escale + oy)
	}
}

// sharedTf is what Render composes now.
func sharedTf(scale, offX, offY, zoom, panX, panY float64, canvasW, canvasH float32) (func(layeredgraph.Point) (float32, float32), float32) {
	ccx, ccy := float64(canvasW)/2, float64(canvasH)/2
	cm := cam.Camera{
		Zoom: float32(scale), PanX: float32(offX), PanY: float32(offY),
		MinZoom: unboundedMinZoom, MaxZoom: unboundedMaxZoom,
	}
	cm.ZoomAround(float32(zoom), float32(ccx), float32(ccy))
	cm.Translate(float32(panX), float32(panY))
	return func(p layeredgraph.Point) (float32, float32) {
		return cm.ToScreen(float32(p.X), float32(p.Y))
	}, cm.Zoom
}

func TestSharedCameraReproducesTheLegacyTransform(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		scale := rapid.Float64Range(0.01, 50).Draw(rt, "scale")
		offX := rapid.Float64Range(-500, 500).Draw(rt, "offX")
		offY := rapid.Float64Range(-500, 500).Draw(rt, "offY")
		// Render clamps the user zoom to this range before composing.
		zoom := rapid.Float64Range(0.2, 5).Draw(rt, "zoom")
		panX := rapid.Float64Range(-800, 800).Draw(rt, "panX")
		panY := rapid.Float64Range(-800, 800).Draw(rt, "panY")
		cw := rapid.Float32Range(64, 2000).Draw(rt, "cw")
		ch := rapid.Float32Range(64, 2000).Draw(rt, "ch")

		old := legacyTf(scale, offX, offY, zoom, panX, panY, cw, ch)
		nw, escale := sharedTf(scale, offX, offY, zoom, panX, panY, cw, ch)

		require.InDelta(t, scale*zoom, float64(escale), 1e-3*scale*zoom,
			"the composed scale is what the node and font sizes are multiplied by")

		for _, p := range []layeredgraph.Point{
			{X: 0, Y: 0}, {X: 10, Y: 10}, {X: 400, Y: 300}, {X: -120, Y: 90},
		} {
			ox, oy := old(p)
			nx, ny := nw(p)
			// float32 against float64 accumulation: scale the tolerance with
			// the magnitude the two arrive at.
			tol := 1e-2 * max(1, float64(abs32(ox)))
			require.InDelta(t, float64(ox), float64(nx), tol, "x at %v", p)
			tol = 1e-2 * max(1, float64(abs32(oy)))
			require.InDelta(t, float64(oy), float64(ny), tol, "y at %v", p)
		}
	})
}

func TestFitCentresTheLayoutInTheCanvas(t *testing.T) {
	lay := &layeredgraph.Layout{Width: 200, Height: 100}
	scale, offX, offY, cw, ch := fit(lay, 800, 400)
	require.Equal(t, float32(800), cw)
	require.Equal(t, float32(400), ch)
	// The tighter axis decides: 100 tall into 400 less the margin.
	require.InDelta(t, 4*(1-2*fitPad), scale, 1e-9)
	// Centred: the margins are equal on each side.
	require.InDelta(t, (800-200*scale)/2, offX, 1e-9)
	require.InDelta(t, (400-100*scale)/2, offY, 1e-9)
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}
