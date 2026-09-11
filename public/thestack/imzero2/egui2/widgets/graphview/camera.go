package graphview

// camera maps world units to canvas pixels: screen = world · zoom + pan.
// minZ and maxZ bound the zoom; zero takes the package defaults.
type camera struct {
	zoom float32
	panX float32
	panY float32
	minZ float32
	maxZ float32
}

const (
	minZoom = 0.01
	maxZoom = 100
)

// setLimits installs the zoom bounds (Options.ZoomMin / ZoomMax, zero for
// the defaults) and clamps the current zoom to them.
func (c *camera) setLimits(lo, hi float32) {
	if lo <= 0 {
		lo = minZoom
	}
	if hi <= 0 {
		hi = maxZoom
	}
	if hi < lo {
		hi = lo
	}
	c.minZ, c.maxZ = lo, hi
	c.zoom = c.clamp(c.zoom)
}

// clamp bounds a zoom to the limits, the defaults when none are set.
func (c *camera) clamp(z float32) float32 {
	lo, hi := c.minZ, c.maxZ
	if lo <= 0 {
		lo = minZoom
	}
	if hi <= 0 {
		hi = maxZoom
	}
	return min(max(z, lo), hi)
}

// same reports whether the view transform is unchanged, limits aside.
func (c camera) same(o camera) bool {
	return c.zoom == o.zoom && c.panX == o.panX && c.panY == o.panY
}

func (c *camera) toScreen(x, y float32) (sx, sy float32) {
	return x*c.zoom + c.panX, y*c.zoom + c.panY
}

func (c *camera) toWorld(sx, sy float32) (x, y float32) {
	return (sx - c.panX) / c.zoom, (sy - c.panY) / c.zoom
}

// zoomAround scales by factor keeping the canvas point (ax, ay) fixed.
func (c *camera) zoomAround(factor, ax, ay float32) {
	nz := c.clamp(c.zoom * factor)
	wx, wy := c.toWorld(ax, ay)
	c.zoom = nz
	c.panX = ax - wx*nz
	c.panY = ay - wy*nz
}

// fit frames the world box into a w×h canvas leaving pad of the canvas
// clear on each side, centred.
func (c *camera) fit(minX, minY, maxX, maxY, w, h, pad float32) {
	bw := max(maxX-minX, 1e-3)
	bh := max(maxY-minY, 1e-3)
	zx := w * (1 - 2*pad) / bw
	zy := h * (1 - 2*pad) / bh
	z := min(zx, zy)
	if !(z > 0) {
		z = 1
	}
	c.zoom = c.clamp(z)
	cx := (minX + maxX) / 2
	cy := (minY + maxY) / 2
	c.panX = w/2 - cx*c.zoom
	c.panY = h/2 - cy*c.zoom
}
