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

// demandBands demands the bands node lane with the bands SQL substituted for the
// current (dataMinMS, dataMaxMS) extent, returning the retained _tl_band_* result
// for the Timeline's chBands channel (4b-2; the caller MUST Release rec). Empty
// SQL / invalid extent / no client yields (nil, nil). The lane supplies async +
// supersession + last-good; an unchanged (extent, SQL) is a memo hit. The result
// is mapped into inst.bands by setBands, called from the panel's Render.
func (inst *TimelineDriver) demandBands() (rec arrow.RecordBatch, schema *arrow.Schema) {
	if inst.bandsSQLPtr == nil {
		return
	}
	sql := strings.TrimSpace(*inst.bandsSQLPtr)
	if sql == "" || !inst.dataExtentValid || inst.client == nil {
		inst.bandsLoading = false
		inst.bandsLaneErr = nil
		inst.bandsMapErr = nil
		inst.bandsServedSQL = ""
		inst.bandsServedFP = 0
		return
	}
	compiled := substituteBandsRange(sql, inst.dataMinMS, inst.dataMaxMS)
	view := inst.bandsLane.demand(compiled)
	inst.bandsLoading = view.loading
	inst.bandsLaneErr = view.err // mirrored every demand — nil clears (no latch)
	inst.bandsServedSQL = view.sql
	inst.bandsServedFP = view.fingerprint
	return view.rec, view.schema
}

// setBands maps the chBands result into inst.bands, re-mapping only when the
// served (fingerprint, SQL) pair changes (the Map's repack idiom — ADR-0097
// SD4 early cutoff at the observer: a forced re-fetch of the same SQL with
// identical bytes re-maps nothing, but new data under the same SQL does; the
// SQL half keeps a first empty result — fingerprint 0, the zero value —
// distinguishable from "nothing mapped yet"). A successful empty fetch (nil
// rec with a schema) maps to ZERO bands — distinct from "pending" (review
// finding). Called from the Timeline panel's Render before the events render —
// the band producer reads inst.bands.
func (inst *TimelineDriver) setBands(rec arrow.RecordBatch) {
	if inst.bandsServedFP == inst.bandsMappedFP && inst.bandsServedSQL == inst.bandsMappedSQL {
		return
	}
	if rec == nil {
		inst.bands = nil
		inst.bandsSkipped = 0
		inst.bandsMapErr = nil
		inst.bandsMappedFP = inst.bandsServedFP
		inst.bandsMappedSQL = inst.bandsServedSQL
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
	inst.bandsMappedSQL = inst.bandsServedSQL
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
	inst.bandsMappedSQL = ""
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
// Order of precedence: loading > error > pending > populated. The skipped-row
// count renders only when non-trivial. Render-thread-only state (4b).
func (inst *TimelineDriver) bandsStatusLine() (s string) {
	if inst.bandsSQLPtr == nil || strings.TrimSpace(*inst.bandsSQLPtr) == "" {
		return "no bands SQL — fill in to overlay shaded ranges"
	}
	switch {
	case inst.bandsLoading && len(inst.bands) == 0:
		return "fetching bands…"
	case inst.bandsLaneErr != nil:
		return "bands error: " + inst.bandsLaneErr.Error()
	case inst.bandsMapErr != nil:
		return "bands error: " + inst.bandsMapErr.Error()
	case inst.bandsMappedSQL == "":
		return "bands pending — run a query above"
	}
	parts := []string{fmt.Sprintf("%d bands", len(inst.bands))}
	if inst.bandsSkipped > 0 {
		parts = append(parts,
			fmt.Sprintf("%d skipped (unknown color or to<from)", inst.bandsSkipped))
	}
	return strings.Join(parts, " · ")
}
