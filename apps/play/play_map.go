package play

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// MapDriver is the ADR-0096 geo-raster map panel: a walkers slippy map whose
// viewport drives an in-DB-rendered RGBA raster. Each frame it reads the
// previous frame's camera (fetchR15WalkersCamera, cached); once the camera has
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
	ids    *c.WidgetIdStack
	client *Client

	// Controls + display. The map fills the tab body by default (FillAvailable
	// — the leaf is a bounded, no-scroll host, so filling it means nothing
	// overflows or clips); mapWidth/mapHeight apply only when
	// SPINNAKER_PLAY_MAP_SIZE pins a fixed size (fixedSize) for deterministic
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
	inited       bool
	forceRefresh bool

	// renderIdx selects builtinRenders; customColorSQL is the colour expression
	// used when the "Custom" render is active.
	renderIdx      int
	customColorSQL string

	// Debounce on the quantized viewHash: reset the timer whenever it changes,
	// fire only once the camera has been stable for mapDebounce.
	lastViewHash    uint64
	viewStableAt    time.Time
	lastSentVersion uint64

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

	loading bool
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
var mapViewportSignals = [...]SignalID{"vp_min_x", "vp_max_x", "vp_min_y", "vp_max_y", "vp_w", "vp_h"}

const (
	mapRasterID uint64 = 1
	mapDebounce        = 250 * time.Millisecond
	mapMaxDim   uint32 = 1024         // bounds query cost + Arrow size per view
	mercMax            = 4294967295.0 // 0xFFFFFFFF — full Web-Mercator world
)

// mapFetchTimeout bounds one raster round-trip. Generous because a remote()
// source (e.g. adsb.exposed via remoteSecure over a transatlantic link) can
// take ~20s per tile. A var so live tests can adjust it.
var mapFetchTimeout = 60 * time.Second

type mercBox struct{ minX, maxX, minY, maxY uint32 }

// rasterRender is a swappable colour mode (ADR-0096 §SD6). The geometry + density
// header of the raster query (span_*, in_view, px/py/pos, zoom_factor, total,
// max_total, transparency, alpha) is render-agnostic; a render supplies only the
// colour block and an optional extra WHERE. This is what lets the panel target
// any table with mercator_x/mercator_y, not just the ADS-B schema.
type rasterRender struct {
	name string // UI label + fetch-key component
	// colorSQL is spliced into the WITH block after `255 AS alpha,`. In scope:
	// total, max_total, transparency, alpha, plus any table column via aggregates
	// (avg(col), …). It MUST define red, green, blue (Float64, 0..255) and end
	// without a trailing comma. Ignored when custom (customColorSQL is used).
	colorSQL string
	where    string   // optional predicate ANDed with in_view; "" = none
	needs    []string // columns beyond mercator_x/y assumed; nil = table-agnostic
	custom   bool     // colorSQL comes from the panel's editable field
}

// builtinRenders are the selectable colour modes. "Altitude & Velocity" is the
// upstream adsb default (needs altitude+ground_speed); "Density" assumes only
// mercator_x/y so it works on ANY geo-point table; "Custom" takes a user-typed
// red/green/blue expression, matching the playground's arbitrary-table freedom.
var builtinRenders = []rasterRender{
	{
		name:  "Altitude & Velocity",
		needs: []string{"altitude", "ground_speed"},
		colorSQL: `greatest(0, least(avg(altitude), 5000)) / 5000 AS color1,
    greatest(0, least(avg(altitude), 50000)) / 50000 AS color3,
    greatest(0, least(avg(ground_speed), 700)) / 700 AS color2,
    (1 + transparency) / 2 * (1 - color3) * 255 AS red,
    transparency * color1 * 255 AS green,
    color2 * 255 AS blue`,
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
		colorSQL: `greatest(0, least(avg(ground_speed), 700)) / 700 AS s,
    transparency * (1 - s) * 255 AS red,
    transparency * 90 AS green,
    transparency * s * 255 AS blue`,
	},
	{
		name:   "Custom",
		custom: true,
	},
}

func NewMapDriver(ids *c.WidgetIdStack, client *Client) *MapDriver {
	d := &MapDriver{
		ids:    ids,
		client: client,
		// The stable query_id + replace_running_query make a superseding
		// pan/zoom fetch replace its predecessor server-side (SD5).
		lane:      newNodeLane(clientExecutor{client: client, opts: newExecOptions("map")}, memory.NewGoAllocator(), mapFetchTimeout),
		table:     "planes_mercator_sample100",
		sampling:  100,
		opacity:   0.9,
		noTiles:   true,
		live:      true,
		mapWidth:  960,
		mapHeight: 560,
		initLat:   40.0,
		initLon:   0.0,
		initZoom:  4.0,
		// renderIdx 0 = "Altitude & Velocity" (matches the demo table); Custom
		// starts from an editable density expression.
		customColorSQL: "transparency * 255 AS red,\n    transparency * 200 AS green,\n    transparency * 120 AS blue",
	}
	// Scripted-screenshot overrides — the SPINNAKER_PLAY_MAP_* knobs from the
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

// Render draws the controls, the last-good raster overlay, and the map; and,
// once the viewport has settled (debounced), publishes it as the vp_* signals
// and demands the raster node compiled against the frame's signal snapshot
// (sig; emits land next frame — the 5a frame consistency). Order matters: the
// mapRaster overlay is a register-drain node, so it must be emitted BEFORE the
// walkersMap that drains it.
func (inst *MapDriver) Render(sig SignalEnvI, emit SignalEmitterI) {
	inst.renderControls()

	// Previous frame's camera (drained at last frame's end). Reading the cache
	// rather than inline-fetching avoids the dock.Tab render-loop deadlock —
	// same idiom as the walkers demo's demoWalkersCamera.
	cam := c.CurrentApplicationState.StateManager.GetWalkersCamera()
	if cam.Found {
		if cam.ViewHash != inst.lastViewHash {
			inst.lastViewHash = cam.ViewHash
			inst.viewStableAt = time.Now()
		}
		settled := !inst.viewStableAt.IsZero() && time.Since(inst.viewStableAt) >= mapDebounce
		if (inst.live && settled) || inst.forceRefresh {
			inst.forceRefresh = false
			inst.updateViewport(cam.MinLat, cam.MaxLat, cam.MinLon, cam.MaxLon,
				cam.ScreenWidthPx, cam.ScreenHeightPx, emit)
		}
	}

	// Compile the node from the frame snapshot and demand it (non-blocking;
	// a changed viewport or control supersedes the in-flight run via the
	// lane's (SQL, params) memo key; the last-good result is returned while a
	// new one loads). Gated on the full vp_* set — the signals land one frame
	// after the first settle. Re-pack only when the served fingerprint moves.
	if inst.template != "" {
		params := resolveSignalNames(inst.templateReads, nil, sig)
		if hasViewportParams(params) {
			view := inst.lane.demand(compiledNode{SQL: inst.template, Params: params})
			inst.loading = view.loading
			inst.laneErr = view.err // mirrored every demand — nil clears (no latch)
			if view.rec != nil {
				if view.fingerprint != inst.lastPackedFP {
					inst.repack(view.rec, view.params, view.fingerprint)
				}
				view.rec.Release()
			}
		}
	}

	// Draw the last-good raster, pinned to the bounds it was computed for, so it
	// pans/zooms correctly under the camera until the next result lands.
	if inst.packW > 0 && inst.packH > 0 {
		toSend := inst.pixels
		if inst.version == inst.lastSentVersion {
			toSend = []uint32{} // unchanged → reuse the cached texture (empty, NOT nil)
		}
		c.MapRaster(mapRasterID,
			inst.packBounds[0], inst.packBounds[1], inst.packBounds[2], inst.packBounds[3],
			inst.packW, inst.packH, inst.version, toSend,
		).Opacity(float32(inst.opacity)).Send()
		inst.lastSentVersion = inst.version
	}

	// The map (drains the overlay, reports the next camera). noTiles keeps it
	// offline; flip it off for the built-in OSM basemap (needs network).
	// Sizing: fill the (no-scroll, bounded) tab body so the whole map is
	// always visible; a SPINNAKER_PLAY_MAP_SIZE override pins fixed dims
	// instead, keeping scripted captures deterministic across hosts.
	mw := c.WalkersMap(inst.ids.PrepareStr("map"),
		inst.initLat, inst.initLon, inst.noTiles,
	)
	if inst.fixedSize {
		mw = mw.Width(float32(inst.mapWidth)).Height(float32(inst.mapHeight))
	} else {
		mw = mw.FillAvailable(true)
	}
	if !inst.noTiles {
		mw = mw.TileUrl("")
	}
	if !inst.inited {
		mw = mw.SetZoom(inst.initZoom)
		inst.inited = true
	}
	mw.Send()
}

func (inst *MapDriver) renderControls() {
	for range c.Horizontal().KeepIter() {
		c.Label("table").Send()
		c.TextEdit(inst.ids.PrepareStr("map-table"), inst.table, false).
			DesiredWidth(240).SendRespVal(&inst.table)
		c.SliderF64(inst.ids.PrepareStr("map-sampling"), inst.sampling, 1, 100).
			Text("sampling").SendRespVal(&inst.sampling)
	}
	for range c.Horizontal().KeepIter() {
		inst.renderModeCombo()
	}
	if builtinRenders[inst.renderIdx].custom {
		c.TextEdit(inst.ids.PrepareStr("map-color"), inst.customColorSQL, true).
			DesiredWidth(420).SendRespVal(&inst.customColorSQL)
	}
	for range c.Horizontal().KeepIter() {
		c.SliderF64(inst.ids.PrepareStr("map-opacity"), inst.opacity, 0.1, 1.0).
			Text("opacity").SendRespVal(&inst.opacity)
		c.Checkbox(inst.ids.PrepareStr("map-live"), inst.live, "live").SendRespVal(&inst.live)
		c.Checkbox(inst.ids.PrepareStr("map-notiles"), inst.noTiles, "no basemap").
			SendRespVal(&inst.noTiles)
		if c.Button(inst.ids.PrepareStr("map-refresh"),
			c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.requestRefresh()
		}
	}
	c.Label(inst.statusLine()).Send()
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
	b, ok := bboxFromLatLon(minLat, maxLat, minLon, maxLon)
	if !ok {
		return
	}
	w := clampDim(screenW)
	h := clampDim(screenH)
	sampling := uint32(inst.sampling)
	if sampling < 1 {
		sampling = 1
	}
	r := builtinRenders[inst.renderIdx]
	colorSQL := r.colorSQL
	if r.custom {
		colorSQL = inst.customColorSQL
	}
	inst.ensureTemplate(table, sampling, colorSQL, r.where)

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
			gErr = fmt.Errorf("served raster result lacks the %s param", name)
			return
		}
		u, pErr := strconv.ParseUint(raw, 10, 32)
		if pErr != nil {
			gErr = fmt.Errorf("served %s = %q is not a UInt32: %w", name, raw, pErr)
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

// packRaster packs a dense 4×UInt8 (r,g,b,a) raster record into a row-major
// []uint32 of 0xRRGGBBAA. WITH FILL yields exactly w*h rows; the length is
// padded/truncated defensively so the texture upload always matches.
func packRaster(rec arrow.RecordBatch, w, h uint32) (pixels []uint32, err error) {
	if rec.NumCols() < 4 {
		err = fmt.Errorf("raster query must SELECT 4 columns (r,g,b,a); got %d", rec.NumCols())
		return
	}
	ra, ok1 := rec.Column(0).(*array.Uint8)
	ga, ok2 := rec.Column(1).(*array.Uint8)
	ba, ok3 := rec.Column(2).(*array.Uint8)
	aa, ok4 := rec.Column(3).(*array.Uint8)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		err = fmt.Errorf("raster columns must be UInt8 (got %s, %s, %s, %s)",
			rec.Column(0).DataType(), rec.Column(1).DataType(),
			rec.Column(2).DataType(), rec.Column(3).DataType())
		return
	}
	n := int(w) * int(h)
	rows := int(rec.NumRows())
	pixels = make([]uint32, 0, n)
	for i := 0; i < rows; i++ {
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

func (inst *MapDriver) statusLine() string {
	switch {
	case inst.controlErr != "":
		return "config: " + inst.controlErr
	case inst.laneErr != nil:
		return "query error: " + inst.laneErr.Error()
	case inst.packErr != nil:
		return "raster error: " + inst.packErr.Error()
	case inst.loading:
		return "rendering tile…"
	case inst.packW > 0:
		return fmt.Sprintf("%d×%d raster · %s", inst.packW, inst.packH, builtinRenders[inst.renderIdx].name)
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
// lane's (SQL, params) key with the SQL unchanged. Server-verified: ClickHouse
// substitutes the slots everywhere they appear, including the
// `WITH FILL … TO` bound (the wiring check ADR-0096 called out). The header
// assumes only mercator_x/mercator_y; what other columns are needed depends
// on colorSQL. table/sampling stay spliced panel controls.
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
    least((toUInt64(mercator_x - {vp_min_x:UInt32}) * {vp_w:UInt32}) DIV span_x, {vp_w:UInt32} - 1) AS px,
    least((toUInt64(mercator_y - {vp_min_y:UInt32}) * {vp_h:UInt32}) DIV span_y, {vp_h:UInt32} - 1) AS py,
    py * {vp_w:UInt32} + px AS pos,
    (span_x / {vp_w:UInt32}) * (span_y / {vp_h:UInt32}) AS pixel_area,
    pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
    count() AS total,
    greatest(1000000. / %[2]d / zoom_factor, toFloat64(count())) AS max_total,
    pow(total / max_total, 1/5) AS transparency,
    255 AS alpha,
    %[3]s
SELECT round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM %[1]s
WHERE %[4]s
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64({vp_w:UInt32}) * {vp_h:UInt32}`,
		table, sampling, colorSQL, where)
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

// lonToMercX / latToMercY mirror the materialized-column formulas in the
// adsb.exposed setup.sql exactly (the one projection contract — ADR-0096 §SD4),
// so the Go-computed bbox aligns pixel-for-pixel with the SQL's binning.
func lonToMercX(lon float64) uint32 {
	return clampMerc(math.Round(mercMax * (lon + 180.0) / 360.0))
}

// mercXToLon / mercYToLat invert lonToMercX / latToMercY (the SD4 projection
// contract run backwards): repack derives the overlay's lat/lon pin from the
// SERVED vp_* values, so raster and query cannot disagree about the bounds.
// The float64 round-trip error is far below a pixel at any zoom.
func mercXToLon(x float64) float64 {
	return x/mercMax*360.0 - 180.0
}

func mercYToLat(y float64) float64 {
	return math.Atan(math.Exp((0.5-y/mercMax)*2.0*math.Pi))*360.0/math.Pi - 90.0
}

func latToMercY(lat float64) uint32 {
	lat = clampLat(lat)
	return clampMerc(math.Round(mercMax * (0.5 - math.Log(math.Tan((lat+90.0)/360.0*math.Pi))/(2.0*math.Pi))))
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

func clampMerc(v float64) uint32 {
	if v < 0 {
		return 0
	}
	if v > mercMax {
		return uint32(mercMax)
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
// It blocks only statement-breakers (terminator, comment markers, newlines) so
// the inlined source stays inside `FROM <src> WHERE …`. The playground already
// grants arbitrary SQL via the editor, so this is no new capability — just a
// guard against accidental breakage. Returns "" for empty/invalid input.
func sanitizeTable(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.ContainsAny(s, ";\n\r") || strings.Contains(s, "--") || strings.Contains(s, "/*") {
		return ""
	}
	return s
}
