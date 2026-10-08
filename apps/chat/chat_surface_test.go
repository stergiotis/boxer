package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

func findCell(t *testing.T, m surfaceModel, g surfaceGroupE, instance uint64, appId string, op string) *surfaceCell {
	t.Helper()
	for i := range m.cells {
		c := &m.cells[i]
		if c.group == g && c.instance == instance && c.op == op && (appId == "" || c.app == appId) {
			return c
		}
	}
	require.Failf(t, "no cell", "%s %d %s %s", g, instance, appId, op)
	return nil
}

// The surface: two windows of play, one granted in act for one operation
// and one not at all; an edit ceiling; a stopped task whose call still
// counts; a launch granted and used; the desktop not granted.
func TestBuildSurface(t *testing.T) {
	edit := agent.Ceiling{Mode: agent.ModeAct, Effect: app.OperationEffectDocument, Launch: true, Desktop: true}
	in := surfaceInput{
		ceiling: &edit,
		apps: []agent.AppOperations{
			{App: "play", Display: "Play", Operations: []agent.Operation{
				{Name: "get_sql", Effect: "none"}, {Name: "set_sql", Effect: "document"}, {Name: "run", Effect: "run"}}},
			{App: "mdedit", Display: "Markdown"},
		},
		windows: []windowRow{{Key: "12", App: "play", Display: "Play"}, {Key: "17", App: "play", Display: "Play"}, {Key: "3", App: "mdedit", Display: "Markdown"}},
		grants: []grantRow{
			{Task: "task-1", Entries: []string{"17:play:observe"}, Revoked: "stopped", Created: "100"},
			{Task: "task-2", Entries: []string{"12:play:act:set_sql"}, Launches: []string{"mdedit:act:1"}, Created: "200"},
		},
		actions: []actionRow{
			{At: "150", Task: "task-1", Key: "a", Instance: "17", App: "play", Operation: "get_sql", Effect: "none", Decision: "dispatch", Phase: "completed"},
			{At: "250", Task: "task-2", Key: "b", Instance: "12", App: "play", Operation: "set_sql", Effect: "document", Decision: "dispatch", Phase: "accepted"},
			{At: "260", Task: "task-2", Key: "b", Instance: "12", App: "play", Operation: "set_sql", Effect: "document", Decision: "final", Phase: "rendered"},
			{At: "270", Task: "task-2", Key: "c", Instance: "12", App: "play", Operation: "get_sql", Effect: "none", Decision: "dispatch", Phase: "input_required"},
			{At: "280", Task: "task-2", Key: "d", App: "mdedit", Operation: agent.ActionOpenWindow, Decision: "final", Phase: "completed"},
			// The dispatcher's own read and a capture are no cell (ADR-0283 §SD1).
			{At: "285", Key: "e", Instance: "0", Operation: agent.ActionDescribe, Decision: "final", Phase: "completed"},
			{At: "286", Task: "task-2", Key: "f", Instance: "12", Operation: agent.ActionCapture, Decision: "final", Phase: "completed"},
		},
	}
	m := buildSurface(in)
	assert.Equal(t, "task-2", m.task)
	assert.Equal(t, 1, m.ended)

	set := findCell(t, m, groupWindow, 12, "", "set_sql")
	assert.Equal(t, agent.CellStatusGranted, set.status)
	assert.Equal(t, 1, set.calls(), "a dispatch and a final row are one call")
	assert.Equal(t, 1, set.uses[useDone])

	get := findCell(t, m, groupWindow, 12, "", "get_sql")
	assert.Equal(t, agent.CellStatusNotGranted, get.status, "the entry names set_sql only")
	assert.Equal(t, 1, get.uses[useAsked])

	assert.Equal(t, agent.CellStatusAboveCeiling, findCell(t, m, groupWindow, 12, "", "run").status)
	assert.Equal(t, agent.CellStatusGranted, findCell(t, m, groupWindow, 12, "", agent.ActionRaise).status)

	old := findCell(t, m, groupWindow, 17, "", "get_sql")
	assert.Equal(t, agent.CellStatusNotGranted, old.status, "the task that granted it ended")
	assert.Equal(t, 1, old.uses[useDone], "its call still counts")

	open := findCell(t, m, groupLaunch, 0, "mdedit", agent.ActionOpenWindow)
	assert.Equal(t, agent.CellStatusGranted, open.status)
	assert.Equal(t, 1, open.calls())
	assert.Equal(t, agent.CellStatusNotGranted, findCell(t, m, groupLaunch, 0, "play", agent.ActionOpenWindow).status)
	assert.Equal(t, agent.CellStatusNotGranted, findCell(t, m, groupDesktop, 0, "", agent.ActionArrange).status)

	for _, c := range m.cells {
		assert.NotEqual(t, uint64(3), c.instance, "a window whose app offers no operation, neither granted nor used, is not drawn")
		assert.True(t, agent.OnSurface(c.op), "%s is no cell", c.op)
	}
	n, used, calls := m.statusCounts()
	assert.Equal(t, 4, calls)
	assert.Equal(t, 2, used, "set_sql and opening mdedit")
	assert.Equal(t, 2, n[agent.CellStatusAboveCeiling], "run, in both windows")

	var sv surfaceView
	sv.model = &m
	sv.buildGraph()
	assert.Len(t, sv.nodes, 1+4+len(m.cells), "the root, a hub per window and for launches and the desktop, a leaf per cell")
	assert.Len(t, sv.edges, 4+len(m.cells))

	stream, err := surfaceArrow(m.cells)
	require.NoError(t, err)
	rec := decodeStream(t, stream)
	assert.Equal(t, int64(len(m.cells)), rec.NumRows())
}

func TestSqlStringQuotes(t *testing.T) {
	assert.Equal(t, `'a\'b\\c'`, sqlString(`a'b\c`))
}

// A call bucketed as done has an outcome: accepted and running are in
// flight, as the trail views count them (opwire.ResultE).
func TestUseFollowsTheSharedClassification(t *testing.T) {
	assert.Equal(t, useRunning, useOf("accepted"))
	assert.Equal(t, useRunning, useOf("running"))
	assert.Equal(t, useDone, useOf("applied"))
	assert.Equal(t, useAsked, useOf("input_required"))
	assert.Equal(t, useProposed, useOf("proposed"))
	assert.Equal(t, useRefused, useOf("stale"))
	assert.Equal(t, useFailed, useOf("expired"))
}
