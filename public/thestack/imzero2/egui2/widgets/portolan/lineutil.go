package portolan

import (
	"math"

	"github.com/stergiotis/boxer/public/observability/eh"
)

// Simplify drops points from a polyline that a viewer would not miss — the
// radial-distance pass followed by Douglas–Peucker of src/geometry/
// LineUtil.js. tolerance is in pixels; 0 returns a copy unchanged. The input
// slice is not modified.
func Simplify(points []Point, tolerance float64) []Point {
	if tolerance == 0 || len(points) == 0 {
		return append([]Point(nil), points...)
	}
	sq := tolerance * tolerance
	return simplifyDP(reducePoints(points, sq), sq)
}

// PointToSegmentDistance is the distance from p to the segment p1–p2.
func PointToSegmentDistance(p, p1, p2 Point) float64 {
	_, sq := closestPointOnSegmentSq(p, p1, p2)
	return math.Sqrt(sq)
}

// ClosestPointOnSegment is the point of the segment p1–p2 nearest to p.
func ClosestPointOnSegment(p, p1, p2 Point) Point {
	q, _ := closestPointOnSegmentSq(p, p1, p2)
	return q
}

func simplifyDP(points []Point, sqTolerance float64) []Point {
	n := len(points)
	markers := make([]bool, n)
	markers[0], markers[n-1] = true, true
	simplifyDPStep(points, markers, sqTolerance, 0, n-1)
	out := make([]Point, 0, n)
	for i, keep := range markers {
		if keep {
			out = append(out, points[i])
		}
	}
	return out
}

func simplifyDPStep(points []Point, markers []bool, sqTolerance float64, first, last int) {
	maxSqDist, index := 0.0, -1
	for i := first + 1; i <= last-1; i++ {
		_, sq := closestPointOnSegmentSq(points[i], points[first], points[last])
		if sq > maxSqDist {
			index, maxSqDist = i, sq
		}
	}
	if maxSqDist > sqTolerance {
		markers[index] = true
		simplifyDPStep(points, markers, sqTolerance, first, index)
		simplifyDPStep(points, markers, sqTolerance, index, last)
	}
}

func reducePoints(points []Point, sqTolerance float64) []Point {
	reduced := []Point{points[0]}
	prev := 0
	for i := 1; i < len(points); i++ {
		if sqDist(points[i], points[prev]) > sqTolerance {
			reduced = append(reduced, points[i])
			prev = i
		}
	}
	if prev < len(points)-1 {
		reduced = append(reduced, points[len(points)-1])
	}
	return reduced
}

// SegmentClipper clips the consecutive segments of one polyline against a
// rectangle (Cohen–Sutherland), carrying the previous end point's region code
// from one call to the next so a polyline costs one code per vertex rather
// than two. Leaflet keeps that code in a module-level variable; here it is
// explicit state, one clipper per polyline.
type SegmentClipper struct {
	lastCode int
}

// Clip returns the part of a–b inside bounds. ok is false when nothing of the
// segment is inside. useLastCode reuses the code the previous call computed
// for its b — only valid when this a is that b. round rounds the intersection
// points to whole pixels.
func (c *SegmentClipper) Clip(a, b Point, bounds Bounds, useLastCode, round bool) (ca, cb Point, ok bool) {
	var codeA int
	if useLastCode {
		codeA = c.lastCode
	} else {
		codeA = bitCode(a, bounds)
	}
	codeB := bitCode(b, bounds)
	c.lastCode = codeB
	for {
		// both inside
		if codeA|codeB == 0 {
			return a, b, true
		}
		// both outside on the same side
		if codeA&codeB != 0 {
			return a, b, false
		}
		codeOut := codeA
		if codeOut == 0 {
			codeOut = codeB
		}
		p := edgeIntersection(a, b, codeOut, bounds, round)
		newCode := bitCode(p, bounds)
		if codeOut == codeA {
			a, codeA = p, newCode
		} else {
			b, codeB = p, newCode
		}
	}
}

// ClipSegment clips one segment on its own; see SegmentClipper for a polyline.
func ClipSegment(a, b Point, bounds Bounds, round bool) (ca, cb Point, ok bool) {
	var c SegmentClipper
	return c.Clip(a, b, bounds, false, round)
}

// Region codes, Cohen–Sutherland: a point outside the rectangle has one or
// two bits set naming the sides it is beyond.
const (
	codeLeft   = 1
	codeRight  = 2
	codeBottom = 4
	codeTop    = 8
)

func edgeIntersection(a, b Point, code int, bounds Bounds, round bool) Point {
	dx, dy := b.X-a.X, b.Y-a.Y
	min, max := bounds.Min, bounds.Max
	var p Point
	switch {
	case code&codeTop != 0:
		p = Point{a.X + dx*(max.Y-a.Y)/dy, max.Y}
	case code&codeBottom != 0:
		p = Point{a.X + dx*(min.Y-a.Y)/dy, min.Y}
	case code&codeRight != 0:
		p = Point{max.X, a.Y + dy*(max.X-a.X)/dx}
	case code&codeLeft != 0:
		p = Point{min.X, a.Y + dy*(min.X-a.X)/dx}
	}
	if round {
		p = p.Round()
	}
	return p
}

func bitCode(p Point, bounds Bounds) (code int) {
	switch {
	case p.X < bounds.Min.X:
		code |= codeLeft
	case p.X > bounds.Max.X:
		code |= codeRight
	}
	switch {
	case p.Y < bounds.Min.Y:
		code |= codeBottom
	case p.Y > bounds.Max.Y:
		code |= codeTop
	}
	return
}

func sqDist(p1, p2 Point) float64 {
	dx, dy := p2.X-p1.X, p2.Y-p1.Y
	return dx*dx + dy*dy
}

// closestPointOnSegmentSq is the point of p1–p2 nearest to p, and the squared
// distance to it.
func closestPointOnSegmentSq(p, p1, p2 Point) (q Point, sq float64) {
	x, y := p1.X, p1.Y
	dx, dy := p2.X-x, p2.Y-y
	if dot := dx*dx + dy*dy; dot > 0 {
		t := ((p.X-x)*dx + (p.Y-y)*dy) / dot
		if t > 1 {
			x, y = p2.X, p2.Y
		} else if t > 0 {
			x += dx * t
			y += dy * t
		}
	}
	dx, dy = p.X-x, p.Y-y
	return Point{x, y}, dx*dx + dy*dy
}

// PolylineCenter is the point halfway along a polyline by projected length —
// what a label or a popup anchors to. Small shapes (under 1700 m² of
// bounding box) are projected relative to their centroid first, which keeps
// the arithmetic precise near the poles and the antimeridian.
func PolylineCenter(latlngs []LatLng, crs CRSI) (LatLng, error) {
	if len(latlngs) == 0 {
		return LatLng{}, eh.Errorf("portolan: latlngs not passed")
	}
	centroidLatLng, points := projectRelative(latlngs, crs)
	n := len(points)
	halfDist := 0.0
	for i := 0; i < n-1; i++ {
		halfDist += points[i].DistanceTo(points[i+1]) / 2
	}
	var center Point
	if halfDist == 0 {
		center = points[0]
	} else {
		dist := 0.0
		for i := 0; i < n-1; i++ {
			p1, p2 := points[i], points[i+1]
			segDist := p1.DistanceTo(p2)
			dist += segDist
			if dist > halfDist {
				ratio := (dist - halfDist) / segDist
				center = Point{p2.X - ratio*(p2.X-p1.X), p2.Y - ratio*(p2.Y-p1.Y)}
				break
			}
		}
	}
	c := crs.Unproject(center)
	return LatLng{c.Lat + centroidLatLng.Lat, c.Lng + centroidLatLng.Lng}, nil
}

// projectRelative projects latlngs, shifted by their centroid when the shape
// is small enough for that to matter; the shift is returned so the result can
// be shifted back.
func projectRelative(latlngs []LatLng, crs CRSI) (centroidLatLng LatLng, points []Point) {
	bounds := NewLatLngBounds(latlngs...)
	areaBounds := bounds.GetNorthWest().DistanceTo(bounds.GetSouthWest()) *
		bounds.GetNorthEast().DistanceTo(bounds.GetNorthWest())
	if areaBounds < 1700 {
		centroidLatLng = Centroid(latlngs)
	}
	points = make([]Point, len(latlngs))
	for i, l := range latlngs {
		points[i] = crs.Project(LatLng{l.Lat - centroidLatLng.Lat, l.Lng - centroidLatLng.Lng})
	}
	return
}
