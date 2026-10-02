package play

import (
	"github.com/apache/arrow-go/v18/arrow"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func TestPlayCatalogRegisters(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, name := range []string{opGetState, opDescribeResult, opSampleRows, opSetSql, opSetSignal, opShowPane, opListPanes, opBindPane} {
		spec, ok := m.Operations.Lookup(name)
		require.True(t, ok, name)
		assert.True(t, spec.Agents, name)
	}
}

func opsLauncher(t *testing.T) (*PlayLauncher, app.OperationsHandlerI) {
	t.Helper()
	inner := NewPlayApp(nil, newLiveQueryGraph(nil, memory.NewGoAllocator(), 4), "SELECT 1", nil)
	l := &PlayLauncher{inner: inner}
	return l, l.Operations()
}

func TestSetSqlReplacesTheBufferAtOnce(t *testing.T) {
	l, h := opsLauncher(t)
	args, err := buscodec.Encode(SetSqlArgs{Sql: "SELECT 2"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opSetSql, args)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", l.inner.sql)
	assert.Equal(t, "SELECT 2", h.ResourceValue(opsResSql))

	raw, err := h.Snapshot().Query(opGetState, nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[PlayState](raw)
	require.NoError(t, err)
	assert.Equal(t, "SELECT 2", st.Sql)
	assert.Equal(t, "idle", st.Result.Phase)
	assert.Empty(t, st.Destination, "no client, no endpoint to name")

	// ADR-0270 §SD2: the destination a grant must list for the endpoint.
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	raw, err = h.Snapshot().Query(opGetState, nil)
	require.NoError(t, err)
	st, err = buscodec.Decode[PlayState](raw)
	require.NoError(t, err)
	assert.Equal(t, "clickhouse:ch.example:8123", st.Destination)
}

func TestSetSignalCarriesTheTaskAsWriter(t *testing.T) {
	l, h := opsLauncher(t)
	before := h.ResourceValue(opsResSignals)
	args, err := buscodec.Encode(SetSignalArgs{Name: "threshold", Value: "5"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opSetSignal, args)
	require.NoError(t, err)
	assert.NotEqual(t, before, h.ResourceValue(opsResSignals))
	writer, _ := l.inner.graph.signalWriterFor("threshold")
	assert.Equal(t, "task:t", writer)
	assert.False(t, isHumanSignalWriter("task:t"), "the Live breaker counts a task as a machine writer")
}

func TestShowPaneRefusesAnUnknownPane(t *testing.T) {
	_, h := opsLauncher(t)
	args, err := buscodec.Encode(ShowPaneArgs{Pane: "no-such-pane"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{}, opShowPane, args)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	args, err = buscodec.Encode(ShowPaneArgs{Pane: "table"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{}, opShowPane, args)
	require.NoError(t, err)
}

func TestSampleRowsWithoutAResultIsRefused(t *testing.T) {
	_, h := opsLauncher(t)
	_, err := h.Snapshot().Query(opSampleRows, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
}

func TestRunAndSetParamAreInTheRegisteredCatalog(t *testing.T) {
	m, ok := app.LookupManifest((&PlayLauncher{}).Manifest().Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(m.Id))
	for _, name := range []string{opRun, opSetParam} {
		_, found := m.Operations.Lookup(name)
		assert.True(t, found, name)
	}
}

func TestRunNeedsAnAgentsContext(t *testing.T) {
	l, h := opsLauncher(t)
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opRun, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, opRun, nil)
	require.NoError(t, err)
	assert.True(t, l.inner.requestRun)
	assert.Equal(t, obo, l.inner.takeAgentForRun(false), "the task asked for this run")
}

func TestTheAgentMarkClearsWhenThePersonEdits(t *testing.T) {
	l, h := opsLauncher(t)
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	args, err := buscodec.Encode(SetSqlArgs{Sql: "SELECT 3"})
	require.NoError(t, err)
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, opSetSql, args)
	require.NoError(t, err)
	p := l.inner
	p.settleAgentMark()
	p.checkAgentMark()
	assert.Equal(t, obo, p.takeAgentForRun(true), "a Live rerun after the task's input carries its context")
	p.sql = "SELECT 3 -- the person's edit"
	p.checkAgentMark()
	assert.Nil(t, p.takeAgentForRun(true), "after the person's edit the window's work is theirs")
}

// The mark reaches the window's client the moment it changes, so a pane's
// lane started in the same frame is judged by it (ADR-0270 §SD2).
func TestTheAgentMarkReachesTheClient(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	p.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1}
	p.markAgent(obo)
	assert.Equal(t, obo, p.client.agentMark.Load())
	assert.Nil(t, p.takeAgentForRun(false), "the person's Run")
	assert.Nil(t, p.client.agentMark.Load(), "clears the mark before the run is sent")
}

// listPanes asks the window's snapshot for list_panes.
func listPanes(t *testing.T, h app.OperationsHandlerI) (out PaneList) {
	t.Helper()
	raw, err := h.Snapshot().Query(opListPanes, nil)
	require.NoError(t, err)
	out, err = buscodec.Decode[PaneList](raw)
	require.NoError(t, err)
	return
}

func paneNamed(t *testing.T, l PaneList, name string) (ps PaneState) {
	t.Helper()
	for _, p := range l.Panes {
		if p.Name == name {
			return p
		}
	}
	require.Failf(t, "no pane", "%s", name)
	return
}

// ADR-0270 M3: list_panes reports the dock's panes with the strip's
// verdict — unknown before anything has run — and bind_pane points a result
// pane at a split node of the last run, or back.
func TestListAndBindPanes(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	panes := listPanes(t, h)
	require.NotEmpty(t, panes.Panes)
	table := paneNamed(t, panes, "table")
	assert.True(t, table.Bindable)
	assert.Equal(t, PaneDrawUnknown, table.Draws, "nothing has run")
	assert.Empty(t, panes.Nodes)

	bind := func(pane, node string) error {
		args, err := buscodec.Encode(BindPaneArgs{Pane: pane, Node: node})
		require.NoError(t, err)
		_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opBindPane, args)
		return err
	}
	assert.ErrorContains(t, bind("table", "edges"), "no split node edges", "before a run there is no split")
	assert.ErrorContains(t, bind("nosuch", ""), "no pane nosuch")
	for _, ps := range panes.Panes {
		if !ps.Bindable {
			assert.ErrorContains(t, bind(ps.Name, "edges"), "binds to no node", ps.Name)
			break
		}
	}

	p.currentSplit = splitResult{Nodes: []splitNode{{ID: "edges"}, {ID: "main"}}, Sink: "main"}
	before := h.ResourceValue(opsResPanes)
	require.NoError(t, bind("table", "edges"))
	assert.Equal(t, NodeID("edges"), p.tabBindings["table"])
	assert.NotEqual(t, before, h.ResourceValue(opsResPanes), "the binding moves the panes resource")
	panes = listPanes(t, h)
	assert.Equal(t, "edges", paneNamed(t, panes, "table").BoundTo)
	assert.Equal(t, []string{"edges", "main"}, panes.Nodes)

	require.NoError(t, bind("table", ""))
	assert.NotContains(t, p.tabBindings, "table", "left out, the node unbinds")
}

// stubPanel needs one main-channel input and rejects a schema without the
// column it wants.
type stubPanel struct{ want string }

func (inst stubPanel) ID() PanelID { return "stub" }
func (inst stubPanel) Channels() []ChannelSpec {
	return []ChannelSpec{{ID: chMain, Required: true}}
}
func (inst stubPanel) AcceptForChannel(_ ChannelID, schema *arrow.Schema, _ SignalEnvI) (claim ChannelClaim, reason string) {
	if schema == nil {
		return nil, "Run a query to see results."
	}
	if len(schema.FieldIndices(inst.want)) == 0 {
		return nil, "needs a " + inst.want + " column"
	}
	return nil, ""
}
func (inst stubPanel) Render(map[ChannelID]ChannelResult, SignalEmitterI) {}

// A pane draws when every required channel is offered a real schema and
// none rejects it; it does not when one does, in the panel's own words; and
// before anything has landed the answer is unknown.
func TestPaneDraws(t *testing.T) {
	schema := arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.BinaryTypes.String}}, nil)
	draws, _ := paneDraws(stubPanel{want: "id"}, tabVerdict{schema: schema})
	assert.Equal(t, PaneDrawYes, draws)
	draws, reason := paneDraws(stubPanel{want: "country"}, tabVerdict{schema: schema})
	assert.Equal(t, PaneDrawNo, draws)
	assert.Equal(t, "needs a country column", reason)
	draws, reason = paneDraws(stubPanel{want: "id"}, tabVerdict{})
	assert.Equal(t, PaneDrawUnknown, draws)
	assert.Equal(t, "Run a query to see results.", reason)
}
