package camera

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

// These moved with the type out of graphview (ADR-0228 §SD5); the behaviour
// they pin is the behaviour that package had.

func TestFitCentresTheBox(t *testing.T) {
	var c Camera
	c.Fit(100, 100, 300, 200, 800, 400, 0.1)
	sx, sy := c.ToScreen(200, 150)
	require.InDelta(t, 400, sx, 1e-3)
	require.InDelta(t, 200, sy, 1e-3)
	// The tighter axis decides the zoom: 200 wide into 640, 100 tall into 320.
	require.InDelta(t, 3.2, c.Zoom, 1e-5)
}

func TestFitClampsToTheZoomRange(t *testing.T) {
	var c Camera
	c.Fit(0, 0, 10, 10, 4000, 4000, 0.1) // a single node in a huge canvas
	require.Equal(t, float32(DefaultMaxZoom), c.Zoom, "clamped, not reset to 1")
	sx, sy := c.ToScreen(5, 5)
	require.InDelta(t, 2000, sx, 1e-3)
	require.InDelta(t, 2000, sy, 1e-3)
}

func TestFitOnADegenerateBoxDoesNotDivideByZero(t *testing.T) {
	var c Camera
	c.Fit(5, 5, 5, 5, 800, 600, 0.1)
	require.Equal(t, float32(DefaultMaxZoom), c.Zoom)
	sx, sy := c.ToScreen(5, 5)
	require.InDelta(t, 400, sx, 1e-3)
	require.InDelta(t, 300, sy, 1e-3)
}

func TestZoomAroundKeepsTheAnchorFixed(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := Camera{
			Zoom: rapid.Float32Range(0.05, 20).Draw(t, "zoom"),
			PanX: rapid.Float32Range(-1000, 1000).Draw(t, "panX"),
			PanY: rapid.Float32Range(-1000, 1000).Draw(t, "panY"),
		}
		ax := rapid.Float32Range(0, 800).Draw(t, "ax")
		ay := rapid.Float32Range(0, 600).Draw(t, "ay")
		wx, wy := c.ToWorld(ax, ay)
		c.ZoomAround(rapid.Float32Range(0.5, 2).Draw(t, "factor"), ax, ay)
		sx, sy := c.ToScreen(wx, wy)
		tol := float64(1e-2 * max(1, math.Abs(float64(ax))+math.Abs(float64(c.PanX))))
		require.InDelta(t, float64(ax), float64(sx), tol)
		require.InDelta(t, float64(ay), float64(sy), tol)
	})
}

func TestLimitsTakeTheirDefaultsAndCollapseWhenInverted(t *testing.T) {
	var c Camera
	require.Equal(t, float32(DefaultMinZoom), c.Clamp(0.0001))
	require.Equal(t, float32(DefaultMaxZoom), c.Clamp(1e6))

	c = Camera{Zoom: 10, MinZoom: 5, MaxZoom: 1}
	require.Equal(t, float32(5), c.Clamp(10), "an inverted range collapses to the minimum")
	c.ClampZoom()
	require.Equal(t, float32(5), c.Zoom)
}

func TestTranslateAndSameView(t *testing.T) {
	a := Camera{Zoom: 2, PanX: 10, PanY: 20}
	b := a
	require.True(t, a.SameView(b))
	b.Translate(3, -4)
	require.False(t, a.SameView(b))
	require.Equal(t, [2]float32{13, 16}, [2]float32{b.PanX, b.PanY})
	// Limits are not part of the view.
	c := a
	c.MinZoom, c.MaxZoom = 0.5, 50
	require.True(t, a.SameView(c))
}

func TestToScreenAndToWorldRoundTrip(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		c := Camera{
			Zoom: rapid.Float32Range(0.05, 20).Draw(t, "zoom"),
			PanX: rapid.Float32Range(-1000, 1000).Draw(t, "panX"),
			PanY: rapid.Float32Range(-1000, 1000).Draw(t, "panY"),
		}
		x := rapid.Float32Range(-5000, 5000).Draw(t, "x")
		y := rapid.Float32Range(-5000, 5000).Draw(t, "y")
		sx, sy := c.ToScreen(x, y)
		rx, ry := c.ToWorld(sx, sy)
		tol := float64(1e-3 * max(1, math.Abs(float64(x))))
		require.InDelta(t, float64(x), float64(rx), tol)
		require.InDelta(t, float64(y), float64(ry), tol)
	})
}
