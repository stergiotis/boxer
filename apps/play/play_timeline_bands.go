package play

import (
	"fmt"
	"iter"
	"math"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
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

	// timelineBandsPlaceholderMin / Max are the RETIRED textual placeholders
	// (pre-slice-5d): they were string-replaced with toDateTime64() literals.
	// The extent now arrives as the tl_min / tl_max signals, referenced as
	// `{tl_min:DateTime64(3, 'UTC')}` / `{tl_max:DateTime64(3, 'UTC')}` param
	// slots. The constants remain only so demandBands can detect a legacy
	// bands SQL and show a targeted migration hint instead of the server's
	// unknown-identifier error.
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

// formatExtentParam renders an epoch-ms extent bound as the raw value of a
// `{tl_min:DateTime64(3, 'UTC')}` / `{tl_max:…}` param: the UTC-formatted
// string ClickHouse parses under the slot's explicit-UTC type (server-checked
// — never the server's timezone, per the repo's toHour lesson).
func formatExtentParam(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05.000")
}

// bandsFetchTimeout bounds a single bands round-trip on the bands node lane.
const bandsFetchTimeout = 10 * time.Second

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
// silently dropped, so the widget never paints offscreen geometry. inst.bands
// is render-thread-only now (4b: the lane is the sole async part), so this
// reads it directly.
func (inst *TimelineDriver) bandsProducer(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand] {
	return func(yield func(layout.BackgroundBand) bool) {
		for _, b := range inst.bands {
			if b.ToMS < viewMinMS || b.FromMS > viewMaxMS {
				continue
			}
			if !yield(b) {
				return
			}
		}
	}
}

// ensureBandsSlots re-derives the bands SQL's slot names and SET-bound set
// when the editor text changes (one parse per edit, not per frame), and
// detects the retired _time_data_* placeholders for the migration hint. An
// unparseable bands SQL yields no slots — the demand ships it verbatim and
// the server reports, exactly as for the main buffer.
func (inst *TimelineDriver) ensureBandsSlots(sql string) {
	if sql == inst.bandsSlotsFor {
		return
	}
	inst.bandsSlotsFor = sql
	inst.bandsLegacy = strings.Contains(sql, timelineBandsPlaceholderMin) ||
		strings.Contains(sql, timelineBandsPlaceholderMax)
	inst.bandsSlotNames = inst.bandsSlotNames[:0]
	inst.bandsBound = nil
	slots, vals, err := extractSlotsAndParams(sql)
	if err != nil {
		return
	}
	for _, s := range slots {
		inst.bandsSlotNames = append(inst.bandsSlotNames, s.Name)
	}
	inst.bandsBound = make(map[string]bool, len(vals))
	for urlKey := range vals {
		inst.bandsBound[strings.TrimPrefix(urlKey, "param_")] = true
	}
}

// bandsExtentPending reports whether the bands SQL references the tl_min /
// tl_max extent signals (unbound) that the store cannot yet resolve — i.e.
// the Timeline has not rendered events. Such a node waits ("pending"); a
// bands SQL that does not reference the extent runs immediately (new since
// 5d — absolute-time bands no longer wait for events). Other unresolved
// slots do NOT gate: the server's "param not set" error is the informative
// path, as for the main buffer.
func (inst *TimelineDriver) bandsExtentPending(params map[string]string) bool {
	for _, name := range inst.bandsSlotNames {
		if name != string(signalTimelineMin) && name != string(signalTimelineMax) {
			continue
		}
		if inst.bandsBound[name] {
			continue
		}
		if _, resolved := params["param_"+name]; !resolved {
			return true
		}
	}
	return false
}

// demandBands compiles the bands node against the frame's signal snapshot and
// demands it on the bands lane, returning the retained _tl_band_* result for
// the Timeline's chBands channel (4b-2; the caller MUST Release rec). The
// extent arrives as the tl_min/tl_max signals the events render published
// (slice 5d — the textual substitution retired), so an unchanged
// (SQL, signal values) pair is a lane memo hit and a moved extent supersedes
// in flight. Empty SQL / no client yields (nil, nil); a legacy-placeholder
// SQL is not demanded (the status line carries the migration hint). The
// result is mapped into inst.bands by setBands, called from the panel's
// Render.
func (inst *TimelineDriver) demandBands(sig SignalEnvI) (rec arrow.RecordBatch, schema *arrow.Schema) {
	if inst.bandsSQLPtr == nil {
		return
	}
	sql := strings.TrimSpace(*inst.bandsSQLPtr)
	if sql == "" || inst.client == nil {
		inst.bandsLoading = false
		inst.bandsLaneErr = nil
		inst.bandsMapErr = nil
		inst.bandsServedKey = ""
		inst.bandsServedFP = 0
		inst.bandsLegacy = false
		inst.bandsSlotsFor = ""
		return
	}
	inst.ensureBandsSlots(sql)
	if inst.bandsLegacy {
		inst.bandsLoading = false
		inst.bandsLaneErr = nil
		return
	}
	params := resolveSignalNames(inst.bandsSlotNames, inst.bandsBound, sig)
	if inst.bandsExtentPending(params) {
		inst.bandsLoading = false
		inst.bandsLaneErr = nil
		return
	}
	view := inst.bandsLane.demand(compiledNode{SQL: sql, Params: params})
	inst.bandsLoading = view.loading
	inst.bandsLaneErr = view.err // mirrored every demand — nil clears (no latch)
	inst.bandsServedKey = view.key
	inst.bandsServedFP = view.fingerprint
	return view.rec, view.schema
}

// setBands maps the chBands result into inst.bands, re-mapping only when the
// served (fingerprint, compiled key) pair changes (the Map's repack idiom —
// ADR-0097 SD4 early cutoff at the observer: a forced re-fetch of the same
// inputs with identical bytes re-maps nothing, but new data under the same
// inputs does; the key half keeps a first empty result — fingerprint 0, the
// zero value — distinguishable from "nothing mapped yet"). A successful empty
// fetch (nil rec with a schema) maps to ZERO bands — distinct from "pending"
// (review finding). Called from the Timeline panel's Render before the events
// render — the band producer reads inst.bands.
func (inst *TimelineDriver) setBands(rec arrow.RecordBatch) {
	if inst.bandsServedFP == inst.bandsMappedFP && inst.bandsServedKey == inst.bandsMappedKey {
		return
	}
	if rec == nil {
		inst.bands = nil
		inst.bandsSkipped = 0
		inst.bandsMapErr = nil
		inst.bandsMappedFP = inst.bandsServedFP
		inst.bandsMappedKey = inst.bandsServedKey
		return
	}
	bands, skipped, mapErr := mapBandsRecord(rec)
	if mapErr != nil {
		inst.bandsMapErr = mapErr
		return
	}
	inst.bands = bands
	inst.bandsSkipped = skipped
	inst.bandsMapErr = nil
	inst.bandsMappedFP = inst.bandsServedFP
	inst.bandsMappedKey = inst.bandsServedKey
}

// clearBands resets the band data when the chBands channel is unfilled (empty
// bands SQL or no result). It leaves bandsLaneErr alone — demandBands owns the
// lane error (so a failing bands query still surfaces its error in the status);
// the map error belongs to the retired mapping and clears with it.
func (inst *TimelineDriver) clearBands() {
	inst.bands = nil
	inst.bandsSkipped = 0
	inst.bandsMapErr = nil
	inst.bandsMappedFP = 0
	inst.bandsMappedKey = ""
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
			HintText("-- bands SELECT; {tl_min:DateTime64(3, 'UTC')} / {tl_max:…} carry the events extent").
			SendRespVal(inst.bandsSQLPtr)

		for range c.Horizontal().KeepIter() {
			if c.Button(inst.ids.PrepareStr("bands-run"),
				c.Atoms().Text("Run bands").Keep()).
				SendResp().HasPrimaryClicked() {
				// Force a re-fetch against a possibly-changed source. The
				// fingerprint guard in setBands re-maps iff the re-fetched
				// bytes actually differ — no mapped-state reset needed.
				inst.bandsLane.forget()
			}
			c.Separator().Vertical().Send()
			for rt := range c.RichTextLabel(inst.bandsStatusLine()) {
				rt.Small().Weak()
			}
		}
	}
}

// bandsStatusLine formats the one-line summary shown next to the Run button.
// Order of precedence: legacy hint > loading > error > pending > populated.
// The skipped-row count renders only when non-trivial. Render-thread-only
// state (4b).
func (inst *TimelineDriver) bandsStatusLine() (s string) {
	if inst.bandsSQLPtr == nil || strings.TrimSpace(*inst.bandsSQLPtr) == "" {
		return "no bands SQL — fill in to overlay shaded ranges"
	}
	switch {
	case inst.bandsLegacy:
		return "bands SQL uses the retired " + timelineBandsPlaceholderMin + " / " +
			timelineBandsPlaceholderMax + " placeholders — reference " +
			"{tl_min:DateTime64(3, 'UTC')} / {tl_max:DateTime64(3, 'UTC')} instead"
	case inst.bandsLoading && len(inst.bands) == 0:
		return "fetching bands…"
	case inst.bandsLaneErr != nil:
		return "bands error: " + inst.bandsLaneErr.Error()
	case inst.bandsMapErr != nil:
		return "bands error: " + inst.bandsMapErr.Error()
	case inst.bandsMappedKey == "":
		return "bands pending — run a query above"
	}
	parts := []string{fmt.Sprintf("%d bands", len(inst.bands))}
	if inst.bandsSkipped > 0 {
		parts = append(parts,
			fmt.Sprintf("%d skipped (unknown color or to<from)", inst.bandsSkipped))
	}
	return strings.Join(parts, " · ")
}
