package bindings

import (
	"bytes"
	"encoding/binary"
	"iter"
	"math"

	"github.com/stergiotis/boxer/public/functional"
	"github.com/stergiotis/boxer/public/keelson/runtime/widgethandle"
	"github.com/stergiotis/boxer/public/thestack/fffi2/typed"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
)

// Id returns the widget id stamped on this Frame at construction time,
// suitable for looking up the previous-frame response flags via
// [StateManager.GetResponseByIdRaw]. Used by out-of-package widget
// compositions (e.g. widgets/badge) that emit a Frame and want SendResp
// semantics.
func (inst FrameFluid) Id() uint64 { return inst.id }

// Id returns the widget id stamped on this Window at construction time,
// suitable for binding the title-bar X to a `*bool` via OpenBound +
// [StateManager.AddR10Databinding] (the egui::Window `.open(&mut bool)`
// idiom — see ADR-0026, `feedback_egui_native_affordances`). Mirrors
// [FrameFluid.Id] so out-of-package widget compositions can wire native
// close affordances without reaching into the unexported `id` field.
func (inst WindowFluid) Id() uint64 { return inst.id }

func (inst ButtonFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}
func (inst NodeLeafFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}
func (inst RadioButtonFluid) SendRespVal(val *bool) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR10Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst NodeDirFluid) SendIter() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.Send()
		defer func() {
			NodeDirClose(0).Send()
		}()
		yield(functional.NilIteratorValue)
	}
}
func (inst SliderF64Fluid) SendRespVal(val *float64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9F64Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst CheckboxFluid) SendRespVal(val *bool) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR10Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst TextEditFluid) SendRespVal(val *string) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9SDatabinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst DragValueF64Fluid) SendRespVal(val *float64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9F64Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst DragValueU64Fluid) SendRespVal(val *uint64) ResponseFlagsE {
	inst.Send()
	s := CurrentApplicationState.StateManager
	id := inst.id
	s.AddR9U64Databinding(id, val)
	return s.GetResponseByIdRaw(id)
}
func (inst SelectableLabelFluid) SendResp() ResponseFlagsE {
	inst.Send()
	return CurrentApplicationState.StateManager.GetResponseByIdRaw(inst.id)
}

// PlotResponse carries the most-recent in-plot primary click, in plot-data
// coordinates. Filled by PlotFluid.SendResp via FetchR15PlotPointer; carries
// the standard one-frame lag — a click on frame N is reported on frame N+1.
// Clicked is false (and X/Y are NaN-or-zero) when there was no recent click
// or the latest click was on a different plot.
type PlotResponse struct {
	X, Y    float64
	Clicked bool
}

// SendResp emits the plot opcode and reads the cached r15 plot-pointer
// state (populated last frame by StateManager.Sync). Single-slot
// semantics — only the latest click across all plots is retained — so
// we filter on the plot widget id and report Clicked=false if the
// latest click belongs to a different plot.
//
// The cache read replaces an earlier inline FetchR15PlotPointer call:
// inline fetches inside a deferred-block capture (e.g. dock.Tab
// bodies, post-M3) buffer rather than flush and deadlock the render
// loop. The "one-frame lag" semantics are unchanged — the inline
// fetch already ran on the prior frame's data.
func (inst PlotFluid) SendResp() PlotResponse {
	inst.Send()
	p := CurrentApplicationState.StateManager.GetPlotPointer()
	if !p.Clicked || p.PlotId != inst.id {
		return PlotResponse{}
	}
	return PlotResponse{X: p.X, Y: p.Y, Clicked: true}
}

// Handle returns the runtime-scoped widget handle for the block. Use for
// app-level skip on heavy bodies that should short-circuit when the parent
// is collapsed: `if c.IsBlockSkipped(ch.Handle()) { continue }`.
//
// Reads the previous frame's BLOCK_SKIPPED flag, so it carries the same
// one-frame lag as any r7-derived signal — bodies on the click-to-open
// frame still emit their opcodes (the gate is gone per ADR-0012). The
// short-circuit is a perf hint, not a correctness gate.
func (inst CollapsingHeaderFluid) Handle() widgethandle.WidgetHandle {
	return widgethandle.Make(inst.id)
}
func (inst WindowFluid) Handle() widgethandle.WidgetHandle {
	return widgethandle.Make(inst.id)
}
func (inst ComboBoxFluid) Handle() widgethandle.WidgetHandle {
	return widgethandle.Make(inst.id)
}

// IsBlockSkipped reports whether Rust set BLOCK_SKIPPED on the block in the
// previous frame. Advisory only — see ADR-0012. Bodies emit unconditionally;
// app-level skip is opt-in for callers that want to short-circuit heavy work.
func IsBlockSkipped(h widgethandle.WidgetHandle) bool {
	return CurrentApplicationState.StateManager.GetResponse(h).HasBlockSkipped()
}

// Render captures the two closure bodies as the tooltip's tip and target
// content and sends the hoverUi opcode. The target is rendered in-place
// inside a `ui.scope(...)`; the tip is rendered inside the egui tooltip
// layer when the scope is hovered.
//
//	c.HoverUi().Render(
//	    func() { c.Label("rich tooltip").Send() },
//	    func() { c.Button(ids.PrepareStr("btn"), atoms).Send() },
//	)
func (inst HoverUiFluid) Render(tipBody, targetBody func()) {
	inst.BeginTip(0)
	tipBody()
	inst.EndTip()
	inst.BeginTarget(0)
	targetBody()
	inst.EndTarget()
	inst.Send()
}

// --- egui_dock docking helper (iter-style wrapper over DockAreaRaw) ---

// DockLeafIdT names a leaf in the initial-layout descriptor passed to
// the Rust side on first DockState construction. Returned by InitRoot
// (always 0) and by every Split call (1, 2, …). Pass back to Split to
// nest further splits off that leaf.
type DockLeafIdT uint8

// DockSplitDirE is the direction of a Split, mirroring egui_dock
// 0.19's `Split::{Above,Below,Left,Right}`.
type DockSplitDirE uint8

const (
	DockAbove DockSplitDirE = 0
	DockBelow DockSplitDirE = 1
	DockLeft  DockSplitDirE = 2
	DockRight DockSplitDirE = 3
)

// dockSplitS is one entry in the recorded layout descriptor; serialised
// in declaration order when DockArea closes.
type dockSplitS struct {
	parent  DockLeafIdT
	newLeaf DockLeafIdT
	dir     DockSplitDirE
	frac    float32
	tabs    []uint64
}

// DockAreaFluid is the iter-yielded builder for a dock area. Tab() returns
// an iter.Seq so each tab's (id, title, body) reads as one grouped unit at
// the call site. Tab bodies are captured to detached buffers during
// iteration and flushed in declaration order when the enclosing DockArea
// iter exits, so tab order matches the source code regardless of HashMap
// iteration quirks on the Rust side.
//
// Initial split layout: InitRoot + Split{Above,Below,Left,Right} record
// a layout descriptor encoded into the initialLayout byte slice the
// Rust side consumes on first DockState construction. On subsequent
// frames the persistent dock_states map wins, so the user's drag-drop
// changes survive. Calling neither InitRoot nor Split keeps the
// historical "everything in one leaf" default.
type DockAreaFluid struct {
	idGen         WidgetIdCreatorI
	derivedId     uint64
	ids           []uint64
	titles        []string
	bodies        [][]byte
	noScrollTabs  []uint64
	rootTabs      []uint64
	splits        []dockSplitS
	nextLeafId    DockLeafIdT
	activateTabId uint64
}

// DockArea opens an iter-style dock area scope.
//
// On entry it derives+pushes the dock id via DeriveStacked (matching the
// IdScope / KeepIter pattern used by every other block in the package).
// On exit it emits the dockArea opcode carrying all declared tabs and
// their captured bodies, then pops the dock id via PopIdFromStackChecked.
//
// Inside the scope, any PrepareStr / IdScope used within a tab body is
// XOR'd with the dock id on its way down the stack, so two dock areas in
// the same app with identically-named internal widgets do not collide,
// and moving a tab around a dock does not shift sibling widget ids.
//
// The yielded *DockAreaFluid is valid only for the lifetime of the scope.
//
//	for dock := range c.DockArea(ids.PrepareStr("main")) {
//	    for range dock.Tab(1, "widgets") { /* widgets */ }
//	    for range dock.Tab(2, "data")    { /* etable here — composes fine */ }
//	}
func DockArea(id WidgetIdCreatorI) iter.Seq[*DockAreaFluid] {
	return func(yield func(*DockAreaFluid) bool) {
		fluid := &DockAreaFluid{
			idGen:     id,
			derivedId: id.DeriveStacked(),
		}
		defer func() {
			fluid.send()
			id.PopIdFromStackChecked(fluid.derivedId)
		}()
		yield(fluid)
	}
}

// Tab declares a tab with a stable u64 identifier and a plain-string title.
// The returned iter.Seq yields exactly once; the for-range body emits the
// widgets that become the tab's contents. Under the hood, body opcodes are
// captured into a detached buffer via BeginCapture/EndCapture; on scope
// exit the buffered bytes are injected back into the dock area's deferred
// block map in declaration order.
//
// Tab ids must be stable across frames — they name entries in the
// persistent layout state. The Rust side reconciles via retain_tabs
// (drop ids no longer present) + push_to_first_leaf (add new ones),
// preserving splits and drag-order for everything that stayed.
func (inst *DockAreaFluid) Tab(tabId uint64, title string) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		buf := &bytes.Buffer{}
		fffi := typed.GetCurrentFffiCapture()
		fffi.BeginCapture(buf, binary.LittleEndian)
		defer func() {
			fffi.EndCapture()
			inst.ids = append(inst.ids, tabId)
			inst.titles = append(inst.titles, title)
			inst.bodies = append(inst.bodies, buf.Bytes())
		}()
		yield(functional.NilIteratorValue)
	}
}

// ActivateTab programmatically focuses the given tab this frame (the user's
// later clicks still win — this sets the active tab once, it does not pin
// it). Use it when an affordance delivers content INTO a tab body — e.g. the
// snippet-library Insert splicing into the editor: a hidden tab's body buffer
// is discarded uninterpreted, so a delivery op would be silently lost, and
// the user could not see the result anyway. Passing an id not present this
// frame is a no-op.
func (inst *DockAreaFluid) ActivateTab(tabId uint64) {
	inst.activateTabId = tabId
}

// TabNoScroll declares a tab exactly like Tab, but with the dock's default
// per-tab body ScrollArea disabled on both axes. Use it when the tab body
// owns its pointer/scroll interaction (a map viewport, a canvas): egui
// widgets read wheel input globally, so the wrapping ScrollArea otherwise
// reacts to the same events the widget consumes for pan/zoom — one gesture
// then scrolls the panel AND moves the widget content (the play Map-tab
// zoom flicker). Content overflowing a no-scroll tab is clipped, not
// scrollable — size the body to the leaf.
//
// A paintCanvas that adopts .CaptureScroll() (ADR-0140) consumes the wheel
// itself while hovered, so it no longer needs this to avoid the double-scroll
// and can live under a plain Tab's ScrollArea. TabNoScroll remains for the
// walkers map (below), for widgets that read the wheel globally without
// consuming, and for bodies that must clip rather than scroll.
//
// The walkers read-without-consume half is reported upstream as
// https://github.com/podusowski/walkers/issues/544; when a consuming
// walkers lands, map-hosting tabs can return to plain Tab.
func (inst *DockAreaFluid) TabNoScroll(tabId uint64, title string) iter.Seq[functional.NilIteratorValueType] {
	inst.noScrollTabs = append(inst.noScrollTabs, tabId)
	return inst.Tab(tabId, title)
}

// InitRoot declares the tabs that live in the root leaf of the initial
// layout. Must be called before any Split. Returns DockLeafIdT(0); pass
// that handle to Split as the parent for the first horizontal/vertical
// division. Calling InitRoot with no tabs (or omitting it entirely) is
// equivalent to the historical default: every declared Tab goes into a
// single leaf.
func (inst *DockAreaFluid) InitRoot(tabs ...uint64) (h DockLeafIdT) {
	inst.rootTabs = append(inst.rootTabs[:0], tabs...)
	if inst.nextLeafId == 0 {
		inst.nextLeafId = 1
	}
	return DockLeafIdT(0)
}

// Split records a new leaf split off from `parent`. Returns the new
// leaf's handle so further splits can nest off it. The `frac` is the
// fraction of the parent the OLD node keeps after the split (egui_dock
// 0.19 semantics — see Tree::split_{above,below,left,right} in the
// upstream crate).
//
// Splits run only the first time the dock_state is constructed. Once
// the user drags a tab the persistent state wins; declared splits are
// effectively a preset, not a constraint.
func (inst *DockAreaFluid) Split(parent DockLeafIdT, dir DockSplitDirE, frac float32, tabs ...uint64) (h DockLeafIdT) {
	if inst.nextLeafId == 0 {
		inst.nextLeafId = 1
	}
	h = inst.nextLeafId
	inst.nextLeafId++
	tabsCopy := append([]uint64(nil), tabs...)
	inst.splits = append(inst.splits, dockSplitS{
		parent: parent, newLeaf: h, dir: dir, frac: frac, tabs: tabsCopy,
	})
	return
}

// encodeInitialLayout serialises the rootTabs + splits into the wire
// format the Rust side parses on first DockState construction. Empty
// rootTabs + zero splits returns an empty (non-nil) slice — Rust falls
// back to the default "all tabs in one leaf" behaviour. Must not be
// nil: PutUint8SliceArg writes 0xFFFFFFFF for nil, which read_plain_u8h
// would treat as a 4-billion-byte length and deadlock the interpreter.
func (inst *DockAreaFluid) encodeInitialLayout() (out []byte) {
	if len(inst.rootTabs) == 0 && len(inst.splits) == 0 {
		out = []byte{}
		return
	}
	var buf bytes.Buffer
	buf.WriteByte(1)
	var u32 [4]byte
	binary.LittleEndian.PutUint32(u32[:], uint32(len(inst.rootTabs)))
	buf.Write(u32[:])
	var u64 [8]byte
	for _, t := range inst.rootTabs {
		binary.LittleEndian.PutUint64(u64[:], t)
		buf.Write(u64[:])
	}
	buf.WriteByte(byte(len(inst.splits)))
	for _, s := range inst.splits {
		buf.WriteByte(byte(s.parent))
		buf.WriteByte(byte(s.newLeaf))
		buf.WriteByte(byte(s.dir))
		binary.LittleEndian.PutUint32(u32[:], math.Float32bits(s.frac))
		buf.Write(u32[:])
		binary.LittleEndian.PutUint32(u32[:], uint32(len(s.tabs)))
		buf.Write(u32[:])
		for _, t := range s.tabs {
			binary.LittleEndian.PutUint64(u64[:], t)
			buf.Write(u64[:])
		}
	}
	out = buf.Bytes()
	return
}

// send emits the dockArea opcode. Constructs the low-level DockAreaRaw
// factory with the accumulated ids/titles + encoded initial layout,
// then flushes each buffered tab body into the generated deferred
// block map by opening BeginTabBody and writing the pre-captured bytes
// raw (they are already framed), closing with EndTabBody, and
// forwarding to the raw .Send(). Called by the DockArea iter's
// deferred cleanup — never called directly by users.
func (inst *DockAreaFluid) send() {
	noScroll := inst.noScrollTabs
	if noScroll == nil {
		// nil wire-encodes as WriteNilSlice (0xFFFFFFFF length), which the
		// Rust u64h reader would misparse — same hazard encodeInitialLayout
		// documents for the u8 slice. Always send a real (empty) slice.
		noScroll = []uint64{}
	}
	d := DockAreaRaw(AbsoluteWidgetId(inst.derivedId), inst.ids, inst.titles, inst.encodeInitialLayout(), noScroll, inst.activateTabId)
	fffi := typed.GetCurrentFffiCapture()
	for i, id := range inst.ids {
		d.BeginTabBody(id)
		fffi.AppendRawToCapture(inst.bodies[i])
		d.EndTabBody()
	}
	d.Send()
}

// RichTextScope is a typed wrapper around AtomsFluid that restricts the
// available methods to only those valid inside a RichText/EndRichText pair.
// Use AtomsFluid.BeginRichText(text) to enter this scope and .End() to exit.
type RichTextScope struct{ a AtomsFluid }

// BeginRichText starts a rich text segment. Chain style methods, then call .End().
//
// This is the only public way to open a rich-text segment: the raw sub-protocol
// methods (richText/richTextColored/endRichText and the style methods) are
// unexported on AtomsFluid (see egui2_definition_d_evaluated.go), so an
// unbalanced chain like Atoms().RichTextColored(...).Text(...) no longer
// compiles — the balancing endRichText is issued by RichTextScope.End().
func (inst AtomsFluid) BeginRichText(text string) RichTextScope {
	return RichTextScope{a: inst.richText(text)}
}

// BeginRichTextColored starts a colored rich text segment.
func (inst AtomsFluid) BeginRichTextColored(cl, bk color.Color, text string) RichTextScope {
	return RichTextScope{a: inst.richTextColored(text, cl, bk)}
}

// Strong applies bold styling to the rich-text segment. Strong, Weak, Italics,
// Underline, Strikethrough, Code, Monospace, Small, Heading, Raised, Lowered,
// and the *Color variants each return RichTextScope for chaining.
func (inst RichTextScope) Strong() RichTextScope    { return RichTextScope{a: inst.a.strong()} }
func (inst RichTextScope) Weak() RichTextScope      { return RichTextScope{a: inst.a.weak()} }
func (inst RichTextScope) Italics() RichTextScope   { return RichTextScope{a: inst.a.italics()} }
func (inst RichTextScope) Underline() RichTextScope { return RichTextScope{a: inst.a.underline()} }
func (inst RichTextScope) Strikethrough() RichTextScope {
	return RichTextScope{a: inst.a.strikethrough()}
}
func (inst RichTextScope) Code() RichTextScope           { return RichTextScope{a: inst.a.code()} }
func (inst RichTextScope) Monospace() RichTextScope      { return RichTextScope{a: inst.a.monospace()} }
func (inst RichTextScope) Heading() RichTextScope        { return RichTextScope{a: inst.a.heading()} }
func (inst RichTextScope) Small() RichTextScope          { return RichTextScope{a: inst.a.small()} }
func (inst RichTextScope) SmallRaised() RichTextScope    { return RichTextScope{a: inst.a.smallRaised()} }
func (inst RichTextScope) Raised() RichTextScope         { return RichTextScope{a: inst.a.raised()} }
func (inst RichTextScope) Size(sz float32) RichTextScope { return RichTextScope{a: inst.a.size(sz)} }
func (inst RichTextScope) ExtraLetterSpacing(sp float32) RichTextScope {
	return RichTextScope{a: inst.a.extraLetterSpacing(sp)}
}
func (inst RichTextScope) LineHeight(lh float32) RichTextScope {
	return RichTextScope{a: inst.a.lineHeight(lh)}
}
func (inst RichTextScope) LineHeightDefault() RichTextScope {
	return RichTextScope{a: inst.a.lineHeightDefault()}
}

// TextStyleName selects a custom TextStyle::Name slot — most commonly
// the IDS-bound "ids-display" or "ids-micro" tiers (ADR-0030 §SD3).
// Built-in tiers (Heading/Body/Small/Monospace/Button) stay on their
// dedicated methods (Heading()/Small()/Monospace()).
func (inst RichTextScope) TextStyleName(name string) RichTextScope {
	return RichTextScope{a: inst.a.textStyleName(name)}
}

// End closes the rich text segment and returns to the AtomsFluid scope.
func (inst RichTextScope) End() AtomsFluid { return inst.a.endRichText() }

// --- iter.Seq-based rich text scoping ---

// StyledText opens a rich text scope as an iterator. The defer inside the
// iterator writes EndRichText, so the scope cannot be left unclosed.
//
//	a := c.Atoms()
//	for rt := range a.StyledText("bold") {
//	    rt.Strong()
//	}
//	c.Button(ids, a.Keep()).Send()
func (inst AtomsFluid) StyledText(text string) iter.Seq[RichTextScope] {
	return func(yield func(RichTextScope) bool) {
		scope := inst.BeginRichText(text)
		defer func() { scope.End() }()
		yield(scope)
	}
}

// StyledTextColored opens a colored rich text scope as an iterator.
func (inst AtomsFluid) StyledTextColored(cl, bk color.Color, text string) iter.Seq[RichTextScope] {
	return func(yield func(RichTextScope) bool) {
		scope := inst.BeginRichTextColored(cl, bk, text)
		defer func() { scope.End() }()
		yield(scope)
	}
}

// RichTextLabel displays a single styled rich text label. Style the text inside the loop body.
//
//	for rt := range c.RichTextLabel("hello") {
//	    rt.Strong().Italics()
//	}
func RichTextLabel(text string) iter.Seq[RichTextScope] {
	a := Atoms()
	return func(yield func(RichTextScope) bool) {
		scope := a.BeginRichText(text)
		defer func() { LabelAtoms(scope.End().Keep()).Send() }()
		yield(scope)
	}
}

// RichTextLabelColored displays a single colored styled rich text label.
func RichTextLabelColored(cl, bk color.Color, text string) iter.Seq[RichTextScope] {
	a := Atoms()
	return func(yield func(RichTextScope) bool) {
		scope := a.BeginRichTextColored(cl, bk, text)
		defer func() { LabelAtoms(scope.End().Keep()).Send() }()
		yield(scope)
	}
}

// --- iter.Seq-based deferred block scoping for EndETable ---

// Headers opens a deferred header capture scope as an iterator.
// Replaces the BeginHeaders/EndHeaders pair.
//
//	for range et.Headers(0, 0) {
//	    c.Label("Name").Send()
//	}
func (inst EndETableFluid) Headers(key0 uint32, key1 uint32) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.BeginHeaders(key0, key1)
		defer func() { inst.EndHeaders() }()
		yield(functional.NilIteratorValue)
	}
}

// VisibleRange returns the previous frame's visible (row, col) ranges
// reported by egui_table::prepare. Callers use it to skip emitting cells
// and headers that egui_table would immediately cull. The second return
// is false on the first frame a table is shown (no data yet) — callers
// should fall back to emitting the full range in that case.
//
// The effective visible column set is
//
//	[0..numStickyCols) ∪ [colBegin..colEnd)
//
// Sticky columns are always visible regardless of the scrolled window.
// Use ColVisible to test a single column index.
//
// One-frame lag is inherent: prepare() runs during the PREVIOUS frame's
// Rust render, and the drain happens at end-of-frame Sync. When the
// layout or visible range changes abruptly (scroll jumps, resize), the
// first frame after still emits with the stale window; egui_table fills
// the gaps from its block-map cache on the next frame.
//
// Works for both dense and sparse cell-emission patterns: the caller
// decides how to interpret the ranges — typically by guarding each
// `for range et.Cells(row, col)` with ColVisible.
func (inst EndETableFluid) VisibleRange() (rowBegin, rowEnd uint64, colBegin, colEnd, numStickyCols uint32, ok bool) {
	v, ok := CurrentApplicationState.StateManager.GetEtPrefetch(
		widgethandle.Make(inst.id))
	if !ok {
		return 0, 0, 0, 0, 0, false
	}
	return v.RowBegin, v.RowEnd, v.ColBegin, v.ColEnd, v.NumStickyCols, true
}

// ColVisible reports whether a given column index will actually be drawn
// this frame, given the prefetched window and sticky count from the
// previous frame. On the first frame after a table is shown (no prefetch
// yet) it returns true for every column so the fallback path still emits
// a full set — ok=false in that case.
func (inst EndETableFluid) ColVisible(col uint32) (visible, ok bool) {
	v, ok := CurrentApplicationState.StateManager.GetEtPrefetch(
		widgethandle.Make(inst.id))
	if !ok {
		return true, false
	}
	if col < v.NumStickyCols {
		return true, true
	}
	return col >= v.ColBegin && col < v.ColEnd, true
}

// HasPrimaryClicked reports whether the previous frame's interact-sense
// for this TintedScope registered a primary-button click.
//
// senseClick must have been called on the fluid before KeepIter,
// otherwise no response is published and this returns false.
//
// As with all r7-routed responses there is a one-frame delay: the
// click that fires this frame's render is the click the user made
// on the previous frame's geometry.
func (inst TintedScopeFluid) HasPrimaryClicked() bool {
	return CurrentApplicationState.StateManager.GetResponse(
		widgethandle.Make(inst.id),
	).HasPrimaryClicked()
}

// Cells opens a deferred cell capture scope as an iterator.
// Replaces the BeginCells/EndCells pair.
//
//	for range et.Cells(row, col) {
//	    c.Label(value).Send()
//	}
func (inst EndETableFluid) Cells(key0 uint64, key1 uint32) iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.BeginCells(key0, key1)
		defer func() { inst.EndCells() }()
		yield(functional.NilIteratorValue)
	}
}

// --- iter.Seq-based scoping for NewTable (egui_extras::TableBuilder) ---

// NewTableBody is the body-scope handle yielded by NewTableFluid.Body().
// Tracks the auto-incrementing header / row indices used as deferred
// block map keys. Header() and Row() each capture the current index,
// then push their cells via the inner iterator.
type NewTableBody struct {
	fluid     NewTableFluid
	headerIdx uint32
	rowIdx    uint64
}

// NewTableHeaderRow is yielded by NewTableBody.Header(). Inside the loop
// body, repeated calls to Col() emit one cell each, auto-incrementing
// colIdx for keying into the headers deferred block map.
//
// egui_extras 0.34 only supports a single header row, so multiple calls
// to Header() within one Body() are silently treated as one row by the
// Rust apply (only entries keyed (0, col) are read). The Go helper does
// not enforce this — callers are expected to call Header() at most once.
type NewTableHeaderRow struct {
	body      *NewTableBody
	headerIdx uint32
	colIdx    uint32
}

// NewTableDataRow is yielded by NewTableBody.Row(). One Row() call =
// one row in the body, with a height pushed onto new_table_row_heights.
type NewTableDataRow struct {
	body   *NewTableBody
	rowIdx uint64
	colIdx uint32
}

// Body opens the table-scope iterator. The yielded NewTableBody hands out
// Header() and Row() iterators; Send() is dispatched when the loop exits
// (one frame's table = one Send call). Columns must be pushed via
// NewTableColumn().*.Send() BEFORE entering this loop — the columns are
// drained in registration order at apply time.
//
//	c.NewTableColumn().Initial(200).Resizable(true).Send()
//	c.NewTableColumn().Remainder().AtLeast(240).Send()
//
//	for tbl := range c.NewTable(id).Striped(true).HeaderHeight(28).Body() {
//	    for hdr := range tbl.Header() {
//	        for range hdr.Col() { /* cell 0 */ }
//	        for range hdr.Col() { /* cell 1 */ }
//	    }
//	    for range rows {
//	        for r := range tbl.Row(rowHeight(...)) {
//	            for range r.Col() { /* cell 0 */ }
//	            for range r.Col() { /* cell 1 */ }
//	        }
//	    }
//	}
func (inst NewTableFluid) Body() iter.Seq[*NewTableBody] {
	return func(yield func(*NewTableBody) bool) {
		b := &NewTableBody{fluid: inst}
		defer inst.Send()
		yield(b)
	}
}

// Header opens a header-row scope. egui_extras renders the row at the
// height set via NewTableFluid.HeaderHeight(...) on the parent fluid;
// if HeaderHeight is 0 (default), the Rust apply skips header rendering
// entirely and any cells captured here are dropped at replay.
func (inst *NewTableBody) Header() iter.Seq[*NewTableHeaderRow] {
	return func(yield func(*NewTableHeaderRow) bool) {
		hdr := &NewTableHeaderRow{body: inst, headerIdx: inst.headerIdx}
		inst.headerIdx++
		yield(hdr)
	}
}

// Row opens a data-row scope. The supplied height is pushed onto the
// new_table_row_heights register; row heights drive
// egui_extras::TableBody::heterogeneous_rows on the apply side, so
// per-row variable height is native (no manual fix-up).
func (inst *NewTableBody) Row(height float32) iter.Seq[*NewTableDataRow] {
	return func(yield func(*NewTableDataRow) bool) {
		NewTableRowHeight(height).Send()
		r := &NewTableDataRow{body: inst, rowIdx: inst.rowIdx}
		inst.rowIdx++
		yield(r)
	}
}

// Col opens a header-cell deferred block. Body opcodes between yield
// and return are captured into the headers deferred block map keyed by
// (headerIdx, colIdx), then replayed inside egui_extras' header.col(|ui|...)
// callback at apply time.
func (inst *NewTableHeaderRow) Col() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.body.fluid.BeginHeaders(inst.headerIdx, inst.colIdx)
		defer func() {
			inst.body.fluid.EndHeaders()
			inst.colIdx++
		}()
		yield(functional.NilIteratorValue)
	}
}

// Col opens a row-cell deferred block. Body opcodes between yield and
// return are captured into the rows deferred block map keyed by
// (rowIdx, colIdx), then replayed inside egui_extras' row.col(|ui|...)
// callback at apply time.
func (inst *NewTableDataRow) Col() iter.Seq[functional.NilIteratorValueType] {
	return func(yield func(functional.NilIteratorValueType) bool) {
		inst.body.fluid.BeginRows(inst.rowIdx, inst.colIdx)
		defer func() {
			inst.body.fluid.EndRows()
			inst.colIdx++
		}()
		yield(functional.NilIteratorValue)
	}
}
