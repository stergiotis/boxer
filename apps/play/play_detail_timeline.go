package play

import (
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/semistructured/leeway/common"
	"github.com/stergiotis/boxer/public/semistructured/leeway/streamreadaccess"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// play_detail_timeline.go extends the Detail pane's default rendering with a
// per-row temporal overview: when the selected row carries one or more
// datetime attributes, a compact annotations timeline plots each attribute as
// a numbered flag on a shared UTC axis, with a legend below naming each flag
// and its formatted value. It reuses the timeline widget unmodified (the
// LifeLines annotation idiom); the strip renders above the leeway card / ad-hoc
// sections (see RenderDefaultDetailContent).
//
// Detection is type-first — Arrow Timestamp / Date32 / Date64 columns are
// temporal by their type regardless of schema. It is then widened for the
// leeway path the Detail card serves: a leeway `tv:time:` attribute or the
// backbone entity-timestamp whose width-32 form ClickHouse emits as
// DateTime('UTC') arrives on the Arrow wire as a plain uint32 of epoch
// *seconds*, which a type-only test would miss. Array-valued temporal
// attributes (leeway homogeneous-array time) are out of scope for this cut —
// each item would need its own flag; only scalar cells are plotted.

const (
	msPerDay = 86_400_000
	// singleInstantPadMS is the half-width of the view window drawn around a
	// lone timestamp (or several coincident ones): SetRange needs t1 > t0, and
	// a ±12h window gives a single flag a legible axis instead of a zero-width
	// range.
	singleInstantPadMS = 12 * 60 * 60 * 1000
)

// temporalAttr is one detected datetime attribute of a Detail row: its
// descriptive short label, the moment in epoch milliseconds (UTC), and the
// value formatted for the legend and the flag's hover tooltip.
type temporalAttr struct {
	label   string
	epochMS int64
	valueS  string
}

// detectTemporalAttrs walks the row once and returns its datetime attributes in
// physical column order. classes is the leeway per-column classification (nil
// on the ad-hoc path); it widens detection to the integer-encoded leeway
// temporal columns via the backbone entity-timestamp marker. Null cells and
// non-temporal columns are skipped.
func detectTemporalAttrs(rec arrow.RecordBatch, schema *arrow.Schema, row int64, classes []streamreadaccess.ColumnClass) (out []temporalAttr) {
	if rec == nil || schema == nil || row < 0 || row >= rec.NumRows() {
		return nil
	}
	// Index the classification by Arrow column so the backbone-timestamp test
	// is O(1) per column. Only the entity-timestamp marker matters here; tagged
	// temporal attributes are caught by the tv:time: name prefix instead (the
	// classification does not carry a column's canonical type).
	var backboneTS map[int]bool
	if len(classes) > 0 {
		backboneTS = make(map[int]bool, len(classes))
		for _, cl := range classes {
			if cl.PlainItemType == common.PlainItemTypeEntityTimestamp {
				backboneTS[cl.ArrowIdx] = true
			}
		}
	}
	for i := 0; i < schema.NumFields(); i++ {
		arr := rec.Column(i)
		if arr.IsNull(int(row)) {
			continue
		}
		name := schema.Field(i).Name
		leewayTemporal := backboneTS[i] || hasLeewayTimePrefix(name)
		ms, ok := temporalCellMS(arr, int(row), leewayTemporal)
		if !ok {
			continue
		}
		out = append(out, temporalAttr{
			label:   shortColumnLabel(name),
			epochMS: ms,
			valueS:  formatEpochMS(ms),
		})
	}
	return out
}

// temporalCellMS resolves the (non-null) cell at row to epoch milliseconds
// (UTC). Arrow temporal types are recognised by type unconditionally. When
// leewayTemporal is set — the column is a leeway tv:time: attribute or the
// backbone entity-timestamp — an integer cell is read as epoch *seconds*, the
// shape ClickHouse DateTime('UTC') (width-32 leeway temporal) takes on the
// Arrow wire. A non-temporal column returns ok=false.
func temporalCellMS(arr arrow.Array, row int, leewayTemporal bool) (ms int64, ok bool) {
	switch a := arr.(type) {
	case *array.Timestamp:
		unit := arrow.Second
		if tt, isTs := a.DataType().(*arrow.TimestampType); isTs {
			unit = tt.Unit
		}
		return tsToEpochMS(int64(a.Value(row)), unit), true
	case *array.Date32:
		return int64(a.Value(row)) * msPerDay, true
	case *array.Date64:
		return int64(a.Value(row)), true
	}
	if leewayTemporal {
		if sec, isInt := readEpochSeconds(arr, row); isInt {
			return sec * 1000, true
		}
	}
	return 0, false
}

// readEpochSeconds reads an integer cell as a signed second count. Only the
// widths a leeway DateTime('UTC') column can arrive as are handled (uint32 is
// the canonical one); a non-integer array — e.g. a List of times — returns
// ok=false and is skipped by this cut.
func readEpochSeconds(arr arrow.Array, row int) (sec int64, ok bool) {
	switch a := arr.(type) {
	case *array.Uint32:
		return int64(a.Value(row)), true
	case *array.Int32:
		return int64(a.Value(row)), true
	case *array.Uint64:
		return int64(a.Value(row)), true
	case *array.Int64:
		return a.Value(row), true
	}
	return 0, false
}

// hasLeewayTimePrefix reports whether a physical column name is a leeway
// tv:time: temporal attribute under either naming convention — the canonical
// ':' separator or the '_' ClickHouse column dumps mangle it to. CardDriver
// settles the separator per schema; detection accepts both so it works either
// way.
func hasLeewayTimePrefix(name string) bool {
	return strings.HasPrefix(name, "tv:time:") || strings.HasPrefix(name, "tv_time_")
}

// formatEpochMS renders an epoch-ms moment as a compact UTC wall-clock string
// for the legend and tooltip. Seconds resolution is deliberate: sub-second
// precision would crowd the legend, and the axis carries the coarse placement.
func formatEpochMS(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05")
}

// buildDetailAnnotations turns detected attributes into timeline annotations:
// one numbered flag per attribute (1-based, matching the legend), a qualitative
// palette hue cycled per attribute, and a "label: value" tooltip.
func buildDetailAnnotations(attrs []temporalAttr) (anns []*layout.Annotation) {
	if len(attrs) == 0 {
		return nil
	}
	anns = make([]*layout.Annotation, len(attrs))
	for i, ta := range attrs {
		anns[i] = &layout.Annotation{
			TMS:        ta.epochMS,
			Number:     int32(i + 1),
			PaletteIdx: int32(i % 10),
			Label:      ta.label + ": " + ta.valueS,
		}
	}
	return anns
}

// fitRange computes the pinned view window covering every attribute with a
// small margin. A degenerate extent (one attribute, or several at the same
// instant) widens to a fixed ±12h window so SetRange's t1 > t0 requirement
// holds and a lone flag still reads against a real axis. ok=false only when
// there are no attributes.
func fitRange(attrs []temporalAttr) (lo, hi time.Time, ok bool) {
	if len(attrs) == 0 {
		return time.Time{}, time.Time{}, false
	}
	minMS, maxMS := attrs[0].epochMS, attrs[0].epochMS
	for _, a := range attrs[1:] {
		if a.epochMS < minMS {
			minMS = a.epochMS
		}
		if a.epochMS > maxMS {
			maxMS = a.epochMS
		}
	}
	var pad int64
	if span := maxMS - minMS; span <= 0 {
		pad = singleInstantPadMS // degenerate extent: give a lone flag a real axis
	} else if pad = span / 10; pad < 1 {
		pad = 1 // 10% margin each side, floored so t1 > t0 always holds
	}
	lo = time.UnixMilli(minMS - pad).UTC()
	hi = time.UnixMilli(maxMS + pad).UTC()
	return lo, hi, true
}

// DetailTimeline renders the Detail pane's per-row temporal overview strip. It
// owns a single non-interactive timeline widget reused across rows; the
// annotation set + pinned range are rebuilt only when the (result, row) changes
// — the same early-cutoff the Timeline tab's driver uses.
type DetailTimeline struct {
	ids *c.WidgetIdStack
	tl  *timeline.Timeline

	seenRec arrow.RecordBatch
	seenRow int64
	attrs   []temporalAttr
}

// NewDetailTimeline builds the driver and its widget. The widget is
// non-interactive: this is a read-only overview, the annotation stagger already
// separates colliding flags without a zoom, and a pan/zoom-capturing widget at
// the top of the Detail pane would fight the pane's own scroll. The range is
// pinned per row via SetRange, so the widget's interval-only auto-fit (which
// ignores annotations) never applies.
func NewDetailTimeline(ids *c.WidgetIdStack) (inst *DetailTimeline) {
	inst = &DetailTimeline{ids: ids, seenRow: -1}
	inst.tl = timeline.New(ids, "play-detail-timeline", nil,
		timeline.WithInteractive(false))
	return inst
}

// sync re-detects the row's temporal attributes and pushes the annotation set +
// pinned range into the widget when the (result, row) identity changes. Pure
// widget-state mutation (no ui scope), so it is exercised directly in tests.
func (inst *DetailTimeline) sync(rec arrow.RecordBatch, schema *arrow.Schema, row int64, classes []streamreadaccess.ColumnClass) {
	if rec == inst.seenRec && row == inst.seenRow {
		return
	}
	inst.seenRec = rec
	inst.seenRow = row
	inst.attrs = detectTemporalAttrs(rec, schema, row, classes)
	inst.tl.SetAnnotations(buildDetailAnnotations(inst.attrs))
	if lo, hi, ok := fitRange(inst.attrs); ok {
		inst.tl.SetRange(lo, hi)
	} else {
		// No attributes: drop the pin so a stale range can't linger if the
		// widget is ever rendered before render()'s empty-return guard.
		inst.tl.SetRange(time.Time{}, time.Time{})
	}
}

// render draws the temporal-overview strip for the row and reports whether it
// drew anything (true iff the row has ≥1 temporal attribute). A false return
// lets the caller skip the separator it would otherwise place below the strip.
// Must run inside the Detail body's vertical scope.
func (inst *DetailTimeline) render(rec arrow.RecordBatch, schema *arrow.Schema, row int64, classes []streamreadaccess.ColumnClass) bool {
	inst.sync(rec, schema, row, classes)
	if len(inst.attrs) == 0 {
		return false
	}
	for range c.Vertical().KeepIter() {
		for rt := range c.RichTextLabel("TIMELINE") {
			rt.Small().Weak()
		}
		inst.tl.Render()
		inst.renderLegend()
	}
	return true
}

// renderLegend draws one "● N label value" row per attribute, the swatch hue
// matching the flag's palette index (styletokens.QualitativeCycle) so a reader
// maps a numbered flag on the axis back to its attribute and value.
func (inst *DetailTimeline) renderLegend() {
	for range c.Vertical().KeepIter() {
		for i, ta := range inst.attrs {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabelColored(
					color.Hex(styletokens.QualitativeCycle(i%10).AsHex()),
					color.Transparent, "●") {
					rt.Small()
				}
				for rt := range c.RichTextLabel(strconv.Itoa(i + 1)) {
					rt.Small().Weak()
				}
				for rt := range c.RichTextLabel(ta.label) {
					rt.Small()
				}
				for rt := range c.RichTextLabel(ta.valueS) {
					rt.Small().Weak()
				}
			}
		}
	}
}
