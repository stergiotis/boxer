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
	require.Len(t, specs, 13)

	wantDockID := map[string]uint64{
		"editor": dockTabEditor, "history": dockTabHistory, "preview": dockTabPreview,
		"table": dockTabTable, "projection": dockTabProjection, "timeline": dockTabTimeline,
		"snippets": dockTabSnippets, "map": dockTabMap, "world": dockTabWorld,
		"graph": dockTabGraph, "schema": dockTabSchema, "diagnostics": dockTabDiagnostics,
		"detail": dockTabDetail,
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
	assert.Equal(t, TabZonePreview, seen["preview"].Zone)
	assert.Equal(t, TabZoneSide, seen["detail"].Zone)
	assert.Equal(t, TabZoneBody, seen["table"].Zone, "body is the zero-value zone")

	for id, s := range seen {
		assert.Equal(t, id == "map" || id == "world", s.NoScroll, "NoScroll set for %q", id)
	}

	panelIDs := make([]string, 0, 6)
	for _, s := range specs {
		if s.Panel != nil {
			panelIDs = append(panelIDs, s.ID)
		}
	}
	assert.ElementsMatch(t, []string{"table", "projection", "timeline", "world", "schema", "detail"},
		panelIDs, "chrome registers with a nil PanelI (SD7)")

	// The body zone keeps today's presentation order.
	assert.Equal(t, []uint64{dockTabTable, dockTabProjection, dockTabTimeline, dockTabSnippets,
		dockTabMap, dockTabWorld, dockTabGraph, dockTabSchema, dockTabDiagnostics},
		dockIDsOf(reg.byZone(TabZoneBody)))
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
	require.Len(t, reg.all(), 14)
	assert.Equal(t, TabZoneBody, reg.all()[13].Zone, "embedder tabs default to the body zone")

	// Replace keeps the position and re-validates against the others.
	require.Error(t, reg.Replace("x", TabSpec{ID: "table", DockID: 64, Render: noop}),
		"replacement must not collide with another tab")
	require.NoError(t, reg.Replace("x", TabSpec{ID: "y", DockID: 65, Title: "Y", Render: noop}))
	assert.Equal(t, "y", reg.all()[13].ID)
	require.Error(t, reg.Replace("x", TabSpec{ID: "z", DockID: 66, Render: noop}), "x is gone")

	require.NoError(t, reg.Remove("y"))
	require.Len(t, reg.all(), 13)
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

// The focus reorder: one pure function over the body zone instead of six
// hand-permuted arrays (whose FOCUS_MAP copy had silently dropped Graph).
func TestBodyTabOrderFocusReorder(t *testing.T) {
	body := tabsTestApp().Tabs().byZone(TabZoneBody)
	base := dockIDsOf(body)

	assert.Equal(t, base, bodyTabOrder(body, ""), "no focus ⇒ definition order")
	assert.Equal(t, base, bodyTabOrder(body, "nope"), "unknown id ⇒ definition order")

	got := bodyTabOrder(body, "graph")
	require.Equal(t, dockTabGraph, got[0], "the focused tab moves to the front")
	assert.ElementsMatch(t, base, got, "reordering never drops a tab")
	assert.Len(t, got, len(base))
}

// The focus knobs derive from the tab definitions: one per body tab, named
// BOXER_PLAY_FOCUS_<ID>.
func TestFocusVarsDerivedFromBodyTabs(t *testing.T) {
	wantIDs := []string{"table", "projection", "timeline", "snippets", "map", "world", "graph", "schema", "diagnostics"}
	require.Len(t, focusVars, len(wantIDs))
	for _, id := range wantIDs {
		v, ok := focusVars[id]
		require.True(t, ok, "no focus knob for %q", id)
		assert.Equal(t, "BOXER_PLAY_FOCUS_"+strings.ToUpper(id), v.Spec().Name)
	}
}
