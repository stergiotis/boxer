package treemap

import (
	"math"
	"testing"
)

// =============================================================================
// hatchSegments — corner-trimmed ±45° hatch geometry (hatch.go)
// =============================================================================

// inCornerSquare reports whether (x, y) lies strictly inside one of the four
// rad-sized corner squares of a w×h rect (boundary points are allowed — a
// trimmed endpoint stops exactly on a square's edge).
func inCornerSquare(x, y, w, h, rad, eps float64) bool {
	inX := x < rad-eps || x > w-rad+eps
	inY := y < rad-eps || y > h-rad+eps
	return inX && inY
}

func TestHatchSegments_InBoundsAnd45Degree(t *testing.T) {
	for _, positive := range []bool{false, true} {
		for _, rad := range []float64{0, 3} {
			segs := hatchSegments(120, 80, 6, rad, positive)
			if len(segs) == 0 {
				t.Fatalf("positive=%v rad=%v: no segments", positive, rad)
			}
			for i, s := range segs {
				for _, p := range [][2]float64{{s.X0, s.Y0}, {s.X1, s.Y1}} {
					if p[0] < -1e-9 || p[0] > 120+1e-9 || p[1] < -1e-9 || p[1] > 80+1e-9 {
						t.Errorf("positive=%v rad=%v seg %d endpoint %v out of bounds", positive, rad, i, p)
					}
				}
				dx := s.X1 - s.X0
				dy := s.Y1 - s.Y0
				if math.Abs(math.Abs(dx)-math.Abs(dy)) > 1e-9 {
					t.Errorf("positive=%v rad=%v seg %d not 45°: dx=%v dy=%v", positive, rad, i, dx, dy)
				}
				if dx < 0.5 {
					t.Errorf("positive=%v rad=%v seg %d degenerate or reversed: dx=%v", positive, rad, i, dx)
				}
			}
		}
	}
}

func TestHatchSegments_TrimKeepsEndpointsOutOfCornerSquares(t *testing.T) {
	const w, h, rad = 120.0, 80.0, 3.0
	for _, positive := range []bool{false, true} {
		segs := hatchSegments(w, h, 6, rad, positive)
		for i, s := range segs {
			for _, p := range [][2]float64{{s.X0, s.Y0}, {s.X1, s.Y1}} {
				if inCornerSquare(p[0], p[1], w, h, rad, 1e-9) {
					t.Errorf("positive=%v seg %d endpoint %v inside a corner square", positive, i, p)
				}
			}
		}
	}
}

// Untrimmed endpoints all sit on the rect edges; with rad=0 the trim must be
// a no-op so the historical hatch look is preserved for square-cornered specs.
func TestHatchSegments_ZeroRadiusMatchesUntrimmed(t *testing.T) {
	a := hatchSegments(100, 60, 6, 0, false)
	b := hatchSegments(100, 60, 6, 3, false)
	if len(a) < len(b) {
		t.Fatalf("trimming must not add segments: rad0=%d rad3=%d", len(a), len(b))
	}
	for i, s := range a {
		onEdge := func(x, y float64) bool {
			return x == 0 || y == 0 || x == 100 || y == 60
		}
		if !onEdge(s.X0, s.Y0) || !onEdge(s.X1, s.Y1) {
			t.Errorf("rad=0 seg %d endpoints not on rect edges: %+v", i, s)
		}
	}
}

// A cell narrower than two radii skips trimming (the corner bands overlap);
// the segments must match the untrimmed ones instead of degenerating.
func TestHatchSegments_TinyCellSkipsTrim(t *testing.T) {
	a := hatchSegments(5, 5, 2, 0, false)
	b := hatchSegments(5, 5, 2, 3, false)
	if len(a) != len(b) {
		t.Fatalf("tiny cell: trimmed (%d) and untrimmed (%d) segment counts differ", len(b), len(a))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("tiny cell seg %d differs: %+v vs %+v", i, a[i], b[i])
		}
	}
}

// Lines that lie entirely within a corner square (the extreme corner lines
// that produced the visible overhang) are dropped rather than half-drawn.
func TestHatchSegments_CornerResidentLinesCulled(t *testing.T) {
	// spacing 2 with rad 3 puts the cc=2 line of a 100x60 cell fully inside
	// the top-left square ((0,2)-(2,0)); it must not survive the trim.
	segs := hatchSegments(100, 60, 2, 3, false)
	for i, s := range segs {
		if s.X0 < 3 && s.Y0 < 3 && s.X1 < 3 && s.Y1 < 3 {
			t.Errorf("seg %d lies inside the top-left corner square: %+v", i, s)
		}
	}
}

func TestCornerEscape(t *testing.T) {
	const w, h, rad = 100.0, 60.0, 3.0
	cases := []struct {
		name           string
		x, y, dx, dy   float64
		want           float64
		wantInfinitely bool
	}{
		{"outside any square", 50, 30, 1, 1, 0, false},
		{"on edge but not corner", 0, 30, 1, -1, 0, false},
		{"top-left escaping via x", 0, 2, 1, -1, 3, false},
		{"top-left min of both axes", 1, 2, 1, 1, 1, false},
		{"bottom-right escaping via x backwards", 100, 58, -1, 1, 3, false},
		{"top-left trapped", 0, 2, -1, -1, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := cornerEscape(tc.x, tc.y, w, h, rad, tc.dx, tc.dy)
			if tc.wantInfinitely {
				if !math.IsInf(got, 1) {
					t.Fatalf("want +Inf, got %v", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}
