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
//	point.go          ← src/geometry/Point.js, src/geometry/Bounds.js
//	transformation.go ← src/geometry/Transformation.js
//	latlng.go         ← src/geo/LatLng.js, src/geo/LatLngBounds.js
//	projection.go     ← src/geo/projection/*.js
//	crs.go            ← src/geo/crs/*.js
//	lineutil.go       ← src/geometry/LineUtil.js
//	polyutil.go       ← src/geometry/PolyUtil.js
//
// Two deliberate departures from upstream: pixel rounding uses JavaScript's
// Math.round (half toward +∞, see jsRound), not Go's math.Round; and LatLng
// carries no altitude — nothing in this tree's map has one, and Leaflet's
// own equality ignores it.
//
// The numbers match Leaflet's as far as IEEE double arithmetic in Go allows:
// every ported specification holds with Leaflet's own tolerances, and the
// EPSG:3857 pixel transform is evaluated in JavaScript's order so its pixels
// match bit for bit on amd64. Two things can still move the last bit — Go's
// math.Sin is not V8's fdlibm and differs by an ulp for some inputs, and a
// target that fuses multiply-add (arm64, GOAMD64=v3) rounds a·x+b once where
// JavaScript rounds twice. Neither is visible at tile resolution.
//
// This is milestone M1 of ADR-0204 — the geo and geometry kernel, with
// Leaflet's specifications for it ported as the package's tests. The view,
// the pyramid, the handlers and the widget itself follow in M2–M4.
package portolan
