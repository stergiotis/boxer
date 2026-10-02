package play

import (
	"strings"

	"github.com/apache/arrow-go/v18/arrow"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

func TestPlayCatalogRegisters(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, name := range []string{opGetState, opDescribeResult, opSampleRows, opSetSql, opSetSignal, opShowPane, opListPanes, opBindPane, "query_state", "query_machine", opListSnippets, opReadSnippet, opListFunctions} {
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

// A run the grant does not cover is refused when it is asked for, naming
// the destination; a grant that lists it lets the run through.
func TestAnUncoveredRunIsRefusedWithTheDestinationItNeeds(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}, opRun, nil)
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, refusal.Destinations)
	assert.False(t, l.inner.requestRun, "a refused run is not requested")

	obo := &app.OnBehalfOf{Task: "t", Epoch: 1, Destinations: []string{"clickhouse:ch.example:8123"}}
	_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, opRun, nil)
	require.NoError(t, err)
	assert.True(t, l.inner.requestRun)
}

// The result's lifecycle is mounted: query_machine is the chip's graph,
// query_state the current state and what moves it.
func TestTheQueryMachineIsMounted(t *testing.T) {
	_, h := opsLauncher(t)
	raw, err := h.Snapshot().Query("query_machine", nil)
	require.NoError(t, err)
	m, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Contains(t, m.States, "rows (stale)")
	raw, err = h.Snapshot().Query("query_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "idle", st.Current)
	require.Len(t, st.Next, 1)
	assert.Equal(t, opfsm.Edge{From: "idle", To: "running", Label: "Run"}, st.Next[0])
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

func queryOp[T any](t *testing.T, h app.OperationsHandlerI, op string, args any) (out T) {
	t.Helper()
	var raw []byte
	if args != nil {
		var err error
		raw, err = buscodec.Encode(args)
		require.NoError(t, err)
	}
	res, err := h.Snapshot().Query(op, raw)
	require.NoError(t, err)
	out, err = buscodec.Decode[T](res)
	require.NoError(t, err)
	return
}

// The snippet libraries reach an agent: listed, found by words, and read
// with their SQL ready for set_sql.
func TestTheSnippetsAreInTheCatalog(t *testing.T) {
	_, h := opsLauncher(t)
	all := queryOp[SnippetList](t, h, opListSnippets, nil)
	require.NotEmpty(t, all.Snippets)
	assert.Equal(t, builtinSnippetsKey, all.Snippets[0].Library)

	found := queryOp[SnippetList](t, h, opListSnippets, SnippetSearchArgs{Search: "memberships"})
	require.NotEmpty(t, found.Snippets)
	assert.Less(t, len(found.Snippets), len(all.Snippets), "a search narrows the list")

	sn := queryOp[Snippet](t, h, opReadSnippet, SnippetArgs{Library: found.Snippets[0].Library, Section: found.Snippets[0].Section})
	require.NotEmpty(t, sn.Sql)
	assert.Contains(t, strings.ToUpper(sn.Sql[0]), "SELECT")
	assert.NotContains(t, sn.Sql[0], "```")

	_, err := h.Snapshot().Query(opReadSnippet, mustEncode(t, SnippetArgs{Library: builtinSnippetsKey, Section: "no-such-section"}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
}

// The vocabulary reaches an agent with where each function runs; whether
// the endpoint has a server function stays unknown until the pane probed.
func TestTheVocabularyIsInTheCatalog(t *testing.T) {
	_, h := opsLauncher(t)
	queryOp[FunctionList](t, h, opListFunctions, nil) // the host's registry: empty until its wiring runs
	_, err := h.Snapshot().Query(opListFunctions, mustEncode(t, FunctionArgs{Where: "moon"}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)

	r := sqlvocab.NewRegistry()
	require.NoError(t, RegisterVocabulary(r))
	all, err := listFunctions(r, nil, false, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, all.Functions)
	assert.False(t, all.Probed)
	client, err := listFunctions(r, nil, false, "", "client")
	require.NoError(t, err)
	require.NotEmpty(t, client.Functions)
	for _, f := range client.Functions {
		assert.Equal(t, "client", f.Where)
		assert.Empty(t, f.Installed, "installed is a server function's question")
		assert.Contains(t, f.Call, f.Name+"(")
	}
	server, err := listFunctions(r, nil, false, "", "server")
	require.NoError(t, err)
	require.NotEmpty(t, server.Functions)
	assert.Equal(t, "unknown", server.Functions[0].Installed)
	probed, err := listFunctions(r, map[string]string{server.Functions[0].Name: ""}, true, "", "server")
	require.NoError(t, err)
	assert.Equal(t, "yes", probed.Functions[0].Installed)
	if len(probed.Functions) > 1 {
		assert.Equal(t, "no", probed.Functions[1].Installed)
	}
}

func mustEncode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := buscodec.Encode(v)
	require.NoError(t, err)
	return b
}

// list_panes carries the extras beside origin's verdict: the three-way
// Draws (unknown before anything has landed), which pane is raised, and
// every signal a pane publishes beside the ones the buffer reads.
func TestListPanesReportsDrawsRaisedAndPublishes(t *testing.T) {
	_, h := opsLauncher(t)
	panes := queryOp[PanesState](t, h, opListPanes, nil)
	require.NotEmpty(t, panes.Panes)
	var table, world *PaneState
	for i := range panes.Panes {
		switch panes.Panes[i].Pane {
		case "table":
			table = &panes.Panes[i]
		case "world":
			world = &panes.Panes[i]
		}
	}
	require.NotNil(t, table)
	assert.Equal(t, PaneDrawUnknown, table.Draws, "nothing has landed")
	require.NotNil(t, world)
	assert.Contains(t, world.Publishes, string(signalSelectionCountry))
	assert.Empty(t, world.Writes, "the buffer reads none of them")
}
