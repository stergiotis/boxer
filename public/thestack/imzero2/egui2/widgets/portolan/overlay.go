package portolan

import (
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// The overlay helpers: what an app paints on the map through the Projector
// it receives in Render's callback. They are thin because the painter lane
// already is the renderer (ADR-0204 §SD2): a marker is a filled circle, a
// label a PaintText, a raster a paintImage pinned to geographic bounds. The
// polyline and the polygon carry Leaflet's vector pipeline — project, clip to
// the padded viewport, simplify — so a geometry far larger than the view
// costs what its visible part costs.

// overlayPadding is Leaflet's Renderer padding: geometry is clipped to the
// viewport grown by this fraction on each side, so a stroke does not pop at
// the edge while panning.
const overlayPadding = 0.1

// smoothFactor is Leaflet's Polyline smoothFactor, the Douglas–Peucker
// tolerance in pixels applied after clipping.
const smoothFactor = 1.0

// Marker paints a filled circle of radius px at a geographic point, with a
// hairline darker rim so it reads on any tile.
func (p Projector) Marker(ll LatLng, radius float32, col color.Color) {
	at := p.ToCanvas(ll)
	c.PaintCircleFilled(float32(at.X), float32(at.Y), radius, col).Send()
	c.PaintCircleStroke(float32(at.X), float32(at.Y), radius, color.Hex(0x00000080), styletokens.StrokeHair).Send()
}

// Label paints text anchored at a geographic point, offset by dx, dy pixels;
// anchorH 0/1/2 = left/centre/right, anchorV 0/1/2 = top/centre/bottom.
func (p Projector) Label(ll LatLng, dx, dy float32, anchorH, anchorV uint8, text string, fontSize float32, col color.Color) {
	at := p.ToCanvas(ll)
	c.PaintText(float32(at.X)+dx, float32(at.Y)+dy, anchorH, anchorV, text, fontSize, col).Send()
}

// Polyline paints a line through geographic points given as parallel
// latitude/longitude slices, the way Leaflet's Polyline draws: projected,
// clipped to the padded viewport segment by segment (Cohen–Sutherland, which
// can split the line into parts) and each part simplified at smoothFactor.
func (p Projector) Polyline(lats, lngs []float64, col color.Color, width float32) {
	pts := p.project(lats, lngs)
	if len(pts) < 2 {
		return
	}
	bounds := p.clipBounds()
	var clipper SegmentClipper
	var part []Point
	flush := func() {
		if len(part) >= 2 {
			paintPolyline(Simplify(part, smoothFactor), col, width)
		}
		part = part[:0]
	}
	for j := 0; j+1 < len(pts); j++ {
		a, b, ok := clipper.Clip(pts[j], pts[j+1], bounds, j != 0, true)
		if !ok {
			continue
		}
		part = append(part, a)
		// The segment leaves the viewport, or is the last one: the part ends.
		if b != pts[j+1] || j == len(pts)-2 {
			part = append(part, b)
			flush()
		}
	}
	flush()
}

// Polygon fills and strokes a ring of geographic points (parallel slices; a
// closing vertex equal to the first may be present or not), the way Leaflet's
// Polygon draws: projected, and — only when the ring is convex — simplified
// and clipped to the padded viewport grown by the stroke width. A concave
// ring is handed over whole, for the reason fillRing gives. The fill is ear-clipped, so a
// concave ring renders right; strokeWidth 0 draws the fill alone. Holes are
// not filled — stroke them as Polylines.
func (p Projector) Polygon(lats, lngs []float64, fill, stroke color.Color, strokeWidth float32) {
	p.polygon(lats, lngs, fill, stroke, strokeWidth, false)
}

// ConvexPolygon is Polygon for a ring known to be convex — an H3 cell, a
// circle, a rectangle: the fill is the painter's feathered fan, cheaper and
// antialiased where the ear-clipped mesh is not. A concave ring here renders
// artifacts; use Polygon.
func (p Projector) ConvexPolygon(lats, lngs []float64, fill, stroke color.Color, strokeWidth float32) {
	p.polygon(lats, lngs, fill, stroke, strokeWidth, true)
}

func (p Projector) polygon(lats, lngs []float64, fill, stroke color.Color, strokeWidth float32, convex bool) {
	pts := dropRepeats(p.project(lats, lngs))
	if len(pts) < 3 {
		return
	}
	// Leaflet grows the clip box by the stroke weight so the stroke never
	// shows along the clip edge.
	w := Point{float64(strokeWidth), float64(strokeWidth)}
	bounds := p.clipBounds()
	bounds = BoundsOf(bounds.Min.Subtract(w), bounds.Max.Add(w))
	clipped, ok := fillRing(pts, bounds, convex)
	if !ok {
		return
	}
	if convex {
		// Douglas–Peucker keeps a convex ring convex, so the vertex saving
		// is free there.
		clipped = Simplify(clipped, smoothFactor)
	}
	clipped = dropRepeats(clipped)
	if len(clipped) < 3 {
		return
	}
	// A ring with no area to speak of cannot show concavity, and the ear clip
	// is ill-conditioned on one: the feathered fan is bounded by the ring's
	// own points and robust where the triangulation is not.
	if !convex && absF(ringArea2(clipped)) < 2*minConcaveAreaPx {
		convex = true
	}
	xs, ys := toF32(clipped)
	f := c.PaintPolygonFilled(xs, ys, fill)
	if !convex {
		f = f.Concave()
	}
	if strokeWidth > 0 {
		f = f.Stroke(stroke, strokeWidth)
	}
	f.Send()
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

// A concave ring is not simplified either, for the same reason it is not
// clipped: Douglas–Peucker does not preserve simplicity. Dropping vertices
// from a simple polygon can make its edges cross — measured over the country
// outlines at world view, simplification took the self-intersecting rings
// from 5 to 10 — and an ear clipper has no defined answer for a ring that
// crosses itself. Which vertices survive depends on the view, so the crossing
// appears and disappears as the map moves, and with it the triangles the
// triangulation invents. Leaflet simplifies and fills the same rings without
// trouble because the canvas fill rule does not care.
//
// fillRing prepares the ring a filled polygon is built from, and reports
// whether there is anything to draw.
//
// A **convex** ring is clipped to the window: Sutherland–Hodgman is exact
// there — a convex polygon clipped against a convex window is convex — so the
// clip costs nothing in fidelity and saves the vertices outside the view.
//
// A **concave** ring is not clipped, and this is the one place the port parts
// from Leaflet deliberately. Sutherland–Hodgman inserts connector edges along
// the window boundary when the ring leaves and re-enters it, points that were
// never in the input: a ring straddling the bottom edge comes back closed
// along that edge, which for a geometry as wide as Antarctica is an edge the
// full width of the view. Leaflet clips the same way and never sees it,
// because it fills with the canvas nonzero rule, under which those zero-area
// excursions are invisible. This lane fills by ear-clipping the ring
// (earcutr, interpreter.rs), which turns each of them into a visible triangle
// — and because the surviving vertices shift with the view, into a different
// one every frame. Leaving the ring whole hands the clipping to the canvas
// clip the map already pushes, where it costs nothing to be right.
//
// The cheap half of what the clip bought is kept: a ring wholly outside the
// window is dropped before anything is projected further.
func fillRing(pts []Point, bounds Bounds, convex bool) (out []Point, ok bool) {
	if convex {
		out = ClipPolygon(pts, bounds, true)
		return out, len(out) >= 3
	}
	minX, minY, maxX, maxY := pts[0].X, pts[0].Y, pts[0].X, pts[0].Y
	for _, q := range pts[1:] {
		minX, maxX = min(minX, q.X), max(maxX, q.X)
		minY, maxY = min(minY, q.Y), max(maxY, q.Y)
	}
	if maxX < bounds.Min.X || minX > bounds.Max.X || maxY < bounds.Min.Y || minY > bounds.Max.Y {
		return nil, false
	}
	return pts, true
}

// minConcaveAreaPx is the ring area, in square canvas pixels, below which a
// fill is drawn as a convex fan rather than ear-clipped.
const minConcaveAreaPx = 1.0

// dropRepeats removes consecutive duplicate points, and the closing duplicate
// of a ring that ends on its own first point, returning a slice over the same
// array.
//
// Projection rounds to whole pixels — Leaflet's LatLngToLayerPoint does — so
// at low zoom most of a large outline's vertices land on the same pixel, and
// clipping adds more along the clip edge. Measured over the vendored country
// outlines at world view, about half the rings reaching the fill carried
// repeated vertices. A zero-length edge is nothing to a stroke and is not
// defined for the ear clipper, whose triangulation of such a ring emits stray
// slivers; because the projection shifts sub-pixel every frame, the slivers
// differ every frame, which is what reads as flicker.
func dropRepeats(pts []Point) []Point {
	if len(pts) == 0 {
		return pts
	}
	out := pts[:1]
	for _, q := range pts[1:] {
		if q != out[len(out)-1] {
			out = append(out, q)
		}
	}
	for len(out) >= 2 && out[0] == out[len(out)-1] {
		out = out[:len(out)-1]
	}
	return out
}

// ringArea2 is twice the signed area of a ring (the shoelace sum).
func ringArea2(pts []Point) float64 {
	var a float64
	for i := range pts {
		j := (i + 1) % len(pts)
		a += pts[i].X*pts[j].Y - pts[j].X*pts[i].Y
	}
	return a
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// clipBounds is the padded viewport in canvas pixels (Renderer._updateBounds).
func (p Projector) clipBounds() Bounds {
	size := p.view.Size()
	lo := size.MultiplyBy(-overlayPadding).Round()
	return BoundsOf(lo, lo.Add(size.MultiplyBy(1+2*overlayPadding)).Round())
}

// project turns parallel lat/lng slices into canvas points.
func (p Projector) project(lats, lngs []float64) []Point {
	n := min(len(lats), len(lngs))
	pts := make([]Point, n)
	for i := 0; i < n; i++ {
		pts[i] = p.ToCanvas(LL(lats[i], lngs[i]))
	}
	return pts
}

func toF32(pts []Point) (xs, ys []float32) {
	xs = make([]float32, len(pts))
	ys = make([]float32, len(pts))
	for i, pt := range pts {
		xs[i], ys[i] = float32(pt.X), float32(pt.Y)
	}
	return
}

func paintPolyline(pts []Point, col color.Color, width float32) {
	if len(pts) < 2 {
		return
	}
	xs, ys := toF32(pts)
	c.PaintPolyline(xs, ys, col, width).Send()
}
