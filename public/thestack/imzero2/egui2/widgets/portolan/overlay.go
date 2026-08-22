package portolan

import (
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The overlay helpers: what an app paints on the map through the Projector
// it receives in Render's callback. They are thin — a marker is a filled
// circle, a polyline a projected PaintPolyline, a raster a paintImage pinned
// to geographic bounds — because the painter lane already is the renderer
// (ADR-0204 §SD2); simplification and clipping of large geometries arrive
// with M4.

// Marker paints a filled circle of radius px at a geographic point, with a
// hairline darker rim so it reads on any tile.
func (p Projector) Marker(ll LatLng, radius float32, col color.Color) {
	at := p.ToCanvas(ll)
	c.PaintCircleFilled(float32(at.X), float32(at.Y), radius, col).Send()
	c.PaintCircleStroke(float32(at.X), float32(at.Y), radius, color.Hex(0x00000080), 1).Send()
}

// Label paints text anchored at a geographic point, offset by dx, dy pixels;
// anchorH 0/1/2 = left/centre/right, anchorV 0/1/2 = top/centre/bottom.
func (p Projector) Label(ll LatLng, dx, dy float32, anchorH, anchorV uint8, text string, fontSize float32, col color.Color) {
	at := p.ToCanvas(ll)
	c.PaintText(float32(at.X)+dx, float32(at.Y)+dy, anchorH, anchorV, text, fontSize, col).Send()
}

// Polyline paints a line through geographic points given as parallel
// latitude/longitude slices.
func (p Projector) Polyline(lats, lngs []float64, col color.Color, width float32) {
	n := len(lats)
	if len(lngs) < n {
		n = len(lngs)
	}
	if n < 2 {
		return
	}
	xs := make([]float32, n)
	ys := make([]float32, n)
	for i := 0; i < n; i++ {
		at := p.ToCanvas(LL(lats[i], lngs[i]))
		xs[i], ys[i] = float32(at.X), float32(at.Y)
	}
	c.PaintPolyline(xs, ys, col, width).Send()
}

// Image paints an RGBA raster pinned to geographic bounds — the play map's
// in-DB raster overlay. key names the raster for the send-once protocol:
// pixels ship when contentVersion changes or the host reports the texture
// starved, and an empty slice otherwise (ImageVersionTracker). Call the
// returned fluid's Opacity/Nearest as needed and Send it.
func (p Projector) Image(key string, bounds LatLngBounds, widthPx, heightPx uint32, contentVersion uint64, pixels []uint32) c.PaintImageFluid {
	nw := p.ToCanvas(bounds.GetNorthWest())
	se := p.ToCanvas(bounds.GetSouthEast())
	id := p.m.ids.PrepareStr("portolan-overlay-" + key).Derive()
	send := p.m.overlayTracker.PixelsToSendFor(key, id, contentVersion, pixels)
	return c.PaintImage(id, float32(nw.X), float32(nw.Y), float32(se.X), float32(se.Y), widthPx, heightPx, contentVersion, send)
}
