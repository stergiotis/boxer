package portolan

import "math"

// CRSI is a coordinate reference system (src/geo/crs/*.js): a ProjectionI, the
// Transformation that takes its plane to pixels, the scale at each zoom, and
// the wrapping and distance rules of the space it models. Four are provided —
// EPSG3857 (the default), EPSG4326, EPSG3395 and Simple; there is no way to
// build one from outside the package yet, because nothing in this tree needs
// to (ADR-0204 §SD7).
type CRSI interface {
	// Code is the EPSG name, empty for Simple.
	Code() string
	Projection() ProjectionI
	Transformation() Transformation
	// LatLngToPoint projects and transforms to pixels at a zoom.
	LatLngToPoint(LatLng, float64) Point
	// PointToLatLng inverts LatLngToPoint.
	PointToLatLng(Point, float64) LatLng
	// Project goes to the projection's plane, in its units (metres for the
	// Mercators, degrees for LonLat).
	Project(LatLng) Point
	Unproject(Point) LatLng
	// Scale is the pixel size of the projected world at a zoom; Zoom inverts
	// it.
	Scale(zoom float64) float64
	Zoom(scale float64) float64
	// GetProjectedBounds is the world's pixel extent at a zoom; false for an
	// infinite CRS.
	GetProjectedBounds(zoom float64) (Bounds, bool)
	// Infinite reports a CRSI with no world edge to wrap or clamp at.
	Infinite() bool
	// WrapLng and WrapLat report the wrap range, when the axis wraps.
	WrapLng() (min, max float64, ok bool)
	WrapLat() (min, max float64, ok bool)
	// WrapLatLng brings a point into the wrap range; WrapLatLngBounds shifts
	// a bounds so its centre is.
	WrapLatLng(LatLng) LatLng
	WrapLatLngBounds(LatLngBounds) LatLngBounds
	// Distance between two points in the system's own units.
	Distance(a, b LatLng) float64
}

// earthMeanRadius is Leaflet's EarthCRS.R — the sphere DistanceTo measures
// on. It is not EarthRadius: the projections use the equatorial radius, the
// distance the mean one.
const earthMeanRadius = 6371000.0

// EarthDistance is the great-circle distance in metres between two points on
// a sphere of earthMeanRadius — the haversine form Leaflet's EarthCRS uses.
func EarthDistance(a, b LatLng) float64 {
	rad := math.Pi / 180
	lat1, lat2 := a.Lat*rad, b.Lat*rad
	sinDLat := math.Sin((b.Lat - a.Lat) * rad / 2)
	sinDLon := math.Sin((b.Lng - a.Lng) * rad / 2)
	h := sinDLat*sinDLat + math.Cos(lat1)*math.Cos(lat2)*sinDLon*sinDLon
	c := 2 * math.Atan2(math.Sqrt(h), math.Sqrt(1-h))
	return earthMeanRadius * c
}

func earthWrapLatLng(l LatLng) LatLng {
	return LatLng{l.Lat, wrapNum(l.Lng, -180, 180, true)}
}

func euclideanDistance(a, b LatLng) float64 {
	dx, dy := b.Lng-a.Lng, b.Lat-a.Lat
	return math.Sqrt(dx*dx + dy*dy)
}

// crs is the one implementation behind the four exported systems. Leaflet
// models these as static classes; here they are values built once.
type crs struct {
	code           string
	projection     ProjectionI
	transformation Transformation
	infinite       bool
	wrapLng        *[2]float64
	wrapLat        *[2]float64
	scale          func(zoom float64) float64
	zoom           func(scale float64) float64
	distance       func(a, b LatLng) float64
}

func defaultScale(zoom float64) float64       { return 256 * math.Pow(2, zoom) }
func defaultZoom(scale float64) float64       { return math.Log(scale/256) / math.Ln2 }
func simpleScale(zoom float64) float64        { return math.Pow(2, zoom) }
func simpleZoom(scale float64) float64        { return math.Log(scale) / math.Ln2 }
func (c *crs) Code() string                   { return c.code }
func (c *crs) Projection() ProjectionI        { return c.projection }
func (c *crs) Transformation() Transformation { return c.transformation }
func (c *crs) Project(l LatLng) Point         { return c.projection.Project(l) }
func (c *crs) Unproject(p Point) LatLng       { return c.projection.Unproject(p) }
func (c *crs) Scale(zoom float64) float64     { return c.scale(zoom) }
func (c *crs) Zoom(scale float64) float64     { return c.zoom(scale) }
func (c *crs) Infinite() bool                 { return c.infinite }
func (c *crs) Distance(a, b LatLng) float64   { return c.distance(a, b) }

func (c *crs) LatLngToPoint(l LatLng, zoom float64) Point {
	return c.transformation.Transform(c.projection.Project(l), c.scale(zoom))
}

func (c *crs) PointToLatLng(p Point, zoom float64) LatLng {
	return c.projection.Unproject(c.transformation.Untransform(p, c.scale(zoom)))
}

func (c *crs) GetProjectedBounds(zoom float64) (Bounds, bool) {
	if c.infinite {
		return Bounds{}, false
	}
	b := c.projection.Bounds()
	s := c.scale(zoom)
	return BoundsOf(c.transformation.Transform(b.Min, s), c.transformation.Transform(b.Max, s)), true
}

func (c *crs) WrapLng() (min, max float64, ok bool) {
	if c.wrapLng == nil {
		return 0, 0, false
	}
	return c.wrapLng[0], c.wrapLng[1], true
}

func (c *crs) WrapLat() (min, max float64, ok bool) {
	if c.wrapLat == nil {
		return 0, 0, false
	}
	return c.wrapLat[0], c.wrapLat[1], true
}

func (c *crs) WrapLatLng(l LatLng) LatLng {
	lat, lng := l.Lat, l.Lng
	if c.wrapLng != nil {
		lng = wrapNum(lng, c.wrapLng[0], c.wrapLng[1], true)
	}
	if c.wrapLat != nil {
		lat = wrapNum(lat, c.wrapLat[0], c.wrapLat[1], true)
	}
	return LatLng{lat, lng}
}

func (c *crs) WrapLatLngBounds(b LatLngBounds) LatLngBounds {
	center := b.GetCenter()
	newCenter := c.WrapLatLng(center)
	latShift := center.Lat - newCenter.Lat
	lngShift := center.Lng - newCenter.Lng
	if latShift == 0 && lngShift == 0 {
		return b
	}
	sw, ne := b.GetSouthWest(), b.GetNorthEast()
	return LatLngBoundsOf(
		LatLng{sw.Lat - latShift, sw.Lng - lngShift},
		LatLng{ne.Lat - latShift, ne.Lng - lngShift})
}

var earthWrapLng = [2]float64{-180, 180}

// mercatorTransformation is the EPSG:3857 / EPSG:3395 pixel transformation:
// the projected world, ±π·R metres on a side, mapped onto the unit square
// with y down.
var mercatorTransformation = func() Transformation {
	// Two steps through a variable, not one constant expression: Go folds a
	// constant expression exactly and rounds once, which puts 0.5/(π·R) one
	// ulp from the value JavaScript reaches by rounding Math.PI·R first, and
	// every EPSG:3857 pixel coordinate would inherit the difference.
	piR := math.Pi * EarthRadius
	s := 0.5 / piR
	return NewTransformation(s, 0.5, -s, 0.5)
}()

// EPSG3857 is Web Mercator — the CRSI of every raster tile server and the
// default everywhere in this package.
var EPSG3857 CRSI = &crs{
	code: "EPSG:3857", projection: SphericalMercator, transformation: mercatorTransformation,
	wrapLng: &earthWrapLng, scale: defaultScale, zoom: defaultZoom, distance: EarthDistance,
}

// EPSG900913 is EPSG3857 under its older name.
var EPSG900913 CRSI = &crs{
	code: "EPSG:900913", projection: SphericalMercator, transformation: mercatorTransformation,
	wrapLng: &earthWrapLng, scale: defaultScale, zoom: defaultZoom, distance: EarthDistance,
}

// EPSG4326 is plain latitude/longitude — the equirectangular CRSI, used by
// older tile sets and by WMS.
var EPSG4326 CRSI = &crs{
	code: "EPSG:4326", projection: LonLat, transformation: NewTransformation(1.0/180, 1, -1.0/180, 0.5),
	wrapLng: &earthWrapLng, scale: defaultScale, zoom: defaultZoom, distance: EarthDistance,
}

// EPSG3395 is the elliptical Mercator, rarely used by tile servers.
var EPSG3395 CRSI = &crs{
	code: "EPSG:3395", projection: Mercator, transformation: mercatorTransformation,
	wrapLng: &earthWrapLng, scale: defaultScale, zoom: defaultZoom, distance: EarthDistance,
}

// Simple is a flat pixel space — latitude is y, longitude is x, both in the
// source's own units, nothing wraps and the scale doubles per zoom from 1.
// It is what makes the widget a tiled viewer for any large raster.
var Simple CRSI = &crs{
	projection: LonLat, transformation: NewTransformation(1, 0, -1, 0),
	infinite: true, scale: simpleScale, zoom: simpleZoom, distance: euclideanDistance,
}
