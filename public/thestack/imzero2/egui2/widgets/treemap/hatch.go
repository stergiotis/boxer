package treemap

import "math"

// hatchSegment is one diagonal hatch line in cell-local coordinates.
type hatchSegment struct {
	X0, Y0, X1, Y1 float64
}

// hatchSegments computes the ±45° hatch lines for a w×h cell, trimmed so no
// segment enters the cell's corner cutouts. The cell Frame paints a rounded
// rect; untrimmed lines run to the rect corners and visibly overhang the
// rounded fill (most prominently where a hatched cell meets a dark
// background). A rectangular clip cannot follow the corner radii, so the
// trim is geometric.
//
// Lines are parameterized by cc = x + y for slope -1 (negative angle, the
// default); positive angles mirror Y. Both endpoints of every line lie on
// the rect edges, and at 45° a segment can only meet a corner region at an
// endpoint (never mid-span: a segment whose endpoints clear the corner
// bands stays clear of them), so trimming endpoints suffices.
//
// The trim region approximates each corner cutout with the enclosing
// cornerRad×cornerRad square: endpoints inside a square advance along the
// line until they leave it. The square overcuts the true arc cutout by
// rad²·(1−π/4) per corner (≈2 px² at the default radius 3) — at most one
// extreme line per corner is dropped that would have grazed the fill.
// Segments that cannot escape (entirely inside a corner) collapse and are
// culled by the sub-pixel length check. Cells smaller than 2·cornerRad per
// axis skip trimming: the corner bands overlap there, and at that size the
// hatch is a few pixels with no visible corner to protect.
func hatchSegments(w, h, spacing, cornerRad float64, positive bool) []hatchSegment {
	if spacing <= 0 || w <= 0 || h <= 0 {
		return nil
	}
	if w <= 2*cornerRad || h <= 2*cornerRad {
		cornerRad = 0
	}
	segs := make([]hatchSegment, 0, int((w+h)/spacing)+1)
	dy := -1.0
	if positive {
		dy = 1.0
	}
	for cc := 0.0; cc <= w+h; cc += spacing {
		x0 := math.Max(0, cc-h)
		x1 := math.Min(w, cc)
		var y0, y1 float64
		if positive {
			// slope +1: same parameterization with Y mirrored.
			y0 = h - math.Min(h, cc)
			y1 = h - math.Max(0, cc-w)
		} else {
			y0 = math.Min(h, cc)
			y1 = math.Max(0, cc-w)
		}
		if cornerRad > 0 {
			// Endpoint 0 advances along (+1, dy), endpoint 1 along (-1, -dy);
			// x strictly increases from endpoint 0 to endpoint 1, so crossed
			// endpoints after the trim surface as x1-x0 < 0.5 below.
			t0 := cornerEscape(x0, y0, w, h, cornerRad, 1, dy)
			x0 += t0
			y0 += dy * t0
			t1 := cornerEscape(x1, y1, w, h, cornerRad, -1, -dy)
			x1 -= t1
			y1 -= dy * t1
		}
		// 45° lines: |Δy| == |Δx|, so the x extent alone decides degeneracy.
		// The negated comparison also drops NaN/Inf from untrappable escapes.
		if !(x1-x0 >= 0.5) {
			continue
		}
		segs = append(segs, hatchSegment{X0: x0, Y0: y0, X1: x1, Y1: y1})
	}
	return segs
}

// cornerEscape returns how far a point must advance along the unit-per-axis
// direction (dx, dy) (|dx| = |dy| = 1) to leave the cornerRad-sized corner
// square containing it; 0 when the point is in no corner square. A corner
// square is the intersection of an outer x band and an outer y band, so
// escaping either band suffices — the result is the minimum over the axes
// whose movement leaves their band. +Inf when neither axis escapes (the
// movement only goes deeper or out of the rect); callers cull such lines.
func cornerEscape(x, y, w, h, rad, dx, dy float64) float64 {
	nearL := x < rad
	nearR := x > w-rad
	nearT := y < rad
	nearB := y > h-rad
	if !((nearL || nearR) && (nearT || nearB)) {
		return 0
	}
	escape := math.Inf(1)
	if nearL && dx > 0 {
		escape = math.Min(escape, rad-x)
	}
	if nearR && dx < 0 {
		escape = math.Min(escape, x-(w-rad))
	}
	if nearT && dy > 0 {
		escape = math.Min(escape, rad-y)
	}
	if nearB && dy < 0 {
		escape = math.Min(escape, y-(h-rad))
	}
	return escape
}
