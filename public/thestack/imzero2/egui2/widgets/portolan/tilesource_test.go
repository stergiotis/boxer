// Port of Leaflet's spec/suites/layer/tile/TileLayerSpec.js at upstream commit
// c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9 — the part of TileLayer that
// TileSource carries: the URL template and its keys, the option normalisation
// at construction, the OSM host defaults and the zoom clamping. Each upstream
// `describe` is a Test function, each `it` a subtest named by its upstream
// title; the Go-only pins that follow them (TestTileSource_*) cover what the
// task asked for beyond upstream — TMS and {-y} across EPSG:3857, EPSG:4326
// and Simple, zoomReverse, ClampZoom, the subdomain rule — and say so.
//
// Harness: upstream adds the layer to a 400×400 map at [0, 0] zoom 2 and walks
// the <img> tiles of the layer's container in insertion order (`eachImg`).
// Here tileSpecRequestedURLs drives a Pyramid over a View with the same
// viewport and renders the URL of every tile the pyramid requests, in request
// order, from the wrapped coordinates and the pyramid's GlobalRows — which is
// what `createTile` → `getTileUrl` sees. Leaflet's tile queue is sorted by
// distance to the centre with a stable sort, and so is the pyramid's, so the
// order is the same.
//
// Not ported from upstream:
//   - number of kittens loaded › "Loads 8 kittens zoom 1", "Loads 224, unloads
//     209 kittens on MAD-TRD flyTo()": counts of <img> load events under a
//     fake clock and a flyTo animation; the pyramid's own counts are
//     GridLayerSpec's business (pyramid_test.go).
//   - crossOrigin option › "uses crossOrigin value undefined / true / '' /
//     anonymous / use-credentials" (five its): the <img> crossorigin
//     attribute — DOM.
//   - crossOrigin option › "sets min/maxZoom appropriately with detectRetina":
//     there is no retina detection in the port ({r} is always empty).
//   - #setUrl › "fires only one load event": TileSource has no setUrl/redraw,
//     and the assertion is on <img> load-event timing.
//
// Adapted rather than transliterated:
//   - "Does not replace {-y} on map with infinite CRS": upstream's
//     Util.template throws "No value provided for variable {-y}"; URL has no
//     error channel, so the port leaves the {-y} placeholder in the URL
//     untouched, and that is what is asserted.
//   - "adds OSM attribution if none are provided …": upstream's attribution is
//     an HTML anchor; the port keeps the text in Attribution and the link in
//     AttributionURL.
//   - "doesn't add OSM attribution if it's specifically set as empty":
//     NewTileSource takes no options, so an empty attribution is set on the
//     value afterwards, and the normalisation pass (Normalized) must not put
//     the OSM one back.
//   - "resets invalid min/maxZoom … without detectRetina": the options are
//     fields set after NewTileSource, and the rule is applied by Normalized.

package portolan

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tileSpecKitten stands in for upstream's data: URL of a kitten JPEG — a
// template with no keys and no host.
const tileSpecKitten = "data:image/jpeg;base64,/9j/4AAQSkZJRgABAQAAAQABAAD//gA7Q1JFQVRPUjogZ2QtanBlZyB2MS4w"

// tileSpecViewport is the 400×400 container of the url-template describe.
var tileSpecViewport = Point{400, 400}

// tileSpecRequestedURLs is the port's `eachImg`: the URL of every tile a
// pyramid over a view of that CRS, size, centre and zoom requests, in request
// order.
func tileSpecRequestedURLs(t *testing.T, src TileSource, crs CRSI, size Point, center LatLng, zoom float64) []string {
	t.Helper()
	v := NewView(ViewOptions{CRS: crs, Size: size})
	v.SetView(center, zoom)
	p := NewPyramid(src)
	var urls []string
	p.OnRequest = func(_, wrapped TileCoords) {
		urls = append(urls, src.URL(wrapped, p.GlobalRows()))
	}
	p.SetView(v, false, false)
	require.NotEmpty(t, urls, "no tiles requested")
	return urls
}

// tileSpecURLs is tileSpecRequestedURLs on the url-template describe's map: 400×400,
// EPSG:3857, [0, 0] at zoom 2 unless told otherwise.
func tileSpecURLs(t *testing.T, src TileSource, zoom float64) []string {
	t.Helper()
	return tileSpecRequestedURLs(t, src, EPSG3857, tileSpecViewport, LL(0, 0), zoom)
}

func TestTileLayer_URLTemplate(t *testing.T) {
	t.Run("replaces {y} with y coordinate", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{y}/{x}.png")
		assert.Equal(t, []string{
			"http://example.com/2/1/1.png",
			"http://example.com/2/1/2.png",
			"http://example.com/2/2/1.png",
			"http://example.com/2/2/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("replaces {-y} with inverse y coordinate", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{-y}/{x}.png")
		assert.Equal(t, []string{
			"http://example.com/2/2/1.png",
			"http://example.com/2/2/2.png",
			"http://example.com/2/1/1.png",
			"http://example.com/2/1/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("Does not replace {-y} on map with infinite CRS", func(t *testing.T) {
		// Upstream throws "No value provided for variable {-y}" when the layer
		// is added; the port has no value for it either and leaves the
		// placeholder in the URL.
		src := NewTileSource("http://example.com/{z}/{-y}/{x}.png")
		urls := tileSpecRequestedURLs(t, src, Simple, tileSpecViewport, LL(0, 0), 5)
		for _, u := range urls {
			assert.Contains(t, u, "{-y}", "no value is provided for {-y} on an infinite CRS")
		}
		assert.Equal(t, []string{
			"http://example.com/5/{-y}/-1.png",
			"http://example.com/5/{-y}/0.png",
			"http://example.com/5/{-y}/-1.png",
			"http://example.com/5/{-y}/0.png",
		}, urls)
	})

	t.Run("replaces {s} with [abc] by default", func(t *testing.T) {
		src := NewTileSource("http://{s}.example.com/{z}/{-y}/{x}.png")
		for _, u := range tileSpecURLs(t, src, 2) {
			assert.Contains(t, []string{"a", "b", "c"}, string(u[7]), "url %q", u)
		}
	})

	t.Run("replaces {s} with specified prefixes", func(t *testing.T) {
		src := NewTileSource("http://{s}.example.com/{z}/{-y}/{x}.png").WithSubdomains("qrs")
		for _, u := range tileSpecURLs(t, src, 2) {
			assert.Contains(t, []string{"q", "r", "s"}, string(u[7]), "url %q", u)
		}
	})

	t.Run("uses zoomOffset option", func(t *testing.T) {
		// Map view is at zoom 2: zoom 2 + zoomOffset 1 => z 3 in URL.
		src := NewTileSource("http://example.com/{z}/{y}/{x}.png")
		src.ZoomOffset = 1
		assert.Equal(t, []string{
			"http://example.com/3/1/1.png",
			"http://example.com/3/1/2.png",
			"http://example.com/3/2/1.png",
			"http://example.com/3/2/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("uses negative zoomOffset option", func(t *testing.T) {
		// Map view is at zoom 2: zoom 2 + zoomOffset -3 => z -1 in URL.
		src := NewTileSource("http://example.com/{z}/{y}/{x}.png")
		src.ZoomOffset = -3
		assert.Equal(t, []string{
			"http://example.com/-1/1/1.png",
			"http://example.com/-1/1/2.png",
			"http://example.com/-1/2/1.png",
			"http://example.com/-1/2/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("supports relative tile URLs", func(t *testing.T) {
		// Upstream checks that the browser-resolved img.src ends in the
		// relative path; the port has no base to resolve against, so the
		// template is rendered as given.
		src := NewTileSource("./tiles/{z}/{y}/{x}.png")
		urls := tileSpecURLs(t, src, 2)
		assert.Equal(t, []string{
			"./tiles/2/1/1.png",
			"./tiles/2/1/2.png",
			"./tiles/2/2/1.png",
			"./tiles/2/2/2.png",
		}, urls)
		for i, want := range []string{"/tiles/2/1/1.png", "/tiles/2/1/2.png", "/tiles/2/2/1.png", "/tiles/2/2/2.png"} {
			assert.Contains(t, urls[i], want)
		}
	})

	t.Run("adds OSM attribution if none are provided and is using OSM tiles", func(t *testing.T) {
		// Uses OSM tiles without providing attribution.
		src := NewTileSource("https://tile.openstreetmap.org/{z}/{x}/{y}.png")
		assert.Equal(t, "© OpenStreetMap contributors", src.Attribution)
		assert.Equal(t, "https://www.openstreetmap.org/copyright", src.AttributionURL)
	})

	t.Run("doesn't add OSM attribution if it's specifically set as empty", func(t *testing.T) {
		src := NewTileSource("https://tile.openstreetmap.org/{z}/{x}/{y}.png")
		src.Attribution, src.AttributionURL = "", ""
		src = src.Normalized()
		assert.Equal(t, "", src.Attribution)
		assert.Equal(t, "", src.AttributionURL)
	})

	t.Run("changes OSM URL to https", func(t *testing.T) {
		layerOpenStreetMap := NewTileSource("http://tile.openstreetmap.org/{z}/{x}/{y}.png")
		assert.True(t, strings.HasPrefix(layerOpenStreetMap.URLTemplate, "https://"), layerOpenStreetMap.URLTemplate)

		layerOSM := NewTileSource("http://tile.osm.org/{z}/{x}/{y}.png")
		assert.True(t, strings.HasPrefix(layerOSM.URLTemplate, "https://"), layerOSM.URLTemplate)

		layerOther := NewTileSource("http://someotherurl.org/{z}/{x}/{y}.png")
		assert.True(t, strings.HasPrefix(layerOther.URLTemplate, "http://"), layerOther.URLTemplate)

		// Beyond upstream: the upgrade must leave the template's keys intact
		// (a re-serialised URL would percent-encode the braces).
		assert.Equal(t, "https://tile.openstreetmap.org/{z}/{x}/{y}.png", layerOpenStreetMap.URLTemplate)
		assert.Equal(t, "https://tile.osm.org/{z}/{x}/{y}.png", layerOSM.URLTemplate)
		assert.Equal(t, "http://someotherurl.org/{z}/{x}/{y}.png", layerOther.URLTemplate)
	})

	t.Run("requests tiles with an integer {z} when the map's zoom level is fractional", func(t *testing.T) {
		// zoomSnap 0 is the View's default, so the view sits at 2.3; the
		// tiles come from round(2.3) = 2.
		src := NewTileSource("http://example.com/{z}/{y}/{x}.png")
		assert.Equal(t, []string{
			"http://example.com/2/1/1.png",
			"http://example.com/2/1/2.png",
			"http://example.com/2/2/1.png",
			"http://example.com/2/2/2.png",
		}, tileSpecURLs(t, src, 2.3))
	})

	t.Run("consults options.foo for {foo}", func(t *testing.T) {
		src := NewTileSource("https://tile.openstreetmap.org/{z}/{x}/{y}.png?{foo}")
		src.Vars = map[string]string{"foo": "bar"}
		assert.Equal(t, []string{
			"https://tile.openstreetmap.org/2/1/1.png?bar",
			"https://tile.openstreetmap.org/2/2/1.png?bar",
			"https://tile.openstreetmap.org/2/1/2.png?bar",
			"https://tile.openstreetmap.org/2/2/2.png?bar",
		}, tileSpecURLs(t, src, 2.3))
	})
}

func TestTileLayer_CrossOriginOption(t *testing.T) {
	t.Run("resets invalid min/maxZoom to allow for tiles to be loaded without detectRetina", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		// invalid min/maxZoom
		src.MaxZoom, src.MinZoom = 9, 10
		src = src.Normalized()
		// zooms should be identical so that we can load tiles for the given
		// zoom level
		assert.Equal(t, src.MinZoom, src.MaxZoom)
		assert.Equal(t, 10.0, src.MaxZoom)
	})
}

// ---- beyond upstream ---------------------------------------------------------
//
// TileLayerSpec.js has no it for tms, zoomReverse, the native-zoom clamp or a
// map on another CRS; the pins below cover those for the port. Expected values
// are derived by hand from the upstream arithmetic, not read back from the
// code: at zoom 2 the 400×400 viewport at [0, 0] spans tiles x, y ∈ {1, 2}
// on EPSG:3857 (the world is 4×4 tiles) and x ∈ {3, 4}, y ∈ {1, 2} on
// EPSG:4326 (the world is 8×4 tiles); on Simple at zoom 5 it spans
// x, y ∈ {-1, 0} and the world is unbounded.

func TestTileSource_URLAcrossCRS(t *testing.T) {
	t.Run("tms option flips {y} on EPSG:3857", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.TMS = true
		assert.Equal(t, []string{
			"http://example.com/2/1/2.png",
			"http://example.com/2/2/2.png",
			"http://example.com/2/1/1.png",
			"http://example.com/2/2/1.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("replaces {y} on EPSG:4326", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		assert.Equal(t, []string{
			"http://example.com/2/3/1.png",
			"http://example.com/2/4/1.png",
			"http://example.com/2/3/2.png",
			"http://example.com/2/4/2.png",
		}, tileSpecRequestedURLs(t, src, EPSG4326, tileSpecViewport, LL(0, 0), 2))
	})

	t.Run("replaces {-y} on EPSG:4326 from its 4 rows", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{-y}.png")
		assert.Equal(t, []string{
			"http://example.com/2/3/2.png",
			"http://example.com/2/4/2.png",
			"http://example.com/2/3/1.png",
			"http://example.com/2/4/1.png",
		}, tileSpecRequestedURLs(t, src, EPSG4326, tileSpecViewport, LL(0, 0), 2))
	})

	t.Run("tms option flips {y} on EPSG:4326", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.TMS = true
		assert.Equal(t, []string{
			"http://example.com/2/3/2.png",
			"http://example.com/2/4/2.png",
			"http://example.com/2/3/1.png",
			"http://example.com/2/4/1.png",
		}, tileSpecRequestedURLs(t, src, EPSG4326, tileSpecViewport, LL(0, 0), 2))
	})

	t.Run("tms option leaves {y} alone on an infinite CRS", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.TMS = true
		assert.Equal(t, []string{
			"http://example.com/5/-1/-1.png",
			"http://example.com/5/0/-1.png",
			"http://example.com/5/-1/0.png",
			"http://example.com/5/0/0.png",
		}, tileSpecRequestedURLs(t, src, Simple, tileSpecViewport, LL(0, 0), 5))
	})

	t.Run("requests wrapped {x} across the antimeridian", func(t *testing.T) {
		// At zoom 1 the world is two tiles wide; a 1024-px-wide viewport at
		// [0, 0] spans x ∈ {-1, 0, 1, 2}, and -1 and 2 are asked for as 1
		// and 0.
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		assert.ElementsMatch(t, []string{
			"http://example.com/1/0/0.png", "http://example.com/1/1/0.png",
			"http://example.com/1/0/1.png", "http://example.com/1/1/1.png",
			"http://example.com/1/0/0.png", "http://example.com/1/1/0.png",
			"http://example.com/1/0/1.png", "http://example.com/1/1/1.png",
		}, tileSpecRequestedURLs(t, src, EPSG3857, Point{1024, 256}, LL(0, 0), 1))
	})

	t.Run("leaves {r} empty (no retina detection)", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}{r}.png")
		assert.Equal(t, "http://example.com/2/1/1.png", src.URL(TileCoords{1, 1, 2}, 4))
	})

	t.Run("renders {-y} from the rows given to URL", func(t *testing.T) {
		src := NewTileSource("{z}/{x}/{y}/{-y}")
		assert.Equal(t, "3/5/2/5", src.URL(TileCoords{5, 2, 3}, 1<<3))
		assert.Equal(t, "3/5/2/{-y}", src.URL(TileCoords{5, 2, 3}, 0))
		src.TMS = true
		assert.Equal(t, "3/5/5/5", src.URL(TileCoords{5, 2, 3}, 1<<3))
		assert.Equal(t, "3/5/2/{-y}", src.URL(TileCoords{5, 2, 3}, 0))
	})
}

func TestTileSource_ZoomForURL(t *testing.T) {
	t.Run("zoomReverse counts {z} down from maxZoom", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.ZoomReverse = true
		assert.Equal(t, 16, src.ZoomForURL(2))
		assert.Equal(t, []string{
			"http://example.com/16/1/1.png",
			"http://example.com/16/2/1.png",
			"http://example.com/16/1/2.png",
			"http://example.com/16/2/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("zoomOffset applies after zoomReverse", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.ZoomReverse, src.ZoomOffset = true, 1
		assert.Equal(t, 17, src.ZoomForURL(2))
		src.MaxZoom = 10
		assert.Equal(t, 9, src.ZoomForURL(2))
	})

	t.Run("zoomOffset alone shifts {z}", func(t *testing.T) {
		src := NewTileSource("http://example.com/{z}/{x}/{y}.png")
		src.ZoomOffset = -3
		assert.Equal(t, -1, src.ZoomForURL(2))
		src.ZoomOffset = 0
		assert.Equal(t, 2, src.ZoomForURL(2))
	})
}

func TestTileSource_Normalized(t *testing.T) {
	t.Run("applies Leaflet's defaults", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		assert.Equal(t, tileSpecKitten, src.URLTemplate)
		assert.Equal(t, []string{"a", "b", "c"}, src.Subdomains)
		assert.Equal(t, 0.0, src.MinZoom)
		assert.Equal(t, 18.0, src.MaxZoom)
		assert.Equal(t, Point{256, 256}, src.TileSize)
		assert.Equal(t, 1.0, src.Opacity)
		assert.Equal(t, 2, src.KeepBuffer)
		assert.Equal(t, 0, src.ZoomOffset)
		assert.False(t, src.TMS)
		assert.False(t, src.ZoomReverse)
		assert.False(t, src.NoWrap)
		assert.False(t, src.HasMinNativeZoom)
		assert.False(t, src.HasMaxNativeZoom)
		assert.False(t, src.Bounds.IsValid())
		assert.Equal(t, "", src.ErrorTileURL)
		assert.Equal(t, "", src.Attribution)
	})

	t.Run("keeps maxZoom at or above minZoom", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		src.MinZoom, src.MaxZoom = 10, 9
		src = src.Normalized()
		assert.Equal(t, 10.0, src.MinZoom)
		assert.Equal(t, 10.0, src.MaxZoom)
	})

	t.Run("keeps minZoom at or below maxZoom under zoomReverse", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		src.ZoomReverse = true
		src.MinZoom, src.MaxZoom = 10, 9
		src = src.Normalized()
		assert.Equal(t, 9.0, src.MinZoom)
		assert.Equal(t, 9.0, src.MaxZoom)
	})

	t.Run("leaves a valid zoom order alone", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		src.MinZoom, src.MaxZoom = 3, 7
		src = src.Normalized()
		assert.Equal(t, 3.0, src.MinZoom)
		assert.Equal(t, 7.0, src.MaxZoom)
		src.ZoomReverse = true
		src = src.Normalized()
		assert.Equal(t, 3.0, src.MinZoom)
		assert.Equal(t, 7.0, src.MaxZoom)
	})

	t.Run("splits a string of subdomain letters", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten).WithSubdomains("qrs")
		assert.Equal(t, []string{"q", "r", "s"}, src.Subdomains)
	})
}

func TestTileSource_OSMHosts(t *testing.T) {
	t.Run("detects the OSM host behind {s}", func(t *testing.T) {
		src := NewTileSource("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png")
		assert.Equal(t, "© OpenStreetMap contributors", src.Attribution)
		assert.Equal(t, "https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", src.URLTemplate)
	})

	t.Run("upgrades an OSM subdomain host to https without touching the keys", func(t *testing.T) {
		src := NewTileSource("http://a.tile.osm.org/{z}/{x}/{y}.png")
		assert.Equal(t, "https://a.tile.osm.org/{z}/{x}/{y}.png", src.URLTemplate)
		assert.Equal(t, "© OpenStreetMap contributors", src.Attribution)
		assert.Equal(t, "https://www.openstreetmap.org/copyright", src.AttributionURL)
	})

	t.Run("leaves other hosts alone", func(t *testing.T) {
		src := NewTileSource("http://{s}.example.com/{z}/{x}/{y}.png")
		assert.Equal(t, "http://{s}.example.com/{z}/{x}/{y}.png", src.URLTemplate)
		assert.Equal(t, "", src.Attribution)
		assert.Equal(t, "", src.AttributionURL)
	})
}

func TestTileSource_Subdomain(t *testing.T) {
	t.Run("spreads tiles over the list by |x + y|", func(t *testing.T) {
		src := NewTileSource("http://{s}.example.com/{z}/{x}/{y}.png")
		assert.Equal(t, "c", src.Subdomain(TileCoords{1, 1, 2}))
		assert.Equal(t, "a", src.Subdomain(TileCoords{2, 1, 2}))
		assert.Equal(t, "a", src.Subdomain(TileCoords{1, 2, 2}))
		assert.Equal(t, "b", src.Subdomain(TileCoords{2, 2, 2}))
		assert.Equal(t, "c", src.Subdomain(TileCoords{-1, -1, 2}))
		assert.Equal(t, "b", src.Subdomain(TileCoords{-1, 0, 2}))
		assert.Equal(t, []string{
			"http://c.example.com/2/1/1.png",
			"http://a.example.com/2/2/1.png",
			"http://a.example.com/2/1/2.png",
			"http://b.example.com/2/2/2.png",
		}, tileSpecURLs(t, src, 2))
	})

	t.Run("takes names as well as letters", func(t *testing.T) {
		src := NewTileSource("http://{s}.example.com/{z}/{x}/{y}.png")
		src.Subdomains = []string{"tiles-1", "tiles-2"}
		assert.Equal(t, "http://tiles-1.example.com/2/1/1.png", src.URL(TileCoords{1, 1, 2}, 4))
		assert.Equal(t, "http://tiles-2.example.com/2/2/1.png", src.URL(TileCoords{2, 1, 2}, 4))
	})
}

func TestTileSource_ClampZoom(t *testing.T) {
	t.Run("returns maxNativeZoom when the zoom is larger", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		src.MaxNativeZoom, src.HasMaxNativeZoom = 5, true
		assert.Equal(t, 5, src.ClampZoom(10))
		assert.Equal(t, 5, src.ClampZoom(5.4))
		assert.Equal(t, 4, src.ClampZoom(4))
	})

	t.Run("returns minNativeZoom when the zoom is smaller", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		src.MinNativeZoom, src.HasMinNativeZoom = 5, true
		assert.Equal(t, 5, src.ClampZoom(3))
		assert.Equal(t, 5, src.ClampZoom(4.6))
		assert.Equal(t, 6, src.ClampZoom(6))
	})

	t.Run("rounds a fractional zoom like Math.round", func(t *testing.T) {
		src := NewTileSource(tileSpecKitten)
		assert.Equal(t, 2, src.ClampZoom(2.3))
		assert.Equal(t, 3, src.ClampZoom(2.5))
		assert.Equal(t, 3, src.ClampZoom(2.7))
		assert.Equal(t, -2, src.ClampZoom(-2.5))
		assert.Equal(t, 12, src.ClampZoom(12))
	})

	t.Run("applies a maxNativeZoom set later", func(t *testing.T) {
		// GridLayerSpec's "redraws tiles properly after changing
		// maxNativeZoom": at zoom 12 the level is 12 until maxNativeZoom 11
		// is set.
		src := NewTileSource(tileSpecKitten)
		assert.Equal(t, 12, src.ClampZoom(12))
		src.MaxNativeZoom, src.HasMaxNativeZoom = 11, true
		assert.Equal(t, 11, src.ClampZoom(12))
	})
}

func TestTileCoords_Key(t *testing.T) {
	t.Run("is x:y:z", func(t *testing.T) {
		assert.Equal(t, "1:2:3", TileCoords{1, 2, 3}.Key())
		assert.Equal(t, "-1:0:1", TileCoords{-1, 0, 1}.Key())
	})
}
