// Package camera is the 2D view transform shared by the painter-lane canvas
// widgets that pan and zoom over a world of their own: an isotropic scale and
// a translation, `screen = world · Zoom + Pan`, with the fit, the
// zoom-about-a-point and the limits that go with it (ADR-0228 §SD5).
//
// It was factored out of graphview, whose camera it is verbatim;
// layeredgraph/view composed the same arithmetic inline from its fit and its
// user pan and zoom. The slippy map is deliberately *not* a consumer:
// portolan carries Leaflet's view model, with a coordinate reference system,
// zoom snapping and wrapping that have no place here. It is a source instead
// — its transform factors into this one exactly, which is what lets a graph
// be drawn over a basemap (ADR-0228 §Context).
//
// The zero value is not a usable transform: Zoom 0 maps every world point
// onto Pan. Construct one with Fit, or set Zoom yourself.
package camera

// Default zoom limits, used when a Camera leaves MinZoom or MaxZoom zero.
const (
	DefaultMinZoom = 0.01
	DefaultMaxZoom = 100
)

// Camera maps world units to canvas pixels: screen = world · Zoom + Pan.
// MinZoom and MaxZoom bound Zoom wherever this package changes it — Fit,
// ZoomAround and ClampZoom; a caller assigning Zoom directly is trusted, as
// a host supplying a transform must be. Zero on either takes the default
// above.
type Camera struct {
	Zoom       float32
	PanX, PanY float32
	MinZoom    float32
	MaxZoom    float32
}

// ToScreen maps a world point to canvas pixels.
func (c Camera) ToScreen(x, y float32) (sx, sy float32) {
	return x*c.Zoom + c.PanX, y*c.Zoom + c.PanY
}

// ToWorld maps a canvas pixel to world units.
func (c Camera) ToWorld(sx, sy float32) (x, y float32) {
	return (sx - c.PanX) / c.Zoom, (sy - c.PanY) / c.Zoom
}

// Clamp bounds a zoom to the limits, the defaults when either is unset.
func (c Camera) Clamp(z float32) float32 {
	lo, hi := c.MinZoom, c.MaxZoom
	if lo <= 0 {
		lo = DefaultMinZoom
	}
	if hi <= 0 {
		hi = DefaultMaxZoom
	}
	if hi < lo {
		hi = lo
	}
	return min(max(z, lo), hi)
}

// ClampZoom brings the current zoom inside the limits, for a caller that has
// just changed them.
func (c *Camera) ClampZoom() { c.Zoom = c.Clamp(c.Zoom) }

// SameView reports whether the transform is unchanged, limits aside.
func (c Camera) SameView(o Camera) bool {
	return c.Zoom == o.Zoom && c.PanX == o.PanX && c.PanY == o.PanY
}

// ZoomAround scales by factor while keeping the canvas point (ax, ay) over
// the same world point.
func (c *Camera) ZoomAround(factor, ax, ay float32) {
	nz := c.Clamp(c.Zoom * factor)
	wx, wy := c.ToWorld(ax, ay)
	c.Zoom = nz
	c.PanX = ax - wx*nz
	c.PanY = ay - wy*nz
}

// Translate pans by a canvas-pixel offset, leaving the zoom alone.
func (c *Camera) Translate(dx, dy float32) {
	c.PanX += dx
	c.PanY += dy
}

// Fit frames the world box into a w×h canvas, centred, leaving pad of the
// canvas clear on each side as a fraction. A degenerate box is given a
// minimum extent rather than an infinite zoom, and the result is clamped, so
// a single point in a large canvas stops at MaxZoom instead of resetting.
func (c *Camera) Fit(minX, minY, maxX, maxY, w, h, pad float32) {
	bw := max(maxX-minX, 1e-3)
	bh := max(maxY-minY, 1e-3)
	zx := w * (1 - 2*pad) / bw
	zy := h * (1 - 2*pad) / bh
	z := min(zx, zy)
	if !(z > 0) {
		z = 1
	}
	c.Zoom = c.Clamp(z)
	cx := (minX + maxX) / 2
	cy := (minY + maxY) / 2
	c.PanX = w/2 - cx*c.Zoom
	c.PanY = h/2 - cy*c.Zoom
}
