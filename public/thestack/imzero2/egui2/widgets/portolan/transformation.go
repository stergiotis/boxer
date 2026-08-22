package portolan

// Transformation is the affine map (x, y) ↦ (a·x + b, c·y + d), scaled, that
// takes projected coordinates to pixel coordinates (src/geometry/
// Transformation.js). A CRSI owns one.
type Transformation struct {
	A, B, C, D float64
}

// NewTransformation is the Transformation literal.
func NewTransformation(a, b, c, d float64) Transformation {
	return Transformation{A: a, B: b, C: c, D: d}
}

// Transform applies the map to p and multiplies by scale; a scale of 0 reads
// as 1, as Leaflet's `scale ||= 1` does.
func (t Transformation) Transform(p Point, scale float64) Point {
	if scale == 0 {
		scale = 1
	}
	return Point{scale * (t.A*p.X + t.B), scale * (t.C*p.Y + t.D)}
}

// Untransform inverts Transform.
func (t Transformation) Untransform(p Point, scale float64) Point {
	if scale == 0 {
		scale = 1
	}
	return Point{(p.X/scale - t.B) / t.A, (p.Y/scale - t.D) / t.C}
}
