package portolan

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

var cameraTestPoints = []LatLng{
	LL(47.3769, 8.5417), LL(47.4, 8.6), LL(-33.86, 151.2),
	LL(60.0, -120.0), LL(0, 0), LL(-85, 179), LL(85, -179),
}

func cameraTestView(z float64) *View {
	v := NewView(ViewOptions{})
	v.SetSize(Point{X: 1024, Y: 768})
	v.SetView(LL(47.3769, 8.5417), z)
	return v
}

// The mathematical claim (ADR-0228 §Context): because the CRS scale is a pure
// multiplier, the map's LatLng-to-container transform factors into one
// projection at a reference zoom followed by an isotropic scale and a
// translation. In float64 the two agree to within Leaflet's own rounding of
// layer points to whole pixels.
func TestTransformFactorsIntoScaleAndTranslation(t *testing.T) {
	const refZoom = 0.0
	for _, z := range []float64{0, 3, 7, 11, 14, 16, 18} {
		v := cameraTestView(z)
		s := v.ZoomScaleAt(z, refZoom)
		o := v.PixelOrigin()
		for _, ll := range cameraTestPoints {
			w := v.ProjectAt(ll, refZoom)
			want := v.LatLngToContainerPoint(ll)
			require.LessOrEqual(t, math.Abs(w.X*s-o.X-want.X), 1.0, "zoom %v %v: x", z, ll)
			require.LessOrEqual(t, math.Abs(w.Y*s-o.Y-want.Y), 1.0, "zoom %v %v: y", z, ll)
		}
	}
}

// View.Camera is that factorisation in float32. What it costs is the mantissa,
// and the cost is a property of the magnitude the projection reaches — which
// is why the method's doc sends a caller who needs exact placement at high
// zoom to layer points instead.
func TestCameraMatchesTheFactorisationWithinFloat32(t *testing.T) {
	const refZoom = 0.0
	for _, z := range []float64{0, 3, 7, 11, 14, 16, 18} {
		v := cameraTestView(z)
		s := v.ZoomScaleAt(z, refZoom)
		o := v.PixelOrigin()
		cm := v.Camera(refZoom)
		for _, ll := range cameraTestPoints {
			w := v.ProjectAt(ll, refZoom)
			gx, gy := cm.ToScreen(float32(w.X), float32(w.Y))
			// Two float32 terms, plus a pixel of slack for the rounding
			// above: the scaled world point, and the pan — which is the
			// pixel origin and is by far the larger, ~35 million at zoom 18.
			ulp := math.Pow(2, -23)
			tolX := 1.0 + math.Abs(w.X*s)*ulp + math.Abs(o.X)*ulp
			tolY := 1.0 + math.Abs(w.Y*s)*ulp + math.Abs(o.Y)*ulp
			require.LessOrEqual(t, math.Abs(float64(gx)-(w.X*s-o.X)), tolX, "zoom %v %v: x", z, ll)
			require.LessOrEqual(t, math.Abs(float64(gy)-(w.Y*s-o.Y)), tolY, "zoom %v %v: y", z, ll)
		}
	}
}

// Sub-pixel where it matters: at the zooms a city view uses, the float32
// camera is indistinguishable from the map's own arithmetic.
func TestCameraIsSubPixelBelowZoomFourteen(t *testing.T) {
	const refZoom = 0.0
	for _, z := range []float64{0, 4, 8, 12, 14} {
		v := cameraTestView(z)
		cm := v.Camera(refZoom)
		for _, ll := range []LatLng{LL(47.3769, 8.5417), LL(47.4, 8.6), LL(0, 0)} {
			w := v.ProjectAt(ll, refZoom)
			gx, gy := cm.ToScreen(float32(w.X), float32(w.Y))
			want := v.LatLngToContainerPoint(ll)
			require.LessOrEqual(t, math.Abs(float64(gx)-want.X), 1.0, "zoom %v %v: x", z, ll)
			require.LessOrEqual(t, math.Abs(float64(gy)-want.Y), 1.0, "zoom %v %v: y", z, ll)
		}
	}
}

// The recipe View.Camera's doc points at for exact placement at any zoom.
func TestLayerPointsSurviveFloat32AtEveryZoom(t *testing.T) {
	v := NewView(ViewOptions{})
	v.SetSize(Point{X: 1024, Y: 768})
	for _, z := range []float64{8, 12, 16, 18, 20} {
		v.SetView(LL(47.39, 8.56), z)
		for dlat := 0.0; dlat < 0.05; dlat += 0.01 {
			ll := LL(47.3769+dlat, 8.5417+dlat)
			lp := v.LatLngToLayerPoint(ll)
			require.Equal(t, lp.X, float64(float32(lp.X)), "zoom %v: layer point lost nothing", z)
			require.Equal(t, lp.Y, float64(float32(lp.Y)), "zoom %v", z)
		}
	}
}

// The limits the conversion hands out must not bind a real map scale.
func TestCameraLimitsDoNotBindAMapScale(t *testing.T) {
	v := NewView(ViewOptions{})
	v.SetSize(Point{X: 1024, Y: 768})
	v.SetView(LL(0, 0), 20)
	cm := v.Camera(0)
	require.Equal(t, cm.Zoom, cm.Clamp(cm.Zoom), "a zoom-20 scale is inside the bounds")
}
