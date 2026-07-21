// Package basemap resolves the shared slippy-map basemap tile server from the
// BOXER_MAP_TILE_* environment variables and applies it to a walkers map
// widget. Every app that shows a walkers basemap (play's Map panel,
// terrainscope) routes its tile configuration through here, so a deployment
// points every basemap at a self-hosted GIS with a single BOXER_MAP_TILE_URL —
// no per-app knob, and no traffic to tile.openstreetmap.org once it is set.
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
