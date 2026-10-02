package play

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// gestureLauncher is a play window served by a host engine, with the
// gesture path PlayLauncher.Frame installs (ADR-0270 §SD6).
func gestureLauncher(t *testing.T) (*PlayLauncher, *opengine.Engine) {
	t.Helper()
	l, h := opsLauncher(t)
	eng := opengine.New(playOps.Catalog(), h)
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)
	l.inner.gestureCtx = ctx
	eng.BeginFrame()
	return l, eng
}

func lastEntry(t *testing.T, eng *opengine.Engine) opengine.LogEntry {
	t.Helper()
	log := eng.Log()
	require.NotEmpty(t, log)
	return log[len(log)-1]
}

func TestPersonGesturesAreLoggedAsThePerson(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner

	p.personRun(true)
	e := lastEntry(t, eng)
	assert.Equal(t, opRun, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Equal(t, opwire.PhaseApplied, e.Phase, "the person's run needs no agent context")
	assert.Equal(t, []string{opsResResult}, e.Resources)
	assert.True(t, p.requestRun)
	assert.True(t, p.requestSubquery)
	assert.False(t, p.runIsAuto)
	assert.Nil(t, p.takeAgentForRun(false), "the person's run is the person's")

	p.personSetSql("SELECT 3")
	e = lastEntry(t, eng)
	assert.Equal(t, opSetSql, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResSql))
	assert.Equal(t, "SELECT 3", p.sql)

	p.personSetSignal("threshold", "7", signalWriterHistory)
	e = lastEntry(t, eng)
	assert.Equal(t, opSetSignal, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResSignals))
	assert.Empty(t, p.gestureSignalWriter)
	found := false
	for _, r := range p.graph.signalRows() {
		if r.Name == "threshold" {
			found = true
			assert.Equal(t, "7", r.Raw)
			// The store keeps the surface's writer, so the Live breaker
			// counts the person's write as human.
			assert.Equal(t, signalWriterHistory, r.Writer)
			assert.True(t, isHumanSignalWriter(r.Writer))
		}
	}
	assert.True(t, found)

	p.personShowPane("table")
	e = lastEntry(t, eng)
	assert.Equal(t, opShowPane, e.Op)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResPanes))
	slug, _ := p.tabs.slugForDockID(p.raisedTab)
	assert.Equal(t, "table", slug)
}

func TestPersonSignalDefaultsToTheSignalsSection(t *testing.T) {
	l, _ := gestureLauncher(t)
	args, err := buscodec.Encode(SetSignalArgs{Name: "k", Value: "1"})
	require.NoError(t, err)
	_, err = l.Operations().ApplyCommand(app.OperationCall{Writer: opwire.WriterPerson}, opSetSignal, args)
	require.NoError(t, err)
	for _, r := range l.inner.graph.signalRows() {
		if r.Name == "k" {
			assert.Equal(t, signalWriterEditor, r.Writer)
		}
	}
}

func TestPersonGesturesApplyWithoutAHost(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	require.Nil(t, p.gestureCtx, "an embedder's window gets no gesture path")

	p.personRun(false)
	assert.True(t, p.requestRun)
	assert.False(t, p.requestSubquery)

	p.personSetSql("SELECT 4")
	assert.Equal(t, "SELECT 4", p.sql)

	p.personSetSignal("threshold", "9", signalWriterEditor)
	for _, r := range p.graph.signalRows() {
		if r.Name == "threshold" {
			assert.Equal(t, "9", r.Raw)
			assert.Equal(t, signalWriterEditor, r.Writer)
		}
	}

	p.personShowPane("table")
	slug, _ := p.tabs.slugForDockID(p.raisedTab)
	assert.Equal(t, "table", slug)
}

func TestAgentRunStillNeedsItsContext(t *testing.T) {
	_, h := opsLauncher(t)
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opRun, nil)
	require.Error(t, err)
}

func TestBindPaneBindsAPanelToANodeOfTheSplit(t *testing.T) {
	l, eng := gestureLauncher(t)
	p := l.inner
	p.currentSplit = splitResult{
		Nodes: []splitNode{{ID: "edges", Kind: splitNodeCTE}, {ID: mainNodeID, Kind: splitNodeStatement}},
		Sink:  mainNodeID,
	}
	h := l.Operations()
	bind := func(in BindPaneArgs) error {
		args, err := buscodec.Encode(in)
		require.NoError(t, err)
		_, err = h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opBindPane, args)
		return err
	}
	require.Error(t, bind(BindPaneArgs{Pane: "no-such-pane", Node: "edges"}))
	require.Error(t, bind(BindPaneArgs{Pane: "editor", Node: "edges"}), "a tool pane draws no result")
	err := bind(BindPaneArgs{Pane: "table", Node: "nodes"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "edges", "the refusal names the nodes there are")
	assert.Empty(t, p.tabBindings)

	before := h.ResourceValue(opsResPanes)
	require.NoError(t, bind(BindPaneArgs{Pane: "table", Node: "edges"}))
	assert.Equal(t, NodeID("edges"), p.tabBindings["table"])
	assert.NotEqual(t, before, h.ResourceValue(opsResPanes))

	raw, err := h.Snapshot().Query(opListPanes, nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[PanesState](raw)
	require.NoError(t, err)
	assert.Equal(t, []string{"edges", string(mainNodeID)}, st.Nodes)
	var table, editor *PaneState
	for i := range st.Panes {
		switch st.Panes[i].Pane {
		case "table":
			table = &st.Panes[i]
		case "editor":
			editor = &st.Panes[i]
		}
	}
	require.NotNil(t, table)
	require.NotNil(t, editor)
	assert.True(t, table.Panel)
	assert.False(t, editor.Panel)

	// The Graph pane's toggle and clear go through bind_pane as the person.
	p.personBindPane("table", "")
	e := lastEntry(t, eng)
	assert.Equal(t, opBindPane, e.Op)
	assert.Equal(t, opwire.WriterPerson, e.Writer)
	assert.Empty(t, p.tabBindings)

	p.personBindPane("table", "edges")
	p.personBindPane("chart", "edges")
	p.personClearBindings()
	assert.Empty(t, p.tabBindings)
	assert.Equal(t, opwire.WriterPerson, eng.Writer(opsResPanes))
}
