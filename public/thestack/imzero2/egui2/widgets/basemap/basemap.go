// Package basemap resolves the shared slippy-map basemap tile server from the
// BOXER_MAP_TILE_* environment variables and applies it to a walkers map
// widget. Every app that shows a walkers basemap (play's Map panel,
// terrainscope) routes its tile configuration through here, so a deployment
// points every basemap at a self-hosted GIS with a single BOXER_MAP_TILE_URL —
// no per-app knob, and no traffic to tile.openstreetmap.org once it is set.
// BOXER_MAP_TILE_CA_FILE and BOXER_MAP_TILE_INSECURE_TLS cover the case where
// that GIS serves https under a certificate the renderer's bundled roots do
// not chain to.
//
// The default server is OpenStreetMap, and it is spelled out here as ordinary
// Spec defaults rather than left to the renderer's built-in fallback. The
// endpoint a deployment talks to when nobody configured one is then visible in
// `boxer env list` and doc/env-vars.md, and can be repointed one field at a
// time — a mirror of the same tiles keeps the OSM attribution by overriding
// only the URL. The renderer keeps its own fallback for a walkersMap built
// without .TileUrl at all (the widget demo does that); this package no longer
// takes that path.
package basemap

import (
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// OpenStreetMap, as the values walkers' built-in source hard-codes. Kept as
// named constants so the defaults below read as one coherent tile server
// rather than four unrelated strings, and so the attribution is visibly tied
// to the URL it credits — override the URL alone and you are still crediting
// OSM, which is correct for a mirror and wrong for anything else.
const (
	osmTileURL        = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	osmAttribution    = "OpenStreetMap contributors"
	osmAttributionURL = "https://www.openstreetmap.org/copyright"
	osmMaxZoom        = "19"
)

// The BOXER_MAP_TILE_* registry block (ADR-0009). A shared name may be
// registered only once, so these are declared here rather than per app and
// read by every walkers-basemap consumer. Every var has an OpenStreetMap
// default, so there is always a tile server in effect; setting TileURL
// explicitly is what marks a deployment as having chosen its own, which
// Configured reports and the TLS knobs key on.
var (
	TileURL = env.NewString(env.Spec{
		Name:        "BOXER_MAP_TILE_URL",
		Default:     osmTileURL,
		Description: `XYZ tile-server URL template for slippy-map basemaps, e.g. "http://mygis/{z}/{x}/{y}.png"; must contain the {z}/{x}/{y} placeholders. Defaults to OpenStreetMap, which fetches tiles from tile.openstreetmap.org over the public internet — set this to a self-hosted GIS to keep basemap traffic inside the deployment. Setting it is also what enables the BOXER_MAP_TILE_*_TLS / _CA_FILE knobs.`,
		Category:    env.CategoryE("boxer-map"),
	})

	TileAttribution = env.NewString(env.Spec{
		Name:        "BOXER_MAP_TILE_ATTRIBUTION",
		Default:     osmAttribution,
		Description: "attribution/credit line rendered over the basemap; empty shows none. Defaults to OpenStreetMap's required credit, which stays correct for a mirror of OSM tiles — a different tile source needs whatever credit line its terms of use require.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileAttributionURL = env.NewString(env.Spec{
		Name:        "BOXER_MAP_TILE_ATTRIBUTION_URL",
		Default:     osmAttributionURL,
		Description: "link target behind BOXER_MAP_TILE_ATTRIBUTION; empty renders the credit as plain text. Defaults to the OpenStreetMap copyright page.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileMaxZoom = env.NewInt(env.Spec{
		Name:        "BOXER_MAP_TILE_MAX_ZOOM",
		Default:     osmMaxZoom,
		Description: "highest zoom level served by BOXER_MAP_TILE_URL (1..255); 0 keeps the widget default. Defaults to 19, which is what OpenStreetMap serves.",
		Category:    env.CategoryE("boxer-map"),
	})

	// The two TLS knobs below exist because the renderer's HTTP client
	// trusts the webpki root bundle and nothing else: there is no system
	// trust store to add a private CA to, and SSL_CERT_FILE is ignored. A
	// self-hosted GIS behind an internal CA is therefore unreachable over
	// https without one of these. Prefer the CA file — it keeps
	// certificate verification on.
	TileCAFile = env.NewPath(env.Spec{
		Name:        "BOXER_MAP_TILE_CA_FILE",
		Description: "path to a PEM certificate bundle added to the trust roots when fetching from BOXER_MAP_TILE_URL, for a tile server behind an internal CA; certificate verification stays on. Must hold the issuing CA — a bare self-signed server certificate is not accepted as its own trust anchor, and needs BOXER_MAP_TILE_INSECURE_TLS instead. Ignored unless BOXER_MAP_TILE_URL is set explicitly, and superseded by BOXER_MAP_TILE_INSECURE_TLS.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileInsecureTLS = env.NewBool(env.Spec{
		Name:        "BOXER_MAP_TILE_INSECURE_TLS",
		Description: "disable TLS certificate verification for BOXER_MAP_TILE_URL tile requests. Accepts any certificate, so it also accepts an interceptor's: use it only against a tile server you control on a trusted network, and prefer BOXER_MAP_TILE_CA_FILE. Ignored unless BOXER_MAP_TILE_URL is set explicitly — it never applies to the default OpenStreetMap server.",
		Category:    env.CategoryE("boxer-map"),
	})
)

// Configured reports whether the deployment chose its own tile server, i.e.
// BOXER_MAP_TILE_URL is set in the environment and not just whitespace. It
// asks Lookup rather than Get on purpose: Get now always returns a URL,
// because the OpenStreetMap default is a real value, so "a URL exists" no
// longer distinguishes anything.
//
// Consumers that default a map to "no basemap" (play's Map panel) consult this
// to turn the basemap on, which keeps that panel offline by default rather
// than reaching for public-internet tiles unasked; consumers that always show
// a basemap (terrainscope) can ignore it and just call Apply. Apply also gates
// the TLS knobs on it, so neither can weaken the connection to the default
// public server.
func Configured() bool {
	raw, set := TileURL.Lookup()
	return set && strings.TrimSpace(raw) != ""
}

// Apply sets the tile-server methods on mw from the BOXER_MAP_TILE_* vars and
// returns the updated fluid. The URL is always sent now that it has an
// OpenStreetMap default, so the renderer's own built-in-source fallback is
// reached only by a walkersMap built without .TileUrl — never through here.
// What that sends for an unconfigured deployment is the same endpoint, the
// same credit line and the same max zoom the built-in source hard-codes, so
// the default remains behaviour-preserving; it is just stated rather than
// implied. Tile size is left at the widget default (256px), outside the
// configurable knob set.
//
// The TLS pair is gated on Configured rather than on a non-empty URL. That
// gate used to be the same thing; with a default URL it no longer is, and the
// distinction is load-bearing — otherwise a stray BOXER_MAP_TILE_INSECURE_TLS
// would disable certificate verification against tile.openstreetmap.org, which
// is exactly the connection nobody has any business weakening. Neither knob
// applies until a deployment names its own server.
//
// Apply runs once per frame per map, so it does no I/O — the CA file travels
// as a path and is read renderer-side, once per tile-source construction. The
// renderer also owns the precedence when both TLS knobs are set (insecure
// wins, CA file ignored, warned once) because that is where the log line is
// cheap; Go emitting it here would fire every frame.
func Apply(mw c.WalkersMapFluid) c.WalkersMapFluid {
	url := strings.TrimSpace(TileURL.Get())
	if url == "" {
		// Only reachable if the default is emptied deliberately. Leave the
		// widget on its built-in source rather than sending an empty template
		// the renderer would expand into unfetchable URLs.
		return mw
	}
	mw = mw.TileUrl(url)
	if attr := strings.TrimSpace(TileAttribution.Get()); attr != "" {
		mw = mw.TileAttribution(attr)
		// Only meaningful alongside a credit line: walkers renders the link on
		// the attribution text, so a URL without text has nothing to attach to.
		if attrURL := strings.TrimSpace(TileAttributionURL.Get()); attrURL != "" {
			mw = mw.TileAttributionUrl(attrURL)
		}
	}
	if zoom, set := clampMaxZoom(TileMaxZoom.Get()); set {
		mw = mw.TileMaxZoom(zoom)
	}
	if Configured() {
		if ca := strings.TrimSpace(TileCAFile.Get()); ca != "" {
			mw = mw.TileCaFile(ca)
		}
		if TileInsecureTLS.Get() {
			mw = mw.TileInsecureTls(true)
		}
	}
	return mw
}

// clampMaxZoom maps the BOXER_MAP_TILE_MAX_ZOOM int64 into the widget's uint8
// tileMaxZoom argument. A value <=0 is "unset" (set=false → keep the widget's
// own default of 19); anything above the uint8 ceiling saturates rather than
// wrapping.
func clampMaxZoom(mz int64) (zoom uint8, set bool) {
	if mz <= 0 {
		return 0, false
	}
	if mz > 255 {
		mz = 255
	}
	return uint8(mz), true
}
