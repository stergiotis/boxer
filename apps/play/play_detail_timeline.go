package play

import (
	"fmt"
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
// per-row temporal overview: when the selected row has one or more datetime
// attributes, a compact non-interactive timeline renders above the card. Each
// attribute becomes one legend entry; on the shared UTC axis:
//
//   - a scalar timestamp, or each item of an instant array, is a numbered flag
//     (all items of one attribute share the attribute's number and colour);
//   - a leeway co-section begin/end pair (e.g. tv:timeRange:beginIncl +
//     tv:timeRange:endExcl) is a set of interval bars on a labelled lane.
//
// It reuses the timeline widget unmodified (annotations + intervals in one
// view). See RenderDefaultDetailContent for the placement.
//
// Detection is Arrow-type-first: a Timestamp / Date32 / Date64 column — or a
// List of those — is temporal by type, which also skips the leeway
// support/membership columns (their inner type is integer/string). It is
// widened for the leeway path to integer-encoded scalar temporal (tv:time: and
// the backbone entity-timestamp, read as epoch seconds — the width-32
// DateTime('UTC') → Arrow uint32 case). Range pairing uses the leeway
// classification: two temporal value columns sharing one SectionName are a
// co-section begin/end pair. Integer-encoded temporal *arrays* are out of scope
// for this cut (they'd need Class==Value gating to tell the value column from a
// same-typed support column).

const (
	msPerDay = 86_400_000
	// singleInstantPadMS is the half-width of the view window drawn around a
	// degenerate extent (one moment, or several coincident ones): SetRange needs
	// t1 > t0, and a ±12h window gives a lone flag a legible axis.
	singleInstantPadMS = 12 * 60 * 60 * 1000
	// maxDetailMarks caps the flags + bars actually drawn so a pathological
	// array can't flood the strip; the surplus is reported as an overflow note.
	maxDetailMarks = 64
)

type temporalKind uint8

const (
	kindInstants temporalKind = iota // points → numbered flags
	kindIntervals                    // spans → lane bars
)

type temporalSpan struct {
	fromMS int64
	toMS   int64
}

// temporalAttr is one temporal attribute of the row: a named group of instants
// (a scalar or an instant array) or intervals (a co-section begin/end pair).
// paletteIdx drives the attribute's legend number, flag/bar colour, and — for
// instants — the number painted on every one of its flags.
type temporalAttr struct {
	label      string
	paletteIdx int
	kind       temporalKind
	points     []int64        // kindInstants
	spans      []temporalSpan // kindIntervals
}

// detectTemporalAttrs walks the row once and returns its temporal attributes in
// physical column order, plus the count of marks dropped by the density cap.
// classes is the leeway per-column classification (nil off the leeway path); it
// supplies the SectionName that pairs co-section begin/end columns into ranges,
// and the backbone entity-timestamp marker.
func detectTemporalAttrs(rec arrow.RecordBatch, schema *arrow.Schema, row int64, classes []streamreadaccess.ColumnClass) (attrs []temporalAttr, dropped int) {
	if rec == nil || schema == nil || row < 0 || row >= rec.NumRows() {
		return nil, 0
	}
	classByIdx := make(map[int]streamreadaccess.ColumnClass, len(classes))
	for _, cl := range classes {
		classByIdx[cl.ArrowIdx] = cl
	}

	// Pass 1: collect every temporal column with its moment(s) and, when it is a
	// leeway value column, its section (the range-pairing key).
	type tcol struct {
		name    string
		section string
		moments []int64
	}
	var tcols []tcol
	var buf []int64
	for i := 0; i < schema.NumFields(); i++ {
		arr := rec.Column(i)
		if arr.IsNull(int(row)) {
			continue
		}
		name := schema.Field(i).Name
		cl, hasCl := classByIdx[i]
		leewayTemporal := (hasCl && cl.PlainItemType == common.PlainItemTypeEntityTimestamp) || hasLeewayTimePrefix(name)
		buf = cellMoments(arr, int(row), leewayTemporal, buf[:0])
		if len(buf) == 0 {
			continue
		}
		section := ""
		if hasCl && cl.Class == streamreadaccess.ColumnRoleClassValue {
			section = cl.SectionName.String()
		}
		tcols = append(tcols, tcol{name: name, section: section, moments: append([]int64(nil), buf...)})
	}

	// Pass 2: a section holding exactly two temporal value columns is a
	// co-section begin/end pair → one interval attribute; everything else is an
	// instants attribute. Emitted in first-encounter order.
	bySection := make(map[string][]int)
	for i, tc := range tcols {
		if tc.section != "" {
			bySection[tc.section] = append(bySection[tc.section], i)
		}
	}
	consumed := make([]bool, len(tcols))
	for i, tc := range tcols {
		if consumed[i] {
			continue
		}
		if members := bySection[tc.section]; tc.section != "" && len(members) == 2 {
			a, b := tcols[members[0]], tcols[members[1]]
			consumed[members[0]] = true
			consumed[members[1]] = true
			attrs = append(attrs, temporalAttr{
				label: sectionLabel(tc.section),
				kind:  kindIntervals,
				spans: zipSpans(a.moments, b.moments),
			})
			continue
		}
		consumed[i] = true
		attrs = append(attrs, temporalAttr{
			label:  shortColumnLabel(tc.name),
			kind:   kindInstants,
			points: tc.moments,
		})
	}

	attrs, dropped = capMarks(attrs)
	for i := range attrs {
		attrs[i].paletteIdx = i
	}
	return attrs, dropped
}

// cellMoments appends every epoch-ms moment in the (non-null) cell at row to
// dst: a scalar temporal → one moment; a List / LargeList of temporal → one per
// item (null items skipped). leewayTemporal lets an integer scalar read as epoch
// seconds. A non-temporal column appends nothing.
func cellMoments(arr arrow.Array, row int, leewayTemporal bool, dst []int64) []int64 {
	switch a := arr.(type) {
	case *array.List:
		start, end := a.ValueOffsets(row)
		return listMoments(a.ListValues(), start, end, leewayTemporal, dst)
	case *array.LargeList:
		start, end := a.ValueOffsets(row)
		return listMoments(a.ListValues(), start, end, leewayTemporal, dst)
	default:
		if ms, ok := temporalCellMS(arr, row, leewayTemporal); ok {
			dst = append(dst, ms)
		}
	}
	return dst
}

// listMoments appends the temporal moments of inner[start:end]. A non-temporal
// inner type yields nothing (temporalCellMS rejects every element), so a leeway
// support array (List of UInt64, etc.) contributes no moments.
func listMoments(inner arrow.Array, start, end int64, leewayTemporal bool, dst []int64) []int64 {
	for idx := start; idx < end; idx++ {
		if ms, ok := temporalCellMS(inner, int(idx), leewayTemporal); ok {
			dst = append(dst, ms)
		}
	}
	return dst
}

// zipSpans pairs the i-th moment of two co-section columns into a span, ordered
// [min,max] so it holds regardless of which column is begin vs end. Extra items
// in the longer column (mismatched cardinality — a malformed pair) are dropped.
func zipSpans(a, b []int64) (spans []temporalSpan) {
	n := min(len(a), len(b))
	spans = make([]temporalSpan, 0, n)
	for i := 0; i < n; i++ {
		lo, hi := a[i], b[i]
		if hi < lo {
			lo, hi = hi, lo
		}
		spans = append(spans, temporalSpan{fromMS: lo, toMS: hi})
	}
	return spans
}

// capMarks trims attributes so the total flags + bars drawn stays within
// maxDetailMarks, dropping surplus items from the tail and returning how many
// were dropped. Attributes left empty by the trim are removed.
func capMarks(in []temporalAttr) (out []temporalAttr, dropped int) {
	marks := 0
	for _, a := range in {
		room := maxDetailMarks - marks
		switch a.kind {
		case kindIntervals:
			if len(a.spans) > room {
				dropped += len(a.spans) - max(room, 0)
				a.spans = a.spans[:max(room, 0)]
			}
			marks += len(a.spans)
			if len(a.spans) > 0 {
				out = append(out, a)
			}
		default:
			if len(a.points) > room {
				dropped += len(a.points) - max(room, 0)
				a.points = a.points[:max(room, 0)]
			}
			marks += len(a.points)
			if len(a.points) > 0 {
				out = append(out, a)
			}
		}
	}
	return out, dropped
}

// temporalCellMS resolves the (non-null) cell at row to epoch milliseconds
// (UTC). Arrow temporal types are recognised by type unconditionally. When
// leewayTemporal is set — the column is a leeway tv:time: attribute or the
// backbone entity-timestamp — an integer cell is read as epoch *seconds*, the
// shape a width-32 DateTime('UTC') leeway column takes on the Arrow wire. A
// non-temporal cell returns ok=false.
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
// the canonical one).
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
// ':' separator or the '_' ClickHouse column dumps mangle it to.
func hasLeewayTimePrefix(name string) bool {
	return strings.HasPrefix(name, "tv:time:") || strings.HasPrefix(name, "tv_time_")
}

// sectionLabel renders a leeway SectionName (canonicalised, e.g. "time-range")
// as the descriptive label for a range attribute.
func sectionLabel(section string) string {
	return section
}

// formatEpochMS renders an epoch-ms moment as a compact UTC wall-clock string
// for the legend and tooltip. Seconds resolution is deliberate: sub-second
// precision would crowd the legend, and the axis carries the coarse placement.
func formatEpochMS(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04:05")
}

// summary is the descriptive string shown beside an attribute in the legend: a
// lone timestamp verbatim; an instant array or interval set as a count plus its
// extent. Uses only "·" (U+00B7) and "…" (U+2026), both present in the UI font.
func (inst temporalAttr) summary() string {
	switch inst.kind {
	case kindIntervals:
		lo, hi, ok := inst.extent()
		if !ok {
			return ""
		}
		if len(inst.spans) == 1 {
			return formatEpochMS(lo) + " … " + formatEpochMS(hi)
		}
		return fmt.Sprintf("%d windows · %s … %s", len(inst.spans), formatEpochMS(lo), formatEpochMS(hi))
	default:
		if len(inst.points) == 1 {
			return formatEpochMS(inst.points[0])
		}
		lo, hi, ok := inst.extent()
		if !ok {
			return ""
		}
		return fmt.Sprintf("%d items · %s … %s", len(inst.points), formatEpochMS(lo), formatEpochMS(hi))
	}
}

// extent returns the [earliest, latest] epoch-ms across the attribute's points
// or span endpoints, ok=false when it carries none.
func (inst temporalAttr) extent() (lo, hi int64, ok bool) {
	track := func(t int64) {
		if !ok {
			lo, hi, ok = t, t, true
			return
		}
		lo, hi = min(lo, t), max(hi, t)
	}
	for _, p := range inst.points {
		track(p)
	}
	for _, s := range inst.spans {
		track(s.fromMS)
		track(s.toMS)
	}
	return lo, hi, ok
}

// buildDetailAnnotations turns the instant attributes into flags: every point of
// an attribute shares the attribute's 1-based number and palette hue, with a
// "label: value" tooltip carrying the per-item value.
func buildDetailAnnotations(attrs []temporalAttr) (anns []*layout.Annotation) {
	for _, a := range attrs {
		if a.kind != kindInstants {
			continue
		}
		for _, p := range a.points {
			anns = append(anns, &layout.Annotation{
				TMS:        p,
				Number:     int32(a.paletteIdx + 1),
				PaletteIdx: int32(a.paletteIdx % 10),
				Label:      a.label + ": " + formatEpochMS(p),
			})
		}
	}
	return anns
}

// buildDetailIntervals turns the interval attributes into lane bars: every span
// of an attribute lands on one lane labelled with the attribute name, coloured
// by KindID against the qualitative palette (WithIntervalColors) so it matches
// the legend swatch.
func buildDetailIntervals(attrs []temporalAttr) (ivs []*layout.IntervalEvent) {
	for _, a := range attrs {
		if a.kind != kindIntervals {
			continue
		}
		for _, s := range a.spans {
			ivs = append(ivs, &layout.IntervalEvent{
				FromMS:   s.fromMS,
				ToMS:     s.toMS,
				KindID:   int32(a.paletteIdx % 10),
				LaneHint: a.label,
			})
		}
	}
	return ivs
}

// fitRange computes the pinned view window covering every point and span
// endpoint with a small margin. A degenerate extent widens to a fixed ±12h
// window so SetRange's t1 > t0 requirement holds. ok=false only when there is
// nothing to plot.
func fitRange(attrs []temporalAttr) (lo, hi time.Time, ok bool) {
	var minMS, maxMS int64
	have := false
	track := func(t int64) {
		if !have {
			minMS, maxMS, have = t, t, true
			return
		}
		minMS, maxMS = min(minMS, t), max(maxMS, t)
	}
	for _, a := range attrs {
		for _, p := range a.points {
			track(p)
		}
		for _, s := range a.spans {
			track(s.fromMS)
			track(s.toMS)
		}
	}
	if !have {
		return time.Time{}, time.Time{}, false
	}
	var pad int64
	if span := maxMS - minMS; span <= 0 {
		pad = singleInstantPadMS
	} else if pad = span / 10; pad < 1 {
		pad = 1
	}
	return time.UnixMilli(minMS - pad).UTC(), time.UnixMilli(maxMS + pad).UTC(), true
}

// DetailTimeline renders the Detail pane's per-row temporal overview strip. It
// owns a single non-interactive timeline widget reused across rows; the
// annotation/interval sets + pinned range are rebuilt only when the
// (result, row) changes — the early-cutoff the Timeline tab's driver uses.
type DetailTimeline struct {
	ids *c.WidgetIdStack
	tl  *timeline.Timeline

	seenRec arrow.RecordBatch
	seenRow int64
	attrs   []temporalAttr
	dropped int
}

// NewDetailTimeline builds the driver and its widget. The widget is
// non-interactive (a read-only overview; the annotation stagger separates
// colliding flags without a zoom, and a pan/zoom-capturing widget at the top of
// the Detail pane would fight the pane's own scroll). WithIntervalColors pins
// the lane-bar palette to the qualitative cycle so a range bar matches its
// legend swatch. The range is pinned per row via SetRange, so the widget's
// interval-only auto-fit never applies.
func NewDetailTimeline(ids *c.WidgetIdStack) (inst *DetailTimeline) {
	inst = &DetailTimeline{ids: ids, seenRow: -1}
	inst.tl = timeline.New(ids, "play-detail-timeline", nil,
		timeline.WithInteractive(false),
		timeline.WithIntervalColors(qualitativePalette()))
	return inst
}

// qualitativePalette is the 10-entry categorical cycle used for both flags
// (the widget applies it to Annotation.PaletteIdx internally) and lane bars
// (via WithIntervalColors + IntervalEvent.KindID), so an attribute's flag, bar,
// and legend swatch share one hue.
func qualitativePalette() []color.Color {
	p := make([]color.Color, 10)
	for i := range p {
		p[i] = color.Hex(styletokens.QualitativeCycle(i).AsHex())
	}
	return p
}

// sync re-detects the row's temporal attributes and pushes the flag/bar sets +
// pinned range into the widget when the (result, row) identity changes. Pure
// widget-state mutation (no ui scope), so it is exercised directly in tests.
func (inst *DetailTimeline) sync(rec arrow.RecordBatch, schema *arrow.Schema, row int64, classes []streamreadaccess.ColumnClass) {
	if rec == inst.seenRec && row == inst.seenRow {
		return
	}
	inst.seenRec = rec
	inst.seenRow = row
	inst.attrs, inst.dropped = detectTemporalAttrs(rec, schema, row, classes)
	inst.tl.SetAnnotations(buildDetailAnnotations(inst.attrs))
	inst.tl.SetIntervals(buildDetailIntervals(inst.attrs))
	if lo, hi, ok := fitRange(inst.attrs); ok {
		inst.tl.SetRange(lo, hi)
	} else {
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

// renderLegend draws one "● N label summary" row per attribute — the swatch hue
// matching the attribute's flags/bar — so a reader maps a numbered flag or a
// labelled lane back to its attribute and value(s). A trailing note reports any
// marks the density cap dropped.
func (inst *DetailTimeline) renderLegend() {
	for range c.Vertical().KeepIter() {
		for i, a := range inst.attrs {
			for range c.Horizontal().KeepIter() {
				for rt := range c.RichTextLabelColored(
					color.Hex(styletokens.QualitativeCycle(i%10).AsHex()),
					color.Transparent, "●") {
					rt.Small()
				}
				for rt := range c.RichTextLabel(strconv.Itoa(i + 1)) {
					rt.Small().Weak()
				}
				for rt := range c.RichTextLabel(a.label) {
					rt.Small()
				}
				for rt := range c.RichTextLabel(a.summary()) {
					rt.Small().Weak()
				}
			}
		}
		if inst.dropped > 0 {
			for rt := range c.RichTextLabel(fmt.Sprintf("+%d more not shown", inst.dropped)) {
				rt.Small().Weak()
			}
		}
	}
}
