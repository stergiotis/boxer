package play

import (
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/config/env"
	"github.com/stergiotis/boxer/public/observability/eh"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
)

// play_tabs.go is ADR-0097 slice 6a: the tab registry — every dock tab is a
// registered TabSpec, and Render's dock block is one loop over the set. Result
// panels are the specs carrying a PanelI; chrome registers with a nil Panel
// (SD7 preserved structurally, D1). The set is instance-scoped and frozen at
// the first Render (D4) so an embedder customizes it between construction and
// mounting, the same window SetDetailContent uses. Dock ids are frozen (D3):
// the Rust-side persisted dock layout keys on them — built-ins keep 1..13,
// embedder tabs allocate ≥64.

// TabZoneE places a tab in the initial dock layout (before the user drags —
// the persisted dock state wins afterwards). The zero value is the body zone,
// where embedder tabs land by default.
type TabZoneE uint8

const (
	TabZoneBody    TabZoneE = iota // the main body leaf (result views)
	TabZoneEditor                  // the editor leaf (Editor, History)
	TabZonePreview                 // split right of the editor leaf
	TabZoneSide                    // split right of the body leaf (Detail)
)

// TabFrame is the per-frame view a tab body renders from: the active result
// snapshot plus the frame's signal env and emitter. It decouples tab bodies
// from Render's locals; per-tab STATE stays wherever it lives today and
// migrates behind factories opportunistically (slice-6 D2).
type TabFrame struct {
	Rec      arrow.RecordBatch
	Schema   *arrow.Schema
	NumRows  int64
	Loading  bool
	Elapsed  time.Duration
	Summary  Summary
	Executed time.Time
	Err      error
	Sig      SignalEnvI
	Emit     SignalEmitterI
}

// TabSpec declares one dock tab. ID is the stable human slug ("table",
// "map") keying the focus knobs and, later, per-channel bindings; DockID is
// the frozen dock identity (D3). Panel is the PanelI for result panels and
// nil for chrome. NoScroll opts out of the dock's default per-tab
// ScrollArea — for panes that consume wheel/zoom gestures themselves (Map)
// or size from the available remainder (World). Lazy routes the body
// through a widgets/lazypane gate: while the host discards the tab's
// buffer (inactive tab), only a probe + loading placeholder is emitted;
// the real body lands one frame after activation. Opt in for heavy bodies
// only — a lazy tab shows a one-frame loading tick on switch.
type TabSpec struct {
	ID       string
	DockID   uint64
	Title    string
	Zone     TabZoneE
	NoScroll bool
	Lazy     bool
	Panel    PanelI
	Render   func(f *TabFrame)

	// ShapeContract marks a panel whose acceptance turns on the RESULT'S
	// COLUMN SHAPE — the Timeline's `_tl_*` contract, the World's country
	// column, the Kanban's `lane`+`title`, the Network's `edges` CTE. Their
	// rejection is worth advertising on the dock strip (the `×` mark of the
	// 2026-07-27 Update); the universal panes accept any schema and reject
	// only on interaction state ("select a row"), where a mark would be
	// permanent and carry no information.
	ShapeContract bool
	// Writes are the signal names this tab may publish — the write-back half
	// of the reactive surface, declared so the strip can mark a pane that
	// drives the current query BEFORE the first interaction (provenance knows
	// a writer only afterwards). It lives here rather than on PanelI because
	// the Map writes `vp_*` without being a PanelI at all. The companions the
	// dispatcher stamps (`selection_node`, `selection_id`) are implied by
	// declaring `selection` — see declaredWrites.
	Writes []SignalID
}

// TabRegistry is a PlayApp instance's tab set (D4): mutate between
// construction and the first Render via Add/Replace/Remove; the first Render
// freezes it. Not safe for concurrent use — it is render-thread state.
type TabRegistry struct {
	specs  []TabSpec
	frozen bool
}

func (inst *TabRegistry) validate(spec TabSpec, replaceIdx int) (err error) {
	if spec.ID == "" || spec.DockID == 0 || spec.Render == nil {
		err = eh.Errorf("tab %q: ID, a non-zero DockID, and Render are required", spec.ID)
		return
	}
	for i := range inst.specs {
		if i == replaceIdx {
			continue
		}
		if inst.specs[i].ID == spec.ID {
			err = eh.Errorf("tab %q: duplicate ID", spec.ID)
			return
		}
		if inst.specs[i].DockID == spec.DockID {
			err = eh.Errorf("tab %q: DockID %d already taken by %q", spec.ID, spec.DockID, inst.specs[i].ID)
			return
		}
	}
	return
}

func (inst *TabRegistry) mutable(op string) (err error) {
	if inst.frozen {
		err = eh.Errorf("tab registry: %s after the first Render — customize between construction and mounting", op)
	}
	return
}

// Add appends a tab. New body-zone tabs render after the built-ins.
func (inst *TabRegistry) Add(spec TabSpec) (err error) {
	if err = inst.mutable("Add"); err != nil {
		return
	}
	if err = inst.validate(spec, -1); err != nil {
		return
	}
	inst.specs = append(inst.specs, spec)
	return
}

// Replace swaps the tab with spec.ID == id, keeping its position. The
// replacement may change every field including DockID (a different pane
// identity for the persisted layout).
func (inst *TabRegistry) Replace(id string, spec TabSpec) (err error) {
	if err = inst.mutable("Replace"); err != nil {
		return
	}
	for i := range inst.specs {
		if inst.specs[i].ID != id {
			continue
		}
		if err = inst.validate(spec, i); err != nil {
			return
		}
		inst.specs[i] = spec
		return
	}
	err = eh.Errorf("tab %q: not registered", id)
	return
}

// Remove drops the tab with the given id.
func (inst *TabRegistry) Remove(id string) (err error) {
	if err = inst.mutable("Remove"); err != nil {
		return
	}
	for i := range inst.specs {
		if inst.specs[i].ID == id {
			inst.specs = append(inst.specs[:i], inst.specs[i+1:]...)
			return
		}
	}
	err = eh.Errorf("tab %q: not registered", id)
	return
}

func (inst *TabRegistry) freeze() { inst.frozen = true }

// all returns the specs in registration order. Callers must not mutate.
func (inst *TabRegistry) all() []TabSpec { return inst.specs }

// Specs returns a copy of the registered tabs in registration order — the
// read surface for embedders (asserting their registrations) and, later, the
// binding UI. Mutate the set only through Add/Replace/Remove.
func (inst *TabRegistry) Specs() (out []TabSpec) {
	out = make([]TabSpec, len(inst.specs))
	copy(out, inst.specs)
	return
}

// byZone returns the specs of one layout zone, in registration order.
func (inst *TabRegistry) byZone(z TabZoneE) (out []TabSpec) {
	out = make([]TabSpec, 0, len(inst.specs))
	for i := range inst.specs {
		if inst.specs[i].Zone == z {
			out = append(out, inst.specs[i])
		}
	}
	return
}

// panels returns the registered PanelI values in registration order — the
// channel inventory and (later) the binding UI read this.
func (inst *TabRegistry) panels() (out []PanelI) {
	out = make([]PanelI, 0, len(inst.specs))
	for i := range inst.specs {
		if inst.specs[i].Panel != nil {
			out = append(out, inst.specs[i].Panel)
		}
	}
	return
}

// builtinTabDef is the static half of a built-in tab — shared by defaultTabs
// (which attaches the per-instance closures) and the focus-knob derivation
// (package init). Listing order is presentation order per zone.
type builtinTabDef struct {
	id       string
	dockID   uint64
	title    string
	zone     TabZoneE
	noScroll bool
	lazy     bool
	// shapeContract / writes are the strip-mark declarations (TabSpec's
	// fields of the same name).
	shapeContract bool
	writes        []SignalID
}

// Lazy marks (see TabSpec.Lazy): heavy bodies whose per-frame cost is wasted
// while their tab is hidden — rasters (map, world), plots (timeline), the
// etable-backed projection, the graph view, the schema inspector, and the
// text-heavy history/diagnostics panes. Deliberately eager: editor (the
// snippet-insert delivery target), table (the most-trafficked result view,
// spared the one-frame loading tick), snippets (trivial body), and the
// preview/detail tabs. Detail is alone in its leaf, so a gate would never
// fire; Preview now shares one with Docs and so CAN be hidden, but its body
// is a memoised CodeView over already-computed text — cheap enough that the
// one-frame tick a gate costs would be the more visible of the two. Data pipelines are unaffected either
// way: lane demand, updatePreview and the diagnostics probe run before the
// tab bodies (see Render), so a lazy tab reveals with fresh data.
var builtinTabDefs = []builtinTabDef{
	{id: "editor", dockID: dockTabEditor, title: "Editor", zone: TabZoneEditor},
	{id: "history", dockID: dockTabHistory, title: "History", zone: TabZoneEditor, lazy: true},
	// Docs and Preview share the leaf to the right of the editor: both are
	// reference surfaces you read WHILE editing, which is what that leaf is
	// for. Docs is listed first, so a fresh layout opens on it; the dock
	// persists whichever the user then picks.
	//
	// A TOOL pane, not a result panel — it reads the editor's caret, not the
	// query result, so it registers with no PanelI. Lazy because a hidden
	// pane must not keep asking the server what the caret is on.
	{id: "docs", dockID: dockTabDocs, title: "Docs", zone: TabZonePreview, lazy: true},
	{id: "preview", dockID: dockTabPreview, title: "Preview", zone: TabZonePreview},
	{id: "table", dockID: dockTabTable, title: "Table", writes: []SignalID{signalSelection}},
	{id: "projection", dockID: dockTabProjection, title: "Projection", lazy: true,
		writes: []SignalID{signalSelection}},
	{id: "timeline", dockID: dockTabTimeline, title: "Timeline", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection, signalTimelineMin, signalTimelineMax}},
	{id: "snippets", dockID: dockTabSnippets, title: "Snippets"},
	// NoScroll: the walkers map reads wheel/zoom input globally (no
	// consumption), so the dock's default body ScrollArea would scroll the
	// panel in the same gesture that pans/zooms the map.
	// The Map is chrome (no PanelI) that nonetheless publishes its viewport —
	// the case that puts Writes on the spec rather than on the panel.
	{id: "map", dockID: dockTabMap, title: "Map", noScroll: true, lazy: true,
		writes: mapViewportSignals[:]},
	// NoScroll: the world choropleth sizes its map image from
	// ui.available_size() (zero-box FitAspectMax); inside the dock's
	// auto-shrinking ScrollArea, zero is a stable fixed point after a
	// tab-activation layout pass. A no-scroll leaf is bounded, so the
	// available size is the real remainder; overflow clips, as on Map.
	{id: "world", dockID: dockTabWorld, title: "World", noScroll: true, lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection, signalSelectionCountry}},
	{id: "kanban", dockID: dockTabKanban, title: "Kanban", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection}},
	// The Network tab draws the result as a node-link graph (ADR-0129). Its
	// title is deliberately not "Graph" — that is the dataflow chrome below.
	// It writes nothing: selection is local to the driver in v1.
	{id: "network", dockID: dockTabNetwork, title: "Network", lazy: true, shapeContract: true},
	{id: "graph", dockID: dockTabGraph, title: "Graph", lazy: true},
	{id: "schema", dockID: dockTabSchema, title: "Schema", lazy: true},
	{id: "diagnostics", dockID: dockTabDiagnostics, title: "Diagnostics", lazy: true},
	{id: "passes", dockID: dockTabPasses, title: "Passes", lazy: true},
	// Flow draws the ACTIVE node's clause-level dataflow derived from the SQL
	// itself (ADR-0153) — inside one statement, where the Graph tab's boxes
	// end. A TOOL pane like Docs: it reads the split, not the query result, so
	// it registers with no PanelI and writes nothing (selection is local to
	// the driver).
	{id: "flow", dockID: dockTabFlow, title: "Flow", lazy: true},
	{id: "detail", dockID: dockTabDetail, title: "Detail", zone: TabZoneSide},
}

// focusVars are the BOXER_PLAY_FOCUS_<ID> scripted-screenshot knobs, one per
// built-in body tab, derived from the tab definitions (slice 6a — this
// replaces six hand-registered specs and their hand-permuted reorder blocks;
// TABLE, PROJECTION and SNIPPETS knobs are new with the derivation).
var focusVars = registerFocusVars()

func registerFocusVars() (out map[string]*env.StringVar) {
	out = make(map[string]*env.StringVar, len(builtinTabDefs))
	for _, def := range builtinTabDefs {
		if def.zone != TabZoneBody {
			continue
		}
		out[def.id] = env.NewString(env.Spec{
			Name:        "BOXER_PLAY_FOCUS_" + strings.ToUpper(def.id),
			Description: "non-empty makes " + def.title + " the default-active body tab (scripted screenshots)",
			Category:    env.CategoryE("boxer-play"),
		})
	}
	return
}

// focusedTabID returns the body-tab id a BOXER_PLAY_FOCUS_* knob selects
// (first set knob in definition order), or "".
func focusedTabID() string {
	for _, def := range builtinTabDefs {
		if v, ok := focusVars[def.id]; ok && v.Get() != "" {
			return def.id
		}
	}
	return ""
}

// bodyTabOrder maps the body-zone specs to their dock ids with the focused
// tab (when set and present) moved to the front — a fresh dock leaf activates
// its first tab. Pure; the env read stays in focusedTabID.
func bodyTabOrder(specs []TabSpec, focusedID string) (out []uint64) {
	out = make([]uint64, 0, len(specs))
	var focused []uint64
	for i := range specs {
		if specs[i].ID == focusedID {
			focused = append(focused, specs[i].DockID)
			continue
		}
		out = append(out, specs[i].DockID)
	}
	out = append(focused, out...)
	return
}

// dockIDForSlug resolves a tab's human slug to its frozen DockID (D3) — the
// editor-delivery seam's ActivateTab reads it. ok is false for an unknown slug.
func (inst *TabRegistry) dockIDForSlug(id string) (dockID uint64, ok bool) {
	for i := range inst.specs {
		if inst.specs[i].ID == id {
			return inst.specs[i].DockID, true
		}
	}
	return
}

// slugForDockID is dockIDForSlug's inverse — the workingset composer maps
// the last tab play raised back to the slug a PlayLaunch names it by
// (ADR-0148 §SD8). ok is false for 0 (nothing raised) and for a dock id no
// registered tab claims.
func (inst *TabRegistry) slugForDockID(dockID uint64) (id string, ok bool) {
	if dockID == 0 {
		return
	}
	for i := range inst.specs {
		if inst.specs[i].DockID == dockID {
			return inst.specs[i].ID, true
		}
	}
	return
}

// dockIDsOf projects specs onto their dock ids, in order.
func dockIDsOf(specs []TabSpec) (out []uint64) {
	out = make([]uint64, len(specs))
	for i := range specs {
		out[i] = specs[i].DockID
	}
	return
}

// scrollTab wraps a chrome body in the vertical ScrollArea the dock call
// sites used to add — moved into the tab bodies so the registry loop stays
// uniform (slice 6a).
func scrollTab(body func()) {
	for range c.ScrollArea().Vscroll(true).AutoShrink(false, false).KeepIter() {
		body()
	}
}

// defaultTabs builds the built-in tab set over a PlayApp: the static defs
// plus per-instance Render closures and PanelI values (D2 — state stays on
// PlayApp for now; ownership migrates per tab when something needs it).
// Called at the end of NewPlayApp, after the drivers exist.
func defaultTabs(inst *PlayApp) (reg *TabRegistry) {
	reg = &TabRegistry{specs: make([]TabSpec, 0, len(builtinTabDefs))}
	for _, def := range builtinTabDefs {
		spec := TabSpec{ID: def.id, DockID: def.dockID, Title: def.title, Zone: def.zone,
			NoScroll: def.noScroll, Lazy: def.lazy,
			ShapeContract: def.shapeContract, Writes: def.writes}
		switch def.id {
		case "editor":
			spec.Render = func(f *TabFrame) { inst.renderEditorTab() }
		case "history":
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderHistoryTab) }
		case "preview":
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderPreviewTab) }
		case "table":
			spec.Panel = tablePanel{app: inst}
			spec.Render = func(f *TabFrame) { inst.renderTableTab(f.Rec, f.Schema, f.NumRows, f.Loading, f.Err, f.Executed) }
		case "projection":
			spec.Panel = projectionPanel{app: inst}
			spec.Render = func(f *TabFrame) { inst.renderProjectionTab(f.Rec, f.Loading, f.Err, f.Executed) }
		case "timeline":
			spec.Panel = timelinePanel{driver: inst.timeline}
			spec.Render = func(f *TabFrame) { inst.renderTimelineTab(f.Rec, f.Schema, f.Loading, f.Err) }
		case "snippets":
			spec.Render = func(f *TabFrame) { inst.renderSnippetsTab() }
		case "map":
			// The Map is a panel-authored node on its own lane (5c), not a
			// PanelI: it renders the driver directly.
			spec.Render = func(f *TabFrame) { inst.mapDriver.Render(f.Sig, inst.sigEmit.as(signalWriterMap)) }
		case "world":
			spec.Panel = worldPanel{driver: inst.worldDriver}
			spec.Render = func(f *TabFrame) { inst.renderWorldTab(f.Rec, f.Schema, f.Loading, f.Err, f.Executed) }
		case "kanban":
			spec.Panel = kanbanPanel{driver: inst.kanbanDriver}
			spec.Render = func(f *TabFrame) { inst.renderKanbanTab(f.Rec, f.Schema, f.Loading, f.Err, f.Executed) }
		case "network":
			// The panel reads its two named CTEs off the split (not the active
			// result), so the body ignores the frame; scrollTab mirrors the
			// System graph chrome, whose view.Render canvas pans/zooms inside a
			// ScrollArea.
			spec.Panel = layeredGraphPanel{driver: inst.networkDriver}
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderNetworkTab) }
		case "graph":
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderGraphTab) }
		case "schema":
			spec.Panel = schemaPanel{app: inst}
			spec.Render = func(f *TabFrame) { inst.renderSchemaTab(f.Rec, f.Schema, f.Loading, f.Err) }
		case "diagnostics":
			spec.Render = func(f *TabFrame) {
				scrollTab(func() { inst.renderDiagnosticsTab(f.NumRows, f.Elapsed, f.Summary, f.Executed, f.Err) })
			}
		case "passes":
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderPassesTab) }
		case "flow":
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderFlowTab) }
		case "docs":
			// No scrollTab: the body scrolls its own markdown, below a header
			// that must stay put while the document under it moves.
			spec.Render = func(f *TabFrame) { inst.renderDocsTab() }
		case "detail":
			spec.Panel = detailPanel{app: inst}
			spec.Render = func(f *TabFrame) { inst.renderDetailTab(f.Rec, f.Schema, f.Executed) }
		}
		if err := reg.Add(spec); err != nil {
			// The defs are a static table; a duplicate here is a
			// programming error, not a runtime condition.
			panic(err)
		}
	}
	return
}
