// Package heatmapscroll composes the colormap package with the
// scrollingTexture widget (ADR-0058) into a single, opinionated wrapper
// for "streaming scalar → colour heatmap" use cases: audio spectrograms,
// RF waterfalls, thermal streams, rolling metrics heatmaps. It is a
// semi-retained widget (ADR-0267): the object owns the ring's write cursor
// and the frame's staged columns, New takes its [Options], Render draws it
// once per frame and returns its [Events].
//
// # Ownership model
//
//   - The caller owns the data stream: HeatmapScroll does not pull
//     samples; it exposes PushColumn which the caller invokes with one
//     column of heightSlots float32 samples per "time step".
//   - The wrapper owns the ring-buffer write cursor (head) and the
//     per-frame staging buffer of freshly-mapped RGBA columns.
//   - The Rust widget owns the GPU TextureHandle (per ADR-0058 SD10,
//     texture storage is encapsulated inside the scrollingTexture
//     opcode; frame-LRU reaps it after 600 idle frames, or Release
//     drops it immediately).
//
// # Per-frame loop
//
//	hs := heatmapscroll.New(ids, "spectrogram", cfg, heatmapscroll.Options{WidthSlots: 512, HeightSlots: 1024})
//	// ... each frame:
//	for _, col := range columnsThisFrame {
//	    stats := hs.PushColumn(col)
//	    if stats.BadSamples > 0 { log.Warn(...) }
//	}
//	ev := hs.Render(0, 0) // or hs.RenderFill(fallbackW, fallbackH)
//	if ev.Hovered { /* ev.Row, ev.Col */ }
//	if ev.Clicked { ... }
//
// Hover and click readouts are one frame behind the pixels that produced
// them, per ADR-0058 "Consequences / Negative" (FFFI r9/r10 databindings
// reset each Sync). Callers that need zero-lag readout should track the
// pointer themselves and index into the data ring they already own.
package heatmapscroll

import (
	"fmt"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

// Orientation mirrors c.OrientationE so callers of HeatmapScroll don't
// need a second import for the scroll-direction enum. A named type
// (not an alias) per CODINGSTANDARDS CS008; values cross to the binding
// layer via the uint8 conversion in Render.
type Orientation c.OrientationE

// Filter mirrors c.FilterE so callers of HeatmapScroll don't need a
// second import for the texture-filter enum. Named type per CS008.
type Filter c.FilterE

// ScrollLeft — append right, scroll left; classical audio spectrogram.
// Alias for c.OrientationScrollLeftE. See ADR-0058 SD8.
const ScrollLeft Orientation = Orientation(c.OrientationScrollLeftE)

// ScrollRight — append left, scroll right; mirror of ScrollLeft.
// Alias for c.OrientationScrollRightE.
const ScrollRight Orientation = Orientation(c.OrientationScrollRightE)

// ScrollUp — append bottom, scroll up; vertical sibling of ScrollLeft.
// Alias for c.OrientationScrollUpE.
const ScrollUp Orientation = Orientation(c.OrientationScrollUpE)

// ScrollDown — append top, scroll down; classical RF waterfall.
// Alias for c.OrientationScrollDownE.
const ScrollDown Orientation = Orientation(c.OrientationScrollDownE)

// FilterNearest — nearest-neighbour sampling; default. See ADR-0058 SD3.
const FilterNearest Filter = Filter(c.FilterNearestE)

// FilterLinear — bilinear sampling; blurs across neighbouring columns.
// See ADR-0058 SD3 for why the default is Nearest for scientific data.
const FilterLinear Filter = Filter(c.FilterLinearE)

// Options configures a HeatmapScroll (ADR-0267 W11). The widget re-reads
// [HeatmapScroll.Opts] on every Render, so a change is an assignment; a
// change of the ring's shape restarts it from a blank texture.
type Options struct {
	// WidthSlots and HeightSlots are the ring's dimensions: columns of
	// history and samples per column. Both must be positive (New panics
	// otherwise).
	WidthSlots, HeightSlots uint32
	// Orientation is the scroll direction; the zero value is [ScrollLeft].
	Orientation Orientation
	// Filter is the GPU sampling mode; the zero value is [FilterNearest]
	// (ADR-0058 SD3).
	Filter Filter
	// CaptureScroll opts the widget into owning the mouse wheel while the
	// pointer is over it (ADR-0140): the scroll delta is delivered through
	// Events.Wheel and zeroed for everything else that frame, so an enclosing
	// ScrollArea does not scroll under a waterfall that pans on the wheel.
	CaptureScroll bool
	// CaptureZoom opts the widget into reading the zoom gesture (ctrl+wheel /
	// pinch) while the pointer is over it (ADR-0140); the factor arrives
	// through Events.Wheel with the hover anchor.
	CaptureZoom bool
}

// Events is what one Render reports. Everything is one frame behind the
// pixels that produced it (ADR-0058 "Consequences / Negative").
type Events struct {
	// Hovered says the pointer is over the widget; Row is then the bin index
	// (0 .. HeightSlots-1) and Col the ring position (0 .. WidthSlots-1).
	Hovered  bool
	Row, Col uint32
	// Clicked reports a primary click on the widget rect.
	Clicked bool
	// Wheel is the scroll / zoom the widget captured while the pointer was
	// over it, with the pointer's position relative to the widget origin as
	// the zoom anchor; the identity {0, 0, 1, NaN, NaN} when it did not own
	// the wheel — capture off, pointer elsewhere, or nothing scrolled.
	Wheel c.CanvasWheelValue
}

// HeatmapScroll is a streaming-scalar heatmap widget. Construct once
// with New, push columns each frame with PushColumn, and call Render
// once per frame to emit the underlying scrollingTexture opcode.
//
// Not goroutine-safe; expected to be used from the UI goroutine.
type HeatmapScroll struct {
	// Opts is re-read on every Render; a change is an assignment.
	Opts Options

	ids      *c.WidgetIdStack
	scopeKey string
	cfg      *colormap.Config

	// shape is the ring shape the texture was last rendered with; a change
	// in Opts restarts the ring.
	shapeW, shapeH uint32

	head         uint32
	pending      []uint32 // mapped RGBA columns queued for this frame's Render
	pendingCount uint32

	hoverRc uint64 // r9_u64 databound; packed (row<<32)|col or u64::MAX
	clicked bool   // r10 databound; primary-click on previous frame

	totalStats colormap.ColumnStats // accumulated across all PushColumn calls
}

// New constructs a HeatmapScroll with the given scope key, colormap
// configuration and options. Panics if the ring dimensions are zero or if
// ids or cfg is nil. scopeKey must be unique within the caller's current
// WidgetIdStack scope; it identifies the widget across frames so the
// Rust-side texture cache can key on it.
func New(ids *c.WidgetIdStack, scopeKey string, cfg *colormap.Config, opts Options) *HeatmapScroll {
	if ids == nil {
		panic("heatmapscroll: New requires a non-nil WidgetIdStack")
	}
	if cfg == nil {
		panic("heatmapscroll: New requires a non-nil colormap.Config")
	}
	if opts.WidthSlots == 0 || opts.HeightSlots == 0 {
		panic(fmt.Sprintf("heatmapscroll: WidthSlots (%d) and HeightSlots (%d) must be positive", opts.WidthSlots, opts.HeightSlots))
	}
	return &HeatmapScroll{
		Opts:     opts,
		ids:      ids,
		scopeKey: scopeKey,
		cfg:      cfg,
		shapeW:   opts.WidthSlots,
		shapeH:   opts.HeightSlots,
		hoverRc:  ^uint64(0), // start with "not hovered" sentinel
		pending:  make([]uint32, 0),
	}
}

// SetConfig replaces the colormap configuration used by subsequent
// PushColumn calls. Does NOT re-map already-pushed columns: the live
// gradient-swap path described in ADR-0058 SD1 requires retaining the
// original f32 samples, which this wrapper does not do yet. If you need
// that today, Release and re-push from your retained source.
func (inst *HeatmapScroll) SetConfig(cfg *colormap.Config) {
	if cfg == nil {
		panic("heatmapscroll: SetConfig requires a non-nil colormap.Config")
	}
	inst.cfg = cfg
}

// PushColumn maps one column of heightSlots samples through the current
// colormap.Config and queues the result for the next Render. Returns the
// per-column stats (bad / underflow / overflow counts). Panics if
// len(samples) != heightSlots — a silent truncation would misalign the
// ring and corrupt later columns.
func (inst *HeatmapScroll) PushColumn(samples []float32) (stats colormap.ColumnStats) {
	if uint32(len(samples)) != inst.Opts.HeightSlots {
		panic(fmt.Sprintf("heatmapscroll: PushColumn expects %d samples, got %d", inst.Opts.HeightSlots, len(samples)))
	}
	base := len(inst.pending)
	// Grow pending by one column's worth. A later implementation can
	// cap pending at widthSlots (if more than that many PushColumns
	// happen in one frame, older ones are redundant — the Rust side
	// overwrites them anyway), but for now we ship everything the
	// caller gives us; the texture upload loop is O(new_count).
	need := base + int(inst.Opts.HeightSlots)
	if cap(inst.pending) < need {
		grown := make([]uint32, need, need*2)
		copy(grown, inst.pending)
		inst.pending = grown
	} else {
		inst.pending = inst.pending[:need]
	}
	stats = inst.cfg.Map(samples, inst.pending[base:need])
	inst.totalStats.Add(stats)
	inst.pendingCount++
	return
}

// Render emits the scrollingTexture opcode with the columns queued by
// PushColumn since the last Render, binds the r9_u64 / r10 databindings,
// advances head and reports last frame's interaction. Call once per frame,
// even if no columns were pushed (the widget still needs to render its
// current texture content). w and h are the rendered rect in logical pixels;
// 0 along an axis keeps the slot-count default (1 slot = 1 px), a positive
// value stretches the texture to that size via egui's painter sampler, with
// hover (row, col) converted back to slot units so the readout stays in ring
// space.
//
// Host starvation (StateManager.TextureStarved): the ring texture is
// (re)created host-side on first show, on a slot-shape change, and after
// the idle LRU evicted it while the widget went uninterpreted (a hidden
// dock tab — imztop's panels). Columns shipped in that window are gone;
// the host reports the id and Render resets head to 0 so the ring restarts
// honestly from a blank texture instead of desyncing around a gap.
func (inst *HeatmapScroll) Render(w, h float32) (ev Events) {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		ev = inst.render(w, h)
	}
	return
}

// RenderFill is Render sized to the pane: the room left in the parent, read
// back through a probe one frame behind, with the fallbacks serving until it
// reports (ADR-0267 W12).
func (inst *HeatmapScroll) RenderFill(fallbackW, fallbackH float32) (ev Events) {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		w, h, ok := c.CapturePaneSize(inst.ids.ProbeSeq("pane"))
		if !ok || w < 1 || h < 1 {
			w, h = fallbackW, fallbackH
		}
		ev = inst.render(w, h)
	}
	return
}

// render runs inside the widget's id scope.
func (inst *HeatmapScroll) render(w, h float32) (ev Events) {
	o := inst.Opts
	if o.WidthSlots == 0 || o.HeightSlots == 0 {
		panic(fmt.Sprintf("heatmapscroll: Opts.WidthSlots (%d) and HeightSlots (%d) must be positive", o.WidthSlots, o.HeightSlots))
	}
	if o.WidthSlots != inst.shapeW || o.HeightSlots != inst.shapeH {
		// The host recreates the texture on a shape change; the columns
		// staged for the old shape do not fit the new one.
		inst.shapeW, inst.shapeH = o.WidthSlots, o.HeightSlots
		inst.head = 0
		inst.pending = inst.pending[:0]
		inst.pendingCount = 0
	}
	sm := c.CurrentApplicationState.StateManager
	// Separate PrepareStr creators: same derived value, but each creator is
	// a single-use state machine (a second Derive on one panics).
	texId := inst.ids.PrepareStr("texture").Derive()
	if sm.TextureStarved(texId) {
		inst.head = 0
	}
	f := c.ScrollingTexture(
		inst.ids.PrepareStr("texture"),
		o.WidthSlots,
		o.HeightSlots,
		uint8(o.Orientation),
		uint8(o.Filter),
		inst.head,
		inst.pendingCount,
		inst.pending,
		w,
		h,
	)
	if o.CaptureScroll {
		f = f.CaptureScroll()
	}
	if o.CaptureZoom {
		f = f.CaptureZoom()
	}
	f.SendRespVal(&inst.hoverRc, &inst.clicked)

	if inst.pendingCount > 0 {
		inst.head = (inst.head + inst.pendingCount) % o.WidthSlots
	}
	inst.pending = inst.pending[:0]
	inst.pendingCount = 0

	ev.Row, ev.Col, ev.Hovered = c.UnpackHoverRc(inst.hoverRc)
	ev.Clicked = inst.clicked
	ev.Wheel = sm.GetCanvasWheel(widgethandle.Make(texId))
	return
}

// Release emits the scrollingTextureRelease opcode, dropping the
// Rust-side TextureHandle for this widget id immediately. Intended for
// predictable lifecycle callers (tab close, demo teardown); otherwise
// the frame-LRU reaps idle entries after ~10 s at 60 Hz (ADR-0058 SD7).
func (inst *HeatmapScroll) Release() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		c.ScrollingTextureRelease(inst.ids.PrepareStr("texture")).Send()
	}
}

// Head returns the current ring-buffer write cursor. Useful for callers
// maintaining a parallel retained sample ring — they can translate
// (ring_col) readouts back to (logical_time_step) using head.
func (inst *HeatmapScroll) Head() uint32 { return inst.head }

// TotalStats returns the cumulative bad / under / over counts across
// all PushColumn calls since construction (or since ResetTotalStats).
func (inst *HeatmapScroll) TotalStats() colormap.ColumnStats { return inst.totalStats }

// ResetTotalStats zeroes the running stats counter. Does not affect
// already-mapped columns.
func (inst *HeatmapScroll) ResetTotalStats() { inst.totalStats = colormap.ColumnStats{} }

// Size returns the widget's ring dimensions, Opts.WidthSlots and
// Opts.HeightSlots.
func (inst *HeatmapScroll) Size() (widthSlots, heightSlots uint32) {
	return inst.Opts.WidthSlots, inst.Opts.HeightSlots
}
