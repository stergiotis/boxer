package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression tests for the Panes menu (ADR-0097 2026-07-27 Update): the row
// model is pure enough to assert without a frame, and it is the surface that
// carries the reasons the strip's one-glyph marks cannot.

func menuRowByID(t *testing.T, rows []paneMenuRow, id string) paneMenuRow {
	t.Helper()
	for _, r := range rows {
		if r.TabID == id {
			return r
		}
	}
	require.FailNow(t, "no such row", id)
	return paneMenuRow{}
}

// The two groups: every result pane shows, and only signal-writing panes whose
// names the buffer reads drive.
func TestPaneMenuGroups(t *testing.T) {
	app := tabsTestApp()
	app.currentSplit = markSplit()
	app.paramSlots = markSlots(signalSelectionCountry)
	app.frameSig = app.graph.signals()

	shows, drives := app.paneMenuRows(markWorldOK)

	showIDs := make([]string, 0, len(shows))
	for _, r := range shows {
		showIDs = append(showIDs, r.TabID)
	}
	assert.Equal(t, []string{"table", "projection", "timeline", "world", "kanban", "network", "schema", "detail"},
		showIDs, "one row per PanelI-bearing tab, in strip order")

	require.Len(t, drives, 1, "only the World writes a name this buffer reads")
	assert.Equal(t, "world", drives[0].TabID)
	assert.Equal(t, []string{signalSelectionCountry}, drives[0].Drives)
	assert.Empty(t, drives[0].Unfilled, "a reserved String signal defaults to empty")
}

// A rejecting row carries the panel's OWN reason — the same string its body
// shows — and an accepting one carries none.
func TestPaneMenuCarriesPanelReasons(t *testing.T) {
	app := tabsTestApp()
	app.currentSplit = markSplit()
	app.frameSig = app.graph.signals()

	shows, _ := app.paneMenuRows(markWorldOK)

	assert.Empty(t, menuRowByID(t, shows, "world").Reject, "a text column is all the World needs")
	assert.Empty(t, menuRowByID(t, shows, "table").Reject)

	tl := menuRowByID(t, shows, "timeline").Reject
	assert.Contains(t, tl, timelineSlotTime, "the Timeline names the column it wants")

	kb := menuRowByID(t, shows, "kanban").Reject
	assert.Contains(t, kb, kanbanLaneCol)

	// Interaction-gated reasons appear here though they are never strip marks.
	detail := menuRowByID(t, shows, "detail").Reject
	assert.Contains(t, detail, "Select a row")
}

// A channel filled by a named node: absent from the split reports the panel's
// no-input prose; present-but-unexecuted says nothing rather than describing
// a node that exists as missing.
func TestPaneMenuNamedNodeChannels(t *testing.T) {
	app := tabsTestApp()
	app.frameSig = app.graph.signals()

	app.currentSplit = markSplit()
	shows, _ := app.paneMenuRows(markWorldOK)
	assert.Contains(t, menuRowByID(t, shows, "network").Reject, "edges",
		"no `edges` CTE in the buffer — the panel says what it wants")

	withEdges := markSplit()
	withEdges.Nodes = append(withEdges.Nodes, splitNode{ID: networkEdgesNodeID, Kind: splitNodeCTE})
	app.currentSplit = withEdges
	shows, _ = app.paneMenuRows(markWorldOK)
	assert.Empty(t, menuRowByID(t, shows, "network").Reject,
		"the node is there; its columns need an execution to judge (SD2)")
}

// Before anything runs the menu still explains itself, and marks nothing.
func TestPaneMenuBeforeFirstRun(t *testing.T) {
	app := tabsTestApp()
	app.frameSig = app.graph.signals()

	shows, drives := app.paneMenuRows(nil)
	assert.Empty(t, drives, "an empty buffer reads nothing")
	assert.Contains(t, menuRowByID(t, shows, "table").Reject, "Run a query")

	world := TabSpec{ID: "world", Title: "World", ShapeContract: true, Panel: worldPanel{}}
	assert.Equal(t, tabMarkNone, tabMark(&world, tabVerdict{}),
		"the strip stays silent on the same state the menu describes")
}

// A bound pane is judged against ITS node's schema, and its row names the node.
func TestPaneMenuFollowsBinding(t *testing.T) {
	app := tabsTestApp()
	app.currentSplit = markSplit()
	app.frameSig = app.graph.signals()
	app.resolvedNodes = map[string]NodeID{"kanban": "board"}
	app.boundViews = map[NodeID]laneView{"board": {schema: markKanbanOK}}

	shows, _ := app.paneMenuRows(markWorldOK)
	kb := menuRowByID(t, shows, "kanban")
	assert.Equal(t, NodeID("board"), kb.Node, "the row names the node feeding it")
	assert.Empty(t, kb.Reject, "judged against the bound node's schema, not the active result")

	tbl := menuRowByID(t, shows, "table")
	assert.Empty(t, tbl.Node, "an unbound pane names no node")
}

// The unfilled marker names the gesture that unblocks a refused Run.
func TestDescribeDrivenSignals(t *testing.T) {
	assert.Equal(t, "vp_min_x, vp_max_x", describeDrivenSignals([]string{"vp_min_x", "vp_max_x"}, nil))
	assert.Equal(t, "vp_min_x (needs a value), vp_max_x",
		describeDrivenSignals([]string{"vp_min_x", "vp_max_x"}, []string{"vp_min_x"}))
}

// The Map drives without being a PanelI: it appears in the drives group only.
func TestPaneMenuMapDrivesWithoutPanel(t *testing.T) {
	app := tabsTestApp()
	app.currentSplit = markSplit()
	app.paramSlots = markSlots("vp_min_x")
	app.frameSig = app.graph.signals()

	shows, drives := app.paneMenuRows(markWorldOK)
	for _, r := range shows {
		assert.NotEqual(t, "map", r.TabID, "the Map consumes no result, so it shows none")
	}
	row := menuRowByID(t, drives, "map")
	assert.Equal(t, []string{"vp_min_x"}, row.Drives)
	assert.Equal(t, []string{"vp_min_x"}, row.Unfilled, "nothing has written the viewport yet")
}

// A Run refused for an unfilled input never produces a split, and that is
// exactly when the pane writing the missing name most wants pointing at — so
// the relation reads the BUFFER, not the last split.
func TestPaneMenuDrivesWithoutASplit(t *testing.T) {
	app := tabsTestApp()
	app.paramSlots = markSlots("vp_min_x")
	app.frameSig = app.graph.signals()
	require.Empty(t, app.currentSplit.Nodes, "nothing has run")

	_, drives := app.paneMenuRows(nil)
	row := menuRowByID(t, drives, "map")
	assert.Equal(t, []string{"vp_min_x"}, row.Unfilled)

	mapSpec := TabSpec{ID: "map", Title: "Map", Writes: mapViewportSignals[:]}
	assert.Equal(t, tabMarkBlocked, tabMark(&mapSpec, tabVerdict{reads: markReads("vp_min_x")}),
		"the strip says the same: this pane is what the refused Run is waiting on")
}
