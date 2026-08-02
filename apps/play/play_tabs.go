package play

import (
	"slices"
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
	TabZoneBody   TabZoneE = iota // the main body leaf (result views)
	TabZoneEditor                 // the editor leaf (Editor, History)
	// TabZoneTools is the leaf split right of the editor: the tool panes,
	// read WHILE editing. A tool pane is chrome (nil Panel) whose input is
	// the buffer or what is derived from it — the caret, the split, the
	// pre-execute pipeline — as opposed to a result panel, which is fed the
	// query result over a channel (ADR-0097 Update 2026-08-01). It was
	// TabZonePreview when Preview was its only occupant.
	TabZoneTools
	TabZoneSide // split right of the body leaf (Detail)
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
// fire; Preview shares one and so CAN be hidden, but its body
// is a memoised CodeView over already-computed text — cheap enough that the
// one-frame tick a gate costs would be the more visible of the two. Data pipelines are unaffected either
// way: lane demand, updatePreview and the diagnostics probe run before the
// tab bodies (see Render), so a lazy tab reveals with fresh data.
var builtinTabDefs = []builtinTabDef{
	{id: "editor", dockID: dockTabEditor, title: "Editor", zone: TabZoneEditor},
	{id: "history", dockID: dockTabHistory, title: "History", zone: TabZoneEditor, lazy: true},

	// The tool panes, in the leaf right of the editor (ADR-0097 Update
	// 2026-08-01). Each is chrome fed by the buffer or by what is derived
	// from it, so none carries a PanelI, and each is read while editing
	// rather than while looking at rows. Docs is listed first, so a fresh
	// layout opens on it; the dock keeps whichever the user then picks.
	//
	// Docs reads the editor's caret. Lazy because a hidden pane must not
	// keep asking the server what the caret is on.
	{id: "docs", dockID: dockTabDocs, title: "Docs", zone: TabZoneTools, lazy: true},
	{id: "preview", dockID: dockTabPreview, title: "Preview", zone: TabZoneTools},
	// Flow draws the ACTIVE node's clause-level dataflow derived from the SQL
	// itself (ADR-0153) — inside one statement, where the Graph tab's boxes
	// end. It follows the split or, in caret mode, the statement under the
	// cursor: the same input as Docs. Selection is local to the driver, so it
	// writes nothing.
	{id: "flow", dockID: dockTabFlow, title: "Flow", zone: TabZoneTools, lazy: true},
	// Passes draws the pre-execute rewrite pipeline over the statement Run
	// would ship — keyed on the buffer and the passreg catalog, never on a
	// row (ADR-0119 M3).
	{id: "passes", dockID: dockTabPasses, title: "Passes", zone: TabZoneTools, lazy: true},
	// Diagnostics is six buffer-fed sections (statement, rewrites, column
	// resolution, security context, split, signal emits) and one that reports
	// the last run. It moves whole: a run error is read while fixing the SQL
	// that caused it, and Preview and Graph both point here for prose they
	// decline to own.
	{id: "diagnostics", dockID: dockTabDiagnostics, title: "Diagnostics", zone: TabZoneTools, lazy: true},
	// Snippets is an editor tool: its input is the help corpus and its output
	// is the buffer. Its splice needs the editor visible, which this leaf
	// satisfies — it is a SIBLING of the editor leaf, not the same one, and
	// the delivery ops raise the editor leaf, so Snippets cannot hide itself
	// with its own click.
	{id: "snippets", dockID: dockTabSnippets, title: "Snippets", zone: TabZoneTools},

	{id: "table", dockID: dockTabTable, title: "Table", writes: []SignalID{signalSelection}},
	{id: "projection", dockID: dockTabProjection, title: "Projection", lazy: true,
		writes: []SignalID{signalSelection}},
	{id: "timeline", dockID: dockTabTimeline, title: "Timeline", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection, signalTimelineMin, signalTimelineMax}},
	// NoScroll: the walkers map reads wheel/zoom input globally (no
	// consumption), so the dock's default body ScrollArea would scroll the
	// panel in the same gesture that pans/zooms the map.
	// The Map is chrome (no PanelI) that nonetheless publishes its viewport —
	// the case that puts Writes on the spec rather than on the panel.
	{id: "map", dockID: dockTabMap, title: "Map", noScroll: true, lazy: true,
		writes: mapViewportSignals[:]},
	// Scrolls, unlike Map: the world choropleth now draws into a canvas it
	// sizes from a ui-rect probe of the pane width (ADR-0114 Update
	// 2026-08-01), so it no longer needs a bounded leaf to read an available
	// size from — and a canvas is a fixed box, so a pane too short for the
	// map's aspect scrolls instead of clipping its southern hemisphere.
	{id: "world", dockID: dockTabWorld, title: "World", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection, signalSelectionCountry}},
	{id: "kanban", dockID: dockTabKanban, title: "Kanban", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelection}},
	// The Network tab draws the result as a node-link graph (ADR-0129). Its
	// title is deliberately not "Graph" — that is the dataflow chrome below.
	// It publishes the clicked vertex id as `selection_key` (a value); the
	// row-index `selection` stays local to the driver, per ADR-0129 §SD4.
	{id: "network", dockID: dockTabNetwork, title: "Network", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelectionKey}},
	// The Sankey tab draws the result as a flow-quantity diagram (ADR-0159).
	// Its inputs are the `flows`/`nodes` CTEs, so its selection is local for
	// the same reason the Network's is; a pinned node publishes its id.
	{id: "sankey", dockID: dockTabSankey, title: "Sankey", lazy: true, shapeContract: true,
		writes: []SignalID{signalSelectionKey}},
	// Graph stays in the body against its classification: its input is the
	// split and the signal store, so by the criterion above it is a tool pane,
	// but its subject is the SESSION's reactive wiring rather than the
	// statement being typed, and it is a canvas plus the Signals section — it
	// wants more room than the tools leaf has (ADR-0097 Update 2026-08-01).
	{id: "graph", dockID: dockTabGraph, title: "Graph", lazy: true},
	// Schema reads like a tool pane and is not one: its input is the RESULT's
	// Arrow schema, which is why it carries a PanelI at all.
	{id: "schema", dockID: dockTabSchema, title: "Schema", lazy: true},
	{id: "detail", dockID: dockTabDetail, title: "Detail", zone: TabZoneSide},
}

// focusVars are the BOXER_PLAY_FOCUS_<ID> scripted-screenshot knobs, one per
// built-in tab, derived from the tab definitions (slice 6a — this replaces six
// hand-registered specs and their hand-permuted reorder blocks).
//
// The derivation covers every zone, not just the body (ADR-0097 Update
// 2026-08-01): a knob that exists only while its tab happens to sit in the body
// leaf is a knob that disappears when the tab is re-zoned, which is how the
// Preview tab came to have none. Two of them are inert — Editor and Detail are
// each first in their leaf already — and registering them anyway is what keeps
// the set from needing a hand-maintained exception list.
var focusVars = registerFocusVars()

func registerFocusVars() (out map[string]*env.StringVar) {
	out = make(map[string]*env.StringVar, len(builtinTabDefs))
	for _, def := range builtinTabDefs {
		out[def.id] = env.NewString(env.Spec{
			Name:        "BOXER_PLAY_FOCUS_" + strings.ToUpper(def.id),
			Description: "non-empty makes " + def.title + " the default-active tab in its dock leaf (scripted screenshots)",
			Category:    env.CategoryE("boxer-play"),
		})
	}
	return
}

// focusedTabIDs returns the ids whose BOXER_PLAY_FOCUS_* knob is set, in
// definition order. One takes effect per leaf: zoneTabOrder raises the earliest
// of its own zone's specs named here, so knobs set in two zones focus a tab in
// each rather than contending. Within one zone the tab earliest in definition
// order picks — a degenerate scripting case either way.
func focusedTabIDs() (out []string) {
	for _, def := range builtinTabDefs {
		if v, ok := focusVars[def.id]; ok && v.Get() != "" {
			out = append(out, def.id)
		}
	}
	return
}

// zoneTabOrder maps one zone's specs to their dock ids with the first focused
// tab among them moved to the front — a fresh dock leaf activates its first
// tab. Pure; the env read stays in focusedTabIDs.
func zoneTabOrder(specs []TabSpec, focused []string) (out []uint64) {
	out = make([]uint64, 0, len(specs))
	raise := indexOfFirstFocused(specs, focused)
	if raise >= 0 {
		out = append(out, specs[raise].DockID)
	}
	for i := range specs {
		if i != raise {
			out = append(out, specs[i].DockID)
		}
	}
	return
}

// indexOfFirstFocused returns the position of the earliest spec named by
// focused, or -1 when this zone holds none of them. It scans the zone rather
// than the knob list, so the result turns on the SET of focused ids and not on
// the order they arrive in.
func indexOfFirstFocused(specs []TabSpec, focused []string) (idx int) {
	for i := range specs {
		if slices.Contains(focused, specs[i].ID) {
			return i
		}
	}
	return -1
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
		case "sankey":
			// Reads its two named CTEs off the split, not the active result, so
			// the body ignores the frame. Scrolled, like the Network tab: the
			// plot is a fixed box sized from the pane WIDTH (the only dimension
			// a non-contending probe yields), so a short leaf has to scroll
			// rather than clip. The two do not fight over the wheel — implot
			// captures scroll only while the pointer is over the plot, and
			// zeroes the delta the ScrollArea would read (ADR-0140).
			spec.Panel = sankeyPanel{driver: inst.sankeyDriver}
			spec.Render = func(f *TabFrame) { scrollTab(inst.renderSankeyTab) }
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
