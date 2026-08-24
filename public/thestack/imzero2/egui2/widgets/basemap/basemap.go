// Package basemap resolves the shared slippy-map basemap tile server from the
// BOXER_MAP_TILE_* environment variables and hands it to the portolan map
// widget as a TileSource and LoaderOptions. Every app that shows a basemap
// (play's Map panel, terrainscope, the widget gallery) routes its tile
// configuration through here, so a deployment points every basemap at a
// self-hosted GIS with a single BOXER_MAP_TILE_URL — no per-app knob, and no
// traffic to tile.openstreetmap.org once it is set. BOXER_MAP_TILE_CA_FILE and
// BOXER_MAP_TILE_INSECURE_TLS cover the case where that GIS serves https under
// a certificate the process's roots do not chain to.
//
// The default server is OpenStreetMap, and it is spelled out here as ordinary
// Spec defaults rather than left to a built-in fallback. The endpoint a
// deployment talks to when nobody configured one is then visible in
// `boxer env list` and doc/env-vars.md, and can be repointed one field at a
// time — a mirror of the same tiles keeps the OSM attribution by overriding
// only the URL.
//
// The TLS knobs take effect only when BOXER_MAP_TILE_URL is set explicitly
// (Configured): with the default server in place, BOXER_MAP_TILE_INSECURE_TLS
// would disable certificate verification against tile.openstreetmap.org, which
// is exactly the connection nobody has any business weakening. Neither knob
// applies until a deployment names its own server.
//
// The two are not the same size. BOXER_MAP_TILE_CA_FILE moves the trust anchor
// and leaves everything else alone; BOXER_MAP_TILE_INSECURE_TLS stops
// authenticating the peer, and so also lowers the protocol floor to TLS 1.0
// and admits the legacy cipher suites, which is what makes it usable against
// the old appliance it exists for. Prefer the CA file.
package basemap

import (
	"strings"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
)

// OpenStreetMap, the values the retired walkers binding hard-coded. Kept as
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
// read by every basemap consumer. Every var has an OpenStreetMap
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

	// The two TLS knobs below date from the retired renderer-side tile
	// client, which trusted the webpki root bundle and nothing else — no
	// system trust store to add a private CA to, SSL_CERT_FILE ignored. The
	// Go tile loader honours the system store, so a private CA installed
	// there works without them; they remain for a CA that is not, and for
	// the insecure escape hatch. Prefer the CA file — it keeps certificate
	// verification on.
	TileCAFile = env.NewPath(env.Spec{
		Name:        "BOXER_MAP_TILE_CA_FILE",
		Description: "path to a PEM certificate bundle added to the trust roots when fetching from BOXER_MAP_TILE_URL, for a tile server behind an internal CA; certificate verification stays on. Must hold the issuing CA — a bare self-signed server certificate is not accepted as its own trust anchor, and needs BOXER_MAP_TILE_INSECURE_TLS instead. Ignored unless BOXER_MAP_TILE_URL is set explicitly, and superseded by BOXER_MAP_TILE_INSECURE_TLS.",
		Category:    env.CategoryE("boxer-map"),
	})

	TileInsecureTLS = env.NewBool(env.Spec{
		Name:        "BOXER_MAP_TILE_INSECURE_TLS",
		Description: "disable TLS certificate verification for BOXER_MAP_TILE_URL tile requests. Accepts any certificate, so it also accepts an interceptor's: use it only against a tile server you control on a trusted network, and prefer BOXER_MAP_TILE_CA_FILE. Also drops the protocol floor to TLS 1.0 and admits the legacy cipher suites (static-RSA key exchange, 3DES, RC4), so an old server is reachable rather than failing on version or cipher negotiation — once the peer is unauthenticated neither one protects anything. Ignored unless BOXER_MAP_TILE_URL is set explicitly — it never applies to the default OpenStreetMap server.",
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

// PortolanSource is the registry's basemap as a portolan tile source — the
// portolan-typed twin of Apply (ADR-0204 §SD1): the URL (OpenStreetMap unless
// BOXER_MAP_TILE_URL says otherwise), the attribution and the max zoom.
func PortolanSource() (src portolan.TileSource) {
	src = portolan.NewTileSource(strings.TrimSpace(TileURL.Get()))
	if attr := strings.TrimSpace(TileAttribution.Get()); attr != "" {
		src.Attribution = attr
		if attrURL := strings.TrimSpace(TileAttributionURL.Get()); attrURL != "" {
			src.AttributionURL = attrURL
		}
	}
	if zoom, set := clampMaxZoom(TileMaxZoom.Get()); set {
		src.MaxZoom = float64(zoom)
		src = src.Normalized()
	}
	return
}

// PortolanLoader is the registry's TLS pair as loader options. The knobs bite
// only when a custom BOXER_MAP_TILE_URL is configured — a private CA must not
// be trusted for the public default — and they keep their names and meanings,
// honoured by Go's http.Transport (ADR-0204 §SD4) where the retired binding's
// renderer-side client honoured them before.
func PortolanLoader() (opts portolan.LoaderOptions) {
	if !Configured() {
		return
	}
	opts.CAFile = strings.TrimSpace(TileCAFile.Get())
	opts.InsecureTLS = TileInsecureTLS.Get()
	return
}
