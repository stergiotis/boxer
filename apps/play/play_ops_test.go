package play

import (
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"testing"

	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/sqlvocab"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
	"github.com/stergiotis/boxer/public/semistructured/leeway/lwsql"
)

func TestPlayCatalogRegisters(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	require.NoError(t, m.Operations.Validate())
	for _, name := range []string{opGetState, opDescribeResult, opSampleRows, opSetSql, opSetSignal, opShowPane, opListPanes, opBindPane, "query_state", "query_machine", opListSnippets, opReadSnippet, opListFunctions, opListDatasets, opBindDataset, opGetChart, opSetChartOptions, opGetDist, opSetDistOptions, opGetSeries, opSetSeriesOptions, opGetTimeline, opSetTimelineOptions, opSetTimelineWindow, opGetTreemap, opSetTreemapOptions, opSelectTreemapNode, opGetIcicle, opSetIcicleOptions, opSelectIcicleFrame, opGetKanban, opGetCards, opSetCardsOptions, opGetWorld, opSetWorldOptions, opGetVectorfield, opSetVectorfieldView, opGetNetwork, opSetNetworkOptions, opSelectNetworkNode, opGetGraphview, opSetGraphviewOptions, opSelectGraphviewNodes, opGetSankey, opSetSankeyOptions, opSelectSankeyNode, opGetTable, opSetTableOptions, opGetFiles, opSetFilesOptions, opSelectFilesPath, opGetChatPane, opGetMap, opSetMapView, opSetMapOptions, opGetExperiments, opSetExperiments, opSqlFlow, opExplainSql, opLookupDocs, opGetDetail, opListGlosses, opCompleteSql, opEndpointFunctions, opGetQueryGraph, opObserveNode, opListHistory, opCancelRun, opCancelProjection, opPublishProjection, opDeleteSignal, opSetRunOptions} {
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

// ADR-0270, update of 2026-10-05: describe_result quotes the statement's
// and the data's column names; set_signal is a document command on signals.
func TestDescribeResultAndSetSignalCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opDescribeResult)
	require.True(t, ok)
	assert.True(t, spec.Untrusted)
	assert.Equal(t, app.OperationEffectNone, spec.Effect)
	assert.Equal(t, []string{opsResResult}, spec.Reads)
	spec, ok = m.Operations.Lookup(opSetSignal)
	require.True(t, ok)
	assert.Equal(t, app.OperationEffectDocument, spec.Effect)
	assert.Equal(t, []string{opsResSignals}, spec.Writes)
	assert.False(t, spec.Untrusted)
}

// The person's "observe in panels" changes what list_panes reports, so it
// moves the panes resource.
func TestObservingANodeMovesThePanesResource(t *testing.T) {
	l, h := opsLauncher(t)
	before := h.ResourceValue(opsResPanes)
	l.inner.observedNode = "edges"
	assert.NotEqual(t, before, h.ResourceValue(opsResPanes))
}

// A pane that reads its CTEs off the split by name ignores a binding, so
// bind_pane refuses one and names what the pane reads.
func TestBindPaneRefusesAFramelessPane(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.currentSplit = splitResult{
		Nodes: []splitNode{{ID: "edges", Kind: splitNodeCTE}, {ID: mainNodeID, Kind: splitNodeStatement}},
		Sink:  mainNodeID,
	}
	bind := func(pane string) error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opBindPane, mustEncode(t, BindPaneArgs{Pane: pane, Node: "edges"}))
		return err
	}
	for pane, cte := range map[string]string{"network": "`edges`", "graphview": "`graph_opts`", "sankey": "`flows`", "vectorfield": "`vector_field`"} {
		err := bind(pane)
		var refusal *app.OperationRefusal
		require.ErrorAs(t, err, &refusal, pane)
		assert.Contains(t, err.Error(), cte, pane)
		assert.Contains(t, err.Error(), "set_sql", pane)
	}
	assert.Empty(t, p.tabBindings)
	require.NoError(t, bind("table"))
}

// A subquery run is checked at the call on the text it ships, as a whole
// run is; before, it was accepted and failed in the status line.
func TestAnUncoveredSubqueryRunIsRefusedAtTheCall(t *testing.T) {
	l, h := opsLauncher(t)
	l.inner.client = NewClient(ClientConfig{URL: "http://ch.example:8123/"}, nil)
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}, opRun,
		mustEncode(t, RunArgs{Subquery: true}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Equal(t, []string{"clickhouse:ch.example:8123"}, refusal.Destinations)
	assert.False(t, l.inner.requestRun)
}

// The window's class ceiling is judged at the call, ahead of the agent
// limits, so the refusal names the parameter; a write is refused there too.
func TestARunTheCeilingOrTheLimitsBlockIsRefusedAtTheCall(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.client = NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	obo := &app.OnBehalfOf{Task: "t", Epoch: 1, Destinations: []string{"clickhouse:localhost:8123"}}
	run := func() error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, opRun, nil)
		return err
	}

	p.swapSql("-- play: expr cond = number IN (SELECT h FROM url('http://x/y', 'CSV'))\n" +
		"SELECT number FROM numbers(10) WHERE {cond:Expr}")
	p.SetSecurityCeiling(analysis.QuerySecurityRead)
	err := run()
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, err.Error(), "{cond}")
	assert.False(t, p.requestRun)

	p.SetSecurityCeiling(analysis.QuerySecurityMutating)
	p.swapSql("INSERT INTO t SELECT 1")
	err = run()
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, err.Error(), "mutating")
	assert.False(t, p.requestRun)
}

// A pane output — re-published as the pane draws, or never followed — is
// refused for an agent, naming the pane's command; the person's Signals
// section still writes it.
func TestSetSignalRefusesPaneOutputs(t *testing.T) {
	l, h := opsLauncher(t)
	set := func(writer, name string) error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: writer}, opSetSignal, mustEncode(t, SetSignalArgs{Name: name, Value: "1"}))
		return err
	}
	for name, cmd := range map[SignalID]string{
		signalTimelineFrom: "set_timeline_window", signalTimelineMax: "get_timeline",
		signalVfMinLat: "set_vectorfield_view", "vp_min_x": "set_map_view", signalGvSelection: "select_graphview_nodes",
	} {
		err := set("task:t", string(name))
		var refusal *app.OperationRefusal
		require.ErrorAs(t, err, &refusal, name)
		assert.Contains(t, err.Error(), cmd, name)
		_, held := l.inner.graph.signalWriterFor(name)
		assert.False(t, held, name)
	}
	require.NoError(t, set("task:t", string(signalVfT)), "the Vector field pane follows vf_t")
	require.NoError(t, set("task:t", string(signalSelectionCountry)))
	require.NoError(t, set(opwire.WriterPerson, string(signalTimelineFrom)), "the person's Signals section")
}

// selection moves with its companions: the node it indexes, and the row's
// key, read off the result the panels draw.
func TestSetSignalSelectionStampsItsCompanions(t *testing.T) {
	l, h := opsLauncher(t)
	g := l.inner.graph
	rec := int64Rec(selectionKeyCol, 10, 20, 30)
	g.mainLane.finish("SELECT", nil, time.Now(), rec, rec.Schema(), rec.NumRows(), Summary{}, nil, runstream.Terminal{})
	set := func(in SetSignalArgs) error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opSetSignal, mustEncode(t, in))
		return err
	}
	require.NoError(t, set(SetSignalArgs{Name: string(signalSelection), Value: "1"}))
	sig := g.signals()
	for name, want := range map[SignalID]string{signalSelection: "1", signalSelectionNode: string(mainNodeID), signalSelectionKey: "20"} {
		p, ok := sig.Get(string(name))
		require.True(t, ok, name)
		assert.Equal(t, want, p.Raw, name)
		writer, _ := g.signalWriterFor(name)
		assert.Equal(t, "task:t", writer, name)
	}
	require.NoError(t, set(SetSignalArgs{Name: string(signalSelection), Value: "2"}))
	p, _ := g.signals().Get(string(signalSelectionKey))
	assert.Equal(t, "30", p.Raw, "the key follows the row")

	var refusal *app.OperationRefusal
	require.ErrorAs(t, set(SetSignalArgs{Name: string(signalSelection), Value: "3"}), &refusal, "past the last row")
	require.ErrorAs(t, set(SetSignalArgs{Name: string(signalSelection), Value: "first"}), &refusal, "not a row")
	require.ErrorAs(t, set(SetSignalArgs{Name: string(signalSelection), Value: "0", Node: "edges"}), &refusal, "a node not on screen")
	require.ErrorAs(t, set(SetSignalArgs{Name: "threshold", Value: "0", Node: string(mainNodeID)}), &refusal, "node is selection's")
	p, _ = g.signals().Get(string(signalSelection))
	assert.Equal(t, "2", p.Raw, "a refused write leaves the cursor")
}

// list_functions v2: the search is the Vocabulary pane's (every word must
// match, across name, doc and family), a client macro names the server
// functions its expansion lacks once the probe answered, and what the
// endpoint carries beyond the rosters is listed as the pane lists it.
func TestListFunctionsSearchesAsThePaneDoesAndNamesWhatIsMissing(t *testing.T) {
	r := sqlvocab.NewRegistry()
	require.NoError(t, RegisterVocabulary(r))
	all, err := listFunctions(r, nil, false, "", "")
	require.NoError(t, err)

	var macro FunctionInfo
	for _, f := range all.Functions {
		if len(f.Dependencies) > 0 && strings.Contains(f.Doc, " ") {
			macro = f
			break
		}
	}
	require.NotEmpty(t, macro.Name, "the build declares a macro with dependencies")
	assert.Empty(t, macro.MissingDependencies, "unprobed: not known, not all present")

	// Two words, one from the name and one from the doc: the v1 search took
	// the whole query as one substring and found nothing.
	word := strings.Fields(macro.Doc)[0]
	found, err := listFunctions(r, nil, false, macro.Name+" "+word, "")
	require.NoError(t, err)
	names := make([]string, 0, len(found.Functions))
	for _, f := range found.Functions {
		names = append(names, f.Name)
	}
	assert.Contains(t, names, macro.Name)

	installed := map[string]string{"myOwnHelper": "", "not plain!": ""}
	for _, f := range all.Functions {
		if f.Where == "server" {
			installed[f.Name] = ""
		}
	}
	delete(installed, macro.Dependencies[0])
	probed, err := listFunctions(r, installed, true, "", "")
	require.NoError(t, err)
	assert.Equal(t, 1, probed.UnlistedExtras, "a name that is not an identifier is counted, not repeated")
	var sawMacro, sawExtra bool
	for _, f := range probed.Functions {
		if f.Name == macro.Name && f.Where == macro.Where {
			sawMacro = true
			assert.Contains(t, f.MissingDependencies, macro.Dependencies[0])
		}
		if f.Name == "myOwnHelper" {
			sawExtra = true
			assert.Equal(t, "server", f.Where)
			assert.Equal(t, "yes", f.Installed)
			assert.Equal(t, vocabFamilyUndeclared, f.Family)
		}
	}
	assert.True(t, sawMacro)
	assert.True(t, sawExtra || probed.Truncated, "the endpoint's own helper is listed")
}

// sample_rows v2: a NULL cell is listed apart from an empty one (ADR-0269
// §SD10), and a leeway column is asked for and reported by its handle as
// well as by its physical name.
func TestSampleRowsKeepsNullApartAndSpeaksHandles(t *testing.T) {
	l, h := opsLauncher(t)
	// A result that is wholly leeway-shaped, as the Table's header labels
	// only such a result; note stands for a column that carries NULLs.
	names := slices.Clone(schemaWithSymbol)
	const note = "tv:symbol:hr:hr:u64:47:::0::data"
	labels := lwsql.BuildLabels(names)
	require.Equal(t, "symbol:value", labels["tv:symbol:value:val:s:124::I:0::data"], "the fixture is leeway-shaped")

	mem := memory.NewGoAllocator()
	fields := make([]arrow.Field, 0, len(names))
	cols := make([]arrow.Array, 0, len(names))
	for _, n := range names {
		b := array.NewStringBuilder(mem)
		switch n {
		case note:
			b.AppendNull()
			b.Append("")
		default:
			b.Append("a")
			b.Append("b")
		}
		cols = append(cols, b.NewArray())
		b.Release()
		fields = append(fields, arrow.Field{Name: n, Type: arrow.BinaryTypes.String, Nullable: true})
	}
	schema := arrow.NewSchema(fields, nil)
	rec := array.NewRecordBatch(schema, cols, 2)
	l.inner.graph.mainLane.finish("SELECT …", nil, time.Now(), rec, schema, 2, Summary{}, nil, runstream.Terminal{})

	out := queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Fields: []string{"symbol:value", note}})
	require.Equal(t, []string{"tv:symbol:value:val:s:124::I:0::data", note}, out.Columns)
	require.Equal(t, []string{"symbol:value", "symbol:hr"}, out.Handles)
	require.Equal(t, [][]string{{"a", ""}, {"b", ""}}, out.Rows)
	require.Equal(t, [][]int32{{1}, {}}, out.Nulls, "row 0's note is NULL, row 1's is empty text")

	out = queryOp[SampleRows](t, h, opSampleRows, SampleArgs{Fields: []string{"tv:symbol:value:val:s:124::I:0::data"}})
	require.Equal(t, []string{"symbol:value"}, out.Handles, "the physical name is still accepted")
	require.Nil(t, out.Nulls, "no NULL in the sample, no Nulls")

	_, err := h.Snapshot().Query(opSampleRows, mustEncode(t, SampleArgs{Fields: []string{"symbol:nope"}}))
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)

	m := (&PlayLauncher{}).Manifest()
	spec, ok := m.Operations.Lookup(opSampleRows)
	require.True(t, ok)
	require.Equal(t, 2, int(spec.Version))
	require.True(t, spec.Untrusted)
	require.Equal(t, app.OperationEffectNone, spec.Effect)
	require.Equal(t, []string{opsResResult}, spec.Reads)
}
