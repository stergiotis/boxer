//go:build llm_generated_opus47

package fsmview

import (
	"fmt"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/badge"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/inspector"
)

// RendererE selects which level-2 view is rendered inside the popup. The
// table is cheaper at small N; the graph reads better once edges outnumber
// states (force-directed layout via egui_graphs, see [c.GraphLayoutForceDirectedCG]);
// history shows the transition log from oldest to newest.
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

	// graphPrewarmed flips to true after the first renderGraph call has
	// fired .ResetLayout()+.FastForwardSteps(N). egui_graphs's FR layout
	// initialises node positions coincident at (0, 0), so forces cancel
	// and the simulation appears frozen until a user interaction breaks
	// the symmetry; pre-warming converges the layout deterministically
	// before the operator sees the graph for the first time.
	graphPrewarmed bool

	density styletokens.DensityE

	// provenance, when non-zero, is rendered at the top of the popup
	// body as the standard [inspector.ProvenanceChip] so operators can
	// see which source value this FSM is bound to without leaving the
	// popup. Zero value (default) suppresses the chip entirely so
	// existing call sites keep their current visual.
	provenance inspector.Provenance
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
	}
}

func (inst *Widget[T]) renderChip() {
	current := inst.machine.Current()
	label := inst.machine.Label(current)
	emitBadge := func() {
		resp := badge.New(inst.ids.PrepareStr("chip"), label).
			Tone(badge.TonePrimary).
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

// renderGraph emits one GraphNode per state and one GraphEdge per
// transition, then closes with a Graph block running the force-directed-
// with-centre-gravity layout (egui_graphs FR+CG). The active state is
// tinted via the Machine's StateColorFn; edges leaving the current state
// light up with AccentSubtle so the operator reads the next-possible
// transitions at a glance, the rest sit in NeutralBorderFaint.
func (inst *Widget[T]) renderGraph() {
	current := inst.machine.Current()
	for s := range inst.machine.States() {
		rgba := inst.machine.Color(s)
		c.GraphNode(inst.machine.NodeId(s), inst.machine.Label(s)).
			Color(color.Hex(rgba.AsHex())).
			Send()
	}
	nextEdgeColor := color.Hex(styletokens.AccentSubtle.AsHex())
	restEdgeColor := color.Hex(styletokens.NeutralBorderFaint.AsHex())
	for k, label := range inst.machine.Edges() {
		e := c.GraphEdge(inst.machine.NodeId(k.From), inst.machine.NodeId(k.To))
		if k.From == current {
			e = e.Color(nextEdgeColor)
		} else {
			e = e.Color(restEdgeColor)
		}
		if label != "" {
			e = e.Label(label)
		}
		e.Send()
	}
	g := c.Graph(inst.ids.PrepareStr("graph")).
		Height(320).
		Layout(uint8(c.GraphLayoutForceDirectedCG)).
		LayoutDt(forceDirectedDt).
		LayoutDamping(forceDirectedDamping).
		LayoutEpsilon(forceDirectedEpsilon).
		LayoutMaxStep(forceDirectedMaxStep).
		LayoutKScale(forceDirectedKScale).
		LayoutCAttract(forceDirectedCAttract).
		LayoutCRepulse(forceDirectedCRepulse).
		LayoutRunning(true).
		FitToScreen(true).
		FitPadding(0.1).
		ZoomAndPan(true).
		DraggingEnabled(true).
		HoverEnabled(true).
		LabelsAlways(true)
	if !inst.graphPrewarmed {
		g = g.ResetLayout().FastForwardSteps(forceDirectedPrewarmSteps)
		inst.graphPrewarmed = true
	}
	g.Send()
}

// FR (Fruchterman-Reingold) defaults for the level-2 graph. egui_graphs
// integrates Verlet-style per-frame; without explicit values these fields
// default to 0 and the simulation never advances. The numbers mirror the
// graphs-demo defaults (egui2_hl_graphs_demo.go) which converge cleanly on
// small graphs (≤20 nodes). Knobs aren't yet surfaced on the public API —
// promote to widget options if a caller needs to tune them.
const (
	forceDirectedDt       float32 = 0.05
	forceDirectedDamping  float32 = 0.3
	forceDirectedEpsilon  float32 = 0.1
	forceDirectedMaxStep  float32 = 20.0
	forceDirectedKScale   float32 = 1.0
	forceDirectedCAttract float32 = 1.0
	forceDirectedCRepulse float32 = 1.0
	// forceDirectedPrewarmSteps is the number of FR simulation iterations
	// emitted via .FastForwardSteps on the first Graph-view frame so the
	// layout is converged before the operator looks. 200 is the value the
	// graphs demo's "fast-forward" button uses and converges small (≤20-
	// node) graphs to a stable shape; tune if larger FSMs settle slowly.
	forceDirectedPrewarmSteps uint32 = 200
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
