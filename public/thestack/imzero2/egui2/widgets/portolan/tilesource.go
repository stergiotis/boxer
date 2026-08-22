package portolan

import (
	"math"
	"net/url"
	"strconv"
	"strings"
)

// TileCoords addresses one tile of one zoom level. X may lie outside the
// world's range when the map wraps: the pyramid keeps tiles by their
// unwrapped coordinates and asks the source for the wrapped ones.
type TileCoords struct {
	X, Y, Z int
}

// Key is the "x:y:z" form Leaflet keys its tile cache by.
func (c TileCoords) Key() string {
	return strconv.Itoa(c.X) + ":" + strconv.Itoa(c.Y) + ":" + strconv.Itoa(c.Z)
}

// TileSource describes where raster tiles come from — src/layer/tile/
// TileLayer.js's options, minus the DOM and retina detection. The zero value
// is unusable; NewTileSource fills in Leaflet's defaults.
type TileSource struct {
	// URLTemplate holds {z}, {x}, {y}, and optionally {s} (a subdomain),
	// {-y} (TMS row) and any key of Vars.
	URLTemplate string
	// Subdomains rotate through {s}; Leaflet's default is a, b, c.
	Subdomains []string
	// Vars are extra template keys.
	Vars map[string]string
	// Attribution is shown on the map; AttributionURL is where it links.
	Attribution, AttributionURL string

	MinZoom, MaxZoom float64
	// MinNativeZoom/MaxNativeZoom bound the tiles actually requested; beyond
	// them the nearest native level is scaled (HasMin/MaxNativeZoom guard).
	MinNativeZoom, MaxNativeZoom       int
	HasMinNativeZoom, HasMaxNativeZoom bool
	// TileSize in pixels, 256 by default.
	TileSize Point
	// ZoomOffset shifts {z}; ZoomReverse counts it down from MaxZoom; TMS
	// flips {y}.
	ZoomOffset  int
	ZoomReverse bool
	TMS         bool
	// Bounds, when valid, limits the tiles requested; NoWrap stops the map
	// repeating across the antimeridian.
	Bounds LatLngBounds
	NoWrap bool
	// ErrorTileURL, when set, is shown for tiles that failed to load.
	ErrorTileURL string
	// Opacity of the layer, 1 by default.
	Opacity float64
	// KeepBuffer is how many tiles beyond the viewport are kept loaded, 2
	// by default.
	KeepBuffer int
}

// NewTileSource applies Leaflet's defaults and TileLayer's option
// normalisation to a template: the OSM hosts get their attribution and https
// when given http, MaxZoom is kept at or above MinZoom (or the reverse under
// ZoomReverse), and a string of subdomain letters becomes one per letter.
func NewTileSource(urlTemplate string) TileSource {
	s := TileSource{
		URLTemplate: urlTemplate,
		Subdomains:  []string{"a", "b", "c"},
		MinZoom:     0,
		MaxZoom:     18,
		TileSize:    Point{256, 256},
		Opacity:     1,
		KeepBuffer:  2,
	}
	// The host is read off a parse with {s} stood in for — a brace is not a
	// host character to net/url, though it is to the WHATWG parser Leaflet
	// uses — and the scheme is upgraded on the template itself, since
	// re-serialising the parsed URL would percent-encode the braces.
	if u, err := url.Parse(strings.NewReplacer("{s}", "s").Replace(urlTemplate)); err == nil {
		host := u.Hostname()
		if strings.HasSuffix(host, "tile.openstreetmap.org") || strings.HasSuffix(host, "tile.osm.org") {
			if s.Attribution == "" {
				s.Attribution = "© OpenStreetMap contributors"
				s.AttributionURL = "https://www.openstreetmap.org/copyright"
			}
			if u.Scheme == "http" {
				s.URLTemplate = "https" + urlTemplate[len("http"):]
			}
		}
	}
	return s.Normalized()
}

// Normalized applies the zoom-order rule Leaflet applies at construction:
// MaxZoom ≥ MinZoom, or MinZoom ≤ MaxZoom under ZoomReverse.
func (s TileSource) Normalized() TileSource {
	if s.ZoomReverse {
		s.MinZoom = math.Min(s.MaxZoom, s.MinZoom)
	} else {
		s.MaxZoom = math.Max(s.MinZoom, s.MaxZoom)
	}
	if s.TileSize == (Point{}) {
		s.TileSize = Point{256, 256}
	}
	if s.Opacity == 0 {
		s.Opacity = 1
	}
	return s
}

// WithSubdomains sets {s}'s rotation from a string of letters or names.
func (s TileSource) WithSubdomains(subdomains string) TileSource {
	s.Subdomains = strings.Split(subdomains, "")
	return s
}

// ZoomForURL is the {z} a tile of tileZoom is requested with, after
// ZoomReverse and ZoomOffset.
func (s TileSource) ZoomForURL(tileZoom int) int {
	zoom := tileZoom
	if s.ZoomReverse {
		zoom = int(s.MaxZoom) - zoom
	}
	return zoom + s.ZoomOffset
}

// Subdomain picks {s} for a tile: Leaflet spreads consecutive tiles over the
// list by |x + y|.
func (s TileSource) Subdomain(c TileCoords) string {
	if len(s.Subdomains) == 0 {
		return ""
	}
	idx := c.X + c.Y
	if idx < 0 {
		idx = -idx
	}
	return s.Subdomains[idx%len(s.Subdomains)]
}

// URL renders the template for a (wrapped) tile. globalRows is the number of
// tile rows at the tile's zoom, used for {-y} and TMS; pass 0 for an infinite
// CRS, where neither is defined: {y} is then not flipped and {-y} is left in
// the URL as it is (Leaflet throws for it).
func (s TileSource) URL(c TileCoords, globalRows int) string {
	y := c.Y
	pairs := []string{
		"{s}", s.Subdomain(c),
		"{x}", strconv.Itoa(c.X),
		"{z}", strconv.Itoa(s.ZoomForURL(c.Z)),
		"{r}", "",
	}
	if globalRows > 0 {
		invertedY := globalRows - 1 - c.Y
		if s.TMS {
			y = invertedY
		}
		pairs = append(pairs, "{-y}", strconv.Itoa(invertedY))
	}
	pairs = append(pairs, "{y}", strconv.Itoa(y))
	for k, v := range s.Vars {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(s.URLTemplate)
}

// ClampZoom applies MinNativeZoom/MaxNativeZoom and rounds, which is what
// TileLayer does to the level it requests tiles for.
func (s TileSource) ClampZoom(zoom float64) int {
	z := int(jsRound(zoom))
	if s.HasMinNativeZoom && z < s.MinNativeZoom {
		return s.MinNativeZoom
	}
	if s.HasMaxNativeZoom && z > s.MaxNativeZoom {
		return s.MaxNativeZoom
	}
	return z
}
