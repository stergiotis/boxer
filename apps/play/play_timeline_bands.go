package play

import (
	"context"
	"fmt"
	"iter"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// Background-band column contract — disjoint from the main `_tl_*` slot
// inventory so a band query is unambiguous on its own. Bands are emitted
// as a separate ClickHouse SELECT that the user authors in the Timeline
// tab's collapsible "bands SQL" editor; the main query's `_tl_time`
// extent is substituted in via the placeholders below.
const (
	timelineSlotBandFrom  = "_tl_band_from"
	timelineSlotBandTo    = "_tl_band_to"
	timelineSlotBandColor = "_tl_band_color"
	timelineSlotBandLabel = "_tl_band_label"

	// timelineBandsPlaceholderMin / Max are textually replaced in the
	// user-typed bands SQL with toDateTime64() literals derived from
	// the main result's _tl_time extent. Plain `replace` rather than
	// CH parameter binding because CH parameter names cannot start
	// with an underscore.
	timelineBandsPlaceholderMin = "_time_data_min"
	timelineBandsPlaceholderMax = "_time_data_max"
)

// bandAlpha is the alpha applied to every named IDS token before it
// reaches BackgroundBand.Color. ~19% opacity is the IDS guidance for
// recessive overlays (foreground intervals + rug events must stay
// legible on top); per-band override is intentionally not exposed.
const bandAlpha uint8 = 0x30

// bandColorTokens maps IDS dot-notation token names to the source
// styletokens.RGBA8 entries. Names follow ADR-0031 §SD2 (palette.toml).
// Edit this map in lockstep with the band-color section of
// doc/howto/play-timeline-panel.md; an unknown name surfaces as a
// per-row warning in the bands status line rather than a silent
// fallback so the SQL author can fix it.
var bandColorTokens = map[string]styletokens.RGBA8{
	"neutral.bg.faint":   styletokens.NeutralBgFaint,
	"neutral.bg.surface": styletokens.NeutralBgSurface,
	"neutral.bg.panel":   styletokens.NeutralBgPanel,
	"neutral.subtle":     styletokens.NeutralSubtle,
	"neutral.default":    styletokens.NeutralDefault,
	"neutral.strong":     styletokens.NeutralStrong,
	"info.subtle":        styletokens.InfoSubtle,
	"info.default":       styletokens.InfoDefault,
	"info.strong":        styletokens.InfoStrong,
	"success.subtle":     styletokens.SuccessSubtle,
	"success.default":    styletokens.SuccessDefault,
	"success.strong":     styletokens.SuccessStrong,
	"warning.subtle":     styletokens.WarningSubtle,
	"warning.default":    styletokens.WarningDefault,
	"warning.strong":     styletokens.WarningStrong,
	"error.subtle":       styletokens.ErrorSubtle,
	"error.default":      styletokens.ErrorDefault,
	"error.strong":       styletokens.ErrorStrong,
	"accent.subtle":      styletokens.AccentSubtle,
	"accent.default":     styletokens.AccentDefault,
	"accent.strong":      styletokens.AccentStrong,
}

// bandColorByName resolves an IDS dot-notation token name to a packed
// RGBA8 value with bandAlpha applied. Returns ok=false for unknown
// names; the caller decides whether to drop the row or report.
func bandColorByName(name string) (packed uint32, ok bool) {
	t, hit := bandColorTokens[strings.ToLower(strings.TrimSpace(name))]
	if !hit {
		return
	}
	t.A = bandAlpha
	packed = t.AsHex()
	ok = true
	return
}

// substituteBandsRange replaces _time_data_min / _time_data_max in the
// bands SQL with CH-parseable toDateTime64 literals derived from the
// main result's time extent. Substitution is plain text (CH parameter
// binding rejects leading-underscore names) so the rewritten SQL stays
// inspectable end-to-end.
func substituteBandsRange(sql string, minMS, maxMS int64) (out string) {
	fmtLit := func(ms int64) string {
		t := time.UnixMilli(ms).UTC()
		return fmt.Sprintf("toDateTime64('%s', 3, 'UTC')",
			t.Format("2006-01-02 15:04:05.000"))
	}
	out = strings.ReplaceAll(sql, timelineBandsPlaceholderMin, fmtLit(minMS))
	out = strings.ReplaceAll(out, timelineBandsPlaceholderMax, fmtLit(maxMS))
	return
}

// bandsCacheKey identifies a fetched bands batch. Two SELECTs with the
// same SQL but different (min, max) substitutions produce different
// results, so all three components participate in the key.
type bandsCacheKey struct {
	MinMS int64
	MaxMS int64
	SQL   string
}

// bandsCacheSize bounds the in-memory LRU of fetched band batches. Each
// entry is small (a handful of bands times the BackgroundBand struct);
// the cap exists so a user rapidly cycling through ranges doesn't pin
// stale results forever.
const bandsCacheSize = 8

// bandsCacheEntry is one LRU slot: the key (min, max, sql) and the
// materialised band slice. The slice is shared, not copied — the band
// data is immutable once published, so the producer closure can yield
// from it directly without further allocation.
type bandsCacheEntry struct {
	Key   bandsCacheKey
	Bands []layout.BackgroundBand
}

// bandsFetchTimeout bounds a single bands round-trip. The fetch runs on a
// background goroutine (see runBandsFetch) so this is defence-in-depth — a
// wedged ClickHouse can no longer pin a goroutine indefinitely.
const bandsFetchTimeout = 10 * time.Second

// fetchBands runs the bands SQL through the existing play *Client,
// substituting the (min, max) scalars, and returns the resulting
// background bands. Called from a background goroutine (runBandsFetch), never
// the render thread. Unknown color names are dropped silently from the band
// list; the count of skipped rows surfaces in skipped so the UI can flag them.
func fetchBands(client *Client, sql string, minMS, maxMS int64) (bands []layout.BackgroundBand, skipped int, err error) {
	rewritten := substituteBandsRange(sql, minMS, maxMS)
	ctx, cancel := context.WithTimeout(context.Background(), bandsFetchTimeout)
	defer cancel()
	rdr, body, _, fErr := client.ExecuteArrowStream(ctx, rewritten, memory.NewGoAllocator())
	if fErr != nil {
		err = fErr
		return
	}
	defer func() {
		rdr.Release()
		_ = body.Close()
	}()

	for rdr.Next() {
		rec := rdr.Record()
		bs, sk, mapErr := mapBandsRecord(rec)
		if mapErr != nil {
			err = mapErr
			return
		}
		bands = append(bands, bs...)
		skipped += sk
	}
	if rdr.Err() != nil {
		err = rdr.Err()
		return
	}
	return
}

// mapBandsRecord projects a single Arrow RecordBatch into
// []BackgroundBand by walking the schema once for column indices, then
// reading typed cells per row. Type mismatches are returned as errors;
// per-row null skips are silent; unknown color names increment skipped
// without aborting the batch.
func mapBandsRecord(rec arrow.RecordBatch) (bands []layout.BackgroundBand, skipped int, err error) {
	if rec == nil || rec.NumRows() == 0 {
		return
	}
	schema := rec.Schema()
	colFrom := schema.FieldIndices(timelineSlotBandFrom)
	colTo := schema.FieldIndices(timelineSlotBandTo)
	colColor := schema.FieldIndices(timelineSlotBandColor)
	colLabel := schema.FieldIndices(timelineSlotBandLabel)
	if len(colFrom) == 0 || len(colTo) == 0 || len(colColor) == 0 {
		err = fmt.Errorf("bands SQL must return %q, %q, %q (and optionally %q); got %v",
			timelineSlotBandFrom, timelineSlotBandTo, timelineSlotBandColor,
			timelineSlotBandLabel, fieldNames(schema))
		return
	}
	fromArr, ok := rec.Column(colFrom[0]).(*array.Timestamp)
	if !ok {
		err = fmt.Errorf("%q must be a Timestamp column (got %s)",
			timelineSlotBandFrom, rec.Column(colFrom[0]).DataType())
		return
	}
	toArr, ok := rec.Column(colTo[0]).(*array.Timestamp)
	if !ok {
		err = fmt.Errorf("%q must be a Timestamp column (got %s)",
			timelineSlotBandTo, rec.Column(colTo[0]).DataType())
		return
	}
	if !isStringLikeType(rec.Column(colColor[0]).DataType()) {
		err = fmt.Errorf("%q must be a String / Binary column (got %s)",
			timelineSlotBandColor, rec.Column(colColor[0]).DataType())
		return
	}
	colorArr := rec.Column(colColor[0])
	var labelArr arrow.Array
	if len(colLabel) > 0 {
		if !isStringLikeType(rec.Column(colLabel[0]).DataType()) {
			err = fmt.Errorf("%q must be a String / Binary column (got %s)",
				timelineSlotBandLabel, rec.Column(colLabel[0]).DataType())
			return
		}
		labelArr = rec.Column(colLabel[0])
	}

	unitFrom := tsUnitOf(fromArr.DataType())
	unitTo := tsUnitOf(toArr.DataType())

	n := rec.NumRows()
	bands = make([]layout.BackgroundBand, 0, n)
	for i := range n {
		if fromArr.IsNull(int(i)) || toArr.IsNull(int(i)) || colorArr.IsNull(int(i)) {
			continue
		}
		colorName := readStringCell(colorArr, int(i))
		packed, hit := bandColorByName(colorName)
		if !hit {
			skipped++
			continue
		}
		from := tsToEpochMS(int64(fromArr.Value(int(i))), unitFrom)
		to := tsToEpochMS(int64(toArr.Value(int(i))), unitTo)
		if to < from {
			skipped++
			continue
		}
		bands = append(bands, layout.BackgroundBand{
			FromMS: from,
			ToMS:   to,
			Color:  packed,
			Label:  readStringCell(labelArr, int(i)),
		})
	}
	return
}

func tsUnitOf(dt arrow.DataType) (u arrow.TimeUnit) {
	if tt, ok := dt.(*arrow.TimestampType); ok {
		u = tt.Unit
	}
	return
}

func fieldNames(schema *arrow.Schema) (names []string) {
	names = make([]string, 0, schema.NumFields())
	for _, f := range schema.Fields() {
		names = append(names, f.Name)
	}
	return
}

// bandsProducer is the BackgroundBandProducer closure handed to the
// composite widget at construction. It filters inst.bandsView (the
// render-thread snapshot refreshed each frame by refreshBandsView) by the
// per-frame view range — bands fully outside the visible window are
// silently dropped, so the widget never paints offscreen geometry. Reading
// bandsView (not the shared bands) keeps this off the lock and clear of the
// fetch goroutine.
func (inst *TimelineDriver) bandsProducer(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
	return func(yield func(layout.BackgroundBand) bool) {
		for _, b := range inst.bandsView {
			if b.ToMS < viewMinMS || b.FromMS > viewMaxMS {
				continue
			}
			if !yield(b) {
				return
			}
		}
	}
}

// maybeFetchBands runs the panel-local bands SQL against the current
// (dataMinMS, dataMaxMS) extent and refreshes inst.bands. The cache is
// consulted first so re-running the same main query (same extent + same
// SQL) is a no-op. force bypasses the cache (used by the "Run bands"
// button to re-execute against an externally-changed source table).
func (inst *TimelineDriver) maybeFetchBands(force bool) {
	if inst.bandsSQLPtr == nil {
		return
	}
	sql := strings.TrimSpace(*inst.bandsSQLPtr)
	if sql == "" || !inst.dataExtentValid || inst.client == nil {
		inst.clearBands()
		return
	}

	key := bandsCacheKey{MinMS: inst.dataMinMS, MaxMS: inst.dataMaxMS, SQL: sql}

	inst.bandsMu.Lock()
	if !force {
		// Already showing this key, a fetch for it is in flight, or it's
		// cached — nothing new to do.
		if inst.bandsHaveFetched && key == inst.bandsLastFetchKey {
			inst.bandsMu.Unlock()
			return
		}
		if inst.bandsInFlight && key == inst.bandsInFlightKey {
			inst.bandsMu.Unlock()
			return
		}
		if cached, hit := inst.bandsCacheLookupLocked(key); hit {
			inst.bands = cached
			inst.bandsErr = nil
			inst.bandsSkipped = 0
			inst.bandsLastFetchKey = key
			inst.bandsHaveFetched = true
			inst.bandsMu.Unlock()
			return
		}
	}
	// Spawn the fetch on a background goroutine; the render loop repaints
	// every frame (PlayApp.Render), so the result is picked up next frame.
	inst.bandsInFlight = true
	inst.bandsInFlightKey = key
	inst.bandsMu.Unlock()

	go inst.runBandsFetch(key)
}

// runBandsFetch performs one bands round-trip off the render thread and
// publishes the result under bandsMu — but only if it is still the current
// in-flight key. A newer query extent or bands-SQL edit (or a clearBands)
// supersedes us: the staleness guard then discards this result, mirroring
// Projector.run's `inst.cancel == cancel` check.
func (inst *TimelineDriver) runBandsFetch(key bandsCacheKey) {
	start := time.Now()
	bands, skipped, err := fetchBands(inst.client, key.SQL, key.MinMS, key.MaxMS)
	dur := time.Since(start)

	inst.bandsMu.Lock()
	defer inst.bandsMu.Unlock()
	if !inst.bandsInFlight || inst.bandsInFlightKey != key {
		return // superseded or cleared while we were fetching
	}
	inst.bandsInFlight = false
	inst.bandsFetchDur = dur
	inst.bandsFetchedAt = time.Now()
	inst.bandsSkipped = skipped
	inst.bandsLastFetchKey = key
	inst.bandsHaveFetched = true
	if err != nil {
		inst.bands = nil
		inst.bandsErr = err
		return
	}
	inst.bands = bands
	inst.bandsErr = nil
	inst.bandsCacheStoreLocked(key, bands)
}

// clearBands resets all band state (the SQL went empty, the extent became
// invalid, or there's no client). Flipping bandsInFlight off makes any
// in-flight goroutine's publish a no-op via the staleness guard.
func (inst *TimelineDriver) clearBands() {
	inst.bandsMu.Lock()
	inst.bands = nil
	inst.bandsErr = nil
	inst.bandsSkipped = 0
	inst.bandsHaveFetched = false
	inst.bandsInFlight = false
	inst.bandsMu.Unlock()
}

// refreshBandsView copies the shared (goroutine-written) band slice into the
// render-thread-only bandsView under the lock. Called once per frame in
// TimelineDriver.Render before tl.Render(); bandsProducer then reads bandsView
// lock-free. The slice is immutable once published, so sharing the header is safe.
func (inst *TimelineDriver) refreshBandsView() {
	inst.bandsMu.Lock()
	inst.bandsView = inst.bands
	inst.bandsMu.Unlock()
}

// bandsCacheLookupLocked returns the cached band slice for key, if any.
// Caller must hold bandsMu. Lookups touch the entry (move-to-front) so the
// LRU eviction is recency-ordered. The slice is shared with the caller —
// bands are immutable once published.
func (inst *TimelineDriver) bandsCacheLookupLocked(key bandsCacheKey) (bands []layout.BackgroundBand, ok bool) {
	for i, e := range inst.bandsCache {
		if e.Key == key {
			bands = e.Bands
			ok = true
			if i > 0 {
				moved := inst.bandsCache[i]
				copy(inst.bandsCache[1:i+1], inst.bandsCache[:i])
				inst.bandsCache[0] = moved
			}
			return
		}
	}
	return
}

// bandsCacheStoreLocked inserts (key, bands) at the front of the LRU,
// evicting the oldest entry when the cap is exceeded. Caller must hold
// bandsMu. A forced refetch can leave a superseded duplicate key behind;
// the lookup path returns the front (newest) one and the stale duplicate
// ages out.
func (inst *TimelineDriver) bandsCacheStoreLocked(key bandsCacheKey, bands []layout.BackgroundBand) {
	entry := bandsCacheEntry{Key: key, Bands: bands}
	if len(inst.bandsCache) < bandsCacheSize {
		inst.bandsCache = append([]bandsCacheEntry{entry}, inst.bandsCache...)
		return
	}
	copy(inst.bandsCache[1:], inst.bandsCache[:len(inst.bandsCache)-1])
	inst.bandsCache[0] = entry
}

// renderBandsControls emits the collapsible bands-SQL editor + Run
// button + status line above the timeline canvas. State edits land
// directly in *inst.bandsSQLPtr via the TextEdit binding; the parent
// PlayApp persists the value across sessions.
func (inst *TimelineDriver) renderBandsControls() {
	if inst.bandsSQLPtr == nil {
		return
	}
	header := c.WidgetText().Text("Background bands (optional)").Keep()
	for range c.CollapsingHeader(inst.ids.PrepareStr("bands-hdr"), header).KeepIter() {
		c.TextEdit(inst.ids.PrepareStr("bands-sql"), *inst.bandsSQLPtr, true).
			CodeEditor().
			DesiredRows(3).
			DesiredWidth(float32(math.Inf(1))).
			HintText("-- bands SELECT; _time_data_min / _time_data_max substituted from result extent").
			SendRespVal(inst.bandsSQLPtr)

		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("bands-run"),
				c.Atoms().Text("Run bands").Keep()).
				SendResp().HasPrimaryClicked() {
				inst.maybeFetchBands(true)
			}
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel(inst.bandsStatusLine()) {
				rt.Small().Weak()
			}
		}
	}
}

// bandsStatusLine formats the one-line summary shown next to the Run
// button. Order of precedence: error > empty > populated. Skipped-row
// count and fetch duration only render when non-trivial.
func (inst *TimelineDriver) bandsStatusLine() (s string) {
	if inst.bandsSQLPtr == nil || strings.TrimSpace(*inst.bandsSQLPtr) == "" {
		return "no bands SQL — fill in to overlay shaded ranges"
	}
	inst.bandsMu.Lock()
	inFlight := inst.bandsInFlight
	err := inst.bandsErr
	haveFetched := inst.bandsHaveFetched
	n := len(inst.bands)
	dur := inst.bandsFetchDur
	skipped := inst.bandsSkipped
	inst.bandsMu.Unlock()

	switch {
	case inFlight:
		return "fetching bands…"
	case err != nil:
		return "bands error: " + err.Error()
	case !haveFetched:
		return "bands pending — run a query above"
	}
	parts := []string{
		fmt.Sprintf("%d bands", n),
		fmt.Sprintf("fetched in %s", dur.Round(time.Millisecond)),
	}
	if skipped > 0 {
		parts = append(parts,
			fmt.Sprintf("%d skipped (unknown color or to<from)", skipped))
	}
	return strings.Join(parts, " · ")
}
