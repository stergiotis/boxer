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
// previous frame's camera (fetchR15WalkersCamera, cached), and once the camera
// has settled it injects the viewport mercator bbox into a bbox-variant raster
// query run on a panel-local async lane (the play_timeline_bands precedent),
// packs the 4×UInt8 result to RGBA, and draws it as a mapRaster overlay pinned
// to the viewport's lat/lon bounds.
//
// First cut: the render is the upstream "Altitude & Velocity" colour math
// generalised to an arbitrary viewport, so it assumes the adsb.exposed schema
// (mercator_x / mercator_y / altitude / ground_speed). The table + sampling are
// user controls; values are inlined as literals (this is a power-user SQL
// playground — the editor already grants arbitrary query access). Not yet wired:
// the keepBuffer margin, the progressive sampling ladder, hover→info queries,
// and a configurable render — all SD10 deferrals in the ADR.
type MapDriver struct {
	ids    *c.WidgetIdStack
	client *Client

	// Controls + display.
	table        string
	sampling     float64
	opacity      float64
	noTiles      bool
	live         bool
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
	lastViewHash     uint64
	viewStableAt     time.Time
	lastRequestedKey mapFetchKey
	lastSentVersion  uint64

	// lane runs the raster query off the render thread (ADR-0097 3f): the
	// node-graph nodeLane replaces the former bespoke fetch goroutine + staleness
	// guard (non-blocking demand, supersession, last-good — all reused). demandedSQL
	// is what the lane is asked for; sqlMeta maps each demanded SQL to the
	// bounds/dims it covers, so a last-good result (served during a supersede) draws
	// with its own bounds.
	lane        *nodeLane
	demandedSQL string
	sqlMeta     map[string]rasterMeta

	// Packed result (render-thread only): re-packed when the served result's
	// content fingerprint changes (ADR-0097 SD4 early cutoff at the observer —
	// a re-fetch returning identical bytes repacks nothing; a same-SQL re-fetch
	// with NEW data does repack, which a served-SQL guard would miss).
	// lastPackedSQL still pins the packed view's bounds in sqlMeta.
	lastPackedFP  uint64
	lastPackedSQL string
	pixels        []uint32 // packed 0xRRGGBBAA, row-major, row 0 = north
	packW         uint32
	packH         uint32
	packBounds    [4]float64 // minLat, minLon, maxLat, maxLon the pixels cover
	version       uint64     // bumped per re-pack → mapRaster contentVersion

	loading bool
	// Two error owners, so neither can latch a stale message (review finding):
	// laneErr mirrors the lane's error EVERY demand (nil clears it); packErr
	// belongs to the last repack attempt (cleared on success).
	laneErr    error
	packErr    error
	controlErr string
}

// rasterMeta is the geographic bounds + pixel dims a demanded raster SQL covers.
type rasterMeta struct {
	bounds [4]float64
	w, h   uint32
}

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

// mapFetchKey dedups fetches. viewHash (quantized center/zoom/size) absorbs
// camera jitter so a still map doesn't re-fetch; table/sampling/w/h are the
// other inputs that change the result. The raw bbox floats are NOT in the key
// (they would jitter under a stable viewHash) — they ride alongside as the SQL
// inputs and the overlay bounds.
type mapFetchKey struct {
	viewHash   uint64
	table      string
	sampling   uint32
	w          uint32
	h          uint32
	colorSQL   string
	extraWhere string
}

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
		sqlMeta:   make(map[string]rasterMeta),
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

// Render draws the controls, the last-good raster overlay, and the map; and
// kicks a (debounced) fetch when the viewport has settled. Order matters: the
// mapRaster overlay is a register-drain node, so it must be emitted BEFORE the
// walkersMap that drains it.
func (inst *MapDriver) Render() {
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
			inst.updateDemand(cam.ViewHash, cam.MinLat, cam.MaxLat, cam.MinLon, cam.MaxLon,
				cam.ScreenWidthPx, cam.ScreenHeightPx)
		}
	}

	// Demand the raster on the node lane (non-blocking; supersedes the in-flight
	// run on a new view; returns the last-good while a new one is loading). Re-pack
	// only when the served result's fingerprint changes.
	if inst.demandedSQL != "" {
		view := inst.lane.demand(inst.demandedSQL)
		inst.loading = view.loading
		inst.laneErr = view.err // mirrored every demand — nil clears (no latch)
		if view.rec != nil {
			if view.fingerprint != inst.lastPackedFP {
				inst.repack(view.rec, view.sql, view.fingerprint)
			}
			view.rec.Release()
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
	mw := c.WalkersMap(inst.ids.PrepareStr("map"),
		inst.initLat, inst.initLon, inst.noTiles,
	).Width(float32(inst.mapWidth)).Height(float32(inst.mapHeight))
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
	inst.forceRefresh = true
	inst.lastRequestedKey = mapFetchKey{} // re-demand even if the view is unchanged
	inst.lane.forget()                    // re-execute even for the identical SQL
}

// updateDemand builds the bbox raster query for the current viewport and makes
// it the lane's desired SQL, deduped by mapFetchKey (so a still map doesn't
// re-demand). The lane runs it off the render thread. Bad table names surface in
// the status line; degenerate viewports are skipped.
func (inst *MapDriver) updateDemand(viewHash uint64, minLat, maxLat, minLon, maxLon float64, screenW, screenH float32) {
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
	key := mapFetchKey{viewHash: viewHash, table: table, sampling: sampling, w: w, h: h, colorSQL: colorSQL, extraWhere: r.where}
	if key == inst.lastRequestedKey {
		return
	}
	inst.lastRequestedKey = key
	sql := buildRasterSQL(b, w, h, table, sampling, colorSQL, r.where)
	inst.demandedSQL = sql
	inst.sqlMeta[sql] = rasterMeta{bounds: [4]float64{minLat, minLon, maxLat, maxLon}, w: w, h: h}
	inst.pruneSqlMeta()
}

// pruneSqlMeta bounds the sql→bounds map: only the currently-demanded view and
// the currently-packed one (a last-good candidate) must stay drawable.
func (inst *MapDriver) pruneSqlMeta() {
	const maxMeta = 4
	for k := range inst.sqlMeta {
		if len(inst.sqlMeta) <= maxMeta {
			return
		}
		if k != inst.demandedSQL && k != inst.lastPackedSQL {
			delete(inst.sqlMeta, k)
		}
	}
}

// repack packs the lane's record into the RGBA texture, pinned to the bounds the
// served SQL was computed for. Called only when the served fingerprint changes.
func (inst *MapDriver) repack(rec arrow.RecordBatch, servedSQL string, fingerprint uint64) {
	meta, ok := inst.sqlMeta[servedSQL]
	if !ok {
		return // bounds evicted — keep the prior pack rather than mis-pin
	}
	pixels, err := packRaster(rec, meta.w, meta.h)
	if err != nil {
		inst.packErr = err
		return
	}
	inst.pixels = pixels
	inst.packW = meta.w
	inst.packH = meta.h
	inst.packBounds = meta.bounds
	inst.version++
	inst.lastPackedFP = fingerprint
	inst.lastPackedSQL = servedSQL
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

// buildRasterSQL builds the bbox raster query (ADR-0096 §SD6): a fixed geometry +
// density header (span_*, in_view, px/py/pos, zoom_factor, total, max_total,
// transparency, alpha), then the selected render's colour block spliced in and an
// optional extra WHERE. Values are inlined as literals; the header assumes only
// mercator_x/mercator_y — what other columns are needed depends on colorSQL.
func buildRasterSQL(b mercBox, w, h uint32, table string, sampling uint32, colorSQL, extraWhere string) string {
	where := "in_view"
	if strings.TrimSpace(extraWhere) != "" {
		where = "in_view AND (" + extraWhere + ")"
	}
	return fmt.Sprintf(`WITH
    toUInt64(%[2]d) - %[1]d AS span_x,
    toUInt64(%[4]d) - %[3]d AS span_y,
    mercator_x >= %[1]d AND mercator_x < %[2]d
        AND mercator_y >= %[3]d AND mercator_y < %[4]d AS in_view,
    least((toUInt64(mercator_x - %[1]d) * %[5]d) DIV span_x, %[5]d - 1) AS px,
    least((toUInt64(mercator_y - %[3]d) * %[6]d) DIV span_y, %[6]d - 1) AS py,
    py * %[5]d + px AS pos,
    (span_x / %[5]d) * (span_y / %[6]d) AS pixel_area,
    pow(2, 22) / sqrt(pixel_area) AS zoom_factor,
    count() AS total,
    greatest(1000000. / %[8]d / zoom_factor, toFloat64(count())) AS max_total,
    pow(total / max_total, 1/5) AS transparency,
    255 AS alpha,
    %[9]s
SELECT round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM %[7]s
WHERE %[10]s
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64(%[5]d) * %[6]d`,
		b.minX, b.maxX, b.minY, b.maxY, w, h, table, sampling, colorSQL, where)
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
