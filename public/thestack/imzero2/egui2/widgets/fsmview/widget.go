package fsmview

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/goccyengine"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/layeredgraph/view"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/selector"
	"github.com/zeebo/xxh3"
)

// RendererE selects which level-2 view is rendered inside the popup. The
// table is cheaper at small N; the graph reads better once edges outnumber
// states (static layered / Sugiyama layout via Graphviz in-process, see the
// layeredgraph package and ADR-0069); history shows the transition log
// newest-first, as a scrolling table.
type RendererE uint8

const (
	RendererTable RendererE = iota
	RendererGraph
	RendererHistory
)

// popupAnchorXY is the (x, y) carrier for [Widget.PopupAnchor]. Held by
// pointer on the widget so callers can distinguish "no anchor" (nil →
// fall back to egui's default cascade) from "anchor at (0, 0)" (the
// viewport top-left, a legitimate value).
type popupAnchorXY struct{ X, Y float32 }

// Widget is the two-level FSM viewer. Construct via [New], reuse across
// frames via [Widget.Render]. State (popup open/closed, selected renderer)
// lives on the receiver so multiple widgets coexist without crosstalk.
type Widget[T comparable] struct {
	ids      *c.WidgetIdStack
	scopeKey string
	machine  *Machine[T]

	// title is the human-facing name surfaced in the level-2 popup
	// header so callers with multiple FSMs on screen can tell which
	// machine each popup belongs to. Defaults to scopeKey at
	// construction; override via [Widget.Title]. scopeKey is the right
	// default because it's already a stable short string per instance.
	title string

	popupOpen     bool
	renderer      RendererE
	showSubscript bool

	// popupAnchor pins the level-2 Window's default_pos on first open
	// (egui-relative viewport coordinates in logical pixels). nil leaves
	// egui's cascade behaviour intact. egui retains the user's dragged
	// position across subsequent opens, so the anchor only affects the
	// very first time a fresh widget instance shows the popup. Set via
	// [Widget.PopupAnchor]; unset via [Widget.ClearPopupAnchor].
	popupAnchor *popupAnchorXY

	// autoAnchor, when true, captures the cursor position the frame the
	// chip is clicked and writes it into popupAnchor so the popup opens
	// where the click landed (chip ≈ pointer location). Off by default to
	// preserve M3a-i's manual-anchor semantics; enabling it overrides any
	// previously-set anchor on the click frame. Backed by [StateManager.GetPointer]
	// (R20), so requires the matching FFFI2 binding (added M3a-ii).
	autoAnchor bool

	// graphLayout caches the static layered layout (states + transitions)
	// computed once via the layeredgraph engine: the FSM topology does not
	// change, so only the current-state highlight varies per frame, applied
	// at paint time through view.RenderOpts colour hooks. graphLayoutErr
	// records a layout failure so renderGraph can degrade to a message.
	graphLayout    *layeredgraph.Layout
	graphLayoutErr error
	// graphTopoN/graphTopoE are the state/edge counts the cached graphLayout was
	// built from; a change (Mirror/AddRule grows the Machine at runtime)
	// invalidates the cache. graphIDToState is the node-id→state reverse map,
	// rebuilt alongside the layout and reused each frame by the colour/click
	// hooks (so the hot path doesn't rebuild it).
	graphTopoN     int
	graphTopoE     int
	graphIDToState map[string]T
	// graphViewState carries interactive pan/zoom for the Graph tab across
	// frames (view.Render reads drag/zoom over the canvas and updates it).
	graphViewState view.ViewState

	density styletokens.DensityE

	// provenance, when non-zero, is rendered at the top of the popup
	// body as the standard [inspector.ProvenanceChip] so operators can
	// see which source value this FSM is bound to without leaving the
	// popup. Zero value (default) suppresses the chip entirely so
	// existing call sites keep their current visual.
	provenance inspector.Provenance

	// tethered, set via [Widget.Tethered], promotes the level-1 chip to a
	// tethered inspector summary: the state badge gains an
	// [inspector.AnchorToggle] and the level-2 window is linked back to it by
	// the spring-animated bezier [inspector.AnchorTether] (ADR-0046). Off by
	// default — non-tethered call sites keep the plain chip-click popup.
	tethered bool
	tether   inspector.AnchorTether
	// summaryFn, set via [Widget.Summary], renders a caller-owned addendum
	// (stats / freshness) just right of the state badge in tethered mode.
	summaryFn func()
	// badgeToneFn, set via [Widget.BadgeTone], colours the level-1 badge by
	// state (severity); nil keeps the default TonePrimary. Applies in both
	// tethered and plain modes.
	badgeToneFn func(T) badge.ToneE

	// historyFooterFn, set via [Widget.HistoryFooter], renders a caller-owned
	// action row under the History tab's table. nil (default) emits no footer
	// and no separator.
	historyFooterFn func()

	// historyBuf backs the History tab's per-frame row build. Held on the
	// receiver and truncated rather than reallocated, so an open History tab
	// costs no allocation per frame. Never escapes — callers reach the log
	// through [Widget.HistorySnapshot], which copies.
	historyBuf []historyRow[T]
}

// historyRow is one History-tab row: a recorded transition plus the dwell
// time derived from its predecessor. Built oldest-first (dwell needs the
// preceding entry) and rendered newest-first.
type historyRow[T comparable] struct {
	// seq is the 1-based position within the *retained* window, oldest
	// first — not a lifetime transition counter, which the Machine does not
	// keep. It shifts down by one whenever the ring evicts.
	seq int
	tr  Transition[T]
	// dwell is how long the machine sat in tr.From before this transition
	// fired. hasDwell is false for the oldest retained row (its predecessor
	// has been evicted, or never existed) and whenever a timestamp is
	// missing.
	dwell    time.Duration
	hasDwell bool
}

// New constructs a Widget bound to the given Machine. scopeKey scopes all
// widget ids emitted by Render; pass a stable short string per instance
// ("door-fsm", "card-status", …) so two widgets on the same id stack
// don't collide.
//
// Panics on nil ids stack, nil machine, or empty scopeKey — these are
// programmer errors, not data-shape issues.
func New[T comparable](ids *c.WidgetIdStack, scopeKey string, m *Machine[T]) *Widget[T] {
	if ids == nil {
		panic("fsmview: New requires a non-nil ids stack")
	}
	if scopeKey == "" {
		panic("fsmview: New requires a non-empty scopeKey")
	}
	if m == nil {
		panic("fsmview: New requires a non-nil Machine")
	}
	return &Widget[T]{
		ids:      ids,
		scopeKey: scopeKey,
		machine:  m,
		title:    scopeKey,
		renderer: RendererTable,
		density:  styletokens.ActiveDensity(),
	}
}

// Title overrides the human-facing FSM name shown in the level-2 popup
// header. Defaults to the scopeKey passed to [New]. Set when scopeKey is
// a terse id ("traffic") but the operator-facing label should read
// differently ("Traffic light controller"). Returns the receiver for
// chaining.
func (inst *Widget[T]) Title(name string) *Widget[T] {
	inst.title = name
	return inst
}

// Provenance binds the FSM to its source value's [inspector.Provenance]
// identity card. When set (non-zero), the popup body renders the
// standard [inspector.ProvenanceChip] in its header so operators can
// see which subject / source-app produced the state transitions this
// FSM is reflecting. Zero value (default) suppresses the chip — pure
// receiver-owned FSMs without an external binding leave the popup
// unchanged. Returns the receiver for chaining.
func (inst *Widget[T]) Provenance(p inspector.Provenance) *Widget[T] {
	inst.provenance = p
	return inst
}

// IsOpen reports whether the level-2 popup is currently open. Useful for
// drift-guard tests and for sibling widgets that want to react to the
// popup state.
func (inst *Widget[T]) IsOpen() bool {
	return inst.popupOpen
}

// Open programmatically opens the popup. No-op when already open.
func (inst *Widget[T]) Open() {
	inst.popupOpen = true
}

// Close programmatically dismisses the popup.
func (inst *Widget[T]) Close() {
	inst.popupOpen = false
}

// SelectedRenderer returns the currently-selected level-2 view.
func (inst *Widget[T]) SelectedRenderer() RendererE {
	return inst.renderer
}

// SetRenderer pins which level-2 view opens on the next click.
func (inst *Widget[T]) SetRenderer(r RendererE) {
	inst.renderer = r
}

// ShowSubscript toggles the small "Xs ago" subscript rendered to the right
// of the chip, sourced from [Machine.LastTransition]. Off by default so a
// plain chip stays as compact as possible; enable on surfaces where the
// freshness of the state matters (status bars, dashboards).
func (inst *Widget[T]) ShowSubscript(on bool) *Widget[T] {
	inst.showSubscript = on
	return inst
}

// PopupAnchor pins the level-2 Window's default_pos to (x, y) in egui
// logical pixels (viewport top-left origin). Applies on the first open
// of a fresh widget instance; egui remembers the user's dragged position
// after that. Returns the receiver for chaining.
//
// For click-tracking (popup pops where the chip was clicked) see
// [Widget.AutoAnchor], which captures the pointer via the R20 fetcher
// on each click. The two compose: AutoAnchor overrides the stored
// anchor on each click frame, while PopupAnchor remains the fallback
// for programmatic [Widget.Open] calls where no click happens.
func (inst *Widget[T]) PopupAnchor(x, y float32) *Widget[T] {
	inst.popupAnchor = &popupAnchorXY{X: x, Y: y}
	return inst
}

// ClearPopupAnchor reverts the popup to egui's default cascade
// positioning. No-op when no anchor has been set.
func (inst *Widget[T]) ClearPopupAnchor() *Widget[T] {
	inst.popupAnchor = nil
	return inst
}

// AutoAnchor enables click-tracking: the frame the chip is clicked, the
// widget reads the cursor position from [StateManager.GetPointer] and
// writes it into [Widget.PopupAnchor], so the popup pops where the
// click landed. Off by default; turn on for "tooltip-style" popups that
// should follow the chip across layout shifts. Overrides any previously-
// set manual anchor on the click frame. Returns the receiver for chaining.
func (inst *Widget[T]) AutoAnchor(on bool) *Widget[T] {
	inst.autoAnchor = on
	return inst
}

// Tethered promotes the level-1 chip to a tethered inspector summary: the
// state badge gains an [inspector.AnchorToggle] (the arrow-square-out
// open/close affordance) and the level-2 window is linked back to it by the
// spring-animated bezier [inspector.AnchorTether] — the same connector
// distsummary / regexsummary use (ADR-0046). Pair with [Widget.Summary] for a
// rich stat line and [Widget.Provenance] for the window's identity chip. Off
// by default; non-tethered widgets keep the plain chip-click popup. Returns
// the receiver for chaining.
func (inst *Widget[T]) Tethered() *Widget[T] {
	inst.tethered = true
	inst.tether = inspector.NewAnchorTether(inst.scopeKey)
	return inst
}

// Summary sets the level-1 addendum rendered just right of the state badge in
// tethered mode — the caller emits its own stats / freshness labels (e.g.
// "50 rows · 12ms · 8s ago"). It runs inside the tethered chip's Horizontal,
// so emit inline widgets only. No-op unless [Widget.Tethered] is set. Returns
// the receiver for chaining.
func (inst *Widget[T]) Summary(fn func()) *Widget[T] {
	inst.summaryFn = fn
	return inst
}

// BadgeTone colours the level-1 state badge by mapping the current state to a
// [badge.ToneE] (e.g. error states red, success green). nil (default) keeps
// the badge at TonePrimary. Applies in both plain and tethered modes. Returns
// the receiver for chaining.
func (inst *Widget[T]) BadgeTone(fn func(T) badge.ToneE) *Widget[T] {
	inst.badgeToneFn = fn
	return inst
}

// HistoryFooter sets a caller-owned action row rendered under the History
// tab's table, below a separator — the place for whatever a host wants to do
// with the transition log (publish it, copy it, hand it to a playground). It
// runs inside a [c.Horizontal], so emit inline widgets only, the
// [Widget.Summary] rule.
//
// The widget deliberately supplies no action of its own: what a log is worth
// exporting *to* depends on the host's capabilities, which a widget cannot
// know. Pair with [Widget.HistorySnapshot] inside the click branch to get the
// rows. nil (default) emits neither footer nor separator. Returns the receiver
// for chaining.
func (inst *Widget[T]) HistoryFooter(fn func()) *Widget[T] {
	inst.historyFooterFn = fn
	return inst
}

// HistorySnapshot returns a freshly allocated copy of the retained transition
// log, newest-first — the order the History tab shows. Unlike
// [Machine.HistoryReverse] it hands back plain data that outlives the frame,
// so it is safe to pass to a worker goroutine (the render-thread-snapshot
// rule: gather here, do the blocking work there).
//
// Allocates on every call. Call it in a click branch, not per frame.
func (inst *Widget[T]) HistorySnapshot() []Transition[T] {
	out := make([]Transition[T], 0, inst.machine.HistoryLen())
	for tr := range inst.machine.HistoryReverse() {
		out = append(out, tr)
	}
	return out
}

// Render emits the level-1 chip and, when open, the level-2 popup (window +
// tether). Call once per frame inside an active egui surface (panel or window).
// It is exactly [Widget.RenderChip] followed by [Widget.RenderPopup] in one id
// scope — reach for those two directly only when the chip and the window must
// render at different points in the frame, e.g. the chip inside a dock-tab body
// and the window AFTER the DockArea block (a floating window cannot be spawned
// from inside a dock tab — see schemaview's glyph legend for the same pattern).
//
// The chip renders inline at the current cursor — embed it inside a
// [c.Horizontal] flow or a panel. The popup spawns at egui's default
// cascade position (egui_dock-style retention takes over on subsequent
// frames so user-driven drag positions stick).
func (inst *Widget[T]) Render() {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderChip()
		inst.renderPopupAndTether()
	}
}

// RenderChip emits only the level-1 chip: the state badge, and in tethered mode
// the [inspector.AnchorToggle] plus the toggle-rect capture the bezier anchors
// on. Pair with [Widget.RenderPopup] later in the same frame when the window
// must be emitted away from the chip's call site (the dock-tab case). Callers
// using all-in-one [Widget.Render] never need this.
func (inst *Widget[T]) RenderChip() {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderChip()
	}
}

// RenderPopup emits the level-2 popup window (when open) and, in tethered mode,
// paints the bezier connector back to the toggle captured by [Widget.RenderChip].
// Call it where a floating window is legal — notably AFTER a DockArea block,
// never inside a dock-tab body. No-op when the popup is closed. The toggle↔window
// link is by scope (the tether), so the two calls need not be nested.
func (inst *Widget[T]) RenderPopup() {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderPopupAndTether()
	}
}

// renderPopupAndTether is the shared popup half: the window when open, then the
// bezier (tethered + open). Factored out so [Widget.Render] and
// [Widget.RenderPopup] stay in lock-step.
func (inst *Widget[T]) renderPopupAndTether() {
	if inst.popupOpen {
		inst.renderPopup()
	}
	// Tethered mode: draw the bezier from the level-1 toggle to the open
	// window above everything (PaintAbsoluteOverlay). One-frame lag on
	// first open; gated on popupOpen so the curve vanishes with it.
	if inst.tethered && inst.popupOpen {
		inst.tether.Paint()
	}
}

func (inst *Widget[T]) renderChip() {
	current := inst.machine.Current()
	label := inst.machine.Label(current)
	tone := badge.TonePrimary
	if inst.badgeToneFn != nil {
		tone = inst.badgeToneFn(current)
	}
	emitBadge := func() {
		resp := badge.New(inst.ids.PrepareStr("chip"), label).
			Tone(tone).
			Variant(badge.VariantSolid).
			Size(badge.SizeMd).
			Tooltip(fmt.Sprintf("%s — click for state-machine details", inst.title)).
			SendResp()
		if resp.HasPrimaryClicked() {
			inst.popupOpen = !inst.popupOpen
			// AutoAnchor: snapshot the pointer at the moment of the click
			// and pin the popup to it. The R20 fetcher returns the latest
			// observed pointer position from egui's InputState, which
			// reflects the position the click landed on (one-frame lag is
			// already absorbed by the response cache that gates this
			// branch). Skip on Valid=false (headless / pre-first-pointer).
			if inst.autoAnchor && inst.popupOpen {
				p := c.CurrentApplicationState.StateManager.GetPointer()
				if p.Valid {
					inst.popupAnchor = &popupAnchorXY{X: p.X, Y: p.Y}
				}
			}
		}
	}
	if inst.tethered {
		// Tethered inspector summary: badge · caller summary · AnchorToggle,
		// then stamp the row rect for the bezier tether.
		for range c.Horizontal().KeepIter() {
			emitBadge()
			if inst.summaryFn != nil {
				c.AddSpace(styletokens.GapInline(inst.density))
				inst.summaryFn()
			}
			c.AddSpace(styletokens.GapInline(inst.density))
			if inspector.AnchorToggle(inst.ids.PrepareStr("anchor-toggle"), &inst.popupOpen) {
				// Same AutoAnchor pointer-capture as the badge click, so the
				// window opens near the toggle and the bezier stays short.
				if inst.autoAnchor && inst.popupOpen {
					if p := c.CurrentApplicationState.StateManager.GetPointer(); p.Valid {
						inst.popupAnchor = &popupAnchorXY{X: p.X, Y: p.Y}
					}
				}
			}
			inst.tether.CaptureToggle()
		}
		return
	}
	if !inst.showSubscript {
		emitBadge()
		return
	}
	for range c.Horizontal().KeepIter() {
		emitBadge()
		if sub := inst.subscriptText(); sub != "" {
			c.AddSpace(styletokens.GapInline(inst.density))
			subAtoms := c.Atoms().BeginRichTextColored(
				color.Hex(styletokens.NeutralTextSecondary.AsHex()),
				color.Transparent, sub,
			).Small().End().Keep()
			c.LabelAtoms(subAtoms).Send()
		}
	}
}

// subscriptText resolves the "Xs ago" rendering of the last transition's
// timestamp via dustin/go-humanize. Returns "" when no transition has
// fired yet or the recorded timestamp is the zero value (maxHistory=0).
func (inst *Widget[T]) subscriptText() string {
	last, ok := inst.machine.LastTransition()
	if !ok || last.At.IsZero() {
		return ""
	}
	return humanizeOrAbsolute(last.At)
}

func (inst *Widget[T]) renderPopup() {
	// Title format: "<Name> · <CurrentState>" — the name disambiguates
	// among multiple FSM popups, the current state tells the operator
	// what the popup is showing without scrolling the body.
	title := fmt.Sprintf("%s · %s", inst.title, inst.machine.Label(inst.machine.Current()))
	win := c.Window(inst.ids.PrepareStr("popup"), c.WidgetText().Text(title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		MinWidth(360).
		MinHeight(240)
	if inst.tethered {
		// Tethered inspectors stay foreground (matching distsummary /
		// regexsummary) so the window the bezier points at can't fall behind
		// the panes it's anchored from.
		win = win.AlwaysOnTop(true)
	}
	if inst.popupAnchor != nil {
		win = win.DefaultPos(inst.popupAnchor.X, inst.popupAnchor.Y)
	}
	// Wire the native egui::Window title-bar X to popupOpen via the
	// .open(&mut bool) idiom (feedback_egui_native_affordances /
	// ADR-0026). Per-frame registration is required because R10
	// databindings reset every Sync — same one-frame lag as Checkbox /
	// RadioButton: clicking X on frame N flips popupOpen to false at
	// end-of-frame, then frame N+1 skips renderPopup entirely.
	bindId := win.Id()
	win = win.OpenBound(bindId)
	c.CurrentApplicationState.StateManager.AddR10Databinding(bindId, &inst.popupOpen)
	for range win.KeepIter() {
		if inst.tethered {
			// Stamp the window content rect first (before any content shifts
			// min_rect) so the bezier tether anchors to the window edge.
			inst.tether.CaptureWindow()
		}
		c.AddSpace(styletokens.PaddingInner(inst.density))
		if !inst.provenance.IsZero() {
			inspector.ProvenanceChip(inst.provenance)
			c.Separator().Horizontal().Send()
		}
		inst.renderRendererToggle()
		c.Separator().Horizontal().Send()
		switch inst.renderer {
		case RendererGraph:
			inst.renderGraph()
		case RendererHistory:
			inst.renderHistory()
		default:
			inst.renderTable()
		}
		c.AddSpace(styletokens.PaddingInner(inst.density))
	}
}

func (inst *Widget[T]) renderRendererToggle() {
	historyLabel := fmt.Sprintf("History (%d)", inst.machine.HistoryLen())
	selector.Segmented(inst.ids, "renderer-tabs", &inst.renderer).
		Style(selector.StyleSelectable).
		Gap(styletokens.GapInline(inst.density)).
		Option(RendererTable, "Table").
		Option(RendererGraph, "Graph").
		Option(RendererHistory, historyLabel).
		SendResp()
}

// renderTable emits a labelled key→value row per state, with the active
// state highlighted via badge.TonePrimary. Outgoing transitions are listed
// as a comma-separated string in the second column.
func (inst *Widget[T]) renderTable() {
	current := inst.machine.Current()
	for s := range inst.machine.States() {
		for range c.Horizontal().KeepIter() {
			tone := badge.ToneNeutral
			variant := badge.VariantSoft
			if s == current {
				tone = badge.TonePrimary
				variant = badge.VariantSolid
			}
			badge.New(inst.ids.PrepareStr(fmt.Sprintf("st-%d", inst.machine.NodeId(s))),
				inst.machine.Label(s)).
				Tone(tone).
				Variant(variant).
				Size(badge.SizeSm).
				Send()
			c.AddSpace(styletokens.GapInline(inst.density))
			c.Label(formatOutgoing(inst.machine, s)).Send()
		}
	}
}

// renderGraph draws the FSM as a static layered (Sugiyama) graph: Graphviz
// lays out states + transitions in-process (layeredgraph + goccyengine,
// ADR-0069) and view.Render paints the result through the painter binding —
// no egui_graphs and no force simulation. The layout is computed once and
// cached (topology is static); per frame only the colours change — the active
// state keeps the Machine's StateColorFn tint, edges leaving the current state
// light up with AccentSubtle (the next-possible transitions) and the rest sit
// in NeutralBorderFaint.
func (inst *Widget[T]) renderGraph() {
	// The Machine topology can grow at runtime (Mirror/AddRule), so recompute
	// the cached layout when the state/edge counts change. The `graphLayout ==
	// nil` term also retries while no layout has been built yet, so a transient
	// engine failure recovers on a later frame instead of sticking forever.
	nodeN, edgeN := inst.machineTopology()
	if inst.graphLayout == nil || nodeN != inst.graphTopoN || edgeN != inst.graphTopoE {
		inst.graphLayout, inst.graphLayoutErr = inst.computeGraphLayout()
		inst.graphTopoN, inst.graphTopoE = nodeN, edgeN
		inst.graphIDToState = inst.buildIDToState()
	}
	if inst.graphLayoutErr != nil {
		c.Label("graph layout unavailable: " + inst.graphLayoutErr.Error()).Send()
		return
	}
	if inst.graphLayout == nil {
		return
	}

	current := inst.machine.Current()
	currentID := inst.stateNodeID(current)
	idToState := inst.graphIDToState
	nextEdgeColor := color.Hex(styletokens.AccentSubtle.AsHex())
	restEdgeColor := color.Hex(styletokens.NeutralBorderFaint.AsHex())

	res := view.Render(inst.graphIDBase(), inst.graphLayout, view.RenderOpts{
		CanvasW: fsmGraphCanvasW,
		CanvasH: fsmGraphCanvasH,
		State:   &inst.graphViewState,
		NodeFill: func(id string) (color.Color, bool) {
			if s, ok := idToState[id]; ok {
				return color.Hex(inst.machine.Color(s).AsHex()), true
			}
			return color.Hex(0), false
		},
		EdgeStroke: func(from, _ string) (color.Color, bool) {
			if from == currentID {
				return nextEdgeColor, true
			}
			return restEdgeColor, true
		},
	})

	// Click a state node to drive the FSM to it, when that transition is
	// declared from the current state (mirrors the "Drive the FSM" buttons).
	if res.Clicked != "" {
		if s, ok := idToState[res.Clicked]; ok && s != current && inst.machine.CanTransition(s) {
			_ = inst.machine.Transition(s)
		}
	}
}

// computeGraphLayout builds the GraphModel from the Machine (states → nodes,
// transitions → edges) and lays it out with the process-shared Graphviz
// engine. Called by renderGraph whenever the cached layout is missing or the
// topology changed; the result is cached on the receiver. States that share a
// node id (same label) are merged by the engine, mirroring how the user reads
// them as one state.
func (inst *Widget[T]) computeGraphLayout() (*layeredgraph.Layout, error) {
	eng, err := goccyengine.Shared()
	if err != nil {
		return nil, err
	}
	var m layeredgraph.GraphModel
	for s := range inst.machine.States() {
		m.Nodes = append(m.Nodes, layeredgraph.Node{
			ID:    inst.stateNodeID(s),
			Label: inst.machine.Label(s),
		})
	}
	for k, label := range inst.machine.Edges() {
		m.Edges = append(m.Edges, layeredgraph.Edge{
			From:  inst.stateNodeID(k.From),
			To:    inst.stateNodeID(k.To),
			Label: label,
		})
	}
	return eng.Layout(context.Background(), m, layeredgraph.LayoutOpts{
		RankDir:  layeredgraph.RankDirTopBottom,
		FontSize: 14,
	})
}

// stateNodeID is the layeredgraph node id for a state: the Machine's stable
// per-state NodeId as a string (Graphviz node names are strings).
func (inst *Widget[T]) stateNodeID(s T) string {
	return strconv.FormatUint(inst.machine.NodeId(s), 10)
}

// graphIDBase namespaces this widget's canvas + sense-region ids so two FSM
// graphs on screen do not collide. Derived from the per-instance scopeKey.
func (inst *Widget[T]) graphIDBase() uint64 {
	return xxh3.HashString(inst.scopeKey)
}

// machineTopology returns the current state and edge counts — a cheap,
// allocation-free signal for detecting runtime topology growth (Mirror /
// AddRule) so renderGraph can invalidate the cached layout.
func (inst *Widget[T]) machineTopology() (nodes, edges int) {
	for range inst.machine.States() {
		nodes++
	}
	for range inst.machine.Edges() {
		edges++
	}
	return
}

// buildIDToState maps each state's node id back to the state for the colour and
// click hooks. Rebuilt only when the cached layout is (re)computed, not per
// frame.
func (inst *Widget[T]) buildIDToState() map[string]T {
	m := make(map[string]T, inst.graphTopoN)
	for s := range inst.machine.States() {
		m[inst.stateNodeID(s)] = s
	}
	return m
}

// fsmGraphCanvas{W,H} size the painter canvas the layered graph is drawn into
// inside the level-2 popup. Fixed for v1 (the height matches the prior graph's
// 320px); the layout is fit-to-view into this rect. Responsive width tracking
// is a follow-up.
const (
	fsmGraphCanvasW float32 = 380
	fsmGraphCanvasH float32 = 280
)

// fsmHistCol* size the History tab's columns. The always-emitted five sum to
// just inside the popup's MinWidth, so a default-sized window needs no
// horizontal scroll; every column but the ordinal is resizable, and the
// reason column — emitted only when some retained row carries one — sits last
// so it is what gives when the window is narrow.
const (
	fsmHistColSeq    float32 = 30
	fsmHistColState  float32 = 88
	fsmHistColWhen   float32 = 92
	fsmHistColDwell  float32 = 56
	fsmHistColReason float32 = 120
	// fsmHistoryRowH leaves room for a SizeSm badge plus breathing space
	// from the row gridlines — the same 28px budget logviewer uses for the
	// same badge-in-a-cell shape.
	fsmHistoryRowH float32 = 28
	// fsmHistoryMaxH caps the table's own height, and with it the popup's.
	// Left to the framework the cap is 400px, which — stacked on the title
	// bar, the tab row and the padding — runs a filled log off the bottom of
	// a short viewport. Nine rows is enough to read a burst of transitions
	// without the popup dominating whatever it floats over.
	fsmHistoryMaxH float32 = 252
	// fsmHistoryScrollbarH mirrors the allowance the framework's own height
	// heuristic makes for the horizontal scrollbar, so the natural height
	// computed here agrees with the one it would have computed.
	fsmHistoryScrollbarH float32 = 16
)

// fsmHistId* namespace the History tab's PrepareSeq ids away from each other
// (and leave room for future per-row sites). Sequence ids and label-hashed
// ids share one 64-bit space, so the bases only need to separate this
// widget's own seq users.
const (
	fsmHistIdFrom uint64 = 0x0001_0000
	fsmHistIdTo   uint64 = 0x0002_0000
)

// renderHistory emits the transition log newest-first as a scrolling table:
// ordinal, from-state, to-state, when it fired, how long the machine had sat
// in the from-state, and the optional reason.
//
// It is an ETable rather than a flow of rows because the popup Window has no
// scroll of its own (egui's Window does not scroll by default, and the
// binding exposes no option for it) — a 64-entry log emitted as plain rows
// grows the window past the screen with no way to reach the tail. ETable
// bounds itself, scrolls internally, and lets the per-frame emission be
// gated on [c.EndETableFluid.VisibleRange] so only drawn rows build cells.
//
// Empty history shows a single muted "no transitions yet" line so the panel
// doesn't read as broken.
func (inst *Widget[T]) renderHistory() {
	rows := inst.historyRows()
	if len(rows) == 0 {
		emptyAtoms := c.Atoms().BeginRichTextColored(
			color.Hex(styletokens.NeutralTextSecondary.AsHex()),
			color.Transparent, "no transitions yet").
			Small().End().Keep()
		c.LabelAtoms(emptyAtoms).Send()
		inst.renderHistoryFooter()
		return
	}

	// The reason column costs its width in every popup, so it is offered only
	// when the machine actually records one — [Machine.MirrorWithMetadata]
	// callers (e.g. a validity FSM's rejection text). Plain Transition /
	// Mirror histories keep the narrower five-column table.
	numCols := uint32(5)
	for i := range rows {
		if rows[i].tr.Metadata["reason"] != "" {
			numCols = 6
			break
		}
	}

	// Every column carries a floor, not just a width: egui_table fits a
	// column to its *cell* content, which for the narrow columns is shorter
	// than the header word above them — without the floor, "Dwell" renders
	// clipped over a column of "34ms".
	c.EtColumn(fsmHistColSeq).Resizable(false).RangeMinMax(28, 60).Send()
	c.EtColumn(fsmHistColState).Resizable(true).RangeMinMax(56, 240).Send()
	c.EtColumn(fsmHistColState).Resizable(true).RangeMinMax(56, 240).Send()
	c.EtColumn(fsmHistColWhen).Resizable(true).RangeMinMax(92, 240).Send()
	c.EtColumn(fsmHistColDwell).Resizable(true).RangeMinMax(52, 140).Send()
	if numCols == 6 {
		c.EtColumn(fsmHistColReason).Resizable(true).RangeMinMax(80, 400).Send()
	}

	// Bound the table at the smaller of what the rows need and the cap, so a
	// three-entry log stays three rows tall instead of reserving the cap.
	naturalH := fsmHistoryRowH*float32(len(rows)+1) + fsmHistoryScrollbarH
	et := c.EndETable(inst.ids.PrepareStr("history-table"),
		uint64(len(rows)), fsmHistoryRowH, 1, 0).
		Striped(true).
		MaxHeight(min(naturalH, fsmHistoryMaxH))

	cellPadX := styletokens.PaddingTight(inst.density)
	headers := [...]string{"#", "From", "To", "When", "Dwell", "Reason"}
	for col := uint32(0); col < numCols; col++ {
		for range et.Headers(0, col) {
			c.AddSpace(cellPadX)
			for rt := range c.RichTextLabel(headers[col]) {
				rt.Strong().Small()
			}
		}
	}

	mutedFg := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	muted := func(text string) {
		c.AddSpace(cellPadX)
		atoms := c.Atoms().BeginRichTextColored(mutedFg, color.Transparent, text).
			Small().End().Keep()
		c.LabelAtoms(atoms).Send()
	}
	stateBadge := func(idBase uint64, seq int, label string) {
		c.AddSpace(cellPadX)
		badge.New(inst.ids.PrepareSeq(idBase+uint64(seq)), label).
			Tone(badge.ToneNeutral).
			Variant(badge.VariantSoft).
			Size(badge.SizeSm).
			Send()
	}

	// Emit only the rows egui_table will draw. VisibleRange reports the
	// previous frame's window (one-frame lag, self-correcting) and is absent
	// on the first frame a table is shown, where the full range is emitted.
	rowLo, rowHi := uint64(0), uint64(len(rows))
	if rb, re, _, _, _, ok := et.VisibleRange(); ok {
		// Clamped both ways: the reported window describes the PREVIOUS
		// frame's table, so after the log shrinks (a shorter history bound to
		// the same widget) it can name rows this frame no longer has.
		rowHi = min(re, rowHi)
		rowLo = min(rb, rowHi)
	}
	for i := rowLo; i < rowHi; i++ {
		// rows is oldest-first (dwell reads off the predecessor); the table
		// shows newest-first, so row index i maps to the tail.
		r := rows[len(rows)-1-int(i)]
		for range et.Cells(i, 0) {
			muted(strconv.Itoa(r.seq))
		}
		for range et.Cells(i, 1) {
			stateBadge(fsmHistIdFrom, r.seq, inst.machine.Label(r.tr.From))
		}
		for range et.Cells(i, 2) {
			stateBadge(fsmHistIdTo, r.seq, inst.machine.Label(r.tr.To))
		}
		for range et.Cells(i, 3) {
			muted(humanizeOrAbsolute(r.tr.At))
		}
		for range et.Cells(i, 4) {
			if r.hasDwell {
				muted(compactDuration(r.dwell))
			} else {
				muted("—")
			}
		}
		if numCols == 6 {
			for range et.Cells(i, 5) {
				muted(r.tr.Metadata["reason"])
			}
		}
	}
	et.Send()
	inst.renderHistoryFooter()
}

// historyRows rebuilds the History tab's row set into the receiver-held
// buffer, oldest-first, deriving each entry's dwell from its predecessor's
// timestamp. Oldest-first is the build order because dwell is only defined
// against the preceding transition; the renderer walks it backwards.
//
// A machine's history is contiguous — [Machine.Transition] records from the
// current state and a same-state [Machine.Mirror] is a no-op — so the
// predecessor's timestamp is when the machine entered this row's From state.
// The one gap is the oldest retained row, whose predecessor the ring has
// evicted (or never had): it reports no dwell rather than a wrong one.
func (inst *Widget[T]) historyRows() []historyRow[T] {
	inst.historyBuf = inst.historyBuf[:0]
	var prevAt time.Time
	for tr := range inst.machine.History() {
		row := historyRow[T]{seq: len(inst.historyBuf) + 1, tr: tr}
		row.dwell, row.hasDwell = dwellBetween(prevAt, tr.At)
		prevAt = tr.At
		inst.historyBuf = append(inst.historyBuf, row)
	}
	return inst.historyBuf
}

// dwellBetween reports how long the machine sat in a transition's From state,
// given the preceding transition's timestamp. Reports ok=false rather than a
// wrong number in the three cases where the answer isn't known: no
// predecessor (the oldest retained row, or the ring evicted it), a missing
// timestamp (maxHistory=0), and a pair that runs backwards — wall-clock can
// step back under NTP, and a negative dwell reads as data, not as a clock.
func dwellBetween(prevAt, at time.Time) (d time.Duration, ok bool) {
	if prevAt.IsZero() || at.IsZero() || at.Before(prevAt) {
		return 0, false
	}
	return at.Sub(prevAt), true
}

// renderHistoryFooter emits the caller-owned action row under the table, and
// the separator that sets it off. No-op when no footer was set — a widget
// without one shows neither, so the plain History tab is unchanged.
func (inst *Widget[T]) renderHistoryFooter() {
	if inst.historyFooterFn == nil {
		return
	}
	c.AddSpace(styletokens.GapInline(inst.density))
	c.Separator().Horizontal().Send()
	for range c.Horizontal().KeepIter() {
		inst.historyFooterFn()
	}
}

// compactDuration renders a dwell short enough for a narrow cell: sub-second
// in whole milliseconds, then one decimal of seconds, then Go's own m/h form
// truncated — so a long dwell reads "2m30s", never "2m30.000481922s".
func compactDuration(d time.Duration) string {
	switch {
	case d < 0:
		return "—"
	case d < time.Second:
		return d.Truncate(time.Millisecond).String()
	case d < time.Minute:
		return d.Truncate(100 * time.Millisecond).String()
	default:
		return d.Truncate(time.Second).String()
	}
}

// humanizeOrAbsolute keeps the rendering compact for recent transitions
// ("23s ago", "2m ago") and switches to an absolute UTC timestamp for
// anything older than a day, so a stale entry doesn't read as "1y ago"
// without disambiguation.
func humanizeOrAbsolute(at time.Time) string {
	if at.IsZero() {
		return "(no timestamp)"
	}
	if time.Since(at) > 24*time.Hour {
		return at.UTC().Format("2006-01-02 15:04 UTC")
	}
	return humanize.Time(at)
}

// formatOutgoing builds the comma-separated outgoing-transitions string for
// the table row.
func formatOutgoing[T comparable](m *Machine[T], from T) string {
	var out string
	for k := range m.Edges() {
		if k.From != from {
			continue
		}
		if out != "" {
			out += ", "
		}
		out += m.Label(k.To)
	}
	if out == "" {
		return "—"
	}
	return out
}
