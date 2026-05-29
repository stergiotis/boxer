// Package heatmapscroll composes the colormap package with the
// scrollingTexture widget (ADR-0058) into a single, opinionated wrapper
// for "streaming scalar → colour heatmap" use cases: audio spectrograms,
// RF waterfalls, thermal streams, rolling metrics heatmaps.
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
//	hs := heatmapscroll.New(ids, "spectrogram", cfg, 512, 1024)
//	hs.SetOrientation(heatmapscroll.ScrollLeft)
//	// ... each frame:
//	for _, col := range columnsThisFrame {
//	    stats := hs.PushColumn(col)
//	    if stats.BadSamples > 0 { log.Warn(...) }
//	}
//	hs.Render()
//	if row, col, ok := hs.HoveredCell(); ok { ... }
//	if hs.Clicked() { ... }
//
// Hover and click readouts are one frame behind the pixels that produced
// them, per ADR-0058 "Consequences / Negative" (FFFI r9/r10 databindings
// reset each Sync). Callers that need zero-lag readout should track the
// pointer themselves and index into the data ring they already own.
package heatmapscroll

import (
	"fmt"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/colormap"
)

// Orientation re-exports c.OrientationE so callers of HeatmapScroll
// don't need a second import for the scroll-direction enum.
type Orientation = c.OrientationE

// Filter re-exports c.FilterE so callers of HeatmapScroll don't need a
// second import for the texture-filter enum.
type Filter = c.FilterE

// ScrollLeft — append right, scroll left; classical audio spectrogram.
// Alias for c.OrientationScrollLeftE. See ADR-0058 SD8.
const ScrollLeft Orientation = c.OrientationScrollLeftE

// ScrollRight — append left, scroll right; mirror of ScrollLeft.
// Alias for c.OrientationScrollRightE.
const ScrollRight Orientation = c.OrientationScrollRightE

// ScrollUp — append bottom, scroll up; vertical sibling of ScrollLeft.
// Alias for c.OrientationScrollUpE.
const ScrollUp Orientation = c.OrientationScrollUpE

// ScrollDown — append top, scroll down; classical RF waterfall.
// Alias for c.OrientationScrollDownE.
const ScrollDown Orientation = c.OrientationScrollDownE

// FilterNearest — nearest-neighbour sampling; default. See ADR-0058 SD3.
const FilterNearest Filter = c.FilterNearestE

// FilterLinear — bilinear sampling; blurs across neighbouring columns.
// See ADR-0058 SD3 for why the default is Nearest for scientific data.
const FilterLinear Filter = c.FilterLinearE

// HeatmapScroll is a streaming-scalar heatmap widget. Construct once
// with New, push columns each frame with PushColumn, and call Render
// once per frame to emit the underlying scrollingTexture opcode.
//
// Not goroutine-safe; expected to be used from the UI goroutine.
type HeatmapScroll struct {
	ids      *c.WidgetIdStack
	scopeKey string

	cfg         *colormap.Config
	widthSlots  uint32
	heightSlots uint32
	orientation Orientation
	filter      Filter

	// displayWidthPx / displayHeightPx override the rendered rect size
	// independently of the slot count. 0 keeps the slot-count default
	// (1 slot = 1 px). Non-zero stretches the texture via
	// painter.image's sampler; hover (row, col) is scaled back to slot
	// units inside the Rust widget so the readout stays in ring space.
	displayWidthPx  float32
	displayHeightPx float32

	head         uint32
	pending      []uint32 // mapped RGBA columns queued for this frame's Render
	pendingCount uint32

	hoverRc uint64 // r9_u64 databound; packed (row<<32)|col or u64::MAX
	clicked bool   // r10 databound; primary-click on previous frame

	totalStats colormap.ColumnStats // accumulated across all PushColumn calls
}

// New constructs a HeatmapScroll with the given scope key, colormap
// configuration, and ring dimensions. Panics if widthSlots or
// heightSlots is zero, or if cfg is nil. scopeKey must be unique within
// the caller's current WidgetIdStack scope; it identifies the widget
// across frames so the Rust-side texture cache can key on it.
//
// Defaults: ScrollLeft + FilterNearest (the scientific-visualisation
// defaults called out in ADR-0058 SD3).
func New(ids *c.WidgetIdStack, scopeKey string, cfg *colormap.Config, widthSlots, heightSlots uint32) *HeatmapScroll {
	if ids == nil {
		panic("heatmapscroll: New requires a non-nil WidgetIdStack")
	}
	if cfg == nil {
		panic("heatmapscroll: New requires a non-nil colormap.Config")
	}
	if widthSlots == 0 || heightSlots == 0 {
		panic(fmt.Sprintf("heatmapscroll: widthSlots (%d) and heightSlots (%d) must be positive", widthSlots, heightSlots))
	}
	return &HeatmapScroll{
		ids:         ids,
		scopeKey:    scopeKey,
		cfg:         cfg,
		widthSlots:  widthSlots,
		heightSlots: heightSlots,
		orientation: ScrollLeft,
		filter:      FilterNearest,
		hoverRc:     ^uint64(0), // start with "not hovered" sentinel
		pending:     make([]uint32, 0),
	}
}

// SetOrientation selects one of the four scroll directions.
// Takes effect on the next Render call.
func (inst *HeatmapScroll) SetOrientation(o Orientation) { inst.orientation = o }

// SetDisplaySize overrides the rendered pixel rect independently of
// the slot count: 0 along an axis keeps the historical slot-count
// default (1 slot = 1 px); a positive value stretches the texture
// to that pixel size via egui's painter sampler. Useful when the
// caller wants the heatmap to grow with its enclosing panel without
// re-allocating the underlying ring texture.
//
// Hover (row, col) coordinates are converted back to slot units in
// the Rust widget, so display-size changes do not shift the ring
// readout. Takes effect on the next Render call.
func (inst *HeatmapScroll) SetDisplaySize(widthPx, heightPx float32) {
	inst.displayWidthPx = widthPx
	inst.displayHeightPx = heightPx
}

// SetFilter selects GPU texture sampling (see ADR-0058 SD3).
// Takes effect on the next Render call.
func (inst *HeatmapScroll) SetFilter(f Filter) { inst.filter = f }

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
	if uint32(len(samples)) != inst.heightSlots {
		panic(fmt.Sprintf("heatmapscroll: PushColumn expects %d samples, got %d", inst.heightSlots, len(samples)))
	}
	base := len(inst.pending)
	// Grow pending by one column's worth. A later implementation can
	// cap pending at widthSlots (if more than that many PushColumns
	// happen in one frame, older ones are redundant — the Rust side
	// overwrites them anyway), but for now we ship everything the
	// caller gives us; the texture upload loop is O(new_count).
	need := base + int(inst.heightSlots)
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
// and advances head. Call once per frame, even if no columns were
// pushed (the widget still needs to render its current texture content).
func (inst *HeatmapScroll) Render() {
	creator := inst.ids.PrepareStr(inst.scopeKey)
	c.ScrollingTexture(
		creator,
		inst.widthSlots,
		inst.heightSlots,
		uint8(inst.orientation),
		uint8(inst.filter),
		inst.head,
		inst.pendingCount,
		inst.pending,
		inst.displayWidthPx,
		inst.displayHeightPx,
	).SendRespVal(&inst.hoverRc, &inst.clicked)

	if inst.pendingCount > 0 {
		inst.head = (inst.head + inst.pendingCount) % inst.widthSlots
	}
	inst.pending = inst.pending[:0]
	inst.pendingCount = 0
}

// Release emits the scrollingTextureRelease opcode, dropping the
// Rust-side TextureHandle for this widget id immediately. Intended for
// predictable lifecycle callers (tab close, demo teardown); otherwise
// the frame-LRU reaps idle entries after ~10 s at 60 Hz (ADR-0058 SD7).
func (inst *HeatmapScroll) Release() {
	creator := inst.ids.PrepareStr(inst.scopeKey)
	c.ScrollingTextureRelease(creator).Send()
}

// HoveredCell returns the data-index coordinates currently under the
// pointer, and whether the pointer is over the widget at all. Row is
// the bin index (0 .. heightSlots-1); col is the ring position
// (0 .. widthSlots-1). The value carries a one-frame lag.
func (inst *HeatmapScroll) HoveredCell() (row uint32, col uint32, hovered bool) {
	return c.UnpackHoverRc(inst.hoverRc)
}

// Clicked reports whether egui registered a primary click on the
// widget rect on the previous frame. One-frame lag, same as HoveredCell.
func (inst *HeatmapScroll) Clicked() bool { return inst.clicked }

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

// Size returns the widget's ring dimensions as given to New.
func (inst *HeatmapScroll) Size() (widthSlots, heightSlots uint32) {
	return inst.widthSlots, inst.heightSlots
}
