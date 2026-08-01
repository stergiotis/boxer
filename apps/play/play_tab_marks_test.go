package play

import (
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/env"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression tests for the ADR-0097 2026-07-27 Update: the strip marks. The
// ladder is pure, so most of this exercises tabMark directly against
// hand-built frames; the last two cases check the built-in declarations and
// the composition with 6c's binding decoration.

func markSchema(fields ...arrow.Field) *arrow.Schema { return arrow.NewSchema(fields, nil) }

// markSig is a signal env holding the named signals (value "1").
func markSig(names ...string) SignalEnvI {
	params := make(map[SignalID]env.Param, len(names))
	for _, n := range names {
		params[n] = env.Param{Name: n, Raw: "1"}
	}
	return &signalEnv{params: params, revision: 1}
}

// markSplit is a bare one-node split — the structural half, for the channels
// filled by a named node.
func markSplit() splitResult {
	return splitResult{
		Nodes: []splitNode{{ID: mainNodeID, Kind: splitNodeStatement}},
		Sink:  mainNodeID,
	}
}

// markSlots is the parsed-slot form of markReads, for the app-level paths that
// build the read set off the buffer cache.
func markSlots(names ...SignalID) (out []paramSlot) {
	for _, n := range names {
		out = append(out, paramSlot{Name: string(n), Type: "String"})
	}
	return
}

// markReads is the buffer's referenced slot names — the drive relation's half.
func markReads(names ...SignalID) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[string(n)] = true
	}
	return out
}

var (
	markWorldOK  = markSchema(arrow.Field{Name: "country", Type: arrow.BinaryTypes.String})
	markWorldNo  = markSchema(arrow.Field{Name: "n", Type: arrow.PrimitiveTypes.Int64})
	markKanbanOK = markSchema(
		arrow.Field{Name: kanbanLaneCol, Type: arrow.BinaryTypes.String},
		arrow.Field{Name: kanbanTitleCol, Type: arrow.BinaryTypes.String})
)

func markSpec(id string, shape bool, panel PanelI, writes ...SignalID) *TabSpec {
	return &TabSpec{ID: id, Title: id, ShapeContract: shape, Panel: panel, Writes: writes}
}

// A shape-gated panel that cannot draw the result's columns is marked; the
// same panel with a fitting result is not.
func TestTabMarkShapeReject(t *testing.T) {
	world := markSpec("world", true, worldPanel{})
	assert.Equal(t, tabMarkShapeReject, tabMark(world, tabVerdict{schema: markWorldNo}),
		"no text column — the World cannot resolve countries")
	assert.Equal(t, tabMarkNone, tabMark(world, tabVerdict{schema: markWorldOK}))

	kanban := markSpec("kanban", true, kanbanPanel{})
	assert.Equal(t, tabMarkShapeReject, tabMark(kanban, tabVerdict{schema: markWorldOK}),
		"a board needs `lane` + `title`")
	assert.Equal(t, tabMarkNone, tabMark(kanban, tabVerdict{schema: markKanbanOK}))

	timeline := markSpec("timeline", true, timelinePanel{})
	assert.Equal(t, tabMarkShapeReject, tabMark(timeline, tabVerdict{schema: markKanbanOK}),
		"no _tl_time column")
}

// An unknown offer is silence, not a reject: no result yet, and no split yet
// for a channel filled by a named node.
func TestTabMarkUnknownShapeIsSilent(t *testing.T) {
	world := markSpec("world", true, worldPanel{})
	assert.Equal(t, tabMarkNone, tabMark(world, tabVerdict{}),
		"nothing has run — every panel would reject, which says nothing")

	network := markSpec("network", true, layeredGraphPanel{})
	assert.Equal(t, tabMarkNone, tabMark(network, tabVerdict{schema: markWorldNo}),
		"no split — the edges CTE is unknown, not absent")
	assert.Equal(t, tabMarkShapeReject, tabMark(network, tabVerdict{schema: markWorldNo, split: markSplit()}),
		"a split without an `edges` node cannot fill the required channel")

	withEdges := markSplit()
	withEdges.Nodes = append(withEdges.Nodes, splitNode{ID: networkEdgesNodeID, Kind: splitNodeCTE})
	assert.Equal(t, tabMarkNone, tabMark(network, tabVerdict{schema: markWorldNo, split: withEdges}),
		"the node is there; its columns need an execution to judge (SD2)")
}

// The universal panes are never shape-marked: their rejections are about
// interaction state, and a permanent mark would carry no information.
func TestTabMarkSkipsUniversalPanes(t *testing.T) {
	detail := markSpec("detail", false, detailPanel{})
	_, reason := detailPanel{}.AcceptForChannel(chMain, markWorldOK, nil)
	require.NotEmpty(t, reason, "Detail does reject without a selection")
	assert.Equal(t, tabMarkNone, tabMark(detail, tabVerdict{schema: markWorldOK}),
		"an interaction-gated rejection is not a strip mark")
}

// The signal relation: writing a name the split reads marks the pane a
// control; an unfilled one marks it the blocker; a SET-bound name is neither.
func TestTabMarkSignalRelation(t *testing.T) {
	mapTab := markSpec("map", false, nil, mapViewportSignals[:]...)

	assert.Equal(t, tabMarkNone, tabMark(mapTab, tabVerdict{reads: markReads("unrelated")}),
		"the query does not read the viewport")

	blocked := tabVerdict{reads: markReads("vp_min_x"), sig: markSig()}
	assert.Equal(t, tabMarkBlocked, tabMark(mapTab, blocked),
		"the query reads vp_min_x and nothing has written it")

	drives := tabVerdict{reads: markReads("vp_min_x"), sig: markSig("vp_min_x")}
	assert.Equal(t, tabMarkDrives, tabMark(mapTab, drives))

	pinned := tabVerdict{reads: markReads("vp_min_x"), sig: markSig(), bound: map[string]string{"vp_min_x": "0"}}
	assert.Equal(t, tabMarkNone, tabMark(mapTab, pinned),
		"a SET pins the value — the pane's write is shadowed (D1)")
}

// A reserved String signal defaults to empty rather than blocking a Run, so
// its writer drives without ever being the blocker.
func TestTabMarkReservedStringNeverBlocks(t *testing.T) {
	world := markSpec("world", true, worldPanel{}, signalSelection, signalSelectionCountry)
	v := tabVerdict{schema: markWorldOK, reads: markReads(signalSelectionCountry), sig: markSig()}
	assert.Equal(t, tabMarkDrives, tabMark(world, v))
}

// Declaring `selection` implies the companions the dispatcher stamps.
func TestTabMarkSelectionCompanions(t *testing.T) {
	table := markSpec("table", false, tablePanel{}, signalSelection)
	v := tabVerdict{schema: markWorldOK, reads: markReads(signalSelectionID), sig: markSig(string(signalSelectionID))}
	assert.Equal(t, tabMarkDrives, tabMark(table, v),
		"selection_id rides every selecting pane's click")
}

// Precedence: a pane that cannot draw this result is not a control over it.
func TestTabMarkPrecedence(t *testing.T) {
	world := markSpec("world", true, worldPanel{}, signalSelection, signalSelectionCountry)
	v := tabVerdict{schema: markWorldNo, reads: markReads(signalSelectionCountry), sig: markSig()}
	assert.Equal(t, tabMarkShapeReject, tabMark(world, v))

	// A notice loses to both signal marks, and stands on its own.
	chrome := markSpec("diagnostics", false, nil)
	assert.Equal(t, tabMarkNotice, tabMark(chrome, tabVerdict{notice: true}))
	driving := markSpec("map", false, nil, mapViewportSignals[:]...)
	v = tabVerdict{reads: markReads("vp_min_x"), sig: markSig("vp_min_x"), notice: true}
	assert.Equal(t, tabMarkDrives, tabMark(driving, v))
}

// The glyph set is ASCII: a capture of the running strip showed `●` as tofu
// in the tab label font and `×` colliding with the dock's close glyph.
func TestTabMarkGlyphs(t *testing.T) {
	assert.Equal(t, "", tabMarkNone.glyph())
	assert.Equal(t, "-", tabMarkShapeReject.glyph())
	assert.Equal(t, "!", tabMarkBlocked.glyph())
	assert.Equal(t, "*", tabMarkDrives.glyph())
	assert.Equal(t, "!", tabMarkNotice.glyph(), "attention shares one glyph")
	for _, m := range []tabMarkE{tabMarkShapeReject, tabMarkBlocked, tabMarkDrives, tabMarkNotice} {
		for _, r := range m.glyph() {
			assert.Less(t, r, rune(0x80), "the strip paints ASCII only")
		}
	}
}

// The built-in declarations: shape-gated exactly where acceptance turns on
// the result's columns, and writes declared for every pane that publishes.
func TestBuiltinTabMarkDeclarations(t *testing.T) {
	specs := tabsTestApp().Tabs().all()
	shape := make(map[string]bool, 4)
	writes := make(map[string][]SignalID, 8)
	for _, s := range specs {
		if s.ShapeContract {
			shape[s.ID] = true
		}
		if len(s.Writes) > 0 {
			writes[s.ID] = s.Writes
		}
	}
	assert.Equal(t, map[string]bool{"timeline": true, "world": true, "kanban": true, "network": true}, shape)

	require.Contains(t, writes, "map", "the Map publishes its viewport without being a PanelI")
	assert.Len(t, writes["map"], len(mapViewportSignals))
	assert.Contains(t, writes["world"], signalSelectionCountry)
	assert.Contains(t, writes["timeline"], signalTimelineMin)
	assert.Contains(t, writes["table"], signalSelection)
	assert.NotContains(t, writes, "detail", "Detail is a pure consumer")
	// The Network tab publishes the clicked vertex as a value, never as a row
	// cursor: a `selection` emit from a private lane is clamped away and would
	// jerk the other panels to row 0 (ADR-0129 §SD4).
	assert.Equal(t, []SignalID{signalSelectionKey}, writes["network"])
	assert.NotContains(t, writes["network"], signalSelection)

	for _, s := range specs {
		if s.ShapeContract {
			assert.NotNil(t, s.Panel, "tab %q is shape-gated but has no panel to ask", s.ID)
		}
	}
}

// The title composes: 6c's binding decoration, then the mark.
func TestTabTitleComposesBindingAndMark(t *testing.T) {
	app := tabsTestApp()
	app.currentSplit = markSplit()
	app.paramSlots = markSlots(signalSelectionCountry)
	app.frameSig = app.graph.signals()

	var world TabSpec
	for _, s := range app.Tabs().all() {
		if s.ID == "world" {
			world = s
		}
	}
	require.Equal(t, "World", world.Title)

	f := &TabFrame{Schema: markWorldOK}
	assert.Equal(t, "World *", app.tabTitle(&world, f))

	app.bindTab("world", mainNodeID)
	assert.Equal(t, "World · main *", app.tabTitle(&world, f),
		"the mark follows the binding decoration")

	f = &TabFrame{Schema: markWorldNo}
	assert.Equal(t, "World · main -", app.tabTitle(&world, f))

	assert.Equal(t, "Table", app.tabTitle(specByID(t, app, "table"), f),
		"an unmarked tab keeps its bare title")
}

func specByID(t *testing.T, app *PlayApp, id string) *TabSpec {
	t.Helper()
	for _, s := range app.Tabs().all() {
		if s.ID == id {
			return &s
		}
	}
	require.FailNow(t, "no such tab", id)
	return nil
}
