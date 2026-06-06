//go:build llm_generated_opus47

package fsmview

import (
	"context"
	"fmt"
	"hash/fnv"
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
)

// RendererE selects which level-2 view is rendered inside the popup. The
// table is cheaper at small N; the graph reads better once edges outnumber
// states (static layered / Sugiyama layout via Graphviz in-process, see the
// layeredgraph package and ADR-0069); history shows the transition log from
// oldest to newest.
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
		density:  styletokens.DensityFromEnv(),
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

// Render emits the level-1 chip and, when open, the level-2 popup. Call
// once per frame inside an active egui surface (panel or window).
//
// The chip renders inline at the current cursor — embed it inside a
// [c.Horizontal] flow or a panel. The popup spawns at egui's default
// cascade position (egui_dock-style retention takes over on subsequent
// frames so user-driven drag positions stick).
func (inst *Widget[T]) Render() {
	for range c.IdScope(inst.ids.PrepareStr(inst.scopeKey)) {
		inst.renderChip()
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
	for range c.Horizontal().KeepIter() {
		tableSel := inst.renderer == RendererTable
		graphSel := inst.renderer == RendererGraph
		historySel := inst.renderer == RendererHistory
		if c.SelectableLabel(inst.ids.PrepareStr("tab-tbl"), tableSel, "Table").SendResp().HasPrimaryClicked() {
			inst.renderer = RendererTable
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		if c.SelectableLabel(inst.ids.PrepareStr("tab-grp"), graphSel, "Graph").SendResp().HasPrimaryClicked() {
			inst.renderer = RendererGraph
		}
		c.AddSpace(styletokens.GapInline(inst.density))
		historyLabel := fmt.Sprintf("History (%d)", inst.machine.HistoryLen())
		if c.SelectableLabel(inst.ids.PrepareStr("tab-hist"), historySel, historyLabel).SendResp().HasPrimaryClicked() {
			inst.renderer = RendererHistory
		}
	}
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
	if inst.graphLayout == nil && inst.graphLayoutErr == nil {
		inst.graphLayout, inst.graphLayoutErr = inst.computeGraphLayout()
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
	// Reverse map node-id → state so the colour hooks can reach the Machine's
	// per-state colour. Cheap to rebuild each frame (few states).
	idToState := make(map[string]T)
	for s := range inst.machine.States() {
		idToState[inst.stateNodeID(s)] = s
	}
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
// engine. Called once per widget; the result is cached on the receiver.
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
	h := fnv.New64a()
	_, _ = h.Write([]byte(inst.scopeKey))
	return h.Sum64()
}

// fsmGraphCanvas{W,H} size the painter canvas the layered graph is drawn into
// inside the level-2 popup. Fixed for v1 (the height matches the prior graph's
// 320px); the layout is fit-to-view into this rect. Responsive width tracking
// is a follow-up.
const (
	fsmGraphCanvasW float32 = 380
	fsmGraphCanvasH float32 = 280
)

// renderHistory emits the transition log newest-first. Each row reads as
//
//	from → to    23s ago
//
// with the arrow tinted accent so the eye locks onto the direction. Empty
// history shows a single muted "no transitions yet" line so the panel
// doesn't read as broken.
func (inst *Widget[T]) renderHistory() {
	if inst.machine.HistoryLen() == 0 {
		emptyAtoms := c.Atoms().BeginRichTextColored(
			color.Hex(styletokens.NeutralTextSecondary.AsHex()),
			color.Transparent, "no transitions yet").
			Small().End().Keep()
		c.LabelAtoms(emptyAtoms).Send()
		return
	}
	arrowFg := color.Hex(styletokens.AccentDefault.AsHex())
	mutedFg := color.Hex(styletokens.NeutralTextSecondary.AsHex())
	idx := 0
	for t := range inst.machine.HistoryReverse() {
		for range c.Horizontal().KeepIter() {
			badge.New(inst.ids.PrepareStr(fmt.Sprintf("h-from-%d", idx)),
				inst.machine.Label(t.From)).
				Tone(badge.ToneNeutral).
				Variant(badge.VariantSoft).
				Size(badge.SizeSm).
				Send()
			c.AddSpace(styletokens.GapInline(inst.density))
			arrowAtoms := c.Atoms().BeginRichTextColored(arrowFg, color.Transparent, "→").
				Strong().End().Keep()
			c.LabelAtoms(arrowAtoms).Send()
			c.AddSpace(styletokens.GapInline(inst.density))
			badge.New(inst.ids.PrepareStr(fmt.Sprintf("h-to-%d", idx)),
				inst.machine.Label(t.To)).
				Tone(badge.ToneNeutral).
				Variant(badge.VariantSoft).
				Size(badge.SizeSm).
				Send()
			c.AddSpace(styletokens.GapInline(inst.density))
			when := humanizeOrAbsolute(t.At)
			whenAtoms := c.Atoms().BeginRichTextColored(mutedFg, color.Transparent, when).
				Small().End().Keep()
			c.LabelAtoms(whenAtoms).Send()
		}
		idx++
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
