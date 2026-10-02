package play

import (
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
	for _, name := range []string{opGetState, opDescribeResult, opSampleRows, opSetSql, opSetSignal, opShowPane} {
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
