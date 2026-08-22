package portolan

import (
	"math"
	"strconv"
)

// LatLng is a geographic point in degrees (src/geo/LatLng.js). Lng rather
// than Lon because the port keeps Leaflet's vocabulary for its own fields;
// the widget's API is free to say what it likes. No altitude — see doc.go.
type LatLng struct {
	Lat, Lng float64
}

// LL is the LatLng literal.
func LL(lat, lng float64) LatLng { return LatLng{Lat: lat, Lng: lng} }

// Equals reports equality within Leaflet's default margin of 1e-9 degrees.
func (l LatLng) Equals(o LatLng) bool { return l.EqualsWithin(o, 1.0e-9) }

// EqualsWithin reports whether neither coordinate differs by more than
// maxMargin.
func (l LatLng) EqualsWithin(o LatLng, maxMargin float64) bool {
	margin := math.Max(math.Abs(l.Lat-o.Lat), math.Abs(l.Lng-o.Lng))
	return margin <= maxMargin
}

// String renders "LatLng(lat, lng)" to six decimals.
func (l LatLng) String() string { return l.Format(6) }

// Format renders "LatLng(lat, lng)" to the given number of decimals.
func (l LatLng) Format(precision int) string {
	return "LatLng(" + fmtNum(l.Lat, precision) + ", " + fmtNum(l.Lng, precision) + ")"
}

// DistanceTo is the great-circle distance to o in metres, on Leaflet's
// spherical Earth (EarthDistance).
func (l LatLng) DistanceTo(o LatLng) float64 { return EarthDistance(l, o) }

// Wrap brings the longitude into [-180, 180]; the latitude is untouched.
func (l LatLng) Wrap() LatLng { return earthWrapLatLng(l) }

// ToBounds is the bounds of a square of sizeInMeters on a side centred on
// the point.
func (l LatLng) ToBounds(sizeInMeters float64) LatLngBounds {
	latAccuracy := 180 * sizeInMeters / 40075017
	lngAccuracy := latAccuracy / math.Cos((math.Pi/180)*l.Lat)
	return LatLngBoundsOf(
		LatLng{l.Lat - latAccuracy, l.Lng - lngAccuracy},
		LatLng{l.Lat + latAccuracy, l.Lng + lngAccuracy})
}

// LatLngBounds is a rectangle in geographic coordinates
// (src/geo/LatLngBounds.js), held as its south-west and north-east corners.
// The zero value is the empty bounds; IsValid reports false until the first
// Extend.
type LatLngBounds struct {
	SouthWest, NorthEast LatLng
	valid                bool
}

// NewLatLngBounds is the bounds of the given points; none gives the empty
// bounds.
func NewLatLngBounds(latlngs ...LatLng) (b LatLngBounds) {
	for _, l := range latlngs {
		b = b.Extend(l)
	}
	return
}

// LatLngBoundsOf is the bounds spanned by two corners, in any order.
func LatLngBoundsOf(a, b LatLng) LatLngBounds { return NewLatLngBounds(a, b) }

// IsValid reports whether the bounds has been given at least one point.
func (b LatLngBounds) IsValid() bool { return b.valid }

// Extend returns the bounds grown to include l.
func (b LatLngBounds) Extend(l LatLng) LatLngBounds {
	if !b.valid {
		return LatLngBounds{SouthWest: l, NorthEast: l, valid: true}
	}
	return LatLngBounds{
		SouthWest: LatLng{math.Min(l.Lat, b.SouthWest.Lat), math.Min(l.Lng, b.SouthWest.Lng)},
		NorthEast: LatLng{math.Max(l.Lat, b.NorthEast.Lat), math.Max(l.Lng, b.NorthEast.Lng)},
		valid:     true,
	}
}

// ExtendBounds returns the bounds grown to include o; an empty o changes
// nothing.
func (b LatLngBounds) ExtendBounds(o LatLngBounds) LatLngBounds {
	if !o.valid {
		return b
	}
	return b.Extend(o.SouthWest).Extend(o.NorthEast)
}

// Pad grows the bounds by ratio × its extent on every side.
func (b LatLngBounds) Pad(ratio float64) LatLngBounds {
	sw, ne := b.SouthWest, b.NorthEast
	heightBuffer := math.Abs(sw.Lat-ne.Lat) * ratio
	widthBuffer := math.Abs(sw.Lng-ne.Lng) * ratio
	return LatLngBoundsOf(
		LatLng{sw.Lat - heightBuffer, sw.Lng - widthBuffer},
		LatLng{ne.Lat + heightBuffer, ne.Lng + widthBuffer})
}

// GetCenter is the centre point.
func (b LatLngBounds) GetCenter() LatLng {
	return LatLng{(b.SouthWest.Lat + b.NorthEast.Lat) / 2, (b.SouthWest.Lng + b.NorthEast.Lng) / 2}
}

// GetSouthWest is the south-west corner.
func (b LatLngBounds) GetSouthWest() LatLng { return b.SouthWest }

// GetNorthEast is the north-east corner.
func (b LatLngBounds) GetNorthEast() LatLng { return b.NorthEast }

// GetNorthWest is the north-west corner.
func (b LatLngBounds) GetNorthWest() LatLng { return LatLng{b.GetNorth(), b.GetWest()} }

// GetSouthEast is the south-east corner.
func (b LatLngBounds) GetSouthEast() LatLng { return LatLng{b.GetSouth(), b.GetEast()} }

// GetWest is the west longitude.
func (b LatLngBounds) GetWest() float64 { return b.SouthWest.Lng }

// GetSouth is the south latitude.
func (b LatLngBounds) GetSouth() float64 { return b.SouthWest.Lat }

// GetEast is the east longitude.
func (b LatLngBounds) GetEast() float64 { return b.NorthEast.Lng }

// GetNorth is the north latitude.
func (b LatLngBounds) GetNorth() float64 { return b.NorthEast.Lat }

// Contains reports whether l lies inside or on the bounds.
func (b LatLngBounds) Contains(l LatLng) bool {
	return l.Lat >= b.SouthWest.Lat && l.Lat <= b.NorthEast.Lat &&
		l.Lng >= b.SouthWest.Lng && l.Lng <= b.NorthEast.Lng
}

// ContainsBounds reports whether o lies entirely inside or on the bounds.
func (b LatLngBounds) ContainsBounds(o LatLngBounds) bool {
	return o.SouthWest.Lat >= b.SouthWest.Lat && o.NorthEast.Lat <= b.NorthEast.Lat &&
		o.SouthWest.Lng >= b.SouthWest.Lng && o.NorthEast.Lng <= b.NorthEast.Lng
}

// Intersects reports whether the two rectangles share at least a point,
// touching edges included.
func (b LatLngBounds) Intersects(o LatLngBounds) bool {
	latIntersects := o.NorthEast.Lat >= b.SouthWest.Lat && o.SouthWest.Lat <= b.NorthEast.Lat
	lngIntersects := o.NorthEast.Lng >= b.SouthWest.Lng && o.SouthWest.Lng <= b.NorthEast.Lng
	return latIntersects && lngIntersects
}

// Overlaps reports whether the two rectangles share an area — touching edges
// excluded.
func (b LatLngBounds) Overlaps(o LatLngBounds) bool {
	latOverlaps := o.NorthEast.Lat > b.SouthWest.Lat && o.SouthWest.Lat < b.NorthEast.Lat
	lngOverlaps := o.NorthEast.Lng > b.SouthWest.Lng && o.SouthWest.Lng < b.NorthEast.Lng
	return latOverlaps && lngOverlaps
}

// ToBBoxString renders "west,south,east,north", the form WMS and friends
// take.
func (b LatLngBounds) ToBBoxString() string {
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	return f(b.GetWest()) + "," + f(b.GetSouth()) + "," + f(b.GetEast()) + "," + f(b.GetNorth())
}

// Equals reports whether both bounds are valid and their corners agree within
// LatLng's default margin.
func (b LatLngBounds) Equals(o LatLngBounds) bool { return b.EqualsWithin(o, 1.0e-9) }

// EqualsWithin is Equals with an explicit margin in degrees.
func (b LatLngBounds) EqualsWithin(o LatLngBounds, maxMargin float64) bool {
	return b.valid && o.valid &&
		b.SouthWest.EqualsWithin(o.SouthWest, maxMargin) &&
		b.NorthEast.EqualsWithin(o.NorthEast, maxMargin)
}
