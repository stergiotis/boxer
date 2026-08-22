package portolan

import "math"

// EarthRadius is the WGS84 equatorial radius in metres, the R of both
// Mercator projections.
const EarthRadius = 6378137.0

// SphericalMercatorMaxLatitude is where the spherical Mercator projection
// clamps: the latitude at which the projected map is square.
const SphericalMercatorMaxLatitude = 85.0511287798

// ProjectionI maps geographic coordinates to a flat plane and back
// (src/geo/projection/*.js); a CRSI pairs one with a Transformation to reach
// pixels. Bounds is the projected extent of the world, used to derive the
// tile range at each zoom.
type ProjectionI interface {
	Project(LatLng) Point
	Unproject(Point) LatLng
	Bounds() Bounds
}

// SphericalMercator is EPSG:3857's projection — the one every raster tile
// server uses. Latitudes beyond ±SphericalMercatorMaxLatitude clamp.
var SphericalMercator ProjectionI = sphericalMercator{}

type sphericalMercator struct{}

func (sphericalMercator) Project(ll LatLng) Point {
	d := math.Pi / 180
	lat := math.Max(math.Min(SphericalMercatorMaxLatitude, ll.Lat), -SphericalMercatorMaxLatitude)
	sin := math.Sin(lat * d)
	return Point{EarthRadius * ll.Lng * d, EarthRadius * math.Log((1+sin)/(1-sin)) / 2}
}

func (sphericalMercator) Unproject(p Point) LatLng {
	d := 180 / math.Pi
	return LatLng{(2*math.Atan(math.Exp(p.Y/EarthRadius)) - (math.Pi / 2)) * d, p.X * d / EarthRadius}
}

func (sphericalMercator) Bounds() Bounds {
	d := EarthRadius * math.Pi
	return BoundsOf(Point{-d, -d}, Point{d, d})
}

// LonLat is the equirectangular projection — longitude is x, latitude is y —
// used by EPSG:4326 and by Simple.
var LonLat ProjectionI = lonLat{}

type lonLat struct{}

func (lonLat) Project(ll LatLng) Point  { return Point{ll.Lng, ll.Lat} }
func (lonLat) Unproject(p Point) LatLng { return LatLng{p.Y, p.X} }
func (lonLat) Bounds() Bounds           { return BoundsOf(Point{-180, -90}, Point{180, 90}) }

// Mercator is the elliptical Mercator projection of EPSG:3395, on the WGS84
// ellipsoid.
var Mercator ProjectionI = mercator{}

// mercatorRMinor is the WGS84 polar radius.
const mercatorRMinor = 6356752.314245179

type mercator struct{}

func (mercator) Project(ll LatLng) Point {
	d := math.Pi / 180
	r := EarthRadius
	tmp := mercatorRMinor / r
	e := math.Sqrt(1 - tmp*tmp)
	y := ll.Lat * d
	con := e * math.Sin(y)
	ts := math.Tan(math.Pi/4-y/2) / math.Pow((1-con)/(1+con), e/2)
	y = -r * math.Log(math.Max(ts, 1e-10))
	return Point{ll.Lng * d * r, y}
}

func (mercator) Unproject(p Point) LatLng {
	d := 180 / math.Pi
	r := EarthRadius
	tmp := mercatorRMinor / r
	e := math.Sqrt(1 - tmp*tmp)
	ts := math.Exp(-p.Y / r)
	phi := math.Pi/2 - 2*math.Atan(ts)
	for i, dphi := 0, 0.1; i < 15 && math.Abs(dphi) > 1e-7; i++ {
		con := e * math.Sin(phi)
		con = math.Pow((1-con)/(1+con), e/2)
		dphi = math.Pi/2 - 2*math.Atan(ts*con) - phi
		phi += dphi
	}
	return LatLng{phi * d, p.X * d / r}
}

func (mercator) Bounds() Bounds {
	return BoundsOf(Point{-20037508.34279, -15496570.73972}, Point{20037508.34279, 18764656.23138})
}
