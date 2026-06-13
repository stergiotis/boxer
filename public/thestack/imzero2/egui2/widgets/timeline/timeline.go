// Package timeline provides a calendar-axis interval-event widget for the
// ImZero2 framework. It packs IntervalEvents into non-overlapping lanes
// (greedy left-to-right) and paints them as filled rectangles on a
// PaintCanvas, with a calendar-aware tick axis along the bottom and
// optional rollover context rows (year / date) above the ticks.
//
// Basic usage:
//
//	var tl = timeline.New(ids, "ops-deploys", events,
//	    timeline.WithContainerSize(1024, 220))
//	for range c.Window(...).KeepIter() {
//	    tl.Render()
//	}
//
// Visual tweaks go through WithVisuals + DefaultVisuals; see the Visuals
// type docs for the full surface.
//
// The widget is composite, not an FFFI2 primitive: all state lives on the
// receiver pointer and the caller holds *Timeline across frames. Multiple
// instances coexist safely — Render wraps its body in c.IdScope(scopeKey).
// All time on the wire is int64 epoch milliseconds in UTC; the tick axis
// can be localised via the (currently widget-internal) loc parameter on
// ComputeTickMap.
//
// # Validation policy
//
// Every public symbol classifies its input handling as one of three tiers,
// stated on a `// Validation:` line in the symbol's godoc:
//
//   - Panic: programmer errors (nil where required, structurally
//     impossible input). Fails loudly at the call site so bugs surface
//     during development. Examples: WithContainerSize(0, 0),
//     New(nil ids, ...), WithRange(t1, t0) with t1 <= t0.
//
//   - Nil clears / no-op: data-shaped inputs where a missing value has an
//     obvious semantic ("no data", "no callback"). Documented per option;
//     never panics. Examples: WithPointEvents(nil), WithOnIntervalClick(nil),
//     WithBackgroundBands(nil), WithVisuals(nil).
//
//   - Snapshot at boundary: methods returning state computed during the
//     most recent Render reflect *that* frame, not the live data — see
//     LaneCount, Selection, CursorTime.
package timeline

import (
	"fmt"
	"iter"
	"math"
	"slices"
	"time"

	"github.com/stergiotis/boxer/public/math/numerical/timeticks"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/timeline/layout"
)

// BackgroundBandProducer yields layout.BackgroundBand values lazily for a
// given view range. Called once per frame with the current viewMinMS /
// viewMaxMS; only bands that would be visible need to be yielded. Returns
// an iter.Seq so producers can be a `for t := startOfDay(...); t < end;
// t += day` loop without materialising a slice — bands that scale with
// the view (weekends, office hours, repeating maintenance windows) avoid
// allocation entirely on most frames.
type BackgroundBandProducer func(viewMinMS, viewMaxMS int64) iter.Seq[layout.BackgroundBand]

// Visuals bundles every visual-styling knob the Timeline exposes. All
// pixel dimensions are in egui logical pixels (post-DPI). Callers tweak
// via the WithVisuals mutator option:
//
//	tl := timeline.New(ids, scopeKey, data,
//	    timeline.WithVisuals(func(v *timeline.Visuals) {
//	        v.LaneHeight = 30
//	        v.IntensityColormap = styletokens.SequentialMagma
//	    }))
//
// The struct has no internal invariants and accepts any field values; the
// widget will render whatever it's given, including zeroes (which produce
// degenerate-but-valid output). Use DefaultVisuals() for sensible
// IDS-token-derived starting values; the mutator form ensures callers
// can only modify defaults, never accidentally replace them with a
// zero-valued struct literal.
type Visuals struct {
	// Layout (pixel dimensions)
	LaneHeight       float32
	LaneGap          float32
	AxisHeight       float32
	RolloverRowH     float32
	CornerRadius     float32
	BarMinPx         float32
	RugStripH        float32
	RugGap           float32
	AnnotationBandH  float32
	AnnotationFlagW  float32
	AnnotationFlagH  float32

	// Colors
	BgColor              color.Color
	AxisColor            color.Color
	TickMarkColor        color.Color
	TickLabelColor       color.Color
	RolloverColor        color.Color
	TooltipBgColor       color.Color
	TooltipFgColor       color.Color
	SelectionStrokeColor color.Color
	NowLineColor         color.Color
	AnnotationFgColor    color.Color

	// Flat event fills — used for interval bars and raw rug marks when
	// intensity is NOT the encoded dimension (see WithIntensityEncoding).
	// A sequential colormap is lightness-monotonic from its dark end, so an
	// all-zero-intensity dataset (the common case when the caller never
	// attached an intensity column) would otherwise paint every glyph at the
	// near-background dark end and vanish against BgColor. These flat,
	// legible accent fills keep events readable; they are ignored entirely
	// while intensity encoding is on.
	IntervalColor color.Color
	PointColor    color.Color

	// Categorical colormaps
	IntensityColormap styletokens.SequentialE
	RugColormap       styletokens.SequentialE
}

// DefaultVisuals returns the IDS-token-derived default visual treatment.
// Color tokens resolve via styletokens; layout dimensions are tuned to
// look reasonable at the demo's 1180×280-ish stage. Callers wanting a
// dense / sparse / monospace look should start here and mutate via
// WithVisuals.
func DefaultVisuals() (v Visuals) {
	v = Visuals{
		LaneHeight:        28,
		LaneGap:           4,
		AxisHeight:        22,
		RolloverRowH:      16,
		CornerRadius:      2,
		BarMinPx:          1,
		RugStripH:         24,
		RugGap:            4,
		AnnotationBandH:   18,
		AnnotationFlagW:   26,
		AnnotationFlagH:   14,
		IntensityColormap: styletokens.SequentialBatlow,
		RugColormap:       styletokens.SequentialBatlow,
	}
	v.BgColor = color.Hex(styletokens.NeutralBgSurface.AsHex()).Keep()
	v.AxisColor = color.Hex(styletokens.NeutralBorderFaint.AsHex()).Keep()
	v.TickMarkColor = color.Hex(styletokens.NeutralBorderFaint.AsHex()).Keep()
	v.TickLabelColor = color.Hex(styletokens.NeutralTextSecondary.AsHex()).Keep()
	v.RolloverColor = color.Hex(styletokens.NeutralTextPrimary.AsHex()).Keep()
	v.TooltipBgColor = color.Hex(styletokens.NeutralBgFaint.AsHex()).Keep()
	v.TooltipFgColor = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	v.SelectionStrokeColor = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	v.NowLineColor = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	v.AnnotationFgColor = color.Hex(styletokens.NeutralTextExtreme.AsHex()).Keep()
	// Flat fills for the intensity-off path: the soft accent for the larger
	// bar areas, the brighter info hue for the thin 1-px rug marks that need
	// more punch to read. Both sit at IDS lightness ~0.80 — high contrast
	// against the ~0.24 NeutralBgSurface canvas.
	v.IntervalColor = color.Hex(styletokens.AccentDefault.AsHex()).Keep()
	v.PointColor = color.Hex(styletokens.InfoDefault.AsHex()).Keep()
	return
}

const (
	defaultContainerW        float32 = 1024
	defaultRawPointThreshold int32   = 500
	rangePaddingFraction     float64 = 0.02
	tickMarkHeight           float32 = 6
	tickLabelOffsetY         float32 = 8
	tickLabelFontSize        float32 = 11
	rolloverLabelFontSize    float32 = 10
	tooltipFontSize          float32 = 11
	tooltipLineHeight        float32 = 14
	tooltipPaddingX          float32 = 6
	tooltipPaddingY          float32 = 4
	tooltipCursorOffsetX     float32 = 10
	tooltipCursorOffsetY     float32 = -8
	// tooltipCharWidthPx is the ASCII-only character-width estimate the
	// widget uses to size tooltip boxes and the lane-label band. At 11pt
	// the default proportional font averages ~6.5 px / character for
	// ASCII; for CJK / emoji / wide characters it underestimates by 2–4×
	// and clipped text may result. egui's text-measurement primitives
	// aren't surfaced through FFFI2 yet, so an empirical constant is the
	// pragmatic answer until they are. Promote to Visuals if a caller
	// needs to tune.
	tooltipCharWidthPx       float32 = 6.5
	minZoomMul               float32 = 0.05
	maxZoomMul               float32 = 20.0
	rugMarkWidthPx           float32 = 1.0
	rugDensityMinPx          float32 = 1.0
	// rugDensityMinTint floors the density-mode rug colormap input so even
	// single-event buckets land at a mid-palette brightness instead of the
	// near-bg dark end. Without this, low-count cells visually vanish
	// against NeutralBgSurface — they're there but unreadable. Applies
	// only to density mode; raw mode preserves per-point Intensity intact.
	rugDensityMinTint        float32 = 0.3
	labelPaddingX            float32 = 8
	labelBandMaxW            float32 = 200
	crosshairWidthPx         float32 = 1
	selectionStrokeWidthPx   float32 = 2
	annotationGap            float32 = 4
	annotationFlagPaddingY   float32 = 2
	annotationFlagCornerR    float32 = 2
	annotationDashLen        float32 = 4
	annotationGapLen         float32 = 4
	annotationStrokeW        float32 = 1
	annotationFlagFontSize   float32 = 10
	annotationHitCorridorPx  float32 = 13
	// annotationSelectionInsetPx widens the back-fill rect drawn behind a
	// selected annotation flag — the visible margin between back-fill
	// and flag fill reads as the selection outline. Avoids PaintRectStroke
	// which would clip against the canvas top edge at y=0.
	annotationSelectionInsetPx float32 = 2
	nowLineWidthPx           float32 = 1.5
	emptyFallback                    = 1 * time.Hour
)

// SelectionKindE discriminates what a Timeline currently has selected.
// SelectionNone is the zero value; callers should always inspect Kind
// before reading Interval or Bucket.
type SelectionKindE uint8

const (
	SelectionNone SelectionKindE = iota
	SelectionInterval
	SelectionBucket
	SelectionAnnotation
)

// verticalLayout bundles the per-frame Y geometry of the timeline canvas
// plus the horizontal axis bounds. Computed once at the start of
// renderBody and threaded through every paint* / hitTest* method to
// eliminate the previous 5-position parameter list (topReserved,
// rugTopY, laneBaseY, axisBaselineY, axisStartPx, axisEndPx, ...) those
// methods used to take.
type verticalLayout struct {
	topReserved    float32 // top of any glyph; >0 when annotations reserved
	rugTopY        float32 // top of rug strip (== topReserved)
	laneBaseY      float32 // top of first lane row
	laneAreaH      float32 // total height of all lane rows + gaps
	axisBaselineY  float32 // 1-px axis baseline; lanes end here
	rolloverStartY float32 // first rollover row top
	totalH         float32 // canvas total height (max of content + effH)
	axisStartPx    float32 // == labelW; left edge of the time axis
	axisEndPx      float32 // == effW; right edge of the time axis
}

// clipToAxis returns the input rect-x-range clamped to [axisStartPx,
// axisEndPx], with ok=false when the range is entirely outside the axis
// (no need to paint).
func (vl verticalLayout) clipToAxis(x0, x1 float32) (cx0, cx1 float32, ok bool) {
	if x1 <= vl.axisStartPx || x0 >= vl.axisEndPx {
		return
	}
	cx0 = max(x0, vl.axisStartPx)
	cx1 = min(x1, vl.axisEndPx)
	ok = true
	return
}

// SelectionInfo is the snapshot returned by Timeline.Selection. Interval,
// Bucket, and Annotation are all pointers — non-nil iff Kind matches the
// corresponding SelectionKindE value. The zero value (Kind ==
// SelectionNone, all pointers nil) is what callers see before any click.
type SelectionInfo struct {
	Kind       SelectionKindE
	Interval   *layout.IntervalEvent
	Bucket     *layout.Bucket
	Annotation *layout.Annotation
}

// defaultLODScales is the binning ladder used by Timeline when the caller
// does not pass WithLODScales. Spans 1 ms → 1 week so a single index can
// serve everything from sub-second log streams to multi-month dashboards.
var defaultLODScales = []time.Duration{
	1 * time.Millisecond,
	10 * time.Millisecond,
	100 * time.Millisecond,
	1 * time.Second,
	10 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	30 * time.Minute,
	1 * time.Hour,
	6 * time.Hour,
	24 * time.Hour,
	7 * 24 * time.Hour,
}

// Timeline is a calendar-axis interval-event visualization. It is constructed
// with New and reused across frames via Render. SetIntervals / SetRange
// mutate the widget between frames without losing identity (the scopeKey
// stays stable, so any future stateful sub-widget — hover handle, pan/zoom
// memory — survives data updates).
type Timeline struct {
	ids      *c.WidgetIdStack
	scopeKey string

	intervals   []*layout.IntervalEvent
	points      []*layout.PointEvent
	annotations []*layout.Annotation

	lodIndex          *layout.LODIndex
	lodScales         []time.Duration
	rawPointThreshold int32

	explicitRange  bool
	interactivePin bool
	viewMinMS      int64
	viewMaxMS      int64

	containerW float32

	visuals Visuals

	interactionEnabled bool

	onSelection SelectionListener

	backgroundBands  BackgroundBandProducer
	nowLineEnabled   bool
	intensityEncoded bool

	selection SelectionInfo

	cursorTimeMS    int64
	cursorTimeValid bool

	// Last-frame view snapshot, written at the end of renderBody and
	// read by SelectBucketAt to resolve a time → bucket lookup at the
	// scale the user is currently viewing. lastViewPxWidth == 0 means
	// "no render has happened yet"; callers get a silent no-op.
	lastViewMinMS   int64
	lastViewMaxMS   int64
	lastViewPxWidth int32

	laneAssn layout.LaneAssignment
}

// Option configures a Timeline at construction.
type Option func(inst *Timeline)

// WithContainerWidth sets the widget's initial / first-frame canvas width,
// used before the captureAvailableSize FFFI2 fetcher has surfaced a value
// (typically frame 1 after construction). Each subsequent frame the
// widget auto-fits its width to the parent panel's available_size; height
// is always content-driven (annotation band + rug + lane rows + axis +
// rollover rows) and never padded to fill leftover vertical space.
//
// Validation: panic on w <= 0.
func WithContainerWidth(w float32) Option {
	if w <= 0 {
		panic(fmt.Sprintf("timeline: WithContainerWidth requires positive w (got %v)", w))
	}
	return func(inst *Timeline) {
		inst.containerW = w
	}
}

// WithInteractive toggles pan/zoom/hover-tooltip behaviour. Default true.
// Set false for embedded use where the timeline should render read-only
// (e.g. inside a screenshot tour where the cursor isn't meaningful).
//
// Validation: none — both true and false are valid.
func WithInteractive(enabled bool) Option {
	return func(inst *Timeline) {
		inst.interactionEnabled = enabled
	}
}

// SelectionListener receives every selection change driven by a
// primary-click over the canvas — including click-miss / click-same
// gestures that clear the previous selection (the listener then receives
// a SelectionInfo with Kind == SelectionNone). Programmatic mutators
// (SelectAnnotationByNumber, ClearSelection, SetIntervals, etc.) do NOT
// fire the listener — those are caller-initiated by definition.
type SelectionListener func(sel SelectionInfo)

// WithOnSelection registers a callback that fires on every selection
// change driven by a primary-click over the canvas. Subsumes the previous
// per-kind callbacks (WithOnIntervalClick / WithOnRugBucketClick /
// WithOnAnnotationClick); the listener dispatches on SelectionInfo.Kind:
//
//	timeline.WithOnSelection(func(sel timeline.SelectionInfo) {
//	    switch sel.Kind {
//	    case timeline.SelectionInterval:   useInterval(sel.Interval)
//	    case timeline.SelectionBucket:     useBucket(sel.Bucket)
//	    case timeline.SelectionAnnotation: useAnnotation(sel.Annotation)
//	    case timeline.SelectionNone:       // click cleared selection
//	    }
//	})
//
// Validation: nil disables the listener (no-op).
func WithOnSelection(fn SelectionListener) Option {
	return func(inst *Timeline) {
		inst.onSelection = fn
	}
}

// WithAnnotations attaches Grafana-style time-pinned markers (vertical
// dashed lines + numbered flag at the top of the canvas). Annotations are
// rendered above bars and rug events; their hit corridor extends the full
// vertical data area.
//
// Validation: nil clears (widget renders without annotations).
func WithAnnotations(as []*layout.Annotation) Option {
	return func(inst *Timeline) {
		inst.annotations = as
	}
}

// WithBackgroundBands registers a lazy producer of shaded time ranges
// (weekend overlay, office hours, maintenance windows, alert windows).
// The producer is called once per frame with the current view range and
// must yield only bands that intersect it — typically a one-pass for-loop
// stepping through the relevant calendar units. Bands paint *under* every
// other glyph (lanes, rug, axis) so foreground stays legible; choose low
// alpha (~0x18–0x40) in the BackgroundBand.Color channel.
//
// Validation: nil clears (widget renders without background bands).
func WithBackgroundBands(producer BackgroundBandProducer) Option {
	return func(inst *Timeline) {
		inst.backgroundBands = producer
	}
}

// WithNowLine toggles a vertical "now" line at time.Now() (UTC). Renders
// only when the current wall-clock time falls inside the view range —
// for static / historical views it disappears silently. Solid 1.5 px
// line in Visuals.NowLineColor, painted above the data and below the
// tooltip.
//
// Validation: none — both true and false are valid.
func WithNowLine(enabled bool) Option {
	return func(inst *Timeline) {
		inst.nowLineEnabled = enabled
	}
}

// WithIntensityEncoding toggles whether interval bars and raw rug marks
// derive their fill from the per-event Intensity via IntensityColormap /
// RugColormap (true, the default) or from the flat Visuals.IntervalColor /
// Visuals.PointColor (false). Turn it OFF when the data carries no intensity
// dimension: a sequential colormap is lightness-monotonic from its dark end,
// so an all-zero-intensity dataset would otherwise paint every glyph at the
// near-background dark end and vanish against BgColor. The density rug always
// encodes bucket count and is unaffected by this toggle.
//
// Validation: none — both true and false are valid.
func WithIntensityEncoding(enabled bool) Option {
	return func(inst *Timeline) {
		inst.intensityEncoded = enabled
	}
}

// WithRange forces an explicit [t0,t1] viewport instead of auto-fitting to
// data extent. t1 must be after t0; both are interpreted as UTC moments
// (timezone handling on the tick axis is the timeticks layer's concern).
//
// Validation: panic on !t1.After(t0). To restore auto-fit on a
// constructed Timeline use SetRange(time.Time{}, time.Time{}); the
// constructor option has no zero-value escape because no defaulting
// applies at construction.
func WithRange(t0, t1 time.Time) Option {
	if !t1.After(t0) {
		panic(fmt.Sprintf("timeline: WithRange requires t1 after t0 (got %v, %v)", t0, t1))
	}
	return func(inst *Timeline) {
		inst.explicitRange = true
		inst.viewMinMS = t0.UnixMilli()
		inst.viewMaxMS = t1.UnixMilli()
	}
}

// WithVisuals mutates the widget's Visuals (which start at DefaultVisuals
// values set in New) so callers tweak only the fields they care about
// without leaving the others zero-valued:
//
//	timeline.WithVisuals(func(v *timeline.Visuals) {
//	    v.LaneHeight = 30
//	    v.IntensityColormap = styletokens.SequentialMagma
//	})
//
// Validation: nil modify is treated as a no-op (the default visuals
// remain in place).
func WithVisuals(modify func(v *Visuals)) Option {
	return func(inst *Timeline) {
		if modify == nil {
			return
		}
		modify(&inst.visuals)
	}
}

// WithPointEvents attaches point events (commits, alerts, log entries) to
// the timeline. Rendered above the lane bars as a rug strip — raw vertical
// marks below WithRawPointThreshold visible items, density-binned rects
// above.
//
// Validation: nil clears (no rug strip rendered).
func WithPointEvents(points []*layout.PointEvent) Option {
	return func(inst *Timeline) {
		inst.points = points
	}
}

// WithRawPointThreshold sets the cutoff between raw and density rug-strip
// rendering. Default 500: rendering up to 500 individual vertical lines per
// frame stays well inside an immediate-mode budget; above that, density
// bins are faster and visually less noisy.
//
// Validation: panic on n < 0.
func WithRawPointThreshold(n int32) Option {
	if n < 0 {
		panic(fmt.Sprintf("timeline: WithRawPointThreshold requires n >= 0 (got %d)", n))
	}
	return func(inst *Timeline) {
		inst.rawPointThreshold = n
	}
}

// WithLODScales overrides the LOD bin ladder. Scales must be strictly
// ascending durations; the default ladder ([1ms,10ms,…,1w]) covers most
// log-style and ops-style timelines.
//
// Validation: panic on empty slice (here); BuildLODIndex panics again on
// non-ascending scales when rebuildLOD runs from New.
func WithLODScales(scales []time.Duration) Option {
	if len(scales) == 0 {
		panic("timeline: WithLODScales requires at least one scale")
	}
	return func(inst *Timeline) {
		inst.lodScales = scales
	}
}


// New constructs a Timeline that paints the given intervals. ids and
// scopeKey are required (panic on nil / empty); intervals may be nil for
// an empty-state widget that still renders the axis.
func New(ids *c.WidgetIdStack, scopeKey string, intervals []*layout.IntervalEvent, opts ...Option) (inst *Timeline) {
	if ids == nil {
		panic("timeline: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("timeline: New requires a non-empty scopeKey")
	}
	inst = &Timeline{
		ids:                ids,
		scopeKey:           scopeKey,
		intervals:          intervals,
		lodScales:          defaultLODScales,
		rawPointThreshold:  defaultRawPointThreshold,
		containerW:         defaultContainerW,
		interactionEnabled: true,
		intensityEncoded:   true,
		visuals:            DefaultVisuals(),
	}
	for _, opt := range opts {
		opt(inst)
	}
	inst.rebuildLOD()
	return
}

// rugReserved returns true when the renderer must reserve vertical space
// for the rug strip — i.e. the caller attached points AND non-zero
// rugStripH. Suppressing one or the other lets callers without point data
// keep the legacy intervals-only layout.
func (inst *Timeline) rugReserved() (yes bool) {
	yes = len(inst.points) > 0 && inst.visuals.RugStripH > 0
	return
}

// annotationReserved returns true when the renderer must reserve vertical
// space at the top for the annotation flag band. Zero-annotation timelines
// keep their previous layout.
func (inst *Timeline) annotationReserved() (yes bool) {
	yes = len(inst.annotations) > 0
	return
}

// SetIntervals replaces the intervals shown by this timeline. Safe to call
// between frames; the next Render reflects the new data. Pass nil to clear.
// Any existing interval selection is cleared because the previously-held
// *IntervalEvent pointer may no longer appear in the new slice. Any
// user-driven viewport pin is dropped so new data auto-fits — callers
// who want their pinned range preserved across data swaps should re-pin
// via SetRange after.
func (inst *Timeline) SetIntervals(intervals []*layout.IntervalEvent) {
	inst.intervals = intervals
	if inst.selection.Kind == SelectionInterval {
		inst.selection = SelectionInfo{}
	}
	inst.dropInteractivePin()
}

// SetPoints replaces the point events shown by this timeline and rebuilds
// the LOD index. Safe to call between frames; pass nil to clear. Any
// existing bucket selection is cleared because the LOD index is rebuilt
// from scratch and bucket identity (StartMS) may not survive.
func (inst *Timeline) SetPoints(points []*layout.PointEvent) {
	inst.points = points
	inst.rebuildLOD()
	if inst.selection.Kind == SelectionBucket {
		inst.selection = SelectionInfo{}
	}
	inst.dropInteractivePin()
}

// SetNowLine toggles the vertical "now" line at runtime — runtime
// counterpart to [WithNowLine]. Use this when the caller exposes a
// user-facing toggle (toolbar checkbox, settings panel) so the flip
// preserves pan/zoom state instead of recreating the widget. Cheap
// flag flip; no animation, no selection mutation.
//
// Validation: none — both true and false are valid.
func (inst *Timeline) SetNowLine(enabled bool) {
	inst.nowLineEnabled = enabled
}

// SetIntensityEncoding toggles intensity-driven fills at runtime — runtime
// counterpart to [WithIntensityEncoding]. Use this when the caller's data
// shape varies between frames (e.g. a SQL playground re-resolving its column
// contract per query): flip it off when the new result has no intensity
// column so bars/marks fall back to the flat Visuals.IntervalColor /
// Visuals.PointColor instead of collapsing to the colormap's dark end. Cheap
// flag flip; no selection mutation, no LOD rebuild.
//
// Validation: none — both true and false are valid.
func (inst *Timeline) SetIntensityEncoding(enabled bool) {
	inst.intensityEncoded = enabled
}

// SetAnnotations replaces the annotations shown by this timeline. Safe
// to call between frames; pass nil to clear. Any existing annotation
// selection is cleared because the previously-held *Annotation pointer
// may no longer appear in the new slice — callers wanting to preserve
// a selection across SetAnnotations should re-resolve via
// SelectAnnotationByNumber afterward.
func (inst *Timeline) SetAnnotations(annotations []*layout.Annotation) {
	inst.annotations = annotations
	if inst.selection.Kind == SelectionAnnotation {
		inst.selection = SelectionInfo{}
	}
	inst.dropInteractivePin()
}

// SelectAnnotationByNumber programmatically selects the annotation with
// the given Number. Used by sibling widgets to drive the same selection
// state — e.g. a detail panel that says "show details for annotation #3"
// calls SelectAnnotationByNumber(3) on the timeline. No-op when no
// annotation with that Number exists; never panics.
func (inst *Timeline) SelectAnnotationByNumber(number int32) {
	for _, a := range inst.annotations {
		if a == nil {
			continue
		}
		if a.Number == number {
			inst.selection = SelectionInfo{Kind: SelectionAnnotation, Annotation: a}
			return
		}
	}
}

// SelectIntervalByPointer programmatically selects the given interval
// event. Symmetric with SelectAnnotationByNumber for the cross-widget-
// linking pattern; a sibling widget holding the same *IntervalEvent
// (passed via WithIntervals / SetIntervals) drives the timeline's
// selection by calling this method. No-op when ev is nil or not in the
// current intervals slice; never panics.
func (inst *Timeline) SelectIntervalByPointer(ev *layout.IntervalEvent) {
	if ev == nil {
		return
	}
	if slices.Contains(inst.intervals, ev) {
		inst.selection = SelectionInfo{Kind: SelectionInterval, Interval: ev}
	}
}

// SelectBucketAt programmatically selects the LOD bucket containing tMS
// at the scale of the most recent Render. Symmetric with
// SelectAnnotationByNumber / SelectIntervalByPointer for the cross-
// widget-linking pattern; a sibling histogram or table drives the
// timeline's rug selection by calling this method with the relevant
// epoch-ms.
//
// No-op when:
//   - No render has happened yet (the last-frame view snapshot is empty).
//   - The LOD index is empty (no points attached via WithPointEvents).
//   - tMS falls in a bucket that holds zero events at the picked scale.
//
// Selection resolution uses the SAME view + pxWidth the last Render saw,
// which is also what the user's current screen shows — the picked bucket
// is what would have been hit had the user clicked at tMS on the rug.
func (inst *Timeline) SelectBucketAt(tMS int64) {
	if inst.lodIndex == nil || inst.lastViewPxWidth == 0 {
		return
	}
	b, _, ok := inst.lodIndex.BucketAt(tMS, inst.lastViewMinMS, inst.lastViewMaxMS, inst.lastViewPxWidth)
	if !ok {
		return
	}
	bucket := b
	inst.selection = SelectionInfo{Kind: SelectionBucket, Bucket: &bucket}
}

// Selection returns the current click-driven selection. Kind ==
// SelectionNone before the first click and after any click-miss or
// click-same-to-clear gesture.
func (inst *Timeline) Selection() (sel SelectionInfo) {
	sel = inst.selection
	return
}

// ClearSelection unsets any current selection, equivalent to a click in
// empty canvas space. Use when the host UI provides its own "clear"
// affordance.
func (inst *Timeline) ClearSelection() {
	inst.selection = SelectionInfo{}
}

// CursorTime returns the calendar time under the cursor for the most
// recent Render call, or zero + false when the cursor is not over the
// data area (off canvas, over the label band, before first render, or
// interaction is disabled). Hosts can use this to render a live readout
// — e.g. paired with humanize.Time for a relative-time label.
func (inst *Timeline) CursorTime() (t time.Time, ok bool) {
	if !inst.cursorTimeValid {
		return
	}
	t = time.UnixMilli(inst.cursorTimeMS).UTC()
	ok = true
	return
}

// rebuildLOD rebuilds the LOD index from the current points + scales. Called
// from New after options are applied and from SetPoints. Cheap when points
// is empty (allocates only the empty scale maps).
func (inst *Timeline) rebuildLOD() {
	inst.lodIndex = layout.BuildLODIndex(asPointValues(inst.points), inst.lodScales)
}

// asPointValues unwraps a slice of *PointEvent pointers into a slice of
// PointEvent values for BuildLODIndex (which takes values for clean
// concurrent-read semantics). Nil entries are dropped.
func asPointValues(points []*layout.PointEvent) (out []layout.PointEvent) {
	if len(points) == 0 {
		return
	}
	out = make([]layout.PointEvent, 0, len(points))
	for _, p := range points {
		if p == nil {
			continue
		}
		out = append(out, *p)
	}
	return
}

// SetRange forces a viewport range, equivalent to constructing with
// WithRange(t0,t1). Pass an empty time pair to revert to auto-fit. The
// resulting pin is treated as caller-driven (not interactive) so it
// survives subsequent SetIntervals / SetPoints / SetAnnotations calls.
func (inst *Timeline) SetRange(t0, t1 time.Time) {
	if t0.IsZero() && t1.IsZero() {
		inst.explicitRange = false
		inst.interactivePin = false
		return
	}
	if !t1.After(t0) {
		panic(fmt.Sprintf("timeline: SetRange requires t1 after t0 (got %v, %v)", t0, t1))
	}
	inst.explicitRange = true
	inst.interactivePin = false
	inst.viewMinMS = t0.UnixMilli()
	inst.viewMaxMS = t1.UnixMilli()
}

// computeVerticalLayout is the single source of truth for the timeline's
// per-frame Y geometry + horizontal axis bounds. Called once at the top
// of renderBody and passed to every paint* / hitTest* method downstream.
// Depends on inst.laneAssn being populated and the caller-supplied
// labelW + effW + rolloverRows int. The canvas height is content-driven
// (sum of annotation band + rug + lanes + axis + rollover rows); the
// widget no longer pads to fill any available vertical space the parent
// happens to offer, so a 200-px-of-content timeline placed inside a
// 600-px-tall panel occupies 200 px and lets siblings render directly
// below it instead of staring at empty canvas.
func (inst *Timeline) computeVerticalLayout(rolloverRows int, labelW, effW float32) (vl verticalLayout) {
	vl.axisStartPx = labelW
	vl.axisEndPx = effW
	if inst.annotationReserved() {
		vl.topReserved = inst.visuals.AnnotationBandH + annotationGap
	}
	vl.rugTopY = vl.topReserved
	vl.laneBaseY = vl.topReserved
	if inst.rugReserved() {
		vl.laneBaseY = vl.topReserved + inst.visuals.RugStripH + inst.visuals.RugGap
	}
	laneCount := inst.laneAssn.LaneCount()
	if laneCount > 0 {
		vl.laneAreaH = float32(laneCount)*(inst.visuals.LaneHeight+inst.visuals.LaneGap) - inst.visuals.LaneGap
	} else {
		vl.laneAreaH = inst.visuals.LaneHeight
	}
	vl.axisBaselineY = vl.laneBaseY + vl.laneAreaH + 1
	vl.rolloverStartY = vl.axisBaselineY + inst.visuals.AxisHeight
	vl.totalH = vl.rolloverStartY + float32(rolloverRows)*inst.visuals.RolloverRowH
	return
}

// LaneCount returns the number of lanes assigned by the most recent Render
// — i.e. a snapshot of last frame's PackLanes output, not the count of the
// currently-attached IntervalEvents. Returns 0 before the first Render.
// After SetIntervals (or any data swap) the value remains the *previous*
// frame's count until the next Render reruns the packer.
func (inst *Timeline) LaneCount() (n int32) {
	n = inst.laneAssn.LaneCount()
	return
}

// Render paints the timeline. Call once per frame inside an active egui
// surface (panel or window).
func (inst *Timeline) Render() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderBody()
	}
}

func (inst *Timeline) renderBody() {
	stateMgr := c.CurrentApplicationState.StateManager
	cp := stateMgr.GetCanvasPointer()
	zoom := stateMgr.GetZoomDelta()
	avail := stateMgr.GetAvailableSize()

	effW := inst.effectiveContainerW(avail)
	if inst.interactionEnabled {
		c.CaptureAvailableSize()
		inst.applyZoomInput(zoom, cp, effW)
	}

	inst.laneAssn = layout.PackLanes(inst.intervals)
	labelW := inst.computeLabelW()

	viewMin, viewMax := inst.computeViewRange()
	viewMinMS := viewMin.UnixMilli()
	viewMaxMS := viewMax.UnixMilli()

	tm := layout.ComputeTickMap(viewMin, viewMax, float64(labelW), float64(effW), nil, timeticks.TimeStep{})
	vl := inst.computeVerticalLayout(len(tm.RolloverRows), labelW, effW)

	inst.paintBackgroundBands(tm, vl, viewMinMS, viewMaxMS)
	if inst.rugReserved() {
		inst.paintRug(tm, vl, viewMinMS, viewMaxMS)
	}
	inst.paintLanes(tm, vl)
	inst.paintLaneLabels(vl)
	inst.paintAxis(tm, vl)
	inst.paintRolloverRows(tm, vl)
	inst.paintAnnotations(tm, vl)
	inst.paintNowLine(tm, vl, viewMinMS, viewMaxMS)

	if inst.interactionEnabled && cursorInsideCanvas(cp, effW, vl.totalH) && cp.HoverX >= vl.axisStartPx {
		inst.cursorTimeMS = tm.MapXToMS(float64(cp.HoverX))
		inst.cursorTimeValid = true
		c.PaintLine(cp.HoverX, vl.topReserved, cp.HoverX, vl.axisBaselineY, inst.visuals.AxisColor, crosshairWidthPx).Send()
		inst.paintHoverTooltip(tm, vl, cp.HoverX, cp.HoverY)
		if cp.Clicked {
			inst.dispatchClick(tm, vl, cp.HoverX, cp.HoverY)
		}
	} else {
		inst.cursorTimeValid = false
	}

	canvas := c.PaintCanvas(inst.ids.PrepareStr("canvas"), effW, vl.totalH).
		Background(inst.visuals.BgColor)
	if inst.interactionEnabled {
		canvas = canvas.Sense(true, false, true)
	}
	canvas.Send()

	inst.lastViewMinMS = viewMinMS
	inst.lastViewMaxMS = viewMaxMS
	inst.lastViewPxWidth = int32(vl.axisEndPx - vl.axisStartPx)
}

// dispatchClick updates the selection state and fires the registered
// click callbacks for a primary-click over the canvas. Annotation hits
// take precedence over lane-bar hits, which take precedence over rug-strip
// hits — mirroring the visual z-order (annotations on top). Off-target
// clicks clear the selection. Clicking the already-selected target toggles
// the selection off. cp.Clicked is edge-triggered by egui (true only on
// the frame the release lands), so callers don't need to debounce.
func (inst *Timeline) dispatchClick(tm layout.TickMap, vl verticalLayout, cursorX, cursorY float32) {
	if a := inst.hitTestAnnotation(tm, cursorX); a != nil {
		if inst.selection.Kind == SelectionAnnotation && inst.selection.Annotation == a {
			inst.selection = SelectionInfo{}
		} else {
			inst.selection = SelectionInfo{Kind: SelectionAnnotation, Annotation: a}
		}
		inst.fireSelectionListener()
		return
	}
	if ev, _ := inst.hitTestInterval(tm, vl, cursorX, cursorY); ev != nil {
		if inst.selection.Kind == SelectionInterval && inst.selection.Interval == ev {
			inst.selection = SelectionInfo{}
		} else {
			inst.selection = SelectionInfo{Kind: SelectionInterval, Interval: ev}
		}
		inst.fireSelectionListener()
		return
	}
	if inst.rugReserved() && cursorY >= vl.rugTopY && cursorY <= vl.rugTopY+inst.visuals.RugStripH {
		if b, _, ok := inst.hitTestRugBucket(tm, cursorX); ok {
			if inst.selection.Kind == SelectionBucket && inst.selection.Bucket != nil && inst.selection.Bucket.StartMS == b.StartMS {
				inst.selection = SelectionInfo{}
			} else {
				bucket := b
				inst.selection = SelectionInfo{Kind: SelectionBucket, Bucket: &bucket}
			}
			inst.fireSelectionListener()
			return
		}
	}
	inst.selection = SelectionInfo{}
	inst.fireSelectionListener()
}

func (inst *Timeline) fireSelectionListener() {
	if inst.onSelection != nil {
		inst.onSelection(inst.selection)
	}
}

// hitTestBackgroundBand returns the first band (in producer-yield order)
// whose half-open [FromMS, ToMS) range contains the cursor's time, plus
// ok=true. Bands span the full data height so no Y check is needed.
// Used as the lowest-priority tooltip tier: annotation > interval > rug
// bucket > band — bands are a "context" tooltip, not an event one.
func (inst *Timeline) hitTestBackgroundBand(tm layout.TickMap, cursorX float32) (band layout.BackgroundBand, ok bool) {
	if inst.backgroundBands == nil {
		return
	}
	hoverMS := tm.MapXToMS(float64(cursorX))
	viewMinMS := tm.ViewMin.UnixMilli()
	viewMaxMS := tm.ViewMax.UnixMilli()
	for b := range inst.backgroundBands(viewMinMS, viewMaxMS) {
		if hoverMS >= b.FromMS && hoverMS < b.ToMS {
			band = b
			ok = true
			return
		}
	}
	return
}

// hitTestRugBucket returns the visible LOD bucket whose half-open
// time range contains the cursor's x coordinate (mapped to epoch-ms
// via the tick map), plus the picked scale-ms. Shared by the hover
// tooltip and the click dispatcher so both agree on what the rug strip
// exposes. Single-map-lookup via LODIndex.BucketAt — no per-call
// allocation of the visible-buckets slice.
func (inst *Timeline) hitTestRugBucket(tm layout.TickMap, cursorX float32) (bucket layout.Bucket, scaleMS int64, ok bool) {
	if inst.lodIndex == nil {
		return
	}
	axisWidth := tm.AxisEndPx - tm.AxisStartPx
	if axisWidth <= 0 {
		return
	}
	pxWidth := int32(axisWidth)
	hoverMS := tm.MapXToMS(float64(cursorX))
	bucket, scaleMS, ok = inst.lodIndex.BucketAt(hoverMS, tm.ViewMin.UnixMilli(), tm.ViewMax.UnixMilli(), pxWidth)
	return
}

// computeLabelW returns the pixel width to reserve on the left edge for
// lane labels — sized to the longest non-empty LaneHint. Returns 0 when
// no lane has a hint, so the time axis still extends edge-to-edge in the
// hintless case.
func (inst *Timeline) computeLabelW() (w float32) {
	var widest int32
	for _, lane := range inst.laneAssn.Lanes {
		if lane.Hint == "" {
			continue
		}
		if n := int32(len(lane.Hint)); n > widest {
			widest = n
		}
	}
	if widest == 0 {
		return
	}
	w = float32(widest)*tooltipCharWidthPx + 2*labelPaddingX
	w = min(w, labelBandMaxW)
	return
}

// paintLaneLabels renders each lane's LaneHint in the left-edge band.
// Lanes without a hint contribute no text; the band itself is not painted
// (transparent over canvas background). Text is anchored left + middle.
func (inst *Timeline) paintLaneLabels(vl verticalLayout) {
	if vl.axisStartPx <= 0 {
		return
	}
	for laneIdx, lane := range inst.laneAssn.Lanes {
		if lane.Hint == "" {
			continue
		}
		y := vl.laneBaseY + float32(laneIdx)*(inst.visuals.LaneHeight+inst.visuals.LaneGap) + inst.visuals.LaneHeight/2
		c.PaintText(labelPaddingX, y, anchorLeft, anchorCenter, lane.Hint, tickLabelFontSize, inst.visuals.TickLabelColor).Send()
	}
}

// effectiveContainerW returns the parent's available width from the
// previous frame's captureAvailableSize. Falls back to containerW when
// no capture has landed yet (first frame after construction, or capture
// ran outside a Ui). WithContainerWidth sets the fallback, not a pin —
// auto-fit always wins when a valid capture is available.
func (inst *Timeline) effectiveContainerW(avail c.AvailableSizeValue) (w float32) {
	w = inst.containerW
	if !math.IsNaN(float64(avail.W)) && avail.W > 0 {
		w = avail.W
	}
	return
}

// applyZoomInput mutates the viewport from egui's combined zoom gesture
// detection (Ctrl+scroll, touchpad pinch, +/- keyboard). Zoom is anchored
// at the cursor X so the time under the cursor stays fixed across the
// gesture. zoom.Zoom is a multiplicative factor: 1.0 = no change,
// >1.0 = zoom in (smaller span), <1.0 = zoom out (larger span).
func (inst *Timeline) applyZoomInput(zoom c.ZoomDeltaValue, cp c.CanvasPointerValue, effW float32) {
	if zoom.Zoom == 1.0 || zoom.Zoom <= 0 {
		return
	}
	if math.IsNaN(float64(cp.HoverX)) || effW <= 0 {
		return
	}
	if !inst.pinToCurrentView() {
		return
	}
	spanMS := inst.viewMaxMS - inst.viewMinMS
	if spanMS <= 0 {
		return
	}
	anchorFrac := clamp01(cp.HoverX / effW)
	anchorMS := inst.viewMinMS + int64(float64(anchorFrac)*float64(spanMS))
	// zoom > 1 → smaller span; zoom < 1 → larger span. Invert + clamp.
	mul := clamp01ToRange(1.0/zoom.Zoom, minZoomMul, maxZoomMul)
	newSpan := max(int64(float64(spanMS)*float64(mul)), 1)
	inst.viewMinMS = anchorMS - int64(float64(anchorFrac)*float64(newSpan))
	inst.viewMaxMS = inst.viewMinMS + newSpan
}

// pinToCurrentView materializes the auto-fit viewport into explicit
// viewMinMS/MaxMS on the first user interaction so subsequent pans/zooms
// build on the same baseline the user saw, not on a moving auto-fit
// target that data updates would shift underneath them. The interactivePin
// flag distinguishes user-driven pins from caller-driven WithRange/SetRange
// pins so SetIntervals/Points/Annotations can revert to auto-fit on data
// swap (user pan over stale data would silently hide events otherwise).
//
// Returns false when there is no pin-able state (auto-fit range is
// degenerate — empty data or single-instant collapse) without mutating
// anything. Callers should bail rather than operate on a half-pinned
// state.
func (inst *Timeline) pinToCurrentView() (ok bool) {
	if inst.explicitRange {
		ok = true
		return
	}
	viewMin, viewMax := inst.computeViewRange()
	if !viewMax.After(viewMin) {
		return
	}
	inst.viewMinMS = viewMin.UnixMilli()
	inst.viewMaxMS = viewMax.UnixMilli()
	inst.explicitRange = true
	inst.interactivePin = true
	ok = true
	return
}

// dropInteractivePin reverts to auto-fit if the current pin came from
// user interaction (pan/zoom). Caller-driven pins (WithRange / SetRange)
// are preserved on the assumption the caller meant the absolute window.
// Called from SetIntervals / SetPoints / SetAnnotations.
func (inst *Timeline) dropInteractivePin() {
	if inst.interactivePin {
		inst.explicitRange = false
		inst.interactivePin = false
	}
}

// cursorInsideCanvas reports whether the cached pointer position lies
// within the current canvas bounds. NaN coordinates (canvas not hovered
// last frame) read as outside.
func cursorInsideCanvas(cp c.CanvasPointerValue, effW, totalH float32) (yes bool) {
	if math.IsNaN(float64(cp.HoverX)) || math.IsNaN(float64(cp.HoverY)) {
		return
	}
	yes = cp.HoverX >= 0 && cp.HoverX <= effW && cp.HoverY >= 0 && cp.HoverY <= totalH
	return
}

func clamp01ToRange(v, lo, hi float32) (out float32) {
	switch {
	case v < lo:
		out = lo
	case v > hi:
		out = hi
	default:
		out = v
	}
	return
}

func (inst *Timeline) paintLanes(tm layout.TickMap, vl verticalLayout) {
	for laneIdx, lane := range inst.laneAssn.Lanes {
		y0 := vl.laneBaseY + float32(laneIdx)*(inst.visuals.LaneHeight+inst.visuals.LaneGap)
		y1 := y0 + inst.visuals.LaneHeight
		for _, ev := range lane.Items {
			x0, x1, ok := vl.clipToAxis(float32(tm.MapMSToX(ev.FromMS)), float32(tm.MapMSToX(ev.ToMS)))
			if !ok {
				continue
			}
			if x1-x0 < inst.visuals.BarMinPx {
				x1 = min(x0+inst.visuals.BarMinPx, vl.axisEndPx)
			}
			fill := inst.visuals.IntervalColor
			if inst.intensityEncoded {
				rgba := styletokens.Sequential(inst.visuals.IntensityColormap, clamp01(ev.Intensity))
				fill = color.Hex(rgba.AsHex())
			}
			c.PaintRectFilled(x0, y0, x1, y1, inst.visuals.CornerRadius, fill).Send()
			if inst.selection.Kind == SelectionInterval && inst.selection.Interval == ev {
				c.PaintRectStroke(x0, y0, x1, y1, inst.visuals.CornerRadius, inst.visuals.SelectionStrokeColor, selectionStrokeWidthPx).Send()
			}
		}
	}
}

func (inst *Timeline) paintAxis(tm layout.TickMap, vl verticalLayout) {
	c.PaintLine(vl.axisStartPx, vl.axisBaselineY, vl.axisEndPx, vl.axisBaselineY, inst.visuals.AxisColor, 1.0).Send()
	for _, tick := range tm.Ticks {
		x := float32(tick.X)
		c.PaintLine(x, vl.axisBaselineY, x, vl.axisBaselineY+tickMarkHeight, inst.visuals.TickMarkColor, 1.0).Send()
		c.PaintText(x, vl.axisBaselineY+tickLabelOffsetY, anchorCenter, anchorTop,
			tick.Label, tickLabelFontSize, inst.visuals.TickLabelColor).Send()
	}
}

func (inst *Timeline) paintRolloverRows(tm layout.TickMap, vl verticalLayout) {
	for rowIdx, row := range tm.RolloverRows {
		rowY := vl.rolloverStartY + float32(rowIdx)*inst.visuals.RolloverRowH
		for _, run := range row.Runs {
			cx := float32(run.StartX + (run.EndX-run.StartX)/2)
			c.PaintText(cx, rowY, anchorCenter, anchorTop, run.Label, rolloverLabelFontSize, inst.visuals.RolloverColor).Send()
		}
	}
}

// paintBackgroundBands iterates the registered BackgroundBandProducer
// (if any) over the current view range and paints each yielded band as
// a filled rect spanning from vl.topReserved to vl.axisBaselineY. Off-
// axis bands are clipped silently via vl.clipToAxis. Drawn FIRST so
// every other glyph layers on top; producers are expected to choose
// low-alpha colours so the foreground stays legible.
func (inst *Timeline) paintBackgroundBands(tm layout.TickMap, vl verticalLayout, viewMinMS, viewMaxMS int64) {
	if inst.backgroundBands == nil {
		return
	}
	for band := range inst.backgroundBands(viewMinMS, viewMaxMS) {
		x0, x1, ok := vl.clipToAxis(float32(tm.MapMSToX(band.FromMS)), float32(tm.MapMSToX(band.ToMS)))
		if !ok {
			continue
		}
		c.PaintRectFilled(x0, vl.topReserved, x1, vl.axisBaselineY, 0, color.Hex(band.Color)).Send()
	}
}

// paintNowLine paints a vertical line at the current wall-clock moment
// when WithNowLine(true) is set AND time.Now() falls inside the view.
// Skipped silently for historical / future-only views — no "off-screen
// now" indicator is rendered, by design (Grafana convention).
func (inst *Timeline) paintNowLine(tm layout.TickMap, vl verticalLayout, viewMinMS, viewMaxMS int64) {
	if !inst.nowLineEnabled {
		return
	}
	nowMS := time.Now().UnixMilli()
	if nowMS < viewMinMS || nowMS > viewMaxMS {
		return
	}
	x := float32(tm.MapMSToX(nowMS))
	if x < vl.axisStartPx || x > vl.axisEndPx {
		return
	}
	c.PaintLine(x, vl.topReserved, x, vl.axisBaselineY, inst.visuals.NowLineColor, nowLineWidthPx).Send()
}

// paintAnnotations renders the Grafana-style annotation markers: one
// dashed vertical line per annotation spanning from below the flag band
// to vl.axisBaselineY, plus a numbered flag rect at the top tinted by
// the annotation's PaletteIdx. The selection stroke wraps the flag when
// the annotation is currently selected. Drawn after lanes + axis so the
// flag and dashes layer over the data, matching Grafana convention.
func (inst *Timeline) paintAnnotations(tm layout.TickMap, vl verticalLayout) {
	for _, a := range inst.annotations {
		if a == nil {
			continue
		}
		x := float32(tm.MapMSToX(a.TMS))
		if x < vl.axisStartPx || x > vl.axisEndPx {
			continue
		}
		rgba := styletokens.QualitativeCycle(int(a.PaletteIdx))
		col := color.Hex(rgba.AsHex())

		// The flag is positioned with annotationSelectionInsetPx of clearance
		// above (so the selection back-fill can paint a visible top margin
		// without clipping against the canvas at y=0) AND below (where
		// annotationFlagPaddingY already gives the same clearance against
		// the dashed-line start). AnnotationBandH = FlagH + 2 × inset.
		flagY0 := annotationSelectionInsetPx
		flagY1 := flagY0 + inst.visuals.AnnotationFlagH

		c.PaintDashedLine(x, flagY1+annotationFlagPaddingY, x, vl.axisBaselineY,
			annotationDashLen, annotationGapLen, col, annotationStrokeW).Send()

		flagX0 := x - inst.visuals.AnnotationFlagW/2
		flagX1 := x + inst.visuals.AnnotationFlagW/2
		if inst.selection.Kind == SelectionAnnotation && inst.selection.Annotation == a {
			// Selection "stroke" via a back-fill rect underneath the flag.
			// PaintRectStroke would paint with StrokeKind::Outside which
			// clips against the canvas top edge. Two-fill emulation: paint
			// a slightly larger rect in the stroke colour first, then the
			// flag fill on top — the visible margin reads as the outline.
			c.PaintRectFilled(flagX0-annotationSelectionInsetPx, 0,
				flagX1+annotationSelectionInsetPx, flagY1+annotationSelectionInsetPx,
				annotationFlagCornerR, inst.visuals.SelectionStrokeColor).Send()
		}
		c.PaintRectFilled(flagX0, flagY0, flagX1, flagY1, annotationFlagCornerR, col).Send()
		c.PaintText(x, (flagY0+flagY1)/2, anchorCenter, anchorCenter,
			fmt.Sprintf("%d", a.Number), annotationFlagFontSize, inst.visuals.AnnotationFgColor).Send()
	}
}

// hitTestInterval returns the lane bar under (cursorX, cursorY) and the
// owning lane's Hint, or nil + "" for a miss. Shared by dispatchClick and
// hitTestTooltipText so the two paths can't drift — click and tooltip
// always agree on what's under the cursor.
func (inst *Timeline) hitTestInterval(tm layout.TickMap, vl verticalLayout, cursorX, cursorY float32) (ev *layout.IntervalEvent, hint string) {
	for laneIdx, lane := range inst.laneAssn.Lanes {
		y0 := vl.laneBaseY + float32(laneIdx)*(inst.visuals.LaneHeight+inst.visuals.LaneGap)
		y1 := y0 + inst.visuals.LaneHeight
		if cursorY < y0 || cursorY > y1 {
			continue
		}
		for _, item := range lane.Items {
			x0 := float32(tm.MapMSToX(item.FromMS))
			x1 := float32(tm.MapMSToX(item.ToMS))
			if cursorX >= x0 && cursorX <= x1 {
				ev = item
				hint = lane.Hint
				return
			}
		}
	}
	return
}

// hitTestAnnotation returns the annotation whose dashed-line / flag is
// closest to cursorX within annotationHitCorridorPx, or nil. Used by
// both dispatchClick (precedence over interval/bucket hits) and the
// hover tooltip path.
func (inst *Timeline) hitTestAnnotation(tm layout.TickMap, cursorX float32) (a *layout.Annotation) {
	nearestDist := annotationHitCorridorPx
	for _, candidate := range inst.annotations {
		if candidate == nil {
			continue
		}
		x := float32(tm.MapMSToX(candidate.TMS))
		dist := cursorX - x
		if dist < 0 {
			dist = -dist
		}
		if dist < nearestDist {
			nearestDist = dist
			a = candidate
		}
	}
	return
}

// paintRug renders the top point-event band. Chooses raw vs density mode
// from the visible event count at the LOD index's picked scale.
func (inst *Timeline) paintRug(tm layout.TickMap, vl verticalLayout, viewMinMS, viewMaxMS int64) {
	if inst.lodIndex == nil {
		return
	}
	pxWidth := int32(vl.axisEndPx - vl.axisStartPx)
	if pxWidth <= 0 {
		return
	}
	rugY1 := vl.rugTopY + inst.visuals.RugStripH
	buckets := inst.lodIndex.BucketsForRange(viewMinMS, viewMaxMS, pxWidth)
	var totalVisible int32
	var maxCount int32
	for _, b := range buckets {
		totalVisible += b.Count
		maxCount = max(maxCount, b.Count)
	}
	if totalVisible == 0 {
		return
	}
	if totalVisible <= inst.rawPointThreshold {
		inst.paintRugRaw(tm, vl, vl.rugTopY, rugY1, viewMinMS, viewMaxMS)
		return
	}
	scaleMS := inst.lodIndex.ScaleMSForRange(viewMinMS, viewMaxMS, pxWidth)
	inst.paintRugDensity(tm, vl, vl.rugTopY, rugY1, buckets, scaleMS, maxCount)
}

// paintRugRaw draws one vertical mark per visible PointEvent. Tinted by
// per-point Intensity via the rug colormap; off-screen events are skipped.
func (inst *Timeline) paintRugRaw(tm layout.TickMap, vl verticalLayout, rugY0, rugY1 float32, viewMinMS, viewMaxMS int64) {
	for _, p := range inst.points {
		if p == nil {
			continue
		}
		if p.TMS < viewMinMS || p.TMS > viewMaxMS {
			continue
		}
		x := float32(tm.MapMSToX(p.TMS))
		if x < vl.axisStartPx || x > vl.axisEndPx {
			continue
		}
		mark := inst.visuals.PointColor
		if inst.intensityEncoded {
			rgba := styletokens.Sequential(inst.visuals.RugColormap, clamp01(p.Intensity))
			mark = color.Hex(rgba.AsHex())
		}
		c.PaintLine(x, rugY0, x, rugY1, mark, rugMarkWidthPx).Send()
	}
}

// paintRugDensity draws one rect per visible LOD bucket. Tinted by
// (bucket.Count / maxCount) via the rug colormap so the visually-densest
// region in the current view always saturates at the palette's hot end.
func (inst *Timeline) paintRugDensity(tm layout.TickMap, vl verticalLayout, rugY0, rugY1 float32, buckets []layout.Bucket, scaleMS int64, maxCount int32) {
	if scaleMS <= 0 || maxCount <= 0 {
		return
	}
	inv := 1.0 / float32(maxCount)
	for _, b := range buckets {
		x0, x1, ok := vl.clipToAxis(float32(tm.MapMSToX(b.StartMS)), float32(tm.MapMSToX(b.StartMS+scaleMS)))
		if !ok {
			continue
		}
		if x1-x0 < rugDensityMinPx {
			x1 = min(x0+rugDensityMinPx, vl.axisEndPx)
		}
		tint := clamp01(float32(b.Count) * inv)
		tint = rugDensityMinTint + tint*(1-rugDensityMinTint)
		rgba := styletokens.Sequential(inst.visuals.RugColormap, tint)
		c.PaintRectFilled(x0, rugY0, x1, rugY1, 0, color.Hex(rgba.AsHex())).Send()
		if inst.selection.Kind == SelectionBucket && inst.selection.Bucket != nil && inst.selection.Bucket.StartMS == b.StartMS {
			c.PaintRectStroke(x0, rugY0, x1, rugY1, 0, inst.visuals.SelectionStrokeColor, selectionStrokeWidthPx).Send()
		}
	}
}

// paintHoverTooltip hit-tests the cached cursor against interval bars and
// the rug strip, emitting a tooltip Frame above the data when a hit lands.
// Tooltips for individual intervals show LaneHint + UTC time range +
// duration; rug-strip hits show either the single point (raw mode) or the
// bucket count + window (density mode). One-frame lag relative to actual
// cursor since both inputs come from last frame's StateManager cache.
func (inst *Timeline) paintHoverTooltip(tm layout.TickMap, vl verticalLayout, cursorX, cursorY float32) {
	text := inst.hitTestTooltipText(tm, vl, cursorX, cursorY)
	if text == "" {
		return
	}
	inst.paintTooltipBlock(text, cursorX, cursorY, vl.axisEndPx, vl.totalH)
}

func (inst *Timeline) hitTestTooltipText(tm layout.TickMap, vl verticalLayout, cursorX, cursorY float32) (text string) {
	if a := inst.hitTestAnnotation(tm, cursorX); a != nil {
		text = formatAnnotationTooltip(a)
		return
	}
	if ev, hint := inst.hitTestInterval(tm, vl, cursorX, cursorY); ev != nil {
		text = formatIntervalTooltip(ev, hint)
		return
	}
	if inst.rugReserved() && cursorY >= vl.rugTopY && cursorY <= vl.rugTopY+inst.visuals.RugStripH {
		if rug := inst.formatRugTooltip(tm, cursorX); rug != "" {
			text = rug
			return
		}
	}
	if band, ok := inst.hitTestBackgroundBand(tm, cursorX); ok && band.Label != "" {
		text = formatBandTooltip(band)
	}
	return
}

func formatBandTooltip(b layout.BackgroundBand) (text string) {
	from := time.UnixMilli(b.FromMS).UTC().Format("2006-01-02 15:04")
	to := time.UnixMilli(b.ToMS).UTC().Format("2006-01-02 15:04")
	text = fmt.Sprintf("%s\n%s – %s", b.Label, from, to)
	return
}

func formatAnnotationTooltip(a *layout.Annotation) (text string) {
	t := a.AsTime().Format("2006-01-02 15:04:05")
	if a.Label != "" {
		text = fmt.Sprintf("#%d  %s\n%s", a.Number, a.Label, t)
		return
	}
	text = fmt.Sprintf("#%d\n%s", a.Number, t)
	return
}

func formatIntervalTooltip(ev *layout.IntervalEvent, hint string) (text string) {
	from := ev.AsFromTime().Format("2006-01-02 15:04:05")
	to := ev.AsToTime().Format("15:04:05")
	dur := time.Duration(ev.DurationMS()) * time.Millisecond
	if hint != "" {
		text = fmt.Sprintf("%s\n%s – %s\n%s", hint, from, to, dur)
		return
	}
	text = fmt.Sprintf("%s – %s\n%s", from, to, dur)
	return
}

func (inst *Timeline) formatRugTooltip(tm layout.TickMap, cursorX float32) (text string) {
	b, scaleMS, ok := inst.hitTestRugBucket(tm, cursorX)
	if !ok {
		return
	}
	start := time.UnixMilli(b.StartMS).UTC().Format("2006-01-02 15:04:05")
	end := time.UnixMilli(b.StartMS + scaleMS).UTC().Format("15:04:05")
	text = fmt.Sprintf("%s – %s\n%d event(s)", start, end, b.Count)
	return
}

func (inst *Timeline) paintTooltipBlock(text string, cursorX, cursorY, effW, totalH float32) {
	lines := splitLines(text)
	if len(lines) == 0 {
		return
	}
	var widestChars int32
	for _, line := range lines {
		if n := int32(len(line)); n > widestChars {
			widestChars = n
		}
	}
	boxW := float32(widestChars)*tooltipCharWidthPx + 2*tooltipPaddingX
	boxH := float32(len(lines))*tooltipLineHeight + 2*tooltipPaddingY

	x0 := cursorX + tooltipCursorOffsetX
	y0 := cursorY + tooltipCursorOffsetY - boxH
	if x0+boxW > effW {
		x0 = cursorX - tooltipCursorOffsetX - boxW
	}
	x0 = max(x0, 0)
	x0 = min(x0, effW-boxW)
	y0 = max(y0, 0)
	y0 = min(y0, totalH-boxH)

	c.PaintRectFilled(x0, y0, x0+boxW, y0+boxH, 3, inst.visuals.TooltipBgColor).Send()
	for i, line := range lines {
		tx := x0 + tooltipPaddingX
		ty := y0 + tooltipPaddingY + float32(i)*tooltipLineHeight
		c.PaintText(tx, ty, anchorLeft, anchorTop, line, tooltipFontSize, inst.visuals.TooltipFgColor).Send()
	}
}

func splitLines(s string) (out []string) {
	if s == "" {
		return
	}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return
}

func (inst *Timeline) computeViewRange() (t0, t1 time.Time) {
	if inst.explicitRange {
		t0 = time.UnixMilli(inst.viewMinMS).UTC()
		t1 = time.UnixMilli(inst.viewMaxMS).UTC()
		return
	}
	minMS := int64(math.MaxInt64)
	maxMS := int64(math.MinInt64)
	for _, ev := range inst.intervals {
		if ev == nil {
			continue
		}
		minMS = min(minMS, ev.FromMS)
		maxMS = max(maxMS, ev.ToMS)
	}
	if minMS == int64(math.MaxInt64) {
		now := time.Now().UTC()
		t0 = now.Add(-emptyFallback)
		t1 = now
		return
	}
	span := float64(maxMS - minMS)
	if span <= 0 {
		span = float64(time.Second.Milliseconds())
	}
	padMS := max(int64(span*rangePaddingFraction), 1)
	t0 = time.UnixMilli(minMS - padMS).UTC()
	t1 = time.UnixMilli(maxMS + padMS).UTC()
	return
}

const (
	anchorLeft   uint8 = 0
	anchorCenter uint8 = 1
	anchorTop    uint8 = 0
)

func clamp01(v float32) (out float32) {
	if v != v {
		return
	}
	switch {
	case v <= 0:
		out = 0
	case v >= 1:
		out = 1
	default:
		out = v
	}
	return
}
