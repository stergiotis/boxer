package play

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/basemap"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/portolan/landoverlay"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/sqleditor"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/worldmap"
)

// MapDriver is the ADR-0096 geo-raster map panel: a slippy map whose
// viewport drives an in-DB-rendered RGBA raster. Each frame it reads the
// previous frame's camera (once a keyed fetcher register, now the map's own
// map's handle); once the camera has
// settled it EMITS the viewport as the six reserved vp_* signals (ADR-0096
// §SD6 realized via ADR-0097 slice 5c) and demands a panel-authored node whose
// SQL carries the matching {vp_*:UInt32} slots — a pan changes only the
// compiled params, never the SQL text. The result packs to RGBA and draws as a
// mapRaster overlay pinned to bounds recovered from the served vp_* values.
//
// The table, sampling, and colour render stay panel controls spliced into the
// node template (this is a power-user SQL playground — the editor already
// grants arbitrary query access); a control change is a template change and
// re-executes via the lane's (SQL, params) memo key. Not yet wired: the
// keepBuffer margin, the progressive sampling ladder, and hover→info queries —
// SD10 deferrals in the ADR.
type MapDriver struct {
	ids   *c.WidgetIdStack
	pm    *portolan.Map // the map widget, created on first Render
	tiles *basemap.Tiles
	// land is the offline ground drawn under the raster when there is no
	// basemap, as the Vector field pane does; a nil atlas draws nothing.
	land   *landoverlay.Layer
	atlas  *worldmap.Atlas
	client *Client

	// Controls + display. The map fills the tab body by default (FillAvailable
	// — the leaf is a bounded, no-scroll host, so filling it means nothing
	// overflows or clips); mapWidth/mapHeight apply only when
	// BOXER_PLAY_MAP_SIZE pins a fixed size (fixedSize) for deterministic
	// scripted screenshots.
	table        string
	sampling     float64
	opacity      float64
	noTiles      bool
	live         bool
	fixedSize    bool
	mapWidth     float64
	mapHeight    float64
	initLat      float64
	initLon      float64
	initZoom     float64
	forceRefresh bool

	// refine turns the sampling ladder on (play_map_ladder.go); off, the one
	// table named is read at the manual sampling. ladder is its state, and
	// colorSQL/extraWhere the render the last settle built the template
	// with, which a climb rebuilds it from. packLevel is the level whose
	// raster is on screen. refreshPending marks the settle a Refresh asked
	// for, which lets the ladder climb past its budget.
	refine         bool
	ladder         mapLadder
	colorSQL       string
	extraWhere     string
	packLevel      mapLevel
	refreshPending bool

	// cache opts the raster runs into the server's query cache (off by
	// default; ADR-0096 2026-10-01 cache Update). cacheUse/cacheFresh mirror
	// it and the ladder's Refresh state for the lane's goroutine, which reads
	// them through the lane's ExecOptions.QueryCache on every request.
	cache      bool
	cacheUse   atomic.Bool
	cacheFresh atomic.Bool

	// timeCol names the source's time column; while the Timeline has a
	// window brushed (windowFrom/windowTo, read off the tl_from/tl_to
	// signals each frame), the raster keeps only rows inside it. Empty, or
	// no window, filters nothing.
	timeCol    string
	windowFrom string
	windowTo   string

	// memo keeps the rasters recently drawn (play_map_memo.go). memoShown is
	// the key of the one on screen when it came from memory, and
	// memoOnScreen says the raster on screen is a memo one, which a lane's
	// stale last-good result must not repack over.
	memo         mapMemo
	memoShown    string
	memoOnScreen bool

	// renderIdx selects builtinRenders; customColorSQL is the colour expression
	// used when the "Custom" render is active.
	renderIdx      int
	customColorSQL string

	// The two controls whose value is SQL get the SQL field rather than a
	// plain TextEdit (ADR-0187 §M0). One per control, not one
	// shared: each memoises its own lex job against its own text, so a shared
	// instance would rebuild both on every frame that drew both. Built in
	// NewMapDriver on the driver's ids.
	tableField *sqleditor.Field
	colorField *sqleditor.Field

	// Debounce on the map's view hash: reset the timer whenever it changes,
	// fire only once the view has been stable for mapDebounce.
	lastViewHash uint64
	viewStableAt time.Time

	// lane runs the raster query off the render thread (ADR-0097 3f). Since
	// slice 5c the raster is a panel-authored NODE on the param seam: template
	// is its SQL with the reserved {vp_*:UInt32} slots (ADR-0096 §SD6), and
	// templateReads caches the template's slot names — the compile resolves
	// them against the frame's signal snapshot, so the demand is
	// compiledNode{template, vp_* values} and the lane's (SQL, params) memo
	// key supersedes on any viewport or control change.
	lane          *nodeLane
	template      string
	templateReads []string

	// Packed result (render-thread only): re-packed when the served result's
	// content fingerprint changes (ADR-0097 SD4 early cutoff at the observer —
	// a re-fetch returning identical bytes repacks nothing; a same-input
	// re-fetch with NEW data does repack, which an input-key guard would miss).
	lastPackedFP uint64
	pixels       []uint32 // packed 0xRRGGBBAA, row-major, row 0 = north
	packW        uint32
	packH        uint32
	packBounds   [4]float64 // minLat, minLon, maxLat, maxLon the pixels cover
	version      uint64     // bumped per re-pack → mapRaster contentVersion

	// progress/frameProgress are this panel's own reading of its lane's in-band
	// ticks (ADR-0115 plane A). The app's tracker follows `main`/the observed
	// intermediate; the raster runs on the Map's own lane, so it needs its own —
	// folded once per frame in syncProgress and drawn beside the Cancel.
	progress      progressTracker
	frameProgress progressView
	// stats is the accounting of the fetch that produced the raster on screen,
	// shown in the slot the progress row occupies while one is in flight.
	stats laneStats

	loading bool
	// cancelled says the last fetch was aborted at the Cancel button rather
	// than having landed. It survives until a new run starts, so the status
	// line can say why nothing is loading: silence after a Cancel reads as the
	// button having done nothing.
	cancelled bool
	// Two error owners, so neither can latch a stale message (review finding):
	// laneErr mirrors the lane's error EVERY demand (nil clears it); packErr
	// belongs to the last repack attempt (cleared on success).
	laneErr    error
	packErr    error
	controlErr string
}

// mapViewportSignals are the six reserved panel-written params of the raster
// contract (ADR-0096 §SD6): the viewport mercator bbox (vp_min_y = north) and
// the output raster size. The Map is their writer; the raster template reads
// them — and being ordinary named signals (SD8), nothing stops another node
// from referencing them too.
//
// Read off the signal declaration (ADR-0232 §SD9) rather than spelled again
// here: the emit order below is the declaration's order, which is the order
// the bbox reads.
var mapViewportSignals = signalsWrittenBy("map")

const (
	mapDebounce        = 250 * time.Millisecond
	mapMaxDim   uint32 = 1024 // bounds query cost + Arrow size per view

	// mercWorld is the Web-Mercator world SPAN: the projection maps the globe
	// onto [0, mercWorld). It is 2^32 — the full-UInt32 mercator space that
	// ClickHouse's MVT functions use (MVTBoundingBoxMercator(0,0,0) returns
	// (0, 0, 4294967296, 4294967296), and MVTEncodeGeom documents "Web Mercator
	// over the full UInt32 coordinate range" with the same downward y axis), so
	// a column built with the setup.sql formulas and one built with the MVT
	// functions agree. See the ADR-0096 2026-07-28 Update for why this moved off
	// the upstream adsb.exposed 0xFFFFFFFF.
	mercWorld = 4294967296.0 // 2^32 — full Web-Mercator world span
	// mercUnitMax is the largest REPRESENTABLE coordinate, deliberately distinct
	// from the span above: the columns are UInt32, so the last valid unit is
	// 2^32-1. Keeping the two apart is what makes clampMerc saturate correctly —
	// Go's uint32(4294967296.0) is 0, so a ceiling written against the span
	// would wrap a pole/antimeridian point to the opposite corner of the world
	// instead of pinning it to the edge.
	mercUnitMax = 4294967295 // 0xFFFFFFFF — max UInt32, NOT the world span
)

// mapFetchTimeout bounds one raster round-trip. Generous because a remote()
// source (e.g. adsb.exposed via remoteSecure over a transatlantic link) can
// take ~20s per tile. A var so live tests can adjust it.
var mapFetchTimeout = 60 * time.Second

type mercBox struct{ minX, maxX, minY, maxY uint32 }

// rasterRender is a swappable colour mode (ADR-0096 §SD6). The geometry + density
// header of the raster query (span_*, in_view, px/py/pos, zoom_factor, total,
// max_total, transparency) is render-agnostic; a render supplies only the
// colour block (and, optionally, alpha) and an optional extra WHERE. This is what lets the panel target
// any table with mercator_x/mercator_y, not just the ADS-B schema.
type rasterRender struct {
	name string // UI label + fetch-key component
	// colorSQL is spliced into the WITH block after the shared header. In
	// scope: total, max_total, transparency, plus any table column via
	// aggregates (avg(col), …). It MUST define red, green, blue (Float64,
	// 0..255) and end without a trailing comma. It MAY define alpha; when it
	// does not, the template appends `255 AS alpha` (rasterAlphaRe). Ignored
	// when custom (customColorSQL is used).
	colorSQL string
	where    string   // optional predicate ANDed with in_view; "" = none
	needs    []string // columns beyond mercator_x/y assumed; nil = table-agnostic
	custom   bool     // colorSQL comes from the panel's editable field
}

// builtinRenders are the selectable colour modes; the first is the default.
// "Altitude & Speed" assumes the ADS-B columns; "Density" assumes only
// mercator_x/y so it works on ANY geo-point table; "Custom" takes a user-typed
// red/green/blue expression, matching the playground's arbitrary-table freedom.
var builtinRenders = []rasterRender{
	{
		name:     "Altitude & Speed",
		needs:    []string{"altitude", "ground_speed"},
		colorSQL: altitudeSpeedColorSQL,
	},
	{
		name: "Density",
		colorSQL: `least(1, transparency * 1.5) AS d,
    least(255, d * 255) AS red,
    greatest(0, least(255, (d * 1.6 - 0.45) * 255)) AS green,
    greatest(0, least(255, (d * 2.3 - 1.45) * 255)) AS blue`,
	},
	{
		name:  "Speed",
		needs: []string{"ground_speed"},
		// 600 kt: the ground-speed ceiling of altitudeSpeedColorSQL, same
		// reasoning.
		colorSQL: `greatest(0, least(avg(ground_speed), 600)) / 600 AS s,
    transparency * (1 - s) * 255 AS red,
    transparency * 90 AS green,
    transparency * s * 255 AS blue`,
	},
	{
		name:   "Custom",
		custom: true,
	},
}

// altitudeSpeedColorSQL is the default render, in OKLCH so each channel of
// the encoding moves one perceptual axis: hue ← mean altitude, chroma ← mean
// ground speed, lightness ← density (transparency). It assumes the units ADS-B
// reports: altitude in feet (barometric, may be negative near sea level),
// ground_speed in knots.
//
// The constants come from aviation domain knowledge, not from the data:
//   - 45,000 ft is the altitude ceiling: airliners cruise at FL290–FL410 and
//     most business jets top out near FL450, so higher reports are rare and
//     clamp to the top hue. Altitude enters as its square root so the busy
//     band below ~10,000 ft (terminal areas, where the 250 kt limit applies)
//     takes about half the hue range: ~30° red-orange on the ground, yellow
//     on approach, green at 10,000 ft, cyan in the climb, sky blue at cruise.
//   - 600 kt is the ground-speed ceiling: a jet near Mach 0.85 flies about
//     480–500 kt true airspeed, and a strong jet-stream tailwind adds ~100 kt;
//     faster reports saturate. Speed also enters as its square root, so the
//     split that matters most — light aircraft and helicopters (60–160 kt)
//     against jets (~250 kt under the terminal limit, 450+ at cruise) — gets
//     the widest chroma step; light aircraft read pastel, cruising jets
//     vivid. A chroma floor (a quarter of the maximum) keeps a stationary
//     target's altitude hue readable instead of fading to grey.
//   - Lightness spans 0.22..0.80 over transparency so a lone sample still
//     shows on the dark no-basemap background and the densest
//     pixels stay below white. Chroma shrinks with sqrt(lightness): dim
//     pixels get less colour, as the sRGB gamut does, so they darken towards
//     black rather than a muddy tint.
//
// colorOKLCHToSRGB needs ClickHouse 25.7+ (the adsb how-to verifies on 26.5);
// it clips each channel to the sRGB gamut, and the outer clamp keeps 0..255
// for any input regardless.
const altitudeSpeedColorSQL = `greatest(0, least(avg(altitude), 45000)) / 45000 AS alt_t,
    greatest(0, least(avg(ground_speed), 600)) / 600 AS spd_t,
    0.22 + 0.58 * transparency AS lum,
    0.19 * sqrt(lum / 0.8) * (0.25 + 0.75 * sqrt(spd_t)) AS chroma,
    30 + 230 * sqrt(alt_t) AS hue,
    colorOKLCHToSRGB(tuple(lum, chroma, hue)) AS rgb,
    greatest(0, least(255, tupleElement(rgb, 1))) AS red,
    greatest(0, least(255, tupleElement(rgb, 2))) AS green,
    greatest(0, least(255, tupleElement(rgb, 3))) AS blue`

func NewMapDriver(ids *c.WidgetIdStack, client *Client) *MapDriver {
	opts := newExecOptions("map")
	var d *MapDriver
	opts.QueryCache = func() (use bool, fresh bool) {
		return d.cacheUse.Load(), d.cacheFresh.Load()
	}
	d = &MapDriver{
		ids:        ids,
		client:     client,
		land:       &landoverlay.Layer{},
		tableField: sqleditor.NewField(ids, "map-table"),
		colorField: sqleditor.NewField(ids, "map-color"),
		// The stable query_id + replace_running_query make a superseding
		// pan/zoom fetch replace its predecessor server-side (SD5).
		lane:      newNodeLane(clientExecutor{client: client, opts: opts}, memory.NewGoAllocator(), mapFetchTimeout),
		table:     "planes_mercator_sample100",
		sampling:  100,
		refine:    true,
		timeCol:   "time",
		opacity:   0.9,
		noTiles:   true,
		live:      true,
		mapWidth:  960,
		mapHeight: 560,
		initLat:   40.0,
		initLon:   0.0,
		initZoom:  4.0,
		// renderIdx 0 is the first builtinRenders entry; Custom starts from an
		// editable density expression.
		customColorSQL: "transparency * 255 AS red,\n    transparency * 200 AS green,\n    transparency * 120 AS blue",
	}
	// Scripted-screenshot overrides — the BOXER_PLAY_MAP_* knobs from the
	// app_register.go env-registry block (ADR-0009); unset keeps the defaults
	// above.
	if t := strings.TrimSpace(MapTable.Get()); t != "" {
		d.table = t
	}
	if z := MapZoom.Get(); z > 0 {
		d.initZoom = z
	}
	if la, lo, ok := parseLatLon(MapCenter.Get()); ok {
		d.initLat, d.initLon = la, lo
	}
	if w, h, ok := parseWxH(MapSize.Get()); ok {
		d.mapWidth, d.mapHeight = w, h
		d.fixedSize = true
	}
	// A configured shared tile server (BOXER_MAP_TILE_URL) signals the operator
	// wants basemaps, so show one by default rather than the offline default;
	// the "no basemap" checkbox still toggles it. Unset keeps noTiles=true.
	if basemap.Configured() {
		d.noTiles = false
	}
	if a, err := worldmap.LoadAtlas(); err == nil {
		d.atlas = a
	}
	return d
}

func parseLatLon(s string) (lat, lon float64, ok bool) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 2 {
		return
	}
	la, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lo, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil {
		return
	}
	return la, lo, true
}

func parseWxH(s string) (w, h float64, ok bool) {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(s)), "x")
	if len(parts) != 2 {
		return
	}
	wv, e1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	hv, e2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if e1 != nil || e2 != nil {
		return
	}
	return wv, hv, true
}

// Render draws the controls and the map; the last-good raster rides on the map
// as an overlay through the projector, pinned to the bounds it was computed
// for. Once the view has settled (debounced), the viewport is published as the
// vp_* signals and the raster node is demanded against the frame's signal
// snapshot (sig; emits land next frame — the 5a frame consistency).
func (inst *MapDriver) Render(sig SignalEnvI, emit SignalEmitterI) {
	inst.syncProgress()
	inst.renderControls()

	// The map is created on first render, after the BOXER_PLAY_* seeds have
	// been applied. It owns its view Go-side (ADR-0204), so the old binding's
	// opcode's one-shot SetZoom and the keyed camera register are gone: the
	// view is read directly, and the "no basemap" toggle is a switch on it.
	if inst.pm == nil {
		inst.pm = portolan.New(inst.ids, "play-map", portolan.Options{
			Source:  basemap.PortolanSource(),
			Loader:  basemap.PortolanLoader(inst.tiles),
			Center:  portolan.LL(inst.initLat, inst.initLon),
			Zoom:    inst.initZoom,
			NoTiles: inst.noTiles,
		})
	}
	inst.pm.SetNoTiles(inst.noTiles)

	// The view as drawn last frame (the widget applies its registers one
	// frame behind, like every capture/fetch pair). Debounced on a stable
	// view before the vp_* signals are published.
	if v := inst.pm.View(); v.Loaded() {
		if vh := inst.pm.ViewHash(); vh != inst.lastViewHash {
			inst.lastViewHash = vh
			inst.viewStableAt = time.Now()
		}
		settled := !inst.viewStableAt.IsZero() && time.Since(inst.viewStableAt) >= mapDebounce
		inst.readWindow(sig)
		if (inst.live && settled) || inst.forceRefresh {
			inst.forceRefresh = false
			b, sz := v.Bounds(), v.Size()
			inst.updateViewport(b.GetSouth(), b.GetNorth(), b.GetWest(), b.GetEast(),
				float32(sz.X), float32(sz.Y), emit)
		}
	}

	// Compile the node from the frame snapshot and demand it (non-blocking;
	// a changed viewport or control supersedes the in-flight run via the
	// lane's (SQL, params) memo key; the last-good result is returned while a
	// new one loads). Gated on the full vp_* set — the signals land one frame
	// after the first settle. Re-pack only when the served fingerprint moves.
	if inst.template != "" {
		params := resolveSignalNamesWithDefaults(inst.templateReads, nil, sig)
		if hasViewportParams(params) {
			inst.demandRaster(params)
		}
	}

	// The last-good raster, pinned to the bounds it was computed for, so it
	// pans/zooms correctly under the view until the next result lands.
	// Projector.Image carries the send-once protocol the old raster opcode
	// overlay had — pixels ship on a version bump or when the host reports
	// the texture starved (a hidden tab's discarded upload, the idle LRU).
	// The raster covers one world copy; rasterCopies places it on every copy
	// the view shows, so it follows the reader across the antimeridian.
	overlay := func(p portolan.Projector) {
		vb := p.View().Bounds()
		if inst.noTiles {
			inst.land.Paint(p, inst.atlas, landoverlay.DefaultStyle())
		}
		if inst.packW > 0 && inst.packH > 0 {
			for i, shift := range rasterCopies(inst.packBounds[1], inst.packBounds[3], vb.GetWest(), vb.GetEast()) {
				p.Image(mapRasterKeys[i],
					portolan.LatLngBoundsOf(
						portolan.LL(inst.packBounds[0], inst.packBounds[1]+shift),
						portolan.LL(inst.packBounds[2], inst.packBounds[3]+shift)),
					inst.packW, inst.packH, inst.version, inst.pixels,
				).Opacity(float32(inst.opacity)).Send()
			}
		}
	}

	// Sizing: fill the (no-scroll, bounded) tab body so the whole map is
	// always visible; a BOXER_PLAY_MAP_SIZE override pins fixed dims instead,
	// keeping scripted captures deterministic across hosts.
	if inst.fixedSize {
		inst.pm.Render(float32(inst.mapWidth), float32(inst.mapHeight), overlay)
	} else {
		inst.pm.RenderFill(float32(inst.mapWidth), float32(inst.mapHeight), overlay)
	}
}

// demandRaster demands the raster node for this frame's compiled params,
// repacks a newly served result, and moves the sampling ladder: a derived
// level the server lacks is skipped, and a level whose own result landed
// hands over to the next (play_map_ladder.go).
func (inst *MapDriver) demandRaster(params map[string]string) {
	// Read by the run this demand may start: a Refresh's climb computes
	// every level afresh rather than reading what the cache holds.
	inst.cacheUse.Store(inst.cache)
	inst.cacheFresh.Store(inst.ladder.noBudget)
	if inst.ladder.fresh {
		inst.ladder.fresh = false
		inst.jumpToMemo(params)
	}
	node := compiledNode{SQL: inst.template, Params: params}
	if inst.serveFromMemo(node.key()) {
		inst.memoOnScreen = true
		return
	}
	view := inst.lane.demand(node)
	inst.noteLane(view)
	// The current level's own result, not a last-good one from the
	// level before it.
	landed := view.key == node.key() && !view.loading
	if landed && view.err != nil && isUnknownTable(view.err) && inst.ladder.dropMissing() {
		// A derived level the server does not have: skip it.
		inst.laneErr = nil
		inst.rebuildLevelTemplate()
	}
	if view.rec != nil {
		// A raster drawn from memory stays until this demand's own result
		// lands: the lane's last-good one belongs to another view.
		if view.fingerprint != inst.lastPackedFP && (landed || !inst.memoOnScreen) {
			inst.repack(view.rec, view.params, view.fingerprint)
			inst.memoOnScreen = false
		}
		view.rec.Release()
	}
	if landed && view.err == nil && view.rec != nil {
		// Two levels can serve identical bytes, which repack skips; the
		// level on screen is the one whose result landed either way.
		inst.memoOnScreen = false
		inst.packLevel = inst.ladder.current()
		inst.remember(node.key(), view.elapsed)
		if inst.ladder.served(view.elapsed) {
			inst.rebuildLevelTemplate()
		}
	}
}

// noteLane mirrors one demand's lane state onto the panel. Every field is
// rewritten each demand so none can latch: the loading flag drives the progress
// row, the error is the lane's own (nil clears it — the review finding this
// shape came from), the stats belong to the served result, and a started run
// makes the cancel notice stale.
func (inst *MapDriver) noteLane(view laneView) {
	inst.loading = view.loading
	if view.loading {
		inst.cancelled = false
	}
	inst.laneErr = view.err
	inst.stats = statsFromLane(view)
}

// syncProgress folds this frame's lane tick into the panel's estimator. Called
// once per frame at the top of Render, before renderControls reads
// frameProgress: the damped ETA is stateful, so folding the same remaining work
// twice in a frame is only harmlessly wrong by accident of the damping rule.
//
// The lane id is the tracker's re-anchor witness and there is one lane here, so
// it is constant; a NEW run on it re-anchors through the tracking gate instead
// — the lane clears its fresh flag when a run starts, lands, or is aborted, and
// a frame that observes fresh=false drops the tracking.
func (inst *MapDriver) syncProgress() {
	p, fresh := inst.lane.progressView()
	inst.frameProgress = inst.progress.observe(time.Now(), "map", p, fresh)
}

// cancelFetch aborts the in-flight raster query. The lane keeps the demand key
// it was converging on, so the per-frame demand does not immediately restart it
// (see [nodeLane.abort]): the fetch stays cancelled until the viewport or a
// control changes, or Refresh forces a re-run. The last-good raster keeps
// drawing under it — a cancel stops the fetch, not the map.
func (inst *MapDriver) cancelFetch() {
	inst.lane.abort()
	inst.loading = false
	inst.cancelled = true
}

func (inst *MapDriver) renderControls() {
	inst.renderTableEditor()
	for range c.Horizontal().KeepIter() {
		// With refine on, each level brings its own sampling factor; the
		// slider is the manual factor for a single table.
		c.Checkbox(inst.ids.PrepareStr("map-refine"), inst.refine, "refine").SendRespVal(&inst.refine)
		if !inst.refine {
			c.SliderF64(inst.ids.PrepareStr("map-sampling"), inst.sampling, 1, 100).
				Text("sampling").SendRespVal(&inst.sampling)
		}
		inst.renderModeCombo()
	}
	if builtinRenders[inst.renderIdx].custom {
		inst.renderColorEditor()
	}
	for range c.Horizontal().KeepIter() {
		c.SliderF64(inst.ids.PrepareStr("map-opacity"), inst.opacity, 0.1, 1.0).
			Text("opacity").SendRespVal(&inst.opacity)
		c.Checkbox(inst.ids.PrepareStr("map-live"), inst.live, "live").SendRespVal(&inst.live)
		c.Checkbox(inst.ids.PrepareStr("map-notiles"), inst.noTiles, "no basemap").
			SendRespVal(&inst.noTiles)
		c.Checkbox(inst.ids.PrepareStr("map-cache"), inst.cache, "server cache").
			SendRespVal(&inst.cache)
		if c.Button(inst.ids.PrepareStr("map-refresh"),
			c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.requestRefresh()
		}
		// A raster fetch in flight gets the mini-UI the main query has in the
		// top bar — spinner, Cancel, bar, numbers — pointed at THIS panel's
		// lane. It rides this row rather than taking one of its own: the map
		// fills what the controls leave, so a row that appears with the run
		// would shrink the map, change vp_h, and re-key the demand (see
		// renderLaneProgress). Beside Refresh the row height cannot move.
		//
		// This is also why statusLine has no loading case: the numbers here
		// say it better, and the line below goes on reporting the last-good
		// raster that is still on screen.
		//
		// Between fetches the same slot carries what the last one cost, so the
		// counters climbing during a run settle into the run's accounting
		// rather than vanishing with it.
		switch {
		case inst.loading:
			c.Separator().Vertical().Send()
			if renderLaneProgress(inst.ids, "map-cancel", inst.frameProgress) {
				inst.cancelFetch()
			}
		case inst.stats.valid:
			c.Separator().Vertical().Send()
			diagWeak(formatLaneStats(inst.stats))
		}
	}
	c.Label(inst.statusLine()).Send()
}

// mapTablePaneH caps the table-source editor. Six rows, against the colour
// block's four, and it SHRINKS to its content where the colour pane is pinned —
// the two controls want opposite things and the asymmetry is the point:
//
//   - A source is usually a table name, so a pinned pane would spend four rows
//     of the panel on one word forever.
//   - It is occasionally a formatted statement, so it must be allowed to grow.
//   - The vp_h churn that growth causes, which is what pinned the colour pane,
//     costs nothing here: editing the source changes the node TEMPLATE, which
//     re-keys the raster demand by itself. The map re-fetches either way.
const mapTableRows = 6

var mapTablePaneH = float32(mapTableRows*mapColorRowH + 8.0)

// renderTableEditor draws the raster's table source, folded behind its own
// header.
//
// Multi-line on demand: it starts at one row, grows as the source does, and
// stops at mapTablePaneH with the overflow scrolling inside the pane. That is
// what [sqleditor.FieldFrame.MultiLine] buys over `Rows: 1` — a line break
// becomes a character the user may type, so a subquery or a long table function
// can be laid out readably instead of living on one unreadable line.
//
// The source is a SQL fragment, so it takes the panel's full width rather than
// the fixed 240pt it used to sit at, where a long one wrapped into the map.
//
// What the user types is what the editor keeps; sanitizeTable folds the breaks
// back out on the way into the generated template, so the formatting is an
// editing convenience and not a change to the SQL that runs.
func (inst *MapDriver) renderTableEditor() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("map-table-hdr"),
		c.WidgetText().Text("Table source").Keep()).
		DefaultOpen(true).KeepIter() {
		// Max without a min, and vertical auto-shrink left ON: this pane is a
		// CEILING, not a fixed size. Horizontal auto-shrink off, so the field
		// fills the panel instead of hugging a short table name.
		c.UiSetMaxHeight(mapTablePaneH)
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, true).KeepIter() {
			inst.tableField.Render(sqleditor.FieldFrame{
				Value:     &inst.table,
				Hint:      "table, or a table function",
				Rows:      1,
				MultiLine: true,
				Width:     float32(math.Inf(1)),
			})
		}
		for range c.Horizontal().KeepIter() {
			c.Label("time column").Send()
			c.TextEdit(inst.ids.PrepareStr("map-time-col"), inst.timeCol, false).
				HintText("none").SendRespVal(&inst.timeCol)
			for rt := range c.RichTextLabel("filtered by the Timeline's brushed window") {
				rt.Small().Weak()
			}
		}
	}
}

// readWindow takes the Timeline's brushed window off this frame's signals.
// The unbounded values tl_from/tl_to carry with nothing brushed read as no
// window.
func (inst *MapDriver) readWindow(sig SignalEnvI) {
	inst.windowFrom, inst.windowTo = "", ""
	if sig == nil {
		return
	}
	from, ok1 := sig.Get(signalTimelineFrom)
	to, ok2 := sig.Get(signalTimelineTo)
	if !ok1 || !ok2 || (from.Raw == timelineWindowFloor && to.Raw == timelineWindowCeil) {
		return
	}
	inst.windowFrom, inst.windowTo = from.Raw, to.Raw
}

// windowStatus names the window the raster is filtered to, for the status
// line; empty when it is not filtered.
func (inst *MapDriver) windowStatus() string {
	if ww, _ := inst.windowWhere(); ww == "" {
		return ""
	}
	return fmt.Sprintf("window %s on %s", formatWindow(inst.windowFrom, inst.windowTo), strings.TrimSpace(inst.timeCol))
}

// formatWindow shortens a window's two raw bounds for reading: to the second,
// and the date once when both fall on the same day.
func formatWindow(from, to string) string {
	trim := func(s string) string {
		if i := strings.IndexByte(s, '.'); i >= 0 {
			return s[:i]
		}
		return s
	}
	from, to = trim(from), trim(to)
	if len(from) > 11 && len(to) > 11 && from[:11] == to[:11] {
		to = to[11:]
	}
	return from + " – " + to + " UTC"
}

// mapTimeColRe admits a plain column name for the time column.
var mapTimeColRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// windowWhere is the predicate the brushed window adds to the raster's WHERE,
// read through the tl_from/tl_to slots so a new brush re-keys on params; empty
// with no window or no time column.
func (inst *MapDriver) windowWhere() (where string, err string) {
	col := strings.TrimSpace(inst.timeCol)
	if inst.windowFrom == "" || col == "" {
		return
	}
	if !mapTimeColRe.MatchString(col) {
		err = "time column must be a plain column name"
		return
	}
	where = col + " BETWEEN {tl_from:DateTime64(3, 'UTC')} AND {tl_to:DateTime64(3, 'UTC')}"
	return
}

// Geometry of the custom-colour editor's pane.
//
// The pane exists to make the editor's occupied height CONSTANT. A multiline
// egui TextEdit sizes to max(desired_rows, content), and the map fills what the
// controls leave — so an unbounded editor that grew with the expression would
// shrink the map, change vp_h, and re-key the raster demand (the same coupling
// that keeps the progress row in renderControls on the button row instead of a
// row of its own). Unpinned, a long expression re-fetches the raster on the
// keystroke that adds a line.
//
// Measured through the play tour (apps/play/scenes/05_map_custom_color.scene.md):
// with the pin in place the raster's top edge lands on the same y for the
// default three-line expression and for one several times too tall for the
// pane — the overflow scrolls inside it instead.
const (
	// Desired rows of the field itself — four, which is what egui's multiline
	// default gave this control before it was a SQL field.
	mapColorRows = 4
	// Per-row allowance. A four-row field measured 64pt tall at Standard
	// density, so ~14pt of row plus the TextEdit's own margins; 15 is that
	// rounded up, so the Roomy density's larger row still fits.
	mapColorRowH = 15.0
	// The pinned pane height.
	mapColorPaneH = mapColorRows*mapColorRowH + 8.0
)

// renderColorEditor draws the custom render's colour expression, folded behind
// its own header and pinned to a fixed pane.
//
// Three things this shape buys over the bare field it replaces:
//
//   - Collapsible, so the expression can be written once and folded away; the
//     panel's other controls are one-liners and a four-row editor between them
//     is the clutter. DefaultOpen(true) because the block appears only when the
//     Custom render is selected — selecting it and being shown nothing would
//     read as a broken mode. egui persists the fold per id, so a user who
//     collapses it keeps it collapsed.
//   - A pane of fixed height, so a long expression scrolls inside it (see the
//     consts above for why growth here is worse than untidy).
//   - Infinite desired width, so the editor fills the panel rather than sitting
//     at a fixed 420pt with the rest of the row empty.
//
// The height bound goes on the ui the ScrollArea is constructed IN, not inside
// its body. egui sizes a scroll viewport from the available height at
// construction; a set_max_height on the CONTENT ui re-bounds only the content,
// which is the thing that was already free to grow. With the calls inside, a
// five-row expression grew the pane to five rows — measured, not reasoned.
//
// Min as well as max: the max alone caps the pane, but the ScrollArea still
// shrinks to a short expression, and the height the PANEL gives up is what the
// map's vp_h is computed from. The min is what makes that constant. A short
// expression therefore underfills its pane by a few points, which is the price.
func (inst *MapDriver) renderColorEditor() {
	for range c.CollapsingHeader(inst.ids.PrepareStr("map-color-hdr"),
		c.WidgetText().Text("Colour expression").Keep()).
		DefaultOpen(true).KeepIter() {
		c.UiSetMinHeight(mapColorPaneH)
		c.UiSetMaxHeight(mapColorPaneH)
		for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
			inst.colorField.Render(sqleditor.FieldFrame{
				Value: &inst.customColorSQL,
				Rows:  mapColorRows,
				Width: float32(math.Inf(1)),
			})
		}
	}
}

// renderModeCombo is the colour-mode picker (mirrors play_projection's
// renderColorByCombo). Changing the mode forces a refetch so the new colours
// apply immediately, whether or not "live" is on.
func (inst *MapDriver) renderModeCombo() {
	cur := builtinRenders[inst.renderIdx].name
	for range c.ComboBox(inst.ids.PrepareStr("map-render"),
		c.WidgetText().Text("render").Keep(),
		c.WidgetText().Text(cur).Keep()).
		KeepIter() {
		for i, r := range builtinRenders {
			if c.Button(inst.ids.PrepareSeq(uint64(0x4000+i)),
				c.Atoms().Text(r.name).Keep()).
				Frame(false).
				Selected(i == inst.renderIdx).
				SendResp().HasPrimaryClicked() {
				inst.renderIdx = i
				inst.requestRefresh()
			}
		}
	}
}

// requestRefresh forces a re-fetch of the current view: the request-dedup key
// is cleared AND the lane memo is forgotten. Without the forget, an unchanged
// viewport re-demands the identical SQL and memo-hits — the Refresh button was
// a no-op after the 3f lane migration (review finding); the fingerprint repack
// guard then picks up whatever the re-fetch returns, changed or not.
func (inst *MapDriver) requestRefresh() {
	inst.forceRefresh = true // re-emit even if the camera is unchanged
	inst.lane.forget()       // re-execute even for the identical (SQL, params)
	// Start the ladder over, and let it climb past its budget this time.
	inst.ladder.inputs = ""
	inst.refreshPending = true
	inst.memo.clear() // nothing drawn from memory after an explicit Refresh
}

// rebuildLevelTemplate points the raster node at the ladder's current level,
// with the render the last settle used; the vp_* signals are unchanged, so
// the next demand re-keys on the SQL alone.
func (inst *MapDriver) rebuildLevelTemplate() {
	lv := inst.ladder.current()
	inst.ensureTemplate(lv.table, lv.sampling, inst.colorSQL, inst.extraWhere)
}

// updateViewport publishes the settled viewport as the six reserved vp_*
// signals (ADR-0096 §SD6 via the ADR-0097 signal store): the mercator bbox
// (min_y = north) and the clamped output size. The store dedups unchanged
// values, so a still camera is write-free, and the values are visible to the
// per-frame compile from the NEXT frame's snapshot (5a frame consistency). It
// also (re)builds the node template from the panel controls. Bad table names
// surface in the status line; degenerate viewports are skipped.
func (inst *MapDriver) updateViewport(minLat, maxLat, minLon, maxLon float64, screenW, screenH float32, emit SignalEmitterI) {
	table := sanitizeTable(inst.table)
	if table == "" {
		inst.controlErr = "invalid or empty table name (allowed: letters, digits, '_', '.', and table functions)"
		return
	}
	inst.controlErr = ""
	west, east, fracX := foldViewLon(minLon, maxLon)
	b, ok := bboxFromLatLon(minLat, maxLat, west, east)
	if !ok {
		return
	}
	// The raster covers only the part of the view the bbox kept (one world
	// copy, the poles clamped); size it to that part so its pixels stay
	// screen-sized rather than stretched.
	w := clampDim(screenW * float32(fracX))
	h := clampDim(screenH * float32(latCoverage(minLat, maxLat)))
	sampling := max(uint32(inst.sampling), 1)
	r := builtinRenders[inst.renderIdx]
	colorSQL := r.colorSQL
	if r.custom {
		colorSQL = inst.customColorSQL
	}
	where := r.where
	ww, werr := inst.windowWhere()
	if werr != "" {
		inst.controlErr = werr
		return
	}
	if ww != "" {
		if where != "" {
			where = "(" + where + ") AND "
		}
		where += ww
	}
	// The ladder restarts only when what it was built for changed: this runs
	// on every settled frame, and a restart re-demands the coarsest level.
	levels := mapLadderLevels(table, sampling, inst.refine, inst.ladder.missing)
	// Keyed on the source, not on the levels: dropping a missing level must
	// not read as a change.
	inputs := fmt.Sprintf("%v|%d|%d|%s|%d|%t|%s|%s|%s|%s", b, w, h, table, sampling, inst.refine, colorSQL, where, inst.windowFrom, inst.windowTo)
	if inst.ladder.reset(inputs, levels) {
		inst.ladder.noBudget = inst.refreshPending
	}
	inst.refreshPending = false
	inst.colorSQL, inst.extraWhere = colorSQL, where
	inst.rebuildLevelTemplate()

	emit.Emit("vp_min_x", uint64(b.minX))
	emit.Emit("vp_max_x", uint64(b.maxX))
	emit.Emit("vp_min_y", uint64(b.minY))
	emit.Emit("vp_max_y", uint64(b.maxY))
	emit.Emit("vp_w", uint64(w))
	emit.Emit("vp_h", uint64(h))
}

// ensureTemplate rebuilds the raster node's SQL when a panel control changed
// and re-derives the slot names its compile resolves (parsed off the template;
// a custom colour block outside Grammar1 falls back to the reserved six, so
// the viewport always resolves).
func (inst *MapDriver) ensureTemplate(table string, sampling uint32, colorSQL, extraWhere string) {
	tmpl := rasterTemplateSQL(table, sampling, colorSQL, extraWhere)
	if tmpl == inst.template {
		return
	}
	inst.template = tmpl
	inst.templateReads = inst.templateReads[:0]
	if slots, _, err := extractSlotsAndParams(tmpl); err == nil {
		for _, s := range slots {
			inst.templateReads = append(inst.templateReads, s.Name)
		}
	} else {
		for _, s := range mapViewportSignals {
			inst.templateReads = append(inst.templateReads, string(s))
		}
	}
}

// hasViewportParams reports whether the compiled params carry the full
// reserved vp_* set — the gate for demanding the raster node (the signals
// land one frame after the first settle).
func hasViewportParams(params map[string]string) bool {
	for _, s := range mapViewportSignals {
		if _, found := params["param_"+string(s)]; !found {
			return false
		}
	}
	return true
}

// viewportFromParams recovers the mercator bbox + raster dims from a compiled
// vp_* param set (the served node inputs).
func viewportFromParams(params map[string]string) (b mercBox, w, h uint32, err error) {
	get := func(name SignalID) (v uint32, gErr error) {
		raw, found := params["param_"+string(name)]
		if !found {
			gErr = eb.Build().Str("param", name).Errorf("the served raster result lacks a required param")
			return
		}
		u, pErr := strconv.ParseUint(raw, 10, 32)
		if pErr != nil {
			gErr = eb.Build().Str("param", name).Str("value", raw).Errorf("the served param value is not a UInt32: %w", pErr)
			return
		}
		v = uint32(u)
		return
	}
	if b.minX, err = get("vp_min_x"); err != nil {
		return
	}
	if b.maxX, err = get("vp_max_x"); err != nil {
		return
	}
	if b.minY, err = get("vp_min_y"); err != nil {
		return
	}
	if b.maxY, err = get("vp_max_y"); err != nil {
		return
	}
	if w, err = get("vp_w"); err != nil {
		return
	}
	h, err = get("vp_h")
	return
}

// repack packs the lane's record into the RGBA texture, pinned to lat/lon
// bounds recovered from the SERVED vp_* params themselves (inverse
// Web-Mercator): the overlay is self-describing — raster and query can never
// disagree about the bounds, even when the signals were seeded from elsewhere
// (a history restore) — and the former demanded-SQL→bounds side table retired
// with slice 5c. Called only when the served fingerprint changes.
func (inst *MapDriver) repack(rec arrow.RecordBatch, served map[string]string, fingerprint uint64) {
	b, w, h, err := viewportFromParams(served)
	if err != nil {
		inst.packErr = err
		return
	}
	pixels, err := packRaster(rec, w, h)
	if err != nil {
		inst.packErr = err
		return
	}
	inst.pixels = pixels
	inst.packW = w
	inst.packH = h
	// The y-flip mirrors bboxFromLatLon: min mercator y is the NORTH edge.
	inst.packBounds = [4]float64{
		mercYToLat(float64(b.maxY)), mercXToLon(float64(b.minX)),
		mercYToLat(float64(b.minY)), mercXToLon(float64(b.maxX)),
	}
	inst.version++
	inst.lastPackedFP = fingerprint
	inst.packErr = nil
}

// packRaster packs a raster record into a row-major []uint32 of 0xRRGGBBAA,
// w*h long. Two shapes are read, told apart by the first column's type:
//
//   - sparse — (pos UInt32, r, g, b, a UInt8), one row per non-empty pixel in
//     any order, which the template emits: every other pixel stays 0, and a
//     pos past w*h is dropped;
//   - dense — (r, g, b, a UInt8), w*h rows in pixel order, the
//     `ORDER BY pos WITH FILL` form of the snippet; the length is padded or
//     truncated so the texture upload always matches.
//
// The sparse form replaced the dense one in the template (ADR-0096
// 2026-10-01 sparse Update): WITH FILL was most of the server's time on the
// demo slice, while lz4 makes the empty rows cheap on the wire, so the dense
// form is smaller there above a few percent fill but slower to produce.
// Columns past the raster's are ignored in either shape.
func packRaster(rec arrow.RecordBatch, w, h uint32) (pixels []uint32, err error) {
	if rec.NumCols() >= 5 {
		if pos, ok := rec.Column(0).(*array.Uint32); ok {
			return packSparseRaster(rec, pos, w, h)
		}
	}
	if rec.NumCols() < 4 {
		err = eb.Build().Int64("columns", rec.NumCols()).Errorf("raster query must SELECT 4 columns (r,g,b,a), or (pos, r, g, b, a)")
		return
	}
	ra, ga, ba, aa, err := rgbaColumns(rec, 0)
	if err != nil {
		return
	}
	n := int(w) * int(h)
	rows := int(rec.NumRows())
	pixels = make([]uint32, 0, n)
	for i := range rows {
		pixels = append(pixels, (uint32(ra.Value(i))<<24)|
			(uint32(ga.Value(i))<<16)|(uint32(ba.Value(i))<<8)|uint32(aa.Value(i)))
	}
	if len(pixels) < n {
		pixels = append(pixels, make([]uint32, n-len(pixels))...)
	} else if len(pixels) > n {
		pixels = pixels[:n]
	}
	return
}

// packSparseRaster scatters (pos, r, g, b, a) rows into a zeroed w*h buffer.
func packSparseRaster(rec arrow.RecordBatch, pos *array.Uint32, w, h uint32) (pixels []uint32, err error) {
	ra, ga, ba, aa, err := rgbaColumns(rec, 1)
	if err != nil {
		return
	}
	n := int(w) * int(h)
	pixels = make([]uint32, n)
	for i := range int(rec.NumRows()) {
		p := int(pos.Value(i))
		if p >= n {
			continue
		}
		pixels[p] = (uint32(ra.Value(i)) << 24) | (uint32(ga.Value(i)) << 16) |
			(uint32(ba.Value(i)) << 8) | uint32(aa.Value(i))
	}
	return
}

// rgbaColumns reads the four UInt8 channel columns starting at column from.
func rgbaColumns(rec arrow.RecordBatch, from int) (ra, ga, ba, aa *array.Uint8, err error) {
	var ok [4]bool
	ra, ok[0] = rec.Column(from).(*array.Uint8)
	ga, ok[1] = rec.Column(from + 1).(*array.Uint8)
	ba, ok[2] = rec.Column(from + 2).(*array.Uint8)
	aa, ok[3] = rec.Column(from + 3).(*array.Uint8)
	if !ok[0] || !ok[1] || !ok[2] || !ok[3] {
		err = eb.Build().
			Stringer("r", rec.Column(from).DataType()).Stringer("g", rec.Column(from+1).DataType()).
			Stringer("b", rec.Column(from+2).DataType()).Stringer("a", rec.Column(from+3).DataType()).
			Errorf("raster channel columns must be UInt8")
	}
	return
}

// statusLine is the line under the controls. It carries no "loading" case: the
// progress row above it is drawn under exactly that gate and says the same
// thing with numbers, so while a fetch is in flight this line goes on reporting
// the last-good raster (which is what is still on screen) or an error from the
// run being retried — neither of which the row covers.
func (inst *MapDriver) statusLine() string {
	switch {
	case inst.controlErr != "":
		return "config: " + inst.controlErr
	case inst.laneErr != nil:
		return "query error: " + inst.laneErr.Error()
	case inst.packErr != nil:
		return "raster error: " + inst.packErr.Error()
	case inst.cancelled:
		return "fetch cancelled — pan, zoom, or Refresh to run again"
	case inst.packW > 0:
		msg := fmt.Sprintf("%d×%d raster · %s", inst.packW, inst.packH, builtinRenders[inst.renderIdx].name)
		if ls := inst.ladder.status(inst.packLevel, inst.loading); ls != "" {
			msg += " · " + ls
		}
		if inst.memoOnScreen {
			msg += " · from memory"
		}
		if ws := inst.windowStatus(); ws != "" {
			msg += " · " + ws
		}
		return msg
	default:
		msg := "pan/zoom over a ClickHouse table with mercator_x/mercator_y (e.g. planes_mercator)"
		if needs := builtinRenders[inst.renderIdx].needs; len(needs) > 0 {
			msg += " · '" + builtinRenders[inst.renderIdx].name + "' needs " + strings.Join(needs, ", ")
		}
		return msg
	}
}

// rasterTemplateSQL builds the raster NODE's SQL (ADR-0096 §SD6, realized via
// the ADR-0097 signal store): the fixed geometry + density header (span_*,
// in_view, px/py/pos, zoom_factor, total, max_total, transparency, alpha)
// referencing the six reserved {vp_*:UInt32} slots, then the selected
// render's colour block spliced in and an optional extra WHERE. The viewport
// is NOT in the text — it rides the param_* channel at execution (the values
// come from the vp_* signals the panel emits), so a pan re-executes via the
// lane's (SQL, params) key with the SQL unchanged. The result is sparse — one
// (pos, r, g, b, a) row per non-empty pixel, see packRaster. The header
// assumes only mercator_x/mercator_y; what other columns are needed depends
// on colorSQL. table/sampling stay spliced panel controls.
//
// Integer division is spelled intDiv(a, b), not the `a DIV b` operator: the
// host canonicalises every executed statement (ADR-0108, CanonicalizeFull),
// and grammar1 has no DIV/MOD operator — it mis-parses `expr DIV name` as a
// chained alias and the identifier pass then quotes DIV into a syntax error.
// The function form is the canonical shape and rides through untouched. Do not
// "simplify" it back to the operator.
func rasterTemplateSQL(table string, sampling uint32, colorSQL, extraWhere string) string {
	where := "in_view"
	if strings.TrimSpace(extraWhere) != "" {
		where = "in_view AND (" + extraWhere + ")"
	}
	return fmt.Sprintf(`WITH
    toUInt64({vp_max_x:UInt32}) - {vp_min_x:UInt32} AS span_x,
    toUInt64({vp_max_y:UInt32}) - {vp_min_y:UInt32} AS span_y,
    mercator_x >= {vp_min_x:UInt32} AND mercator_x < {vp_max_x:UInt32}
        AND mercator_y >= {vp_min_y:UInt32} AND mercator_y < {vp_max_y:UInt32} AS in_view,
    least(intDiv(toUInt64(mercator_x - {vp_min_x:UInt32}) * {vp_w:UInt32}, span_x), {vp_w:UInt32} - 1) AS px,
    least(intDiv(toUInt64(mercator_y - {vp_min_y:UInt32}) * {vp_h:UInt32}, span_y), {vp_h:UInt32} - 1) AS py,
    py * {vp_w:UInt32} + px AS pos,
    (span_x / {vp_w:UInt32}) * (span_y / {vp_h:UInt32}) AS pixel_area,
    pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
    count() AS total,
    greatest(1000000. / %[2]d / zoom_factor, toFloat64(count())) AS max_total,
    pow(total / max_total, 1/5) AS transparency,
    %[3]s%[5]s
SELECT toUInt32(pos), round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM %[1]s
WHERE %[4]s
GROUP BY pos`,
		table, sampling, colorSQL, where, alphaClause(colorSQL))
}

// rasterAlphaRe finds a colour block's own alpha definition. Textual on
// purpose: a Custom block may sit outside Grammar1, and a false hit (the alias
// in a comment) costs a visible "unknown identifier alpha" from the server,
// never a silently wrong raster.
var rasterAlphaRe = regexp.MustCompile(`(?i)\bAS\s+alpha\b`)

// alphaClause is the opaque default a colour block gets unless it defines
// alpha itself — a render may carry confidence or density in alpha (ADR-0096
// 2026-10-01 Update). Pixels with no rows stay alpha 0 either way.
func alphaClause(colorSQL string) string {
	if rasterAlphaRe.MatchString(colorSQL) {
		return ""
	}
	return ",\n    255 AS alpha"
}

// mapRasterKeys name the raster's draw slots, one per world copy it can
// appear on at once; each slot keeps its own send-once texture.
var mapRasterKeys = [...]string{"map-raster", "map-raster-1", "map-raster-2"}

// foldViewLon folds a view's longitude span into the one world copy the
// mercator columns cover, for the request (ADR-0096 SD4: mercator_x spans
// lon −180..180 once). A span of a world or more asks for the whole world;
// a narrower one is shifted by whole turns so its midpoint lies in
// [−180, 180), and the part past ±180 is cut — that edge is drawn empty
// rather than requested twice (the straddling case stays deferred). fracX is
// the share of the view's width the folded span keeps.
func foldViewLon(west, east float64) (w, e, fracX float64) {
	span := east - west
	if span <= 0 {
		return west, east, 1
	}
	if span >= 360 {
		return -180, 180, 360 / span
	}
	k := math.Floor(((west+east)/2 + 180) / 360)
	w, e = west-360*k, east-360*k
	kept := min(e, 180) - max(w, -180)
	return max(w, -180), min(e, 180), kept / span
}

// latCoverage is the share of the view's mercator height that lies inside
// the Web-Mercator latitude clamp — below 1 only when the view reaches past
// a pole, at the lowest zooms.
func latCoverage(minLat, maxLat float64) float64 {
	raw := func(lat float64) float64 {
		lat = max(min(lat, 89.999), -89.999)
		return math.Asinh(math.Tan(lat / 180.0 * math.Pi))
	}
	full := raw(maxLat) - raw(minLat)
	if full <= 0 {
		return 1
	}
	kept := raw(clampLat(maxLat)) - raw(clampLat(minLat))
	return min(kept/full, 1)
}

// rasterCopies returns the longitude shifts (whole turns) at which a raster
// spanning [west, east] in lon −180..180 meets the view [viewWest, viewEast]
// — the copy nearest the view first, then its neighbours when the view is
// wide enough to show them, at most len(mapRasterKeys).
func rasterCopies(west, east, viewWest, viewEast float64) (shifts []float64) {
	near := 360 * math.Round(((viewWest+viewEast)/2-(west+east)/2)/360)
	for _, s := range [...]float64{near, near - 360, near + 360} {
		if east+s > viewWest && west+s < viewEast && len(shifts) < len(mapRasterKeys) {
			shifts = append(shifts, s)
		}
	}
	return
}

// bboxFromLatLon converts a lat/lon viewport to the mercator bbox the SQL bins
// on. mercator_x is monotone in lon and mercator_y is monotone-decreasing in
// lat (north = smaller y), so maxLat → minY. Returns ok=false on a degenerate
// (zero-span) viewport.
func bboxFromLatLon(minLat, maxLat, minLon, maxLon float64) (b mercBox, ok bool) {
	if maxLat <= minLat || maxLon <= minLon {
		return
	}
	b = mercBox{
		minX: lonToMercX(minLon),
		maxX: lonToMercX(maxLon),
		minY: latToMercY(maxLat), // north
		maxY: latToMercY(minLat), // south
	}
	if b.maxX <= b.minX || b.maxY <= b.minY {
		return mercBox{}, false
	}
	return b, true
}

// lonToMercX / latToMercY mirror the materialized-column formulas in
// apps/play/demo/adsb/setup.sql (the one projection contract — ADR-0096 §SD4),
// so the Go-computed bbox lines up with the SQL's binning. Both sides scale by
// mercWorld = 2^32, matching ClickHouse's MVT mercator space rather than the
// upstream adsb.exposed 0xFFFFFFFF.
//
// Agreement with setup.sql is EXACT — 0 differing points over a 245861-point
// grid — and three spellings keep it that way, on both sides: math.Round against
// the DDL's floor(x+0.5) (the bare cast truncates, and ClickHouse's round() is
// banker's); multiply-before-divide on the x axis; and asinh rather than log for
// the isometric latitude (see latToMercY). Change one side without the other and
// the raster shifts by a unit on a third of all points.
//
// One residual remains, by construction: a table carrying upstream-convention
// columns — filled by copying them verbatim off remoteSecure rather than
// recomputing on INSERT — reads back up to 1 unit off, from the differing world
// span. That reaches a whole raster pixel only when span_x ≲ vp_w, a viewport
// roughly a centimetre across.
func lonToMercX(lon float64) uint32 {
	return clampMerc(math.Round(mercWorld * (lon + 180.0) / 360.0))
}

// mercXToLon / mercYToLat invert lonToMercX / latToMercY (the SD4 projection
// contract run backwards): repack derives the overlay's lat/lon pin from the
// SERVED vp_* values, so raster and query cannot disagree about the bounds.
// The float64 round-trip error is far below a pixel at any zoom.
func mercXToLon(x float64) float64 {
	return x/mercWorld*360.0 - 180.0
}

func mercYToLat(y float64) float64 {
	return math.Atan(math.Exp((0.5-y/mercWorld)*2.0*math.Pi))*360.0/math.Pi - 90.0
}

// latToMercY uses the isometric latitude in its inverse-Gudermannian form,
// asinh(tan φ), rather than the textbook ln(tan(π/4 + φ/2)). The two are the
// same quantity, but ClickHouse's log() is a fast approximation carrying ~1.4e-9
// of relative error — which the 2^32 scale magnifies to a whole mercator unit,
// putting a ±1 disagreement on ~36% of points against the DDL. Its asinh() is
// correctly rounded, so this form is what makes the two sides agree exactly
// (ADR-0096 2026-07-28 Update). setup.sql must keep the matching spelling.
func latToMercY(lat float64) uint32 {
	lat = clampLat(lat)
	return clampMerc(math.Round(mercWorld * (0.5 - math.Asinh(math.Tan(lat/180.0*math.Pi))/(2.0*math.Pi))))
}

func clampLat(lat float64) float64 {
	const lim = 85.05112878 // Web-Mercator pole clamp
	if lat > lim {
		return lim
	}
	if lat < -lim {
		return -lim
	}
	return lat
}

// clampMerc saturates a projected coordinate into the UInt32 domain. The
// ceiling is mercUnitMax (2^32-1), NOT the mercWorld span (2^32): lon = +180
// and the south pole both project exactly onto the span, and Go converts an
// out-of-range float to uint32 as 0 — so clamping against the span would send
// the antimeridian to x = 0, wrapping a point at the right edge of the world to
// the left edge. Keep the two constants distinct.
func clampMerc(v float64) uint32 {
	if v < 0 {
		return 0
	}
	if v > mercUnitMax {
		return mercUnitMax
	}
	return uint32(v)
}

func clampDim(px float32) uint32 {
	v := uint32(math.Round(float64(px)))
	if v < 16 {
		return 16
	}
	if v > mapMaxDim {
		return mapMaxDim
	}
	return v
}

// sanitizeTable accepts a plain identifier OR a table-function source
// (remote / remoteSecure / url / file / merge / …) so the Map panel can read a
// remote ClickHouse — e.g. adsb.exposed via
// `remoteSecure('…:9440', default.planes_mercator_sample100, 'website', ”)`.
// It blocks only statement-breakers (terminator, comment markers) so the
// inlined source stays inside `FROM <src> WHERE …`. The playground already
// grants arbitrary SQL via the editor, so this is no new capability — just a
// guard against accidental breakage. Returns "" for empty/invalid input.
//
// A newline is NOT a rejection any more: the panel's source editor is
// multi-line on demand, so a source may legitimately be written over several
// lines, and a line break is whitespace to SQL. It is folded to a space, which
// keeps the generated template's FROM clause on one line rather than splicing
// somebody's indentation into the middle of a WITH block.
//
// Folding before the checks is safe in the direction that matters: a break
// becomes a SPACE, so it can only separate characters, never join two into a
// `--` or a `/*` that was not already there.
func sanitizeTable(s string) string {
	s = strings.TrimSpace(foldSourceLines(s))
	if s == "" {
		return ""
	}
	if strings.Contains(s, ";") || strings.Contains(s, "--") || strings.Contains(s, "/*") {
		return ""
	}
	return s
}

// foldSourceLines turns every line break in a table source into one space.
// Deliberately not shared with the identically-shaped fold in sqleditor: that
// one keeps a WIDGET one row tall, this one keeps generated SQL on one line,
// and the two would drift apart the moment either grew a rule of its own.
var sourceLineFolder = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

func foldSourceLines(s string) string {
	if !strings.ContainsAny(s, "\n\r") {
		return s
	}
	return sourceLineFolder.Replace(s)
}
