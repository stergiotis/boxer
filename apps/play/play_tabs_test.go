package play

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Slice-6a regression tests (ADR-0097 "Slice 6 (design)"): the tab registry —
// the built-in enumeration with its frozen dock ids (D3), the instance-scoped
// mutation window (D4), the focus reorder that replaced the hand-permuted
// blocks, and the registry-backed panel inventory.

func tabsTestApp() *PlayApp {
	return NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 10), "")
}

// The built-in set: ids, frozen dock ids, zones, scroll opt-outs, and the
// chrome/panel split (SD7 as structure — Panel non-nil exactly for the
// PanelI result panels).
func TestDefaultTabsEnumeration(t *testing.T) {
	reg := tabsTestApp().Tabs()
	specs := reg.all()
	require.Len(t, specs, 24)

	wantDockID := map[string]uint64{
		"editor": dockTabEditor, "history": dockTabHistory, "preview": dockTabPreview,
		"table": dockTabTable, "projection": dockTabProjection, "timeline": dockTabTimeline,
		"snippets": dockTabSnippets, "map": dockTabMap, "world": dockTabWorld,
		"kanban": dockTabKanban, "network": dockTabNetwork, "sankey": dockTabSankey,
		"dist": dockTabDist, "icicle": dockTabIcicle, "series": dockTabSeries,
		"treemap": dockTabTreemap,
		"graph":   dockTabGraph,
		"schema":  dockTabSchema, "diagnostics": dockTabDiagnostics, "passes": dockTabPasses,
		"docs": dockTabDocs, "flow": dockTabFlow, "detail": dockTabDetail,
		"experiments": dockTabExperiments,
	}
	seen := make(map[string]TabSpec, len(specs))
	for _, s := range specs {
		seen[s.ID] = s
		require.NotNil(t, s.Render, "tab %q has no Render", s.ID)
		assert.Equal(t, wantDockID[s.ID], s.DockID, "tab %q dock id is frozen (D3)", s.ID)
	}
	require.Len(t, seen, len(wantDockID), "every built-in registered exactly once")

	assert.Equal(t, TabZoneEditor, seen["editor"].Zone)
	assert.Equal(t, TabZoneEditor, seen["history"].Zone)
	assert.Equal(t, TabZoneSide, seen["detail"].Zone)
	assert.Equal(t, TabZoneBody, seen["table"].Zone, "body is the zero-value zone")

	// The tool panes: chrome fed by the buffer or by what is derived from it
	// (ADR-0097 Update 2026-08-01). Graph is deliberately NOT among them —
	// it qualifies by input and stays in the body for room — and Schema is
	// not one at all, its input being the result's own schema.
	for _, id := range []string{"docs", "preview", "flow", "passes", "diagnostics", "snippets"} {
		assert.Equal(t, TabZoneTools, seen[id].Zone, "tab %q is a tool pane", id)
		assert.Nil(t, seen[id].Panel, "a tool pane carries no PanelI (SD7)")
	}
	assert.Equal(t, TabZoneBody, seen["graph"].Zone, "Graph stays in the body against its classification")
	assert.Equal(t, TabZoneBody, seen["schema"].Zone, "Schema is fed the result, not the buffer")

	// Map is the only no-scroll leaf: it sizes its raster from the available
	// space, so it needs a bounded one. World left this set when it moved to a
	// probe-sized canvas (ADR-0114 Update 2026-08-01).
	for id, s := range seen {
		assert.Equal(t, id == "map", s.NoScroll, "NoScroll set for %q", id)
	}

	panelIDs := make([]string, 0, 6)
	for _, s := range specs {
		if s.Panel != nil {
			panelIDs = append(panelIDs, s.ID)
		}
	}
	assert.ElementsMatch(t, []string{"table", "projection", "timeline", "world", "kanban", "network", "sankey",
		"dist", "icicle", "series", "treemap", "schema", "detail"},
		panelIDs, "chrome registers with a nil PanelI (SD7)")

	// Presentation order per zone. Docs stays first among the tools so a
	// fresh layout opens on it.
	assert.Equal(t, []uint64{dockTabTable, dockTabProjection, dockTabTimeline,
		dockTabMap, dockTabWorld, dockTabKanban, dockTabNetwork, dockTabSankey, dockTabDist, dockTabIcicle,
		dockTabSeries, dockTabTreemap, dockTabGraph, dockTabSchema},
		dockIDsOf(reg.byZone(TabZoneBody)))
	assert.Equal(t, []uint64{dockTabDocs, dockTabPreview, dockTabFlow, dockTabPasses,
		dockTabDiagnostics, dockTabSnippets, dockTabExperiments},
		dockIDsOf(reg.byZone(TabZoneTools)))
}

// Mutation window (D4): Add/Replace/Remove validate and work before the first
// Render; the freeze rejects everything after.
func TestTabRegistryMutationAndFreeze(t *testing.T) {
	reg := tabsTestApp().Tabs()
	noop := func(*TabFrame) {}

	require.Error(t, reg.Add(TabSpec{ID: "", DockID: 64, Render: noop}), "empty ID")
	require.Error(t, reg.Add(TabSpec{ID: "x", DockID: 0, Render: noop}), "zero DockID")
	require.Error(t, reg.Add(TabSpec{ID: "x", DockID: 64}), "nil Render")
	require.Error(t, reg.Add(TabSpec{ID: "table", DockID: 64, Render: noop}), "duplicate ID")
	require.Error(t, reg.Add(TabSpec{ID: "x", DockID: dockTabTable, Render: noop}), "duplicate DockID")

	require.NoError(t, reg.Add(TabSpec{ID: "x", DockID: 64, Title: "X", Render: noop}))
	require.Len(t, reg.all(), 25)
	assert.Equal(t, TabZoneBody, reg.all()[24].Zone, "embedder tabs default to the body zone")

	// Replace keeps the position and re-validates against the others.
	require.Error(t, reg.Replace("x", TabSpec{ID: "table", DockID: 64, Render: noop}),
		"replacement must not collide with another tab")
	require.NoError(t, reg.Replace("x", TabSpec{ID: "y", DockID: 65, Title: "Y", Render: noop}))
	assert.Equal(t, "y", reg.all()[24].ID)
	require.Error(t, reg.Replace("x", TabSpec{ID: "z", DockID: 66, Render: noop}), "x is gone")

	require.NoError(t, reg.Remove("y"))
	require.Len(t, reg.all(), 24)
	require.Error(t, reg.Remove("y"), "already removed")

	reg.freeze()
	require.Error(t, reg.Add(TabSpec{ID: "late", DockID: 64, Render: noop}))
	require.Error(t, reg.Replace("table", TabSpec{ID: "late", DockID: 64, Render: noop}))
	require.Error(t, reg.Remove("table"))
}

// Specs is the embedder-facing enumeration: a copy, in registration order.
func TestTabRegistrySpecsIsACopy(t *testing.T) {
	reg := tabsTestApp().Tabs()
	specs := reg.Specs()
	require.Len(t, specs, len(reg.all()))
	assert.Equal(t, reg.all()[0].ID, specs[0].ID)
	specs[0].ID = "clobbered"
	assert.NotEqual(t, "clobbered", reg.all()[0].ID, "Specs must return a copy")
}

// The focus reorder: one pure function per zone instead of six hand-permuted
// arrays (whose FOCUS_MAP copy had silently dropped Graph).
func TestZoneTabOrderFocusReorder(t *testing.T) {
	body := tabsTestApp().Tabs().byZone(TabZoneBody)
	base := dockIDsOf(body)

	assert.Equal(t, base, zoneTabOrder(body, nil), "no focus ⇒ definition order")
	assert.Equal(t, base, zoneTabOrder(body, []string{"nope"}), "unknown id ⇒ definition order")

	got := zoneTabOrder(body, []string{"graph"})
	require.Equal(t, dockTabGraph, got[0], "the focused tab moves to the front")
	assert.ElementsMatch(t, base, got, "reordering never drops a tab")
	assert.Len(t, got, len(base))

	// A knob naming a tab in ANOTHER zone leaves this one in definition
	// order: each leaf raises its own, so two knobs no longer contend
	// (ADR-0097 Update 2026-08-01).
	assert.Equal(t, base, zoneTabOrder(body, []string{"passes"}),
		"a tools-zone id must not reorder the body")
	tools := tabsTestApp().Tabs().byZone(TabZoneTools)
	require.Equal(t, dockTabPasses, zoneTabOrder(tools, []string{"passes", "graph"})[0])
	require.Equal(t, dockTabGraph, zoneTabOrder(body, []string{"passes", "graph"})[0])

	// Within one zone, definition order still picks.
	assert.Equal(t, dockTabTable, zoneTabOrder(body, []string{"schema", "table"})[0],
		"the earliest focused id in the zone wins")
}

// The focus knobs derive from the tab definitions: one per built-in tab in
// EVERY zone, named BOXER_PLAY_FOCUS_<ID>. Body-only derivation is what left
// the Preview tab without one when it moved out of the body leaf.
func TestFocusVarsDerivedFromEveryBuiltinTab(t *testing.T) {
	reg := tabsTestApp().Tabs()
	require.Len(t, focusVars, len(reg.all()))
	for _, spec := range reg.all() {
		v, ok := focusVars[spec.ID]
		require.True(t, ok, "no focus knob for %q", spec.ID)
		assert.Equal(t, "BOXER_PLAY_FOCUS_"+strings.ToUpper(spec.ID), v.Spec().Name)
	}
	for _, id := range []string{"preview", "docs", "flow", "passes", "diagnostics", "snippets", "history"} {
		_, ok := focusVars[id]
		assert.True(t, ok, "the non-body zones carry knobs too (%q)", id)
	}
}
