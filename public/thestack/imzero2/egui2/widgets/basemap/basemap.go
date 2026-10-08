// Package basemap resolves the shared slippy-map basemap tile server from the
// BOXER_MAP_TILE_* environment variables: as a TileSource for the portolan
// map widget, and as the "basemap" destination of the host's HTTP egress
// service (ADR-0262), which the widget's tiles are fetched through. Every app that shows a basemap
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
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/httpegress"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
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
	// host's egress service honours the system store, so a private CA installed
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

// DefaultOn reports whether a map that defaults to "no basemap" should show
// one: a tile server was configured, or this is the browser tab, where the
// country outlines that stand in for a basemap cost more per frame than the
// tiles do (ADR-0262 Update 2026-10-03).
func DefaultOn() bool { return !offline.Load() && (Configured() || fetchesDirect) }

// offline is set by SetOffline.
var offline atomic.Bool

// SetOffline makes every map of this process start without a basemap,
// whatever DefaultOn would otherwise say: for a binary that must not reach a
// tile server, such as a demo published on a site whose pages say they load
// nothing from elsewhere (ADR-0291 M2). A person can still switch tiles on;
// a binary that must forbid that refuses the requests themselves.
func SetOffline() { offline.Store(true) }

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
	tmpl := strings.TrimSpace(TileURL.Get())
	if tmpl == "" {
		// Whitespace-only is unset, the predicate Configured uses.
		tmpl = osmTileURL
	}
	src = portolan.NewTileSource(tmpl)
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

// Destination is the basemap's name in the HTTP egress registry
// (ADR-0262 §SD2): an app that shows a basemap declares [ClientCaps] and
// fetches its tiles through `net.http.fetch.basemap`.
const Destination = "basemap"

// userAgent identifies the basemap client to the tile server; the public
// OSM servers refuse an empty one.
const userAgent = "boxer-portolan/0.1 (+https://github.com/stergiotis/boxer)"

func init() {
	httpegress.Register(httpegress.DestinationSpec{
		Name:        Destination,
		Description: "the slippy-map basemap tile server (BOXER_MAP_TILE_URL, OpenStreetMap by default)",
		Resolve:     ResolveDestination,
	})
}

// ResolveDestination is the basemap destination from the BOXER_MAP_TILE_*
// registry: the prefixes of PortolanSource's template, and the TLS pair. The
// knobs bite only when a custom BOXER_MAP_TILE_URL is configured — a
// private CA must not be trusted for the public default — and they keep
// their names and meanings, honoured now by the host's egress service
// (ADR-0262 §SD6) where the tile loader honoured them before.
func ResolveDestination() (d httpegress.Destination, err error) {
	src := PortolanSource()
	d.Prefixes, err = httpegress.TilePrefixes(src.URLTemplate, src.Subdomains)
	if err != nil {
		return
	}
	d.UserAgent = userAgent
	if Configured() {
		d.CAFile = strings.TrimSpace(TileCAFile.Get())
		d.InsecureTLS = TileInsecureTLS.Get()
	}
	return
}

// ClientCaps is the manifest entry an app that shows a basemap declares.
func ClientCaps(reason string) (caps []app.SubjectFilter) {
	return httpegress.ClientCaps(Destination, reason)
}

// Tiles is an app's basemap tile fetcher: a GET against the basemap
// destination over the app's bus. The bus may arrive after construction —
// an app's panes are built before Mount hands it over — so Bind sets it
// late; until then every tile fails with the reason.
type Tiles struct {
	getter atomic.Pointer[httpegress.Getter]
	// Purpose names the consumer on every call, e.g. "play: map".
	purpose string
}

// NewTiles returns a fetcher over bus, which may be nil until Bind.
func NewTiles(bus app.BusI, purpose string) (inst *Tiles) {
	inst = &Tiles{purpose: purpose}
	inst.Bind(bus)
	return
}

// Bind sets the bus the tiles travel over; nil unbinds.
func (inst *Tiles) Bind(bus app.BusI) {
	if bus == nil {
		inst.getter.Store(nil)
		return
	}
	inst.getter.Store(&httpegress.Getter{Client: httpegress.NewClient(bus), Destination: Destination, Purpose: inst.purpose})
}

var errUnbound = errors.New("basemap: no bus bound — the map is not hosted")

// Get fetches one tile.
func (inst *Tiles) Get(ctx context.Context, url string) (data []byte, err error) {
	if fetchesDirect {
		return fetchDirect(ctx, url)
	}
	g := inst.getter.Load()
	if g == nil {
		return nil, errUnbound
	}
	return g.Get(ctx, url)
}

// maxDirectTileBytes bounds a tile fetched directly, like the loader's own cap.
const maxDirectTileBytes = 4 << 20

// fetchDirect fetches one tile over the process's own HTTP transport — the
// browser tab's host transport, where there is no egress service to ask.
func fetchDirect(ctx context.Context, url string) (data []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		err = eh.Errorf("basemap: tile request: %w", err)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		err = eh.Errorf("basemap: tile fetch: %w", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		err = eb.Build().Int("status", resp.StatusCode).Str("url", url).Errorf("basemap: tile fetch")
		return
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxDirectTileBytes+1))
	if err != nil {
		err = eh.Errorf("basemap: tile read: %w", err)
		return
	}
	if len(data) > maxDirectTileBytes {
		err = eb.Build().Int("limit", maxDirectTileBytes).Str("url", url).Errorf("basemap: tile too large")
		data = nil
	}
	return
}

// PortolanLoader is the loader options for a map whose tiles come through
// tiles; nil fetches nothing.
func PortolanLoader(tiles *Tiles) (opts portolan.LoaderOptions) {
	if tiles != nil {
		opts.Fetcher = tiles
	}
	return
}
