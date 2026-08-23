package portolan

import "github.com/stergiotis/boxer/public/observability/eh"

// ClipPolygon clips a polygon ring to a rectangle (Sutherland–Hodgman, one
// edge at a time in Leaflet's order: left, bottom, right, top) and returns
// the clipped ring. round rounds the new vertices to whole pixels. The input
// slice is not modified.
func ClipPolygon(points []Point, bounds Bounds, round bool) []Point {
	codes := make([]int, len(points))
	for i, p := range points {
		codes[i] = bitCode(p, bounds)
	}
	for _, edge := range [...]int{codeLeft, codeBottom, codeRight, codeTop} {
		n := len(points)
		clipped := make([]Point, 0, n)
		clippedCodes := make([]int, 0, n)
		push := func(p Point, code int) {
			clipped = append(clipped, p)
			clippedCodes = append(clippedCodes, code)
		}
		for i, j := 0, n-1; i < n; j, i = i, i+1 {
			a, b := points[i], points[j]
			codeA, codeB := codes[i], codes[j]
			switch {
			case codeA&edge == 0:
				// a is inside this edge; if b was outside, the boundary
				// crossing comes first.
				if codeB&edge != 0 {
					p := edgeIntersection(b, a, edge, bounds, round)
					push(p, bitCode(p, bounds))
				}
				push(a, codeA)
			case codeB&edge == 0:
				// a is outside and b inside: only the crossing survives.
				p := edgeIntersection(b, a, edge, bounds, round)
				push(p, bitCode(p, bounds))
			}
		}
		points, codes = clipped, clippedCodes
	}
	return points
}

// PolygonCenter is the area centroid of a polygon ring in projected space,
// unprojected — what a label or a popup anchors to. A degenerate ring falls
// back to its first point.
func PolygonCenter(latlngs []LatLng, crs CRSI) (LatLng, error) {
	if len(latlngs) == 0 {
		return LatLng{}, eh.Errorf("portolan: latlngs not passed")
	}
	centroidLatLng, points := projectRelative(latlngs, crs)
	area, x, y := 0.0, 0.0, 0.0
	for i, j := 0, len(points)-1; i < len(points); j, i = i, i+1 {
		p1, p2 := points[i], points[j]
		f := p1.Y*p2.X - p2.Y*p1.X
		x += (p1.X + p2.X) * f
		y += (p1.Y + p2.Y) * f
		area += f * 3
	}
	var center Point
	if area == 0 {
		center = points[0]
	} else {
		center = Point{x / area, y / area}
	}
	c := crs.Unproject(center)
	return LatLng{c.Lat + centroidLatLng.Lat, c.Lng + centroidLatLng.Lng}, nil
}

// Centroid is the arithmetic mean of the points in degrees. An empty slice
// gives NaN coordinates (where Leaflet's centroid throws, constructing a
// LatLng from NaN); callers guard.
func Centroid(latlngs []LatLng) LatLng {
	latSum, lngSum, n := 0.0, 0.0, 0.0
	for _, l := range latlngs {
		latSum += l.Lat
		lngSum += l.Lng
		n++
	}
	return LatLng{latSum / n, lngSum / n}
}
