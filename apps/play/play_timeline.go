package play

import (
	"fmt"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
	"github.com/stergiotis/boxer/public/thestack/utfsafe"
)

// Canonical slot names for the timeline panel's column contract. All slots
// use the `_<ns>_<slot>` namespace (leading underscore + 2-3 char tag) per
// the repo convention; leeway columns keep their `tv:`/`sv:`/`av:` colon-
// prefixed names untouched. Edit the contract in lockstep with
// resolveContract and the user-facing reference doc.
const (
	timelineSlotTime      = "_tl_time"
	timelineSlotTimeEnd   = "_tl_time_end"
	timelineSlotLabel     = "_tl_label"
	timelineSlotLane      = "_tl_lane"
	timelineSlotIntensity = "_tl_intensity"
)

type timelineMode uint8

const (
	timelineModeNone timelineMode = iota
	timelineModePoints
	timelineModeIntervals
	timelineModeAnnotations
)

// timelineContract is the resolved column inventory for a given schema.
// Exactly one of two states is valid:
//
//   - Mode != timelineModeNone: the panel renders, ColTime is set, and the
//     mode-specific ColX fields (ColTimeEnd for Intervals, ColLabel for
//     Annotations) are populated. Optional slot indices (ColLane,
//     ColIntensity) are -1 when the slot is absent from the schema.
//
//   - Mode == timelineModeNone and Reject != "": the panel shows Reject as
//     an empty-state hint so the SQL author can debug from the panel.
type timelineContract struct {
	Mode timelineMode

	ColTime      int32
	UnitTime     arrow.TimeUnit
	ColTimeEnd   int32
	UnitTimeEnd  arrow.TimeUnit
	ColLabel     int32
	ColLane      int32
	ColIntensity int32

	Reject string
}

// TimelineDriver bridges QueryStore Arrow results to the composite timeline
// widget. Per-frame: when the result identity changes (schema pointer or
// executed timestamp), the driver re-resolves the column contract and pushes
// the layout-event slices into the widget. Clicks on intervals / annotations
// route back into PlayApp.selectedRow so the Detail tab updates. Bucket
// clicks (Points-mode rug aggregation) carry no per-row id and are ignored.
//
// Bands: a separate panel-local SQL (held on PlayApp, accessed through
// bandsSQLPtr) is run synchronously whenever the main result's _tl_time
// extent changes — the (minMS, maxMS) pair is substituted into the
// user-typed SQL via the timelineBandsPlaceholder* tokens, the resulting
// bands are cached by (minMS, maxMS, sql) under an LRU, and a closure
// over inst.bands feeds the widget's WithBackgroundBands producer each
// frame. See play_timeline_bands.go for the bands-specific helpers.
type TimelineDriver struct {
	ids         *c.WidgetIdStack
	tl          *timeline.Timeline
	client      *Client
	selectedRow *int64
	bandsSQLPtr *string
	nowLinePtr  *bool

	seenSchema   *arrow.Schema
	seenExecuted time.Time
	contract     timelineContract

	dataMinMS       int64
	dataMaxMS       int64
	dataExtentValid bool

	// Bands concurrency model (mirrors Projector in play_projection.go): the
	// fetch runs on a background goroutine so a slow ClickHouse never blocks
	// the render thread. bandsMu guards every field written by that goroutine
	// (and by the render-thread cache-hit path). bandsView is the
	// render-thread-only copy that bandsProducer reads each frame — refreshed
	// from the shared state under the lock in Render, so the producer never
	// touches a slice the goroutine might be replacing.
	bandsMu           sync.Mutex
	bands             []layout.BackgroundBand
	bandsCache        []bandsCacheEntry
	bandsLastFetchKey bandsCacheKey
	bandsHaveFetched  bool
	bandsErr          error
	bandsSkipped      int
	bandsFetchedAt    time.Time
	bandsFetchDur     time.Duration
	bandsInFlight     bool
	bandsInFlightKey  bandsCacheKey

	bandsView []layout.BackgroundBand
}

// NewTimelineDriver constructs the driver and the underlying composite
// widget eagerly (the widget tolerates nil starter data and renders an
// empty axis until SetIntervals/SetPoints/SetAnnotations is called). The
// selectedRow pointer is captured so the WithOnSelection callback can
// push a row index back into PlayApp without a back-reference. The
// client is reused for bands-SQL submissions; bandsSQLPtr points at the
// PlayApp-owned, persisted bands SQL string (mutated by the TextEdit
// inside renderBandsControls). nowLinePtr points at the PlayApp-owned
// "now line" toggle (mutated by the toolbar checkbox); the driver
// pushes its current value into the widget via SetNowLine each frame
// so the flip survives data swaps without recreating the widget.
func NewTimelineDriver(ids *c.WidgetIdStack, selectedRow *int64, client *Client, bandsSQLPtr *string, nowLinePtr *bool) (inst *TimelineDriver) {
	inst = &TimelineDriver{
		ids:         ids,
		client:      client,
		selectedRow: selectedRow,
		bandsSQLPtr: bandsSQLPtr,
		nowLinePtr:  nowLinePtr,
		contract: timelineContract{
			ColTime:      -1,
			ColTimeEnd:   -1,
			ColLabel:     -1,
			ColLane:      -1,
			ColIntensity: -1,
		},
	}
	inst.tl = timeline.New(ids, "play-timeline", nil,
		timeline.WithOnSelection(inst.onSelect),
		timeline.WithBackgroundBands(inst.bandsProducer))
	return
}

func (inst *TimelineDriver) onSelect(sel timeline.SelectionInfo) {
	switch sel.Kind {
	case timeline.SelectionInterval:
		if sel.Interval != nil {
			*inst.selectedRow = int64(sel.Interval.KindID)
		}
	case timeline.SelectionAnnotation:
		if sel.Annotation != nil {
			*inst.selectedRow = int64(sel.Annotation.Number)
		}
	}
	// SelectionBucket: Points-mode rug aggregates 1+ events per cell; no
	// authoritative per-row id is available without re-scanning. Leave
	// selectedRow alone and let the user browse via the Table tab.
	// SelectionNone: user cleared the previous click; preserve selectedRow.
}

// Render paints the Timeline dock tab body. Caller is responsible for the
// nil-rec / loading / query-failed guards (see renderTimelineTab); this
// method assumes a non-nil record and a non-nil schema.
func (inst *TimelineDriver) Render(rec arrow.RecordBatch, schema *arrow.Schema, executed time.Time) {
	if schema != inst.seenSchema || !executed.Equal(inst.seenExecuted) {
		inst.seenSchema = schema
		inst.seenExecuted = executed
		inst.contract = resolveContract(schema)
		ivs, pts, anns := buildEvents(rec, inst.contract)
		inst.tl.SetIntervals(ivs)
		inst.tl.SetPoints(pts)
		inst.tl.SetAnnotations(anns)
		// Only drive the intensity colormap when the query actually projected
		// an _tl_intensity column; otherwise every event carries Intensity==0
		// and the sequential colormap collapses to its near-invisible dark end
		// against the dark canvas. Without intensity, the widget paints flat
		// legible accent fills instead.
		inst.tl.SetIntensityEncoding(inst.contract.ColIntensity >= 0)
		inst.dataMinMS, inst.dataMaxMS, inst.dataExtentValid = extentOfEvents(ivs, pts, anns)
	}
	if inst.contract.Mode == timelineModeNone {
		for range c.Vertical().KeepIter() {
			for rt := range c.RichTextLabel(inst.contract.Reject) {
				rt.Strong()
			}
			c.AddSpace(8)
			inst.RenderContractHelp()
		}
		return
	}
	inst.renderToolbar()
	inst.renderBandsControls()
	inst.maybeFetchBands(false)
	// Refresh the render-thread-only band slice from the shared (goroutine-
	// written) state, so bandsProducer reads a stable snapshot for this frame.
	inst.refreshBandsView()
	inst.tl.Render()
}

// RenderContractHelp emits a descriptive multi-line block listing the
// three column-shape modes the Timeline panel accepts (Points,
// Intervals, Annotations) and the slot constraints. Intended for
// empty-state and rejection-state surfaces so first-time users learn
// the contract from the panel itself instead of having to chase the
// how-to doc. Body line is body-sized; slot rows use the monospace
// style so column names line up visually; the closing note is small +
// weak so it doesn't compete with surrounding controls.
func (inst *TimelineDriver) RenderContractHelp() {
	for range c.Vertical().KeepIter() {
		c.Label("Timeline column contract — return one of these shapes:").Send()
		c.AddSpace(4)
		for _, line := range []string{
			"  Points       _tl_time",
			"  Intervals    _tl_time + _tl_time_end  (+ optional _tl_lane, _tl_intensity)",
			"  Annotations  _tl_time + _tl_label",
		} {
			for rt := range c.RichTextLabel(line) {
				rt.Monospace()
			}
		}
		c.AddSpace(4)
		for rt := range c.RichTextLabel(
			"Timestamps must be DateTime64(N); strings for labels and lanes. " +
				"See doc/howto/play-timeline-panel.md for recipes and the background-bands overlay.") {
			rt.Small().Weak()
		}
	}
}

// renderToolbar emits the always-visible Timeline-tab control strip
// above the bands collapsible. Currently hosts the "Now line" toggle;
// extensible to future per-panel toggles (view-range presets, density
// dropdown, etc.). Pushes the bound bool into the widget every frame
// via SetNowLine — cheap flag flip, no animation reset, idempotent.
func (inst *TimelineDriver) renderToolbar() {
	if inst.nowLinePtr == nil {
		return
	}
	for range c.Horizontal().KeepIter() {
		c.Checkbox(inst.ids.PrepareStr("tl-toolbar-nowline"),
			*inst.nowLinePtr, "Now line").
			SendRespVal(inst.nowLinePtr)
	}
	inst.tl.SetNowLine(*inst.nowLinePtr)
}

// extentOfEvents finds the (minMS, maxMS) covering every event time in
// the current rebuild. Operating on the already-materialised event
// slices avoids a second pass over the Arrow column and uniformly
// covers Points / Intervals / Annotations.
func extentOfEvents(ivs []*layout.IntervalEvent, pts []*layout.PointEvent, anns []*layout.Annotation) (minMS, maxMS int64, ok bool) {
	track := func(t int64) {
		if !ok {
			minMS, maxMS, ok = t, t, true
			return
		}
		if t < minMS {
			minMS = t
		}
		if t > maxMS {
			maxMS = t
		}
	}
	for _, e := range pts {
		if e != nil {
			track(e.TMS)
		}
	}
	for _, e := range ivs {
		if e != nil {
			track(e.FromMS)
			track(e.ToMS)
		}
	}
	for _, e := range anns {
		if e != nil {
			track(e.TMS)
		}
	}
	return
}

// resolveContract walks the schema once, validating per-slot types and
// then running the strict mutually-exclusive mode-selection table. Returns
// either a renderable contract (Mode != None) or a Reject string suitable
// for direct display.
func resolveContract(schema *arrow.Schema) (ct timelineContract) {
	ct.ColTime = -1
	ct.ColTimeEnd = -1
	ct.ColLabel = -1
	ct.ColLane = -1
	ct.ColIntensity = -1

	if schema == nil {
		ct.Reject = fmt.Sprintf(
			"Timeline expected a %q column (+ optional %q for intervals or %q for annotations).",
			timelineSlotTime, timelineSlotTimeEnd, timelineSlotLabel)
		return
	}

	for i, f := range schema.Fields() {
		switch f.Name {
		case timelineSlotTime:
			tt, ok := f.Type.(*arrow.TimestampType)
			if !ok {
				ct.Reject = fmt.Sprintf("%q must be a Timestamp column (got %s).",
					timelineSlotTime, f.Type)
				return
			}
			ct.ColTime = int32(i)
			ct.UnitTime = tt.Unit
		case timelineSlotTimeEnd:
			tt, ok := f.Type.(*arrow.TimestampType)
			if !ok {
				ct.Reject = fmt.Sprintf("%q must be a Timestamp column (got %s).",
					timelineSlotTimeEnd, f.Type)
				return
			}
			ct.ColTimeEnd = int32(i)
			ct.UnitTimeEnd = tt.Unit
		case timelineSlotLabel:
			if !isStringLikeType(f.Type) {
				ct.Reject = fmt.Sprintf("%q must be a String / Binary column (got %s).",
					timelineSlotLabel, f.Type)
				return
			}
			ct.ColLabel = int32(i)
		case timelineSlotLane:
			if !isStringLikeType(f.Type) {
				ct.Reject = fmt.Sprintf("%q must be a String / Binary column (got %s).",
					timelineSlotLane, f.Type)
				return
			}
			ct.ColLane = int32(i)
		case timelineSlotIntensity:
			if !isNumericType(f.Type) {
				ct.Reject = fmt.Sprintf("%q must be a numeric column (got %s).",
					timelineSlotIntensity, f.Type)
				return
			}
			ct.ColIntensity = int32(i)
		}
	}

	hasTime := ct.ColTime >= 0
	hasEnd := ct.ColTimeEnd >= 0
	hasLabel := ct.ColLabel >= 0

	switch {
	case !hasTime && hasEnd && hasLabel:
		ct.Reject = fmt.Sprintf("%q and %q both require %q.",
			timelineSlotTimeEnd, timelineSlotLabel, timelineSlotTime)
		return
	case !hasTime && hasEnd:
		ct.Reject = fmt.Sprintf("%q requires %q.",
			timelineSlotTimeEnd, timelineSlotTime)
		return
	case !hasTime && hasLabel:
		ct.Reject = fmt.Sprintf("%q requires %q.",
			timelineSlotLabel, timelineSlotTime)
		return
	case !hasTime:
		ct.Reject = fmt.Sprintf(
			"Timeline expected a %q column (+ optional %q for intervals or %q for annotations).",
			timelineSlotTime, timelineSlotTimeEnd, timelineSlotLabel)
		return
	case hasEnd && hasLabel:
		ct.Reject = fmt.Sprintf("Ambiguous: remove %q for Intervals or %q for Annotations.",
			timelineSlotLabel, timelineSlotTimeEnd)
		return
	case hasEnd:
		ct.Mode = timelineModeIntervals
	case hasLabel:
		ct.Mode = timelineModeAnnotations
	default:
		ct.Mode = timelineModePoints
	}
	return
}

// buildEvents materialises the per-mode event slice from rec. Returns the
// populated slice for the active mode and nil for the others — callers feed
// all three into the widget so swapping modes between queries clears stale
// data. Rows with a null in any required slot for the active mode are
// skipped silently.
func buildEvents(rec arrow.RecordBatch, ct timelineContract) (ivs []*layout.IntervalEvent, pts []*layout.PointEvent, anns []*layout.Annotation) {
	if rec == nil || ct.Mode == timelineModeNone {
		return
	}
	n := rec.NumRows()
	if n == 0 {
		return
	}

	timeArr, _ := rec.Column(int(ct.ColTime)).(*array.Timestamp)
	if timeArr == nil {
		return
	}

	var endArr *array.Timestamp
	if ct.ColTimeEnd >= 0 {
		endArr, _ = rec.Column(int(ct.ColTimeEnd)).(*array.Timestamp)
	}

	var labelArr arrow.Array
	if ct.ColLabel >= 0 {
		labelArr = rec.Column(int(ct.ColLabel))
	}
	var laneArr arrow.Array
	if ct.ColLane >= 0 {
		laneArr = rec.Column(int(ct.ColLane))
	}
	var intensityArr arrow.Array
	if ct.ColIntensity >= 0 {
		intensityArr = rec.Column(int(ct.ColIntensity))
	}

	switch ct.Mode {
	case timelineModePoints:
		pts = make([]*layout.PointEvent, 0, n)
		for i := range n {
			if timeArr.IsNull(int(i)) {
				continue
			}
			ev := &layout.PointEvent{
				TMS:       tsToEpochMS(int64(timeArr.Value(int(i))), ct.UnitTime),
				KindID:    int32(i),
				Intensity: readIntensityCell(intensityArr, int(i)),
			}
			pts = append(pts, ev)
		}
	case timelineModeIntervals:
		if endArr == nil {
			return
		}
		ivs = make([]*layout.IntervalEvent, 0, n)
		for i := range n {
			if timeArr.IsNull(int(i)) || endArr.IsNull(int(i)) {
				continue
			}
			ev := &layout.IntervalEvent{
				FromMS:    tsToEpochMS(int64(timeArr.Value(int(i))), ct.UnitTime),
				ToMS:      tsToEpochMS(int64(endArr.Value(int(i))), ct.UnitTimeEnd),
				KindID:    int32(i),
				Intensity: readIntensityCell(intensityArr, int(i)),
				LaneHint:  readStringCell(laneArr, int(i)),
			}
			if ev.ToMS < ev.FromMS {
				continue
			}
			ivs = append(ivs, ev)
		}
	case timelineModeAnnotations:
		if labelArr == nil {
			return
		}
		anns = make([]*layout.Annotation, 0, n)
		for i := range n {
			if timeArr.IsNull(int(i)) || labelArr.IsNull(int(i)) {
				continue
			}
			ev := &layout.Annotation{
				TMS:        tsToEpochMS(int64(timeArr.Value(int(i))), ct.UnitTime),
				Number:     int32(i),
				PaletteIdx: int32(i % 10),
				Label:      readStringCell(labelArr, int(i)),
			}
			anns = append(anns, ev)
		}
	}
	return
}

// tsToEpochMS converts an Arrow timestamp value to epoch milliseconds
// (UTC). Unknown units fall through unchanged — the widget will misplace
// the event on the axis but won't crash, and the offending column shows
// up obviously in the rendered timeline.
func tsToEpochMS(v int64, unit arrow.TimeUnit) (ms int64) {
	switch unit {
	case arrow.Second:
		ms = v * 1000
	case arrow.Millisecond:
		ms = v
	case arrow.Microsecond:
		ms = v / 1000
	case arrow.Nanosecond:
		ms = v / 1_000_000
	default:
		ms = v
	}
	return
}

// readStringCell extracts a string from String / LargeString / Binary /
// LargeBinary columns, hex-fallback-safe via utfsafe.EnsureUTF8 so non-UTF-8
// payloads (CH FORMAT ArrowStream emits String as LargeBinary by default)
// can't desync the FFFI wire downstream of c.Label.
func readStringCell(arr arrow.Array, row int) (s string) {
	if arr == nil || arr.IsNull(row) {
		return
	}
	switch a := arr.(type) {
	case *array.String:
		s = utfsafe.EnsureUTF8(a.Value(row))
	case *array.LargeString:
		s = utfsafe.EnsureUTF8(a.Value(row))
	case *array.Binary:
		s = utfsafe.EnsureUTF8(string(a.Value(row)))
	case *array.LargeBinary:
		s = utfsafe.EnsureUTF8(string(a.Value(row)))
	}
	return
}

// readIntensityCell coerces any numeric Arrow value to float32 for the
// widget's [0,1] colormap input. Out-of-range values are clamped by the
// widget; the contract documents that the caller should normalise upstream.
func readIntensityCell(arr arrow.Array, row int) (v float32) {
	if arr == nil || arr.IsNull(row) {
		return
	}
	switch a := arr.(type) {
	case *array.Int8:
		v = float32(a.Value(row))
	case *array.Int16:
		v = float32(a.Value(row))
	case *array.Int32:
		v = float32(a.Value(row))
	case *array.Int64:
		v = float32(a.Value(row))
	case *array.Uint8:
		v = float32(a.Value(row))
	case *array.Uint16:
		v = float32(a.Value(row))
	case *array.Uint32:
		v = float32(a.Value(row))
	case *array.Uint64:
		v = float32(a.Value(row))
	case *array.Float32:
		v = a.Value(row)
	case *array.Float64:
		v = float32(a.Value(row))
	}
	return
}

func isNumericType(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.INT8, arrow.INT16, arrow.INT32, arrow.INT64,
		arrow.UINT8, arrow.UINT16, arrow.UINT32, arrow.UINT64,
		arrow.FLOAT16, arrow.FLOAT32, arrow.FLOAT64:
		ok = true
	}
	return
}

func isStringLikeType(dt arrow.DataType) (ok bool) {
	switch dt.ID() {
	case arrow.STRING, arrow.LARGE_STRING, arrow.BINARY, arrow.LARGE_BINARY:
		ok = true
	}
	return
}
