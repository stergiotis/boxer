// Package basemap resolves the shared slippy-map basemap tile server from the
// BOXER_MAP_TILE_* environment variables and applies it to a walkers map
// widget. Every app that shows a walkers basemap (play's Map panel,
// terrainscope) routes its tile configuration through here, so a deployment
// points every basemap at a self-hosted GIS with a single BOXER_MAP_TILE_URL —
// no per-app knob, and no traffic to tile.openstreetmap.org once it is set.
// BOXER_MAP_TILE_CA_FILE and BOXER_MAP_TILE_INSECURE_TLS cover the case where
// that GIS serves https under a certificate the renderer's bundled roots do
// not chain to.
package basemap

import (
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// The BOXER_MAP_TILE_* registry block (ADR-0009). A shared name may be
// registered only once, so these are declared here rather than per app and
// read by every walkers-basemap consumer. TileURL is the switch: empty (the
// default) keeps the widget's built-in OpenStreetMap source; a non-empty value
// selects a custom XYZ server and makes the two companion knobs meaningful.
var (
	TileURL = env.NewString(env.Spec{
		Name:        "BOXER_MAP_TILE_URL",
		Description: `XYZ tile-server URL template for slippy-map basemaps, e.g. "http://mygis/{z}/{x}/{y}.png"; must contain the {z}/{x}/{y} placeholders. Empty (default) uses the built-in OpenStreetMap source, which fetches tiles from tile.openstreetmap.org.`,
		Category:    env.CategoryE("boxer-map"),
	})

	TileAttribution = env.NewString(env.Spec{
		Name:        "BOXER_MAP_TILE_ATTRIBUTION",
		Description: "attribution/credit line rendered over the basemap for BOXER_MAP_TILE_URL; empty shows none. Ignored unless BOXER_MAP_TILE_URL is set.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileMaxZoom = env.NewInt(env.Spec{
		Name:        "BOXER_MAP_TILE_MAX_ZOOM",
		Description: "highest zoom level served by BOXER_MAP_TILE_URL (1..255); 0 or unset keeps the widget default (19). Ignored unless BOXER_MAP_TILE_URL is set.",
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
		Description: "path to a PEM certificate bundle added to the trust roots when fetching from BOXER_MAP_TILE_URL, for a tile server behind an internal CA; certificate verification stays on. Must hold the issuing CA — a bare self-signed server certificate is not accepted as its own trust anchor, and needs BOXER_MAP_TILE_INSECURE_TLS instead. Ignored unless BOXER_MAP_TILE_URL is set, and superseded by BOXER_MAP_TILE_INSECURE_TLS.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileInsecureTLS = env.NewBool(env.Spec{
		Name:        "BOXER_MAP_TILE_INSECURE_TLS",
		Description: "disable TLS certificate verification for BOXER_MAP_TILE_URL tile requests. Accepts any certificate, so it also accepts an interceptor's: use it only against a tile server you control on a trusted network, and prefer BOXER_MAP_TILE_CA_FILE. Ignored unless BOXER_MAP_TILE_URL is set.",
		Category:    env.CategoryE("boxer-map"),
	})
)

// Configured reports whether a custom tile server is set (BOXER_MAP_TILE_URL
// non-empty after trimming). Consumers that default a map to "no basemap"
// (play's Map panel) consult this to turn the basemap on when a server is
// available; consumers that always show a basemap (terrainscope) can ignore it
// and just call Apply.
func Configured() bool {
	return strings.TrimSpace(TileURL.Get()) != ""
}

// Apply sets the tile-server methods on mw from the BOXER_MAP_TILE_* vars and
// returns the updated fluid. When BOXER_MAP_TILE_URL is empty it returns mw
// unchanged — the walkers widget then keeps its built-in OpenStreetMap source
// (identical to never calling .TileUrl), so wiring Apply in is
// behaviour-preserving for the unconfigured default. Tile size is left at the
// widget default (256px), outside the configurable knob set.
//
// Every knob is gated on a non-empty url, the TLS pair included: the built-in
// OpenStreetMap source keeps its verified public-internet fetch no matter what
// else is in the environment, so a stray BOXER_MAP_TILE_INSECURE_TLS cannot
// downgrade a deployment that never opted into a custom server.
//
// Apply runs once per frame per map, so it does no I/O — the CA file travels
// as a path and is read renderer-side, once per tile-source construction. The
// renderer also owns the precedence when both TLS knobs are set (insecure
// wins, CA file ignored, warned once) because that is where the log line is
// cheap; Go emitting it here would fire every frame.
func Apply(mw c.WalkersMapFluid) c.WalkersMapFluid {
	url := strings.TrimSpace(TileURL.Get())
	if url == "" {
		return mw
	}
	mw = mw.TileUrl(url)
	if attr := strings.TrimSpace(TileAttribution.Get()); attr != "" {
		mw = mw.TileAttribution(attr)
	}
	if zoom, set := clampMaxZoom(TileMaxZoom.Get()); set {
		mw = mw.TileMaxZoom(zoom)
	}
	if ca := strings.TrimSpace(TileCAFile.Get()); ca != "" {
		mw = mw.TileCaFile(ca)
	}
	if TileInsecureTLS.Get() {
		mw = mw.TileInsecureTls(true)
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
