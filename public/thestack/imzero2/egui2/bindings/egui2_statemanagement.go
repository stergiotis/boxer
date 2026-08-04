package bindings

import (
	"iter"
	"math"

	"github.com/stergiotis/boxer/public/containers"
	"github.com/stergiotis/boxer/public/containers/ragged"
	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/imzero2/metrics"
)

// EtPrefetchValue is the per-table visible range reported by egui_table's
// prepare() callback on the previous frame. Available to Go with a
// one-frame lag — callers should have a sensible fallback (e.g. "emit
// everything") for the first frame after a table is shown.
//
// The effective visible column set is {0..NumStickyCols) ∪ [ColBegin..ColEnd).
// Columns before NumStickyCols are always visible regardless of horizontal
// scroll position; the ColBegin/End range covers the non-sticky window.
type EtPrefetchValue struct {
	RowBegin      uint64
	RowEnd        uint64
	ColBegin      uint32
	ColEnd        uint32
	NumStickyCols uint32
}

// EtColWidthsValue is the per-table column widths egui_table settled on
// after reconciling stored state, the user's drag, and its own
// grow-to-fit pass (ADR-0151 §SD4). Pushed only by tables that called
// ApplyWidths, and available with the same one-frame lag as
// EtPrefetchValue — the read happens during the previous frame's Rust
// render and is drained at end-of-frame Sync.
//
// Widths is owned by the state manager and reused across frames; a caller
// that needs to keep it past the current frame must copy it.
type EtColWidthsValue struct {
	Widths []float32
}

// CanvasPointerValue is the cached payload of the R14 canvas-pointer
// register, drained once per frame by StateManager.Sync. Read via
// StateManager.GetCanvasPointer; callers that previously invoked
// Fetcher.FetchR14CanvasPointer inline (e.g. the colorscale hover
// readout) read from this cache instead, because inline fetches inside
// a deferred-block capture scope (e.g. dock.Tab bodies) buffer rather
// than flush and deadlock the render loop.
type CanvasPointerValue struct {
	HoverX  float32
	HoverY  float32
	Clicked bool
}

// WalkersCameraValue is the cached payload of the R15 walkers-camera
// register, drained once per frame by StateManager.Sync. Found is
// false until at least one WalkersMap widget has rendered. Mirrors the
// 16 return values of Fetcher.FetchR15WalkersCamera.
type WalkersCameraValue struct {
	Found          bool
	MapId          uint64
	Zoom           float64
	CenterLat      float64
	CenterLon      float64
	MinLat         float64
	MinLon         float64
	MaxLat         float64
	MaxLon         float64
	ScreenWidthPx  float32
	ScreenHeightPx float32
	HoverLat       float64
	HoverLon       float64
	HoverValid     bool
	Clicked        bool
	ViewHash       uint64
}

// ScrollDeltaValue is the cached payload of the R16 scroll-delta drain.
// Smoothed scroll-wheel delta from egui's InputState for the previous
// frame. Positive Y = scroll up, positive X = scroll right; both in egui
// logical pixels. Use for pan/zoom gestures inside custom-drawn canvases.
type ScrollDeltaValue struct {
	X float32
	Y float32
}

// CanvasWheelValue is the cached payload of the R23 canvas-wheel drain
// (ADR-0140): the scroll/zoom a paintCanvas captured last frame while the
// pointer was over it, via .CaptureScroll() / .CaptureZoom(). ScrollX/ScrollY
// are in egui logical pixels (X+ = right, Y+ = up); Zoom is a multiplicative
// factor (1.0 = no change). HoverX/HoverY are the pointer relative to the
// canvas origin at capture time — the zoom anchor, scoped to this canvas so it
// does not depend on the single-slot global canvas pointer. Absent (the canvas
// did not own the wheel this frame) reads as the identity: {0, 0, 1, NaN, NaN}.
type CanvasWheelValue struct {
	ScrollX float32
	ScrollY float32
	Zoom    float32
	HoverX  float32
	HoverY  float32
}

// nilCanvasWheel is the value returned for a canvas that did not own the wheel
// last frame: no scroll, identity zoom, no hover anchor.
var nilCanvasWheel = CanvasWheelValue{
	Zoom:   1.0,
	HoverX: float32(math.NaN()),
	HoverY: float32(math.NaN()),
}

// CanvasCursorValue is one R24 canvas-pointer row: the canvas's screen
// origin plus the pointer in canvas-relative coordinates (NaN when the
// pointer is neither over the canvas nor dragging it). Per canvas id —
// unlike the single-slot R14 pointer, which is last-canvas-wins and so
// unusable with several canvases in one frame. PosX/PosY are drag-stable
// (interact_pointer_pos first), so a drag keeps reporting positions after
// the pointer crosses the canvas edge. One-frame lag like every register.
type CanvasCursorValue struct {
	OriginX float32
	OriginY float32
	PosX    float32
	PosY    float32
	// Mods is the modifier state carried with the row (bit0 shift,
	// bit1 ctrl, bit2 alt, bit3 command). On a sense region's
	// drag-started frame it comes from the press event itself, so a
	// modifier pressed and released within one batched frame is still
	// seen; otherwise it is the frame-end state.
	Mods uint8
}

// CanvasCursorValue modifier accessors.
func (v CanvasCursorValue) Shift() bool   { return v.Mods&1 != 0 }
func (v CanvasCursorValue) Ctrl() bool    { return v.Mods&2 != 0 }
func (v CanvasCursorValue) Alt() bool     { return v.Mods&4 != 0 }
func (v CanvasCursorValue) Command() bool { return v.Mods&8 != 0 }

// ModifiersValue is the cached payload of the R17 modifiers drain.
// Modifier-key state from egui's InputState for the previous frame.
// Command is the platform-native primary modifier (Cmd on macOS, Ctrl
// elsewhere); prefer it for OS-convention shortcuts. Ctrl and MacCmd
// are the raw physical keys.
type ModifiersValue struct {
	Alt     bool
	Ctrl    bool
	Shift   bool
	MacCmd  bool
	Command bool
}

// AvailableSizeValue is the cached payload of the R18 available-size
// drain. Last captured ui.available_size from a captureAvailableSize op.
// W and H are NaN until the first capture lands inside a Ui scope.
type AvailableSizeValue struct {
	W float32
	H float32
}

// ZoomDeltaValue is the cached payload of the R19 zoom-delta drain.
// Multiplicative zoom factor from egui's combined gesture detection
// (Ctrl+scroll, touchpad pinch, +/- keyboard). 1.0 = no change,
// >1.0 = zoom in, <1.0 = zoom out. Use instead of reading
// ScrollDelta + Modifiers because egui consumes Ctrl+scroll before it
// reaches smooth_scroll_delta.
type ZoomDeltaValue struct {
	Zoom float32
}

// PointerValue is the cached payload of the R20 pointer drain. X/Y are
// the most-recent observed pointer position in egui logical pixels
// (viewport-relative top-left origin). Valid is false until the pointer
// has been seen at least once (headless runs, freshly-opened viewport,
// first frame); X/Y are NaN in that case. Use for click-anchored popups,
// contextual menus, or any "open near the pointer" affordance that
// doesn't have a canvas / plot to anchor to.
type PointerValue struct {
	X     float32
	Y     float32
	Valid bool
}

// UiRectValue is one row of the R21 ui-rect drain — a viewport-absolute
// snapshot of ui.min_rect() taken by a captureUiRect op. Used by the
// bezier-connector affordance (and any future cross-scope affordance) to
// thread one Ui scope's screen rect into another scope's render code.
// One-frame lag, like every other capture/fetch pair in this file.
type UiRectValue struct {
	MinX float32
	MinY float32
	MaxX float32
	MaxY float32
}

// SnarlEventsValue is the cached payload of the snarl-events drain,
// pre-decoded into the public SnarlEvent shape. Empty on the first
// frame after a snarl editor is shown; refilled every Sync.
//
// Stored as a slice on StateManager (rather than emitted to consumers
// via callback) so multiple consumers in the same frame can read
// independently — though in practice exactly one demo per dock tile
// touches snarl events.
type SnarlEventsValue []SnarlEvent

// GraphEventsValue / GraphSelectionValue / GraphMetricsValue cache the
// three egui_graphs fetcher outputs at frame-end. Same rationale as
// SnarlEventsValue.
type GraphEventsValue []GraphEvent
type GraphSelectionValue []GraphSelectedItem
type GraphMetricsValue []GraphMetrics

type StateManager struct {
	responseFlags        *containers.BinarySearchGrowingKV[uint64, ResponseFlagsE]
	r10Databinds         *containers.BinarySearchGrowingKV[uint64, *bool]
	r9F64Databinds       *containers.BinarySearchGrowingKV[uint64, *float64]
	r9U64Databinds       *containers.BinarySearchGrowingKV[uint64, *uint64]
	r9SDatabinds         *containers.BinarySearchGrowingKV[uint64, *string]
	etPrefetch           *containers.BinarySearchGrowingKV[uint64, EtPrefetchValue]
	etColWidths          *containers.BinarySearchGrowingKV[uint64, EtColWidthsValue]
	overriddenBindingIds *containers.HashSet[uint64]
	fetcher              *Fetcher

	// Per-frame inline-fetcher caches. Drained by Sync at frame-end and
	// read by widget code on the following frame via the Get* methods.
	// Pre-M3 these were fetched inline during render; that violated the
	// "fetchers run only at frame end" convention and deadlocked when
	// the render scope was inside a deferred-block capture (e.g. a
	// dock.Tab body).
	r14CanvasPointer CanvasPointerValue
	r15WalkersCamera WalkersCameraValue
	r16ScrollDelta   ScrollDeltaValue
	r17Modifiers     ModifiersValue
	r18AvailableSize AvailableSizeValue
	r19ZoomDelta     ZoomDeltaValue
	r20Pointer       PointerValue
	r21UiRects       map[uint64]UiRectValue
	// r23CanvasWheel holds LAST frame's per-canvas wheel captures (ADR-0140),
	// keyed by canvas widget id. Rebuilt each Sync; read via GetCanvasWheel.
	r23CanvasWheel map[uint64]CanvasWheelValue
	// r24CanvasPointers holds LAST frame's per-canvas pointer rows (ADR-0149
	// M1), keyed by canvas widget id. Rebuilt each Sync; read via
	// GetCanvasCursor.
	r24CanvasPointers map[uint64]CanvasCursorValue
	// r22StarvedTextures holds LAST frame's starved-texture report: ids the
	// host interpreted with no pixels and no usable cache entry (a send-once
	// upload lost to a discarded hidden-tab buffer, or an idle-LRU eviction).
	// Per-frame set, replaced each Sync. Senders consult TextureStarved (the
	// ImageVersionTracker does so via PixelsToSendFor) and re-ship.
	r22StarvedTextures map[uint64]struct{}
	// f1KeyPressed mirrors the per-frame fetchF1KeyPressed drain. True
	// exactly once per physical F1 press (egui's consume_key removes
	// the event from the input queue so other widgets in the same
	// frame don't also react). The carousel's decorateRenderer polls
	// this via GetF1KeyPressed and opens HelpHost on true; widgets
	// inside an app should NOT poll, since they'd race the carousel
	// for the same consumed event.
	f1KeyPressed bool
	// commandEnter mirrors the per-frame fetchCommandEnterPressed drain: the
	// Ctrl/Cmd+Enter and Ctrl/Cmd+Shift+Enter "submit" pair, each true exactly
	// once per physical press and never both. Read via
	// GetCommandEnterPressed. Unlike F1 this is not a runtime binding — no
	// chrome-level consumer exists — and every poller in a frame sees the
	// same press, so an app consumer gates on its window being the shell's
	// active one (app.WindowFocusI); see GetCommandEnterPressed for the
	// contract.
	commandEnter      bool
	commandEnterShift bool
	snarlEvents       SnarlEventsValue
	graphEvents       GraphEventsValue
	graphSelection    GraphSelectionValue
	graphMetrics      GraphMetricsValue
}

func NewStateManager() *StateManager {
	return &StateManager{
		responseFlags:        containers.NewBinarySearchGrowingKVOrdered[uint64, ResponseFlagsE](1024),
		r10Databinds:         containers.NewBinarySearchGrowingKVOrdered[uint64, *bool](128),
		r9F64Databinds:       containers.NewBinarySearchGrowingKVOrdered[uint64, *float64](128),
		r9U64Databinds:       containers.NewBinarySearchGrowingKVOrdered[uint64, *uint64](128),
		r9SDatabinds:         containers.NewBinarySearchGrowingKVOrdered[uint64, *string](128),
		etPrefetch:           containers.NewBinarySearchGrowingKVOrdered[uint64, EtPrefetchValue](16),
		etColWidths:          containers.NewBinarySearchGrowingKVOrdered[uint64, EtColWidthsValue](8),
		overriddenBindingIds: containers.NewHashSet[uint64](128),
		fetcher:              NewFetcher(),
		r21UiRects:           make(map[uint64]UiRectValue, 8),
		r23CanvasWheel:       make(map[uint64]CanvasWheelValue, 8),
		r24CanvasPointers:    make(map[uint64]CanvasCursorValue, 8),
		r22StarvedTextures:   make(map[uint64]struct{}, 8),
	}
}

// GetCanvasPointer returns last frame's R14 canvas-pointer state. Use
// this in widget render bodies instead of calling FetchR14CanvasPointer
// inline — the latter buffers (and deadlocks) inside dock.Tab bodies.
func (inst *StateManager) GetCanvasPointer() CanvasPointerValue {
	return inst.r14CanvasPointer
}

// GetWalkersCamera returns last frame's R15 walkers-camera state.
// Found=false means no WalkersMap has rendered yet.
func (inst *StateManager) GetWalkersCamera() WalkersCameraValue {
	return inst.r15WalkersCamera
}

// GetScrollDelta returns last frame's R16 smoothed scroll-wheel delta.
// Values are in egui logical pixels; X positive = scroll right, Y positive
// = scroll up.
//
// This is the whole-Context global: it is UNSCOPED (every reader in a frame
// sees the same value, regardless of which widget the pointer is over) and
// NON-CONSUMING. For a canvas that should own the wheel only while hovered —
// and fence egui-native ScrollAreas out of the same gesture — prefer
// [StateManager.GetCanvasWheel] with a paintCanvas .CaptureScroll() (ADR-0140).
// Reserve this for a genuine whole-viewport scroll reader.
func (inst *StateManager) GetScrollDelta() ScrollDeltaValue {
	return inst.r16ScrollDelta
}

// GetModifiers returns last frame's R17 modifier-key state. Prefer
// Command over Ctrl when implementing OS-convention shortcuts (it maps
// to Cmd on macOS, Ctrl elsewhere).
func (inst *StateManager) GetModifiers() ModifiersValue {
	return inst.r17Modifiers
}

// GetAvailableSize returns last frame's R18 captured ui.available_size.
// W and H are NaN until a captureAvailableSize op has been emitted from
// inside a Ui scope at least once. One-frame lag: the value reflects the
// previous frame's capture.
//
// Deprecated: r18 is a SINGLE process-wide scalar that the frame's LAST
// capture wins, so two panels using it size each other — the reader has no way
// to tell whose pane it is holding. Use [CapturePaneSize], whose r21 slot is
// per-caller. Every layout consumer in this repo has moved; nothing reads this
// today, and a new reader is a bug in waiting.
func (inst *StateManager) GetAvailableSize() AvailableSizeValue {
	return inst.r18AvailableSize
}

// GetZoomDelta returns last frame's R19 multiplicative zoom factor from
// egui's combined gesture detection. 1.0 = no change. Prefer this over
// reading scroll + modifiers for zoom because Ctrl+scroll is consumed
// by egui before reaching smooth_scroll_delta.
//
// Whole-Context global (unscoped, non-consuming): any hovered canvas in the
// frame sees the same value. For per-canvas zoom ownership — so a gesture over
// one canvas does not also zoom a sibling — prefer [StateManager.GetCanvasWheel]
// with a paintCanvas .CaptureZoom() (ADR-0140).
func (inst *StateManager) GetZoomDelta() ZoomDeltaValue {
	return inst.r19ZoomDelta
}

// GetF1KeyPressed reports whether the user pressed F1 between this
// frame's Sync and the previous one. egui's consume_key has already
// removed the event from the input queue, and the value here is
// per-frame state every reader sees alike — so a global key binding
// is only sane in one of two consumer shapes:
//
//   - a PROCESS-SINGLETON consumer, which is what F1 has: the host
//     chrome (DecorateRenderer) polls it and opens-or-raises HelpHost.
//     Apps must not poll it too — they would act on the same press.
//   - a PER-INSTANCE consumer gated on the shell's active window
//     (the app.WindowFocusI capability), which is what Ctrl+Enter has
//     — see [StateManager.GetCommandEnterPressed]. Without the gate,
//     one press fans out into every open instance of the app.
//
// Apps that need help-focused affordances expose their own buttons /
// shortcuts on top of [app.OpenRef] rather than polling this.
func (inst *StateManager) GetF1KeyPressed() (pressed bool) {
	pressed = inst.f1KeyPressed
	return
}

// GetCommandEnterPressed reports whether the user pressed Ctrl+Enter
// (Cmd+Enter on macOS) or Ctrl+Shift+Enter between this frame's Sync
// and the previous one — the conventional "submit what I am editing"
// pair. The two are mutually exclusive: the modified form is consumed
// first, so a press with Shift down never also reports as plain.
//
// egui's consume_key has already removed the event, so as with
// [StateManager.GetF1KeyPressed] this is the single opportunity to
// react. The value is per-frame state every reader sees alike — with
// the app open in more than one shell window, every instance's poll
// observes the same press, so a consumer must gate on whether ITS
// window is the shell's active one (the app.WindowFocusI frame-context
// capability; absent capability = single-surface host = focused).
// play's claimRunChord is the reference consumer — one press ran a
// query in every open playground before that gate existed. A focused
// TextEdit does not compete for the chord: egui acts on Enter only
// through its return_key, which requires no modifiers.
func (inst *StateManager) GetCommandEnterPressed() (pressed bool, shiftPressed bool) {
	return inst.commandEnter, inst.commandEnterShift
}

// GetUiRect returns last frame's R21 captured ui.min_rect for the given
// seq, plus whether a capture for that seq landed. Callers stamp a Ui
// scope via [c.CaptureUiRect](seq) inside that scope; one frame later
// the rect is readable here. Used by the bezier-connector affordance to
// learn the inspector window's viewport-absolute rect without exposing a
// per-widget rect query.
//
// Semantics worth knowing:
//   - The rect is ui.min_rect (bbox of widgets placed so far). Inside a
//     c.Window body, that's the WINDOW'S CONTENT AREA — title bar and
//     frame padding are NOT included. Use this for "where the window
//     content meets the world" anchoring; for outer-window framing,
//     query egui's stored area rect directly (no API exposed yet).
//   - For a Horizontal layout the rect captured after widget N is the
//     cumulative bbox of widgets 0..=N. Right edge = N's right edge,
//     vertical span = the row's full height. The Horizontal must not
//     wrap or the bottom-right widget's coords will leak in.
//   - One-frame lag: a capture this frame is readable next frame.
//     Frame 1 returns ok=false.
func (inst *StateManager) GetUiRect(seq uint64) (v UiRectValue, ok bool) {
	v, ok = inst.r21UiRects[seq]
	return
}

// GetCanvasWheel returns last frame's R23 wheel capture for the paintCanvas
// identified by the given handle (ADR-0140) — the scroll/zoom that canvas owned
// while the pointer was over it, having opted in via .CaptureScroll() /
// .CaptureZoom(). A canvas that did not own the wheel (pointer elsewhere, or no
// opt-in) reads as the identity {0, 0, 1, NaN, NaN}, so callers can act
// unconditionally: Zoom==1 and ScrollX/Y==0 mean "no gesture for me". Because
// capture is gated on egui's own contains_pointer() hit-test, exactly one
// canvas owns a given gesture — a sibling canvas or a wrapping ScrollArea will
// not also see it. One-frame lag, like every register here.
func (inst *StateManager) GetCanvasWheel(h widgethandle.WidgetHandle) CanvasWheelValue {
	if v, ok := inst.r23CanvasWheel[h.Resolve()]; ok {
		return v
	}
	return nilCanvasWheel
}

// GetCanvasCursor returns last frame's R24 pointer row for the paintCanvas
// identified by the handle: the canvas screen origin plus the drag-stable
// pointer in canvas-relative coordinates (NaN when the pointer is neither
// over the canvas nor dragging it). ok=false means that canvas did not
// render last frame (hidden tab, culled block, first frame).
func (inst *StateManager) GetCanvasCursor(h widgethandle.WidgetHandle) (v CanvasCursorValue, ok bool) {
	v, ok = inst.r24CanvasPointers[h.Resolve()]
	return
}

// GetPointer returns last frame's R20 latest-pointer-position from egui's
// InputState. Valid is false until the pointer has been observed at least
// once; X/Y are NaN in that case. Use for click-anchored popups and
// contextual menus that should open near the cursor — read on the same
// frame as the triggering [ResponseFlagsE.HasPrimaryClicked], the pointer
// will reflect the position the click landed on (one-frame lag).
func (inst *StateManager) GetPointer() PointerValue {
	return inst.r20Pointer
}

// GetSnarlEvents returns last frame's snarl interaction events. The
// returned slice is owned by the StateManager and reused next frame;
// callers that need to retain entries past this frame must copy.
func (inst *StateManager) GetSnarlEvents() SnarlEventsValue {
	return inst.snarlEvents
}

// GetGraphEvents / GetGraphSelection / GetGraphMetrics return last
// frame's egui_graphs cached state. Same ownership rules as
// GetSnarlEvents.
func (inst *StateManager) GetGraphEvents() GraphEventsValue {
	return inst.graphEvents
}
func (inst *StateManager) GetGraphSelection() GraphSelectionValue {
	return inst.graphSelection
}
func (inst *StateManager) GetGraphMetrics() GraphMetricsValue {
	return inst.graphMetrics
}

// GetEtPrefetch returns the previous frame's visible (row, col) ranges for
// the ETable identified by h. The second return is false before the table
// has been rendered once — callers should fall back to emitting everything
// in that case.
func (inst *StateManager) GetEtPrefetch(h widgethandle.WidgetHandle) (EtPrefetchValue, bool) {
	return inst.etPrefetch.Get(h.Resolve())
}

// GetEtColWidths returns the previous frame's settled column widths for
// an etable, for tables that opted in via ApplyWidths. The slice is
// owned by the state manager and reused; copy it to keep it.
func (inst *StateManager) GetEtColWidths(h widgethandle.WidgetHandle) (EtColWidthsValue, bool) {
	return inst.etColWidths.Get(h.Resolve())
}

// applyEtColWidths unpacks the R25 drain into the per-table cache.
//
// The layout is ragged rather than fixed-stride like R9's: ids[i] owns
// counts[i] consecutive entries in vals. The values iterator is consumed
// to exhaustion whatever the ids and counts say, because a partially
// drained fetch desynchronizes the FFI channel for every later reader in
// the frame — a wrong width is a cosmetic bug, a desynced channel is not.
func (inst *StateManager) applyEtColWidths(ids []uint64, counts []uint64, vals iter.Seq[float32]) {
	next, stop := iter.Pull(vals)
	defer stop()
	for i, id := range ids {
		var n int
		if i < len(counts) {
			n = int(counts[i])
		}
		// Reuse the previous frame's backing array: a table's column count
		// is stable frame to frame, so the steady state allocates nothing.
		prev, had := inst.etColWidths.Get(id)
		buf := prev.Widths
		if !had || cap(buf) < n {
			buf = make([]float32, n)
		} else {
			buf = buf[:n]
		}
		for j := range buf {
			v, ok := next()
			if !ok {
				// Truncated payload: keep what arrived rather than
				// publishing zeros a resolver would capture as overrides.
				buf = buf[:j]
				break
			}
			buf[j] = v
		}
		inst.etColWidths.UpsertBatch(id, EtColWidthsValue{Widths: buf})
	}
	for {
		if _, more := next(); !more {
			break
		}
	}
}
func (inst *StateManager) Fetcher() *Fetcher {
	return inst.fetcher
}

// GetResponse returns the response flags for the widget identified by the given handle.
func (inst *StateManager) GetResponse(h widgethandle.WidgetHandle) ResponseFlagsE {
	return inst.responseFlags.GetDefault(h.Resolve(), NilResponseFlags)
}

// TextureStarved reports whether the host flagged the given texture id as
// starved LAST frame: it was interpreted with no pixels and no usable cache
// entry — a send-once upload that went into a discarded hidden-tab buffer,
// or an entry the idle LRU evicted while the widget went uninterpreted. A
// sender that keeps "already sent" memory (ImageVersionTracker, the Map
// raster's lastSentVersion, heatmapscroll's head) must consult this and
// re-ship. One-frame lag, like every register in this file. Ids are in the
// sender's own id space (widget ids; the walkers mapRaster rasterId).
func (inst *StateManager) TextureStarved(id uint64) bool {
	_, ok := inst.r22StarvedTextures[id]
	return ok
}

// GetResponseByIdRaw is the raw-id variant used by Fluid struct methods
// (and out-of-package widget packages such as widgets/badge) that already
// hold the widget's u64 id.
func (inst *StateManager) GetResponseByIdRaw(id uint64) ResponseFlagsE {
	return inst.responseFlags.GetDefault(id, NilResponseFlags)
}
func (inst *StateManager) AddR10Databinding(id uint64, ptr *bool) {
	inst.r10Databinds.UpsertBatch(id, ptr)
}
func (inst *StateManager) AddR9F64Databinding(id uint64, ptr *float64) {
	inst.r9F64Databinds.UpsertBatch(id, ptr)
}
func (inst *StateManager) AddR9U64Databinding(id uint64, ptr *uint64) {
	inst.r9U64Databinds.UpsertBatch(id, ptr)
}
func (inst *StateManager) AddR9SDatabinding(id uint64, ptr *string) {
	inst.r9SDatabinds.UpsertBatch(id, ptr)
}

// OverrideDatabinding marks the widget identified by the given handle as overridden,
// preventing automatic data-binding updates for it.
func (inst *StateManager) OverrideDatabinding(h widgethandle.WidgetHandle) {
	inst.overriddenBindingIds.Add(h.Resolve())
}

// overrideDatabindingRaw is the package-internal variant.
func (inst *StateManager) overrideDatabindingRaw(id uint64) {
	inst.overriddenBindingIds.Add(id)
}
func (inst *StateManager) IterateDatabindingWidgetsByF64Ptr(ptr *float64) iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for id, p := range inst.r9F64Databinds.IteratePairs() {
			if p == ptr {
				if !yield(id) {
					break
				}
			}
		}
	}
}
func (inst *StateManager) OverrideDatabindingF64Ptr(ptr *float64) {
	for id := range inst.IterateDatabindingWidgetsByF64Ptr(ptr) {
		inst.overrideDatabindingRaw(id)
	}
}
func (inst *StateManager) IterateDatabindingWidgetsBySPtr(ptr *string) iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for id, p := range inst.r9SDatabinds.IteratePairs() {
			if p == ptr {
				if !yield(id) {
					break
				}
			}
		}
	}
}
func (inst *StateManager) OverrideDatabindingSPtr(ptr *string) {
	for id := range inst.IterateDatabindingWidgetsBySPtr(ptr) {
		inst.overrideDatabindingRaw(id)
	}
}
func (inst *StateManager) IterateDatabindingWidgetsByBPtr(ptr *bool) iter.Seq[uint64] {
	return func(yield func(uint64) bool) {
		for id, p := range inst.r10Databinds.IteratePairs() {
			if p == ptr {
				if !yield(id) {
					break
				}
			}
		}
	}
}
func (inst *StateManager) OverrideDatabindingBPtr(ptr *bool) {
	for id := range inst.IterateDatabindingWidgetsByBPtr(ptr) {
		inst.overrideDatabindingRaw(id)
	}
}
func applyDataBindings[V any](blacklist *containers.HashSet[uint64], bindings *containers.BinarySearchGrowingKV[uint64, *V], fetcher func() (ids []uint64, vals iter.Seq[V])) {
	ids, vals := fetcher()
	if !bindings.IsEmpty() {
		if blacklist.IsEmpty() {
			for id, val := range ragged.Zip2R(ids, vals) {
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val
				}
			}
		} else {
			for id, val := range ragged.Zip2R(ids, vals) {
				if blacklist.Has(id) {
					continue
				}
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val
				}
			}
		}
		bindings.Reset()
	} else {
		functional.ConsumeIterator(vals)
	}
}
func applyDataBindingsConst[V any](blacklist *containers.HashSet[uint64], bindings *containers.BinarySearchGrowingKV[uint64, *V], ids iter.Seq[uint64], val V, def V) {
	if !bindings.IsEmpty() {
		if blacklist.IsEmpty() {
			for f := range bindings.IterateValues() {
				*f = def
			}
			for id := range ids {
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val
				}
			}
		} else {
			for id, f := range bindings.IteratePairs() {
				if blacklist.Has(id) {
					continue
				}
				*f = def
			}
			for id := range ids {
				if blacklist.Has(id) {
					continue
				}
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val
				}
			}
		}
		bindings.Reset()
	} else {
		functional.ConsumeIterator(ids)
	}
}
func applyDataBindingsConst2[V any](blacklist *containers.HashSet[uint64], bindings *containers.BinarySearchGrowingKV[uint64, *V], fetcher func() (idsVal1 []uint64, idsVal2 iter.Seq[uint64]), val1 V, val2 V) {
	idsVal1, idsVal2 := fetcher()
	if !bindings.IsEmpty() {
		if blacklist.IsEmpty() {
			for _, id := range idsVal1 {
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val1
				}
			}
			for id := range idsVal2 {
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val2
				}
			}
		} else {
			for _, id := range idsVal1 {
				if blacklist.Has(id) {
					continue
				}
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val1
				}
			}
			for id := range idsVal2 {
				if blacklist.Has(id) {
					continue
				}
				f := bindings.GetDefault(id, nil)
				if f != nil {
					*f = val2
				}
			}
		}
		bindings.Reset()
	} else {
		functional.ConsumeIterator(idsVal2)
	}
}
func (inst *StateManager) Sync() {
	seenIds.Clear()
	fetcher := inst.fetcher

	ids, resps := fetcher.FetchR7()
	d := inst.responseFlags
	for id, resp := range ragged.Zip2R(ids, resps) {
		d.UpsertBatch(id, ResponseFlagsE(resp))
	}

	// important: we need to consume all iterators as these directly read from the fffi channel!
	blacklist := inst.overriddenBindingIds

	applyDataBindings(blacklist, inst.r9F64Databinds, func() ([]uint64, iter.Seq[float64]) {
		return fetcher.FetchR9F64()
	})
	applyDataBindings(blacklist, inst.r9U64Databinds, func() ([]uint64, iter.Seq[uint64]) {
		return fetcher.FetchR9U64()
	})
	applyDataBindings(blacklist, inst.r9SDatabinds, func() ([]uint64, iter.Seq[string]) {
		return fetcher.FetchR9S()
	})
	applyDataBindingsConst2(blacklist, inst.r10Databinds, fetcher.FetchR10, true, false)

	// ETable prefetch — packed 5×u64 per id (rowBegin, rowEnd, colBegin,
	// colEnd, numStickyCols). Must consume the iterator fully even if
	// unused so the FFI channel stays in sync.
	{
		etIds, etVals := fetcher.FetchR9EtPrefetch()
		next, stop := iter.Pull(etVals)
		for _, id := range etIds {
			rb, _ := next()
			re, _ := next()
			cb, _ := next()
			ce, _ := next()
			ns, _ := next()
			inst.etPrefetch.UpsertBatch(id, EtPrefetchValue{
				RowBegin: rb, RowEnd: re,
				ColBegin: uint32(cb), ColEnd: uint32(ce),
				NumStickyCols: uint32(ns),
			})
		}
		// Drain any trailing values (shouldn't happen; defensive).
		for {
			_, more := next()
			if !more {
				break
			}
		}
		stop()
	}

	// ETable settled column widths (ADR-0151 §SD4) — ids[i] owns counts[i]
	// entries in widths, laid end to end. Only tables that called
	// ApplyWidths push here, so this is usually empty. As above, the
	// iterator must be consumed fully even when unused or the FFI channel
	// desynchronizes.
	inst.applyEtColWidths(fetcher.FetchR25EtColWidths())

	inst.r9F64Databinds.Reset()
	inst.r9U64Databinds.Reset()
	inst.r9SDatabinds.Reset()
	inst.r10Databinds.Reset()
	blacklist.Clear()

	// Per-frame inline-fetcher snapshot. Each call is one opcode + a
	// small fixed-size payload; the corresponding Rust handler returns
	// zeros / empty arrays when no source widget rendered last frame,
	// so the cost is bounded regardless of which demos are mounted.
	// Widget code reads these caches via the matching Get* method
	// instead of calling Fetcher inline — see CanvasPointerValue
	// docstring for the deadlock rationale.
	{
		hx, hy, clicked := fetcher.FetchR14CanvasPointer()
		inst.r14CanvasPointer = CanvasPointerValue{HoverX: hx, HoverY: hy, Clicked: clicked}
	}
	{
		found, mapId, zoom, cLat, cLon, minLat, minLon, maxLat, maxLon,
			sw, sh, hLat, hLon, hValid, clicked, vh := fetcher.FetchR15WalkersCamera()
		inst.r15WalkersCamera = WalkersCameraValue{
			Found: found, MapId: mapId, Zoom: zoom,
			CenterLat: cLat, CenterLon: cLon,
			MinLat: minLat, MinLon: minLon, MaxLat: maxLat, MaxLon: maxLon,
			ScreenWidthPx: sw, ScreenHeightPx: sh,
			HoverLat: hLat, HoverLon: hLon, HoverValid: hValid,
			Clicked: clicked, ViewHash: vh,
		}
	}
	{
		x, y := fetcher.FetchR16ScrollDelta()
		inst.r16ScrollDelta = ScrollDeltaValue{X: x, Y: y}
	}
	{
		alt, ctrl, shift, macCmd, command := fetcher.FetchR17Modifiers()
		inst.r17Modifiers = ModifiersValue{
			Alt: alt, Ctrl: ctrl, Shift: shift,
			MacCmd: macCmd, Command: command,
		}
	}
	{
		w, h := fetcher.FetchR18AvailableSize()
		inst.r18AvailableSize = AvailableSizeValue{W: w, H: h}
	}
	{
		z := fetcher.FetchR19ZoomDelta()
		inst.r19ZoomDelta = ZoomDeltaValue{Zoom: z}
	}
	{
		x, y, valid := fetcher.FetchR20Pointer()
		inst.r20Pointer = PointerValue{X: x, Y: y, Valid: valid}
	}
	{
		// F1 global help binding. consume_key drains the event from
		// egui's input queue so other widgets in the same frame don't
		// also react — the runtime owns this shortcut exclusively.
		inst.f1KeyPressed = fetcher.FetchF1KeyPressed()
	}
	{
		// Ctrl/Cmd+Enter and Ctrl/Cmd+Shift+Enter. Draining these at
		// frame end is safe where plain Enter would not be: a focused
		// TextEdit reacts to Enter only through its return_key, which
		// carries no modifiers, so the widgets have already declined
		// these two by the time Sync runs.
		inst.commandEnter, inst.commandEnterShift = fetcher.FetchCommandEnterPressed()
	}
	{
		seqs, minX, minY, maxX, maxYSeq := fetcher.FetchR21UiRects()
		for k := range inst.r21UiRects {
			delete(inst.r21UiRects, k)
		}
		i := 0
		for maxY := range maxYSeq {
			if i >= len(seqs) {
				break
			}
			inst.r21UiRects[seqs[i]] = UiRectValue{
				MinX: minX[i],
				MinY: minY[i],
				MaxX: maxX[i],
				MaxY: maxY,
			}
			i++
		}
	}
	{
		ids, scrollXs, scrollYs, zooms, hoverXs, hoverYSeq := fetcher.FetchR23CanvasWheel()
		for k := range inst.r23CanvasWheel {
			delete(inst.r23CanvasWheel, k)
		}
		i := 0
		for hoverY := range hoverYSeq {
			if i >= len(ids) {
				break
			}
			inst.r23CanvasWheel[ids[i]] = CanvasWheelValue{
				ScrollX: scrollXs[i],
				ScrollY: scrollYs[i],
				Zoom:    zooms[i],
				HoverX:  hoverXs[i],
				HoverY:  hoverY,
			}
			i++
		}
	}
	{
		ids, originXs, originYs, posXs, posYs, modsSeq := fetcher.FetchR24CanvasPointers()
		for k := range inst.r24CanvasPointers {
			delete(inst.r24CanvasPointers, k)
		}
		i := 0
		for mods := range modsSeq {
			if i >= len(ids) {
				break
			}
			inst.r24CanvasPointers[ids[i]] = CanvasCursorValue{
				OriginX: originXs[i],
				OriginY: originYs[i],
				PosX:    posXs[i],
				PosY:    posYs[i],
				Mods:    mods,
			}
			i++
		}
	}
	{
		ids := fetcher.FetchR22StarvedTextures()
		clear(inst.r22StarvedTextures)
		for id := range ids {
			inst.r22StarvedTextures[id] = struct{}{}
		}
	}
	{
		editorIds, kinds, nodeIds, portsA, nodeIdsB, portsB, xs, ysSeq := fetcher.FetchSnarlEvents()
		out := inst.snarlEvents[:0]
		i := 0
		for y := range ysSeq {
			if i >= len(editorIds) {
				break
			}
			out = append(out, SnarlEvent{
				EditorId: editorIds[i],
				Kind:     SnarlEventKindE(kinds[i]),
				NodeId:   nodeIds[i],
				PortA:    portsA[i],
				NodeIdB:  nodeIdsB[i],
				PortB:    portsB[i],
				X:        xs[i],
				Y:        y,
			})
			i++
		}
		inst.snarlEvents = out
	}
	{
		graphIds, kinds, keyA, keyBSeq := fetcher.FetchGraphEvents()
		out := inst.graphEvents[:0]
		i := 0
		for kb := range keyBSeq {
			if i >= len(graphIds) {
				break
			}
			out = append(out, GraphEvent{
				GraphId: graphIds[i],
				Kind:    GraphEventKindE(kinds[i]),
				KeyA:    keyA[i],
				KeyB:    kb,
			})
			i++
		}
		inst.graphEvents = out
	}
	{
		graphIds, kinds, keyA, keyBSeq := fetcher.FetchGraphSelection()
		out := inst.graphSelection[:0]
		i := 0
		for kb := range keyBSeq {
			if i >= len(graphIds) {
				break
			}
			out = append(out, GraphSelectedItem{
				GraphId: graphIds[i],
				IsNode:  kinds[i] == 0,
				KeyA:    keyA[i],
				KeyB:    kb,
			})
			i++
		}
		inst.graphSelection = out
	}
	{
		graphIds, nodeCount, edgeCount, frSteps, frLastSeq := fetcher.FetchGraphMetrics()
		out := inst.graphMetrics[:0]
		i := 0
		for last := range frLastSeq {
			if i >= len(graphIds) {
				break
			}
			out = append(out, GraphMetrics{
				GraphId:            graphIds[i],
				NodeCount:          nodeCount[i],
				EdgeCount:          edgeCount[i],
				FrSteps:            frSteps[i],
				FrLastDisplacement: last,
			})
			i++
		}
		inst.graphMetrics = out
	}

	// Drain the per-frame Rust-side timing one extra round-trip per Sync.
	// Cost is one opcode + a 16-byte response; reported with one-frame lag
	// because this fetcher fires inside the very interpret_commands_outer
	// call whose elapsed it would otherwise need to peek at.
	interpretUs, passNr := inst.fetcher.FetchFrameMetrics()
	metrics.Current.RecordRust(interpretUs, passNr)
}
func (inst *StateManager) Reset() {
	inst.responseFlags.Reset()
}
