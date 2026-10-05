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

// popupAnchorXY is the (x, y) carrier for [View.PopupAnchor]. Held by
// pointer on the widget so callers can distinguish "no anchor" (nil →
// fall back to egui's default cascade) from "anchor at (0, 0)" (the
// viewport top-left, a legitimate value).
type popupAnchorXY struct{ X, Y float32 }

// Options configures a View. Every zero value is a default, and the View
// re-reads its [View.Opts] on every render, so a change is an assignment
// (ADR-0267 W11).
type Options[T comparable] struct {
	// Title is the human-facing name surfaced in the level-2 popup header
	// and the chip's tooltip, so callers with multiple FSMs on screen can
	// tell which machine each popup belongs to. Empty takes the scope key,
	// which is already a stable short string per instance.
	Title string
	// ShowSubscript renders the small "Xs ago" subscript to the right of the
	// chip, sourced from [Machine.LastTransition]. Off by default so a plain
	// chip stays as compact as possible; enable on surfaces where the
	// freshness of the state matters (status bars, dashboards).
	ShowSubscript bool
	// AutoAnchor captures the cursor position the frame the chip is clicked
	// and pins the popup there, so it pops where the click landed. Overrides
	// any manual [View.PopupAnchor] on the click frame. Backed by
	// [c.StateManager.GetPointer] (R20).
	AutoAnchor bool
	// Tethered promotes the level-1 chip to a tethered inspector summary: the
	// state badge gains an [inspector.AnchorToggle] and the level-2 window is
	// linked back to it by the spring-animated bezier [inspector.AnchorTether]
	// (ADR-0046). Pair with Summary for a rich stat line and Provenance for
	// the window's identity chip. Off by default — non-tethered widgets keep
	// the plain chip-click popup.
	Tethered bool
	// Renderer is the level-2 view a fresh View opens on (read once at New);
	// [View.SetRenderer] / [View.SelectedRenderer] are the live state.
	Renderer RendererE
	// Provenance binds the FSM to its source value's identity card. When set
	// (non-zero), the popup body renders the standard
	// [inspector.ProvenanceChip] in its header so operators can see which
	// subject / source-app produced the state transitions this FSM reflects.
	Provenance inspector.Provenance
	// BadgeTone colours the level-1 state badge by state (e.g. error states
	// red, success green); nil keeps TonePrimary. A pure function.
	BadgeTone func(T) badge.ToneE
	// Summary renders a caller-owned addendum (stats / freshness) just right
	// of the state badge in tethered mode. It runs inside the chip's
	// Horizontal, so emit inline widgets only. Host-drawn content (W13).
	Summary func()
	// HistoryFooter renders a caller-owned action row under the History tab's
	// table, below a separator — the place for whatever a host wants to do
	// with the transition log (publish it, copy it, hand it to a playground).
	// It runs inside a [c.Horizontal], so emit inline widgets only. The widget
	// deliberately supplies no action of its own: what a log is worth
	// exporting *to* depends on the host's capabilities. Pair with
	// [View.HistorySnapshot] inside the click branch to get the rows. nil
	// emits neither footer nor separator. Host-drawn content (W13).
	HistoryFooter func()
}

// Events is what one Render, RenderChip or RenderPopup reports.
type Events struct {
	// Toggled is true when the user opened or closed the popup this frame
	// (a chip or toggle click; the title-bar close lands a frame later
	// through the databinding and is not reported).
	Toggled bool
	// Driven is true when a click on a graph node drove the machine to a
	// new state this frame.
	Driven bool
}

// View is the two-level FSM viewer, a semi-retained widget (ADR-0267).
// Construct via [New], reuse across frames via [View.Render]. State (popup
// open/closed, selected renderer, the cached graph layout) lives on the
// receiver so multiple views coexist without crosstalk.
type View[T comparable] struct {
	// Opts is re-read on every render.
	Opts Options[T]

	ids      *c.WidgetIdStack
	scopeKey string
	machine  *Machine[T]

	popupOpen bool
	renderer  RendererE
	// events collects what this frame's render produced.
	events Events

	// popupAnchor pins the level-2 Window's default_pos on first open
	// (egui-relative viewport coordinates in logical pixels). nil leaves
	// egui's cascade behaviour intact. egui retains the user's dragged
	// position across subsequent opens, so the anchor only affects the
	// very first time a fresh widget instance shows the popup. Set via
	// [View.PopupAnchor]; unset via [View.ClearPopupAnchor].
	popupAnchor *popupAnchorXY

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
	// graphCanvasW/H hold the last space the window left for the graph, so a
	// frame whose probe has not landed draws at the last good size rather
	// than flashing at the fallback.
	graphCanvasW, graphCanvasH float32

	density styletokens.DensityE

	// tether links the tethered chip's toggle to the popup window. Its key
	// is derived from the id stack on the first render inside the view's
	// scope (a scope-derived id spelled in hex — the inspector infra takes a
	// string), so two views under different host scopes never share it
	// (W6/W7); tetherKey is 0 until then.
	tether    inspector.AnchorTether
	tetherKey uint64

	// historyBuf backs the History tab's per-frame row build. Held on the
	// receiver and truncated rather than reallocated, so an open History tab
	// costs no allocation per frame. Never escapes — callers reach the log
	// through [View.HistorySnapshot], which copies.
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

// New constructs a View bound to the given Machine, with its ids scoped
// under scopeKey on ids — pass a stable short string per instance
// ("door-fsm", "card-status", …) so two views on the same id stack don't
// collide; empty uses "fsmview".
//
// Panics on a nil ids stack or a nil machine — programmer errors, not
// data-shape issues.
func New[T comparable](ids *c.WidgetIdStack, scopeKey string, m *Machine[T], opts Options[T]) *View[T] {
	if ids == nil {
		panic("fsmview: New requires a non-nil ids stack")
	}
	if m == nil {
		panic("fsmview: New requires a non-nil Machine")
	}
	if scopeKey == "" {
		scopeKey = "fsmview"
	}
	return &View[T]{
		Opts:     opts,
		ids:      ids,
		scopeKey: scopeKey,
		machine:  m,
		renderer: opts.Renderer,
		density:  styletokens.ActiveDensity(),
	}
}

// title is the popup / tooltip name: Opts.Title, or the scope key.
func (inst *View[T]) title() string {
	if inst.Opts.Title != "" {
		return inst.Opts.Title
	}
	return inst.scopeKey
}

// ensureTether derives the tether key from the id stack the first time it is
// needed inside the view's scope, and builds the tether.
func (inst *View[T]) ensureTether() {
	if inst.tetherKey != 0 {
		return
	}
	inst.tetherKey = inst.ids.PrepareStr("tether").Derive()
	inst.tether = inspector.NewAnchorTether(inst.tetherKey)
}

// IsOpen reports whether the level-2 popup is currently open. Useful for
// drift-guard tests and for sibling widgets that want to react to the
// popup state.
func (inst *View[T]) IsOpen() bool {
	return inst.popupOpen
}

// Open programmatically opens the popup. No-op when already open.
func (inst *View[T]) Open() {
	inst.popupOpen = true
}

// Close programmatically dismisses the popup.
func (inst *View[T]) Close() {
	inst.popupOpen = false
}

// SelectedRenderer returns the currently-selected level-2 view.
func (inst *View[T]) SelectedRenderer() RendererE {
	return inst.renderer
}

// SetRenderer pins which level-2 view opens on the next click.
func (inst *View[T]) SetRenderer(r RendererE) {
	inst.renderer = r
}

// PopupAnchor pins the level-2 Window's default_pos to (x, y) in egui
// logical pixels (viewport top-left origin). Applies on the first open
// of a fresh widget instance; egui remembers the user's dragged position
// after that. Returns the receiver for chaining.
//
// For click-tracking (popup pops where the chip was clicked) see
// [Options.AutoAnchor], which captures the pointer via the R20 fetcher on
// each click. The two compose: AutoAnchor overrides the stored anchor on
// each click frame, while PopupAnchor remains the fallback for programmatic
// [View.Open] calls where no click happens. Idempotent.
func (inst *View[T]) PopupAnchor(x, y float32) {
	inst.popupAnchor = &popupAnchorXY{X: x, Y: y}
}

// ClearPopupAnchor reverts the popup to egui's default cascade
// positioning. No-op when no anchor has been set.
func (inst *View[T]) ClearPopupAnchor() {
	inst.popupAnchor = nil
}

// HistorySnapshot returns a freshly allocated copy of the retained transition
// log, newest-first — the order the History tab shows. Unlike
// [Machine.HistoryReverse] it hands back plain data that outlives the frame,
// so it is safe to pass to a worker goroutine (the render-thread-snapshot
// rule: gather here, do the blocking work there).
//
// Allocates on every call. Call it in a click branch, not per frame.
func (inst *View[T]) HistorySnapshot() []Transition[T] {
	out := make([]Transition[T], 0, inst.machine.HistoryLen())
	for tr := range inst.machine.HistoryReverse() {
		out = append(out, tr)
	}
	return out
}

// Render emits the level-1 chip and, when open, the level-2 popup (window +
// tether). Call once per frame inside an active egui surface (panel or window).
// It is exactly [View.RenderChip] followed by [View.RenderPopup] in one id
// scope — reach for those two directly only when the chip and the window must
// render at different points in the frame, e.g. the chip inside a dock-tab body
// and the window AFTER the DockArea block (a floating window cannot be spawned
// from inside a dock tab — see schemaview's glyph legend for the same pattern).
//
// The chip renders inline at the current cursor — embed it inside a
// [c.Horizontal] flow or a panel. The popup spawns at egui's default
// cascade position (egui_dock-style retention takes over on subsequent
// frames so user-driven drag positions stick).
func (inst *View[T]) Render() (ev Events) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.events = Events{}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderChip()
		inst.renderPopupAndTether()
	}
	return inst.events
}

// RenderChip emits only the level-1 chip: the state badge, and in tethered mode
// the [inspector.AnchorToggle] plus the toggle-rect capture the bezier anchors
// on. Pair with [View.RenderPopup] later in the same frame when the window
// must be emitted away from the chip's call site (the dock-tab case). Callers
// using all-in-one [View.Render] never need this.
func (inst *View[T]) RenderChip() (ev Events) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.events = Events{}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderChip()
	}
	return inst.events
}

// RenderPopup emits the level-2 popup window (when open) and, in tethered mode,
// paints the bezier connector back to the toggle captured by [View.RenderChip].
// Call it where a floating window is legal — notably AFTER a DockArea block,
// never inside a dock-tab body. No-op when the popup is closed. The toggle↔window
// link is by scope (the tether), so the two calls need not be nested.
func (inst *View[T]) RenderPopup() (ev Events) {
	// Re-resolve: the density preset is runtime-switchable (Layout ▸ Density).
	inst.density = styletokens.ActiveDensity()
	inst.events = Events{}
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderPopupAndTether()
	}
	return inst.events
}

// renderPopupAndTether is the shared popup half: the window when open, then the
// bezier (tethered + open). Factored out so [View.Render] and
// [View.RenderPopup] stay in lock-step.
func (inst *View[T]) renderPopupAndTether() {
	if inst.popupOpen {
		inst.renderPopup()
	}
	// Tethered mode: draw the bezier from the level-1 toggle to the open
	// window above everything (PaintAbsoluteOverlay). One-frame lag on
	// first open; gated on popupOpen so the curve vanishes with it.
	if inst.Opts.Tethered && inst.popupOpen {
		inst.ensureTether()
		inst.tether.Paint()
	}
}

func (inst *View[T]) renderChip() {
	current := inst.machine.Current()
	label := inst.machine.Label(current)
	tone := badge.TonePrimary
	if inst.Opts.BadgeTone != nil {
		tone = inst.Opts.BadgeTone(current)
	}
	emitBadge := func() {
		resp := badge.New(inst.ids.PrepareStr("chip"), label).
			Tone(tone).
			Variant(badge.VariantSolid).
			Size(badge.SizeMd).
			Tooltip(fmt.Sprintf("%s — click for state-machine details", inst.title())).
			SendResp()
		if resp.HasPrimaryClicked() {
			inst.popupOpen = !inst.popupOpen
			inst.events.Toggled = true
			// AutoAnchor: snapshot the pointer at the moment of the click
			// and pin the popup to it. The R20 fetcher returns the latest
			// observed pointer position from egui's InputState, which
			// reflects the position the click landed on (one-frame lag is
			// already absorbed by the response cache that gates this
			// branch). Skip on Valid=false (headless / pre-first-pointer).
			if inst.Opts.AutoAnchor && inst.popupOpen {
				p := c.CurrentApplicationState.StateManager.GetPointer()
				if p.Valid {
					inst.popupAnchor = &popupAnchorXY{X: p.X, Y: p.Y}
				}
			}
		}
	}
	if inst.Opts.Tethered {
		inst.ensureTether()
		// Tethered inspector summary: badge · caller summary · AnchorToggle,
		// then stamp the row rect for the bezier tether.
		for range c.Horizontal().KeepIter() {
			emitBadge()
			if inst.Opts.Summary != nil {
				c.AddSpace(styletokens.GapInline(inst.density))
				inst.Opts.Summary()
			}
			c.AddSpace(styletokens.GapInline(inst.density))
			if inspector.AnchorToggle(inst.ids.PrepareStr("anchor-toggle"), &inst.popupOpen) {
				inst.events.Toggled = true
				// Same AutoAnchor pointer-capture as the badge click, so the
				// window opens near the toggle and the bezier stays short.
				if inst.Opts.AutoAnchor && inst.popupOpen {
					if p := c.CurrentApplicationState.StateManager.GetPointer(); p.Valid {
						inst.popupAnchor = &popupAnchorXY{X: p.X, Y: p.Y}
					}
				}
			}
			inst.tether.CaptureToggle()
		}
		return
	}
	if !inst.Opts.ShowSubscript {
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
func (inst *View[T]) subscriptText() string {
	last, ok := inst.machine.LastTransition()
	if !ok || last.At.IsZero() {
		return ""
	}
	return humanizeOrAbsolute(last.At)
}

func (inst *View[T]) renderPopup() {
	// Title format: "<Name> · <CurrentState>" — the name disambiguates
	// among multiple FSM popups, the current state tells the operator
	// what the popup is showing without scrolling the body.
	title := fmt.Sprintf("%s · %s", inst.title(), inst.machine.Label(inst.machine.Current()))
	// The floating window is the one absolute id the view owns, derived from
	// its scope (ADR-0267 W6).
	winId := c.MakeAbsoluteIdHighEntropy(inst.ids.PrepareStr("popup").Derive())
	win := c.Window(winId, c.WidgetText().Text(title).Keep()).
		DefaultOpen(true).
		Resizable(true).
		Collapsible(false).
		DefaultSize(fsmPopupDefaultW, fsmPopupDefaultH).
		MinWidth(360).
		MinHeight(240)
	if inst.Opts.Tethered {
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
		if inst.Opts.Tethered {
			// Stamp the window content rect first (before any content shifts
			// min_rect) so the bezier tether anchors to the window edge.
			inst.ensureTether()
			inst.tether.CaptureWindow()
		}
		c.AddSpace(styletokens.PaddingInner(inst.density))
		if !inst.Opts.Provenance.IsZero() {
			inspector.ProvenanceChip(inst.Opts.Provenance)
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

func (inst *View[T]) renderRendererToggle() {
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
func (inst *View[T]) renderTable() {
	current := inst.machine.Current()
	for range c.IdScope(inst.ids.PrepareStr("states")) {
		for s := range inst.machine.States() {
			for range c.Horizontal().KeepIter() {
				tone := badge.ToneNeutral
				variant := badge.VariantSoft
				if s == current {
					tone = badge.TonePrimary
					variant = badge.VariantSolid
				}
				// NodeId is a hash of the state's label, so it is a
				// high-entropy id already.
				badge.New(inst.ids.PrepareHighEntropy(inst.machine.NodeId(s)),
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
}

// renderGraph draws the FSM as a static layered (Sugiyama) graph: Graphviz
// lays out states + transitions in-process (layeredgraph + goccyengine,
// ADR-0069) and view.Render paints the result through the painter binding —
// no egui_graphs and no force simulation. The layout is computed once and
// cached (topology is static); per frame only the colours change — the active
// state keeps the Machine's StateColorFn tint, edges leaving the current state
// light up with AccentSubtle (the next-possible transitions) and the rest sit
// in NeutralBorderFaint.
func (inst *View[T]) renderGraph() {
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

	canvasW, canvasH := inst.graphCanvasSize()
	// The layered view takes a raw id base; a scope-derived id keeps two
	// views' canvases apart (W5).
	res := view.Render(inst.ids.PrepareStr("graph").Derive(), inst.graphLayout, view.RenderOpts{
		CanvasW: canvasW,
		CanvasH: canvasH,
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
			if inst.machine.Transition(s) == nil {
				inst.events.Driven = true
			}
		}
	}
}

// computeGraphLayout builds the GraphModel from the Machine (states → nodes,
// transitions → edges) and lays it out with the process-shared Graphviz
// engine. Called by renderGraph whenever the cached layout is missing or the
// topology changed; the result is cached on the receiver. States that share a
// node id (same label) are merged by the engine, mirroring how the user reads
// them as one state.
func (inst *View[T]) computeGraphLayout() (*layeredgraph.Layout, error) {
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
func (inst *View[T]) stateNodeID(s T) string {
	return strconv.FormatUint(inst.machine.NodeId(s), 10)
}

// machineTopology returns the current state and edge counts — a cheap,
// allocation-free signal for detecting runtime topology growth (Mirror /
// AddRule) so renderGraph can invalidate the cached layout.
func (inst *View[T]) machineTopology() (nodes, edges int) {
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
func (inst *View[T]) buildIDToState() map[string]T {
	m := make(map[string]T, inst.graphTopoN)
	for s := range inst.machine.States() {
		m[inst.stateNodeID(s)] = s
	}
	return m
}

// graphCanvasSize is the space the popup leaves for the graph: the window's
// remaining rect, as last frame measured it, less what the popup adds below
// the body — the item gap egui leaves after the canvas, then the padding.
// The canvas filling it is what lets the window be resized: a fixed-size body
// holds egui's window to that size. The probe runs before the canvas, so the
// canvas never sizes itself against its own output. The subtraction has to
// cover everything below the canvas: a resizable egui window grows to its
// content, so any shortfall is added to the window each frame and the window
// creeps taller without end.
func (inst *View[T]) graphCanvasSize() (w, h float32) {
	aw, ah, ok := c.CapturePaneSize(inst.ids.PrepareStr("graph-canvas").Derive())
	if ok && aw > 0 && ah > 0 {
		below := styletokens.GapItems(inst.density) + styletokens.PaddingInner(inst.density) + fsmGraphCanvasSlack
		inst.graphCanvasW = max(aw, fsmGraphCanvasMinW)
		inst.graphCanvasH = max(ah-below, fsmGraphCanvasMinH)
	}
	if inst.graphCanvasW <= 0 || inst.graphCanvasH <= 0 {
		return fsmGraphCanvasFallbackW, fsmGraphCanvasFallbackH
	}
	return inst.graphCanvasW, inst.graphCanvasH
}

// fsmPopup* and fsmGraphCanvas* size the level-2 popup and the graph inside
// it. The popup opens large enough for a graph of a dozen states to read;
// the graph then fills whatever the window leaves it, and the layout is
// fit-to-view into that rect. The fallback is the first frame's, before the
// probe has landed. The slack keeps the canvas a few pixels short of the
// window's bottom so rounding cannot make the window grow by itself.
const (
	fsmPopupDefaultW        float32 = 720
	fsmPopupDefaultH        float32 = 560
	fsmGraphCanvasFallbackW float32 = 380
	fsmGraphCanvasFallbackH float32 = 280
	fsmGraphCanvasMinW      float32 = 200
	fsmGraphCanvasMinH      float32 = 160
	fsmGraphCanvasSlack     float32 = 4
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
func (inst *View[T]) renderHistory() {
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

	// A ceiling, not a height: the table takes what its rows need and stops
	// there, so a three-entry log stays three rows tall instead of
	// reserving the cap. (It used to hand over min(natural, cap) computed
	// here, back when maxHeight meant an exact height.)
	et := c.EndETable(inst.ids.PrepareStr("history-table"),
		uint64(len(rows)), fsmHistoryRowH, 1, 0).
		Striped(true).
		MaxHeight(fsmHistoryMaxH)

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
	// One scope per column, the row's ordinal within it: the two state
	// badges of one row stay apart without id-base arithmetic (W5).
	stateBadge := func(col string, seq int, label string) {
		c.AddSpace(cellPadX)
		for range c.IdScope(inst.ids.PrepareStr(col)) {
			badge.New(inst.ids.PrepareSeq(uint64(seq)), label).
				Tone(badge.ToneNeutral).
				Variant(badge.VariantSoft).
				Size(badge.SizeSm).
				Send()
		}
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
			stateBadge("from", r.seq, inst.machine.Label(r.tr.From))
		}
		for range et.Cells(i, 2) {
			stateBadge("to", r.seq, inst.machine.Label(r.tr.To))
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
func (inst *View[T]) historyRows() []historyRow[T] {
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
func (inst *View[T]) renderHistoryFooter() {
	if inst.Opts.HistoryFooter == nil {
		return
	}
	c.AddSpace(styletokens.GapInline(inst.density))
	c.Separator().Horizontal().Send()
	for range c.Horizontal().KeepIter() {
		inst.Opts.HistoryFooter()
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
