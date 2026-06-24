package play

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

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
	table       string
	sampling    float64
	opacity     float64
	noTiles     bool
	live        bool
	mapWidth    float64
	mapHeight   float64
	initLat     float64
	initLon     float64
	initZoom    float64
	inited      bool
	forceRefresh bool

	// Debounce on the quantized viewHash: reset the timer whenever it changes,
	// fire only once the camera has been stable for mapDebounce.
	lastViewHash    uint64
	viewStableAt    time.Time
	lastRequestedKey mapFetchKey
	lastSentVersion uint64

	// Lane state — written by the fetch goroutine, read on the render thread
	// under mu. Mirrors TimelineDriver's bands lane (staleness-guarded publish).
	mu          sync.Mutex
	inFlight    bool
	inFlightKey mapFetchKey
	haveResult  bool
	resultKey   mapFetchKey
	pixels      []uint32 // packed 0xRRGGBBAA, row-major, row 0 = north
	resW        uint32
	resH        uint32
	bounds      [4]float64 // minLat, minLon, maxLat, maxLon the pixels cover
	version     uint64     // bumped per published result → mapRaster contentVersion
	lastErr     error
	lastDur     time.Duration
}

const (
	mapRasterID uint64 = 1
	mapDebounce        = 250 * time.Millisecond
	mapMaxDim   uint32 = 1024          // bounds query cost + Arrow size per view
	mercMax            = 4294967295.0  // 0xFFFFFFFF — full Web-Mercator world
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
	viewHash uint64
	table    string
	sampling uint32
	w        uint32
	h        uint32
}

type mercBox struct{ minX, maxX, minY, maxY uint32 }

func NewMapDriver(ids *c.WidgetIdStack, client *Client) *MapDriver {
	d := &MapDriver{
		ids:       ids,
		client:    client,
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
	}
	// Scripted-screenshot overrides (SPINNAKER_PLAY_MAP_*), parallel to the
	// SPINNAKER_PLAY_* knobs the play HMI already reads.
	if t := strings.TrimSpace(os.Getenv("SPINNAKER_PLAY_MAP_TABLE")); t != "" {
		d.table = t
	}
	if z := strings.TrimSpace(os.Getenv("SPINNAKER_PLAY_MAP_ZOOM")); z != "" {
		if f, err := strconv.ParseFloat(z, 64); err == nil {
			d.initZoom = f
		}
	}
	if la, lo, ok := parseLatLon(os.Getenv("SPINNAKER_PLAY_MAP_CENTER")); ok {
		d.initLat, d.initLon = la, lo
	}
	if w, h, ok := parseWxH(os.Getenv("SPINNAKER_PLAY_MAP_SIZE")); ok {
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
			inst.tryFetch(cam.ViewHash, cam.MinLat, cam.MaxLat, cam.MinLon, cam.MaxLon,
				cam.ScreenWidthPx, cam.ScreenHeightPx)
		}
	}

	// Draw the last-good raster, pinned to the bounds it was computed for, so it
	// pans/zooms correctly under the camera until the next result lands.
	inst.mu.Lock()
	have := inst.haveResult
	pixels := inst.pixels
	w, h := inst.resW, inst.resH
	bounds := inst.bounds
	version := inst.version
	inst.mu.Unlock()
	if have && w > 0 && h > 0 {
		toSend := pixels
		if version == inst.lastSentVersion {
			toSend = []uint32{} // unchanged → reuse the cached texture (empty, NOT nil)
		}
		c.MapRaster(mapRasterID,
			bounds[0], bounds[1], bounds[2], bounds[3],
			w, h, version, toSend,
		).Opacity(float32(inst.opacity)).Send()
		inst.lastSentVersion = version
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
		c.SliderF64(inst.ids.PrepareStr("map-opacity"), inst.opacity, 0.1, 1.0).
			Text("opacity").SendRespVal(&inst.opacity)
		c.Checkbox(inst.ids.PrepareStr("map-live"), inst.live, "live").SendRespVal(&inst.live)
		c.Checkbox(inst.ids.PrepareStr("map-notiles"), inst.noTiles, "no basemap").
			SendRespVal(&inst.noTiles)
		if c.Button(inst.ids.PrepareStr("map-refresh"),
			c.Atoms().Text("Refresh").Keep()).SendResp().HasPrimaryClicked() {
			inst.forceRefresh = true
			inst.lastRequestedKey = mapFetchKey{} // re-fetch even if the view is unchanged
		}
	}
	c.Label(inst.statusLine()).Send()
}

// tryFetch builds the bbox query for the current viewport and hands it to the
// lane, deduped by mapFetchKey. Degenerate viewports and bad table names are
// skipped (the latter surfaces in the status line).
func (inst *MapDriver) tryFetch(viewHash uint64, minLat, maxLat, minLon, maxLon float64, screenW, screenH float32) {
	table := sanitizeTable(inst.table)
	if table == "" {
		inst.setErr(fmt.Errorf("invalid or empty table name (allowed: letters, digits, '_', '.')"))
		return
	}
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
	key := mapFetchKey{viewHash: viewHash, table: table, sampling: sampling, w: w, h: h}
	if key == inst.lastRequestedKey {
		return
	}
	inst.lastRequestedKey = key
	sql := buildRasterSQL(b, w, h, table, sampling)
	bounds := [4]float64{minLat, minLon, maxLat, maxLon}
	inst.maybeFetch(key, sql, bounds, w, h)
}

// maybeFetch spawns the fetch goroutine unless this exact key is already in
// flight or already the published result.
func (inst *MapDriver) maybeFetch(key mapFetchKey, sql string, bounds [4]float64, w, h uint32) {
	inst.mu.Lock()
	if inst.inFlight && inst.inFlightKey == key {
		inst.mu.Unlock()
		return
	}
	if inst.haveResult && inst.resultKey == key {
		inst.mu.Unlock()
		return
	}
	inst.inFlight = true
	inst.inFlightKey = key
	inst.mu.Unlock()
	go inst.runFetch(key, sql, bounds, w, h)
}

// runFetch performs one raster round-trip off the render thread and publishes
// it under mu — but only if it is still the current in-flight key (a newer
// viewport supersedes us; the staleness guard discards the stale result,
// mirroring runBandsFetch).
func (inst *MapDriver) runFetch(key mapFetchKey, sql string, bounds [4]float64, w, h uint32) {
	start := time.Now()
	pixels, err := inst.fetchRaster(sql, w, h)
	dur := time.Since(start)

	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !inst.inFlight || inst.inFlightKey != key {
		return // superseded while fetching
	}
	inst.inFlight = false
	inst.lastDur = dur
	if err != nil {
		inst.lastErr = err
		return
	}
	inst.lastErr = nil
	inst.pixels = pixels
	inst.resW = w
	inst.resH = h
	inst.bounds = bounds
	inst.version++
	inst.haveResult = true
	inst.resultKey = key
}

// fetchRaster runs the SQL and packs the dense 4×UInt8 (r,g,b,a) result into a
// row-major []uint32 of 0xRRGGBBAA. WITH FILL yields exactly w*h rows; the
// length is padded/truncated defensively so the texture upload always matches.
func (inst *MapDriver) fetchRaster(sql string, w, h uint32) (pixels []uint32, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), mapFetchTimeout)
	defer cancel()
	rdr, body, _, fErr := inst.client.ExecuteArrowStream(ctx, sql, memory.NewGoAllocator())
	if fErr != nil {
		err = fErr
		return
	}
	defer func() {
		rdr.Release()
		_ = body.Close()
	}()

	n := int(w) * int(h)
	pixels = make([]uint32, 0, n)
	for rdr.Next() {
		rec := rdr.Record()
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
		rows := int(rec.NumRows())
		for i := 0; i < rows; i++ {
			pixels = append(pixels, (uint32(ra.Value(i))<<24)|
				(uint32(ga.Value(i))<<16)|(uint32(ba.Value(i))<<8)|uint32(aa.Value(i)))
		}
	}
	if rdr.Err() != nil {
		err = rdr.Err()
		return
	}
	if len(pixels) < n {
		pixels = append(pixels, make([]uint32, n-len(pixels))...)
	} else if len(pixels) > n {
		pixels = pixels[:n]
	}
	return
}

func (inst *MapDriver) setErr(err error) {
	inst.mu.Lock()
	inst.lastErr = err
	inst.mu.Unlock()
}

func (inst *MapDriver) statusLine() string {
	inst.mu.Lock()
	inFlight := inst.inFlight
	have := inst.haveResult
	err := inst.lastErr
	w, h := inst.resW, inst.resH
	dur := inst.lastDur
	inst.mu.Unlock()
	switch {
	case err != nil:
		return "query error: " + err.Error()
	case inFlight:
		return "rendering tile…"
	case have:
		return fmt.Sprintf("%d×%d raster · %s", w, h, dur.Round(time.Millisecond))
	default:
		return "pan/zoom over a ClickHouse table with mercator_x/mercator_y (e.g. planes_mercator)"
	}
}

// buildRasterSQL generalises the adsb.exposed "Altitude & Velocity" tile render
// from a fixed 1024² tile to an arbitrary viewport (ADR-0096 §SD6). Values are
// inlined as literals; mercator_x/mercator_y/altitude/ground_speed are assumed.
func buildRasterSQL(b mercBox, w, h uint32, table string, sampling uint32) string {
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
    greatest(0, least(avg(altitude), 5000)) / 5000 AS color1,
    greatest(0, least(avg(altitude), 50000)) / 50000 AS color3,
    greatest(0, least(avg(ground_speed), 700)) / 700 AS color2,
    255 AS alpha,
    (1 + transparency) / 2 * (1 - color3) * 255 AS red,
    transparency * color1 * 255 AS green,
    color2 * 255 AS blue
SELECT round(red)::UInt8, round(green)::UInt8, round(blue)::UInt8, round(alpha)::UInt8
FROM %[7]s
WHERE in_view
GROUP BY pos
ORDER BY pos WITH FILL FROM 0 TO toUInt64(%[5]d) * %[6]d`,
		b.minX, b.maxX, b.minY, b.maxY, w, h, table, sampling)
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
// `remoteSecure('…:9440', default.planes_mercator_sample100, 'website', '')`.
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
