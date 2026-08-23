// Package portolan is the slippy-map widget's kernel: a Go port of the map
// core of Leaflet (https://github.com/Leaflet/Leaflet — BSD-2-Clause,
// © 2010–2026 Volodymyr Agafonkin, © 2010–2011 CloudMade), taken from
// upstream main at commit c96f31a7a350a07cfbc852cf88e6ca69af5f5ec9 (the
// 2.0.0-alpha.1 line) and re-targeted onto the imzero2 painter lane
// (ADR-0204).
//
// A portolan is the nautical chart ruled with rhumb lines, which is what a
// Web-Mercator basemap is; the name is the house's, the algorithms are
// Leaflet's. What the port preserves is the model — coordinate reference
// systems and projections, view state, the gesture handlers, the tile
// pyramid, the event vocabulary — re-idiomised to Go: value types, methods
// that return new values rather than mutate, errors instead of throws, and
// no attempt at Leaflet's API shape (ADR-0204 §SD2). Provenance lives in this
// comment, in LICENSE-leaflet.txt beside it and in THIRD_PARTY_NOTICES.md, not
// in the package name (§SD3).
//
// Module map, this package's file ← the upstream module it transliterates:
//
//	util.go           ← src/core/Util.js (wrapNum, formatNum, Math.round)
//	point.go          ← src/geometry/Point.js, src/geometry/Bounds.js
//	transformation.go ← src/geometry/Transformation.js
//	latlng.go         ← src/geo/LatLng.js, src/geo/LatLngBounds.js
//	projection.go     ← src/geo/projection/*.js
//	crs.go            ← src/geo/crs/*.js
//	lineutil.go       ← src/geometry/LineUtil.js
//	polyutil.go       ← src/geometry/PolyUtil.js
//	view.go           ← src/map/Map.js (view state, limits, fitBounds)
//	anim.go           ← src/map/Map.js (setView's animations, flyTo), src/dom/PosAnimation.js
//	handlers.go       ← src/map/handler/*.js (drag, wheel, pinch, double click, box zoom, keyboard)
//	pyramid.go        ← src/layer/tile/GridLayer.js
//	tilesource.go     ← src/layer/tile/TileLayer.js (options, URL templating)
//	loader.go         — no upstream module: the fetch, cache and decode the browser did
//	widget.go         — no upstream module: the painter-lane canvas, registers and readback
//	overlay.go        ← src/layer/vector/Polyline.js, Polygon.js, Renderer.js (the clip and simplify pipeline)
//
// The sub-package h3overlay draws H3 cells and regions on the map through
// the h3 wasm bridge; the map itself does not depend on it.
//
// Two deliberate departures from upstream: pixel rounding uses JavaScript's
// Math.round (half toward +∞, see jsRound), not Go's math.Round; and LatLng
// carries no altitude — nothing in this tree's map has one, and Leaflet's
// own equality ignores it.
//
// The numbers match Leaflet's as far as IEEE double arithmetic in Go allows:
// every ported specification holds with Leaflet's own tolerances, and the
// EPSG:3857 pixel transform is evaluated in JavaScript's order so its pixels
// match on amd64. Three things can still move the last bit — jsRound is
// floor(x+0.5), which parts from Math.round at 0.49999999999999994 and above
// 2^52; Go's math.Sin is not V8's fdlibm and differs by an ulp for some
// inputs; and a target that fuses multiply-add (arm64, GOAMD64=v3) rounds
// a·x+b once where JavaScript rounds twice. None is visible at tile
// resolution.
//
// Leaflet's specifications are ported as the package's tests — geo,
// geometry, CRS and projection (M1), MapSpec's view and GridLayerSpec's
// tile counts (M2), the handler and animation specs (M3) — with each test
// file's header listing the upstream cases it leaves out and why. Where the
// port deviates from upstream on purpose the deviation is at the code, and
// the larger ones are in ADR-0204 §SD6.
package portolan
