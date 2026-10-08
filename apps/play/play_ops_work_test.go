package play

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/db/clickhouse/dsl/nanopass/analysis"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/runstream"
)

func TestWorkOpsCatalogEntries(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	for _, c := range []struct {
		name    string
		effect  app.OperationEffectE
		reads   []string
		writes  []string
		gesture bool
	}{
		{opCancelRun, app.OperationEffectRun, nil, []string{opsResResult}, true},
		{opCancelProjection, app.OperationEffectView, nil, []string{opsResProjection}, true},
		{opPublishProjection, app.OperationEffectConsequential, []string{opsResProjection}, []string{opsResDatasets}, true},
		{opDeleteSignal, app.OperationEffectDocument, nil, []string{opsResSignals}, true},
		{opSetRunOptions, app.OperationEffectDocument, nil, []string{opsResRunOptions}, false},
	} {
		spec, ok := m.Operations.Lookup(c.name)
		require.True(t, ok, c.name)
		assert.Equal(t, c.effect, spec.Effect, c.name)
		assert.False(t, spec.Untrusted, c.name)
		assert.True(t, spec.Agents, c.name)
		assert.Equal(t, c.reads, spec.Reads, c.name)
		assert.Equal(t, c.writes, spec.Writes, c.name)
		assert.Equal(t, c.gesture, spec.Gesture != "", c.name)
	}
	run, _ := m.Operations.Lookup(opRun)
	assert.EqualValues(t, 2, run.Version, "run gained Statement")
	proj, _ := m.Operations.Lookup(opGetProjection)
	assert.EqualValues(t, 3, proj.Version, "get_projection returns points only on request")
}

// Every operation list_panes names for a pane is in the catalog, and the
// panes with a reading of their own point at it first.
func TestListPanesNamesEachPanesOperations(t *testing.T) {
	m := (&PlayLauncher{}).Manifest()
	for pane, ops := range paneOperations {
		require.NotEmpty(t, ops, pane)
		for _, op := range ops {
			_, ok := m.Operations.Lookup(op)
			assert.True(t, ok, pane+": "+op)
		}
	}
	_, h := opsLauncher(t)
	raw, err := h.Snapshot().Query(opListPanes, nil)
	require.NoError(t, err)
	panes, err := buscodec.Decode[PanesState](raw)
	require.NoError(t, err)
	seen := 0
	for _, ps := range panes.Panes {
		switch ps.Pane {
		case tablePaneId:
			assert.Equal(t, opGetTable, ps.Ops[0])
			seen++
		case projectionPaneId:
			assert.Contains(t, ps.Ops, opCancelProjection)
			assert.Contains(t, ps.Ops, opPublishProjection)
			seen++
		}
	}
	assert.Equal(t, 2, seen)
}

// A task stops only its own run; the person stops any; a run asked for
// and not yet started is withdrawn.
func TestCancelRunStopsOnlyTheTasksOwnRun(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	cancel := func(task string) error {
		call := app.OperationCall{Writer: "person"}
		if task != "" {
			call = app.OperationCall{Writer: "task:" + task, OnBehalfOf: &app.OnBehalfOf{Task: task, Epoch: 1}}
		}
		_, err := h.ApplyCommand(call, opCancelRun, nil)
		return err
	}
	var refusal *app.OperationRefusal
	require.ErrorAs(t, cancel("t"), &refusal)
	assert.Contains(t, refusal.Error(), "no run is in flight")
	_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t"}, opCancelRun, nil)
	require.ErrorAs(t, err, &refusal, "a task's call carries its context")

	// Asked for in this frame, not started.
	p.setAgentDriven(&app.OnBehalfOf{Task: "t", Epoch: 1})
	p.requestRun, p.agentRunRequested, p.requestStatement = true, true, 2
	require.ErrorAs(t, cancel("u"), &refusal)
	assert.True(t, p.requestRun)
	require.NoError(t, cancel("t"))
	assert.False(t, p.requestRun)
	assert.Zero(t, p.requestStatement)

	// In flight: the lane records whose run it is.
	store := p.graph.mainLane
	stopped := 0
	inFlight := func(obo *app.OnBehalfOf) {
		store.mu.Lock()
		store.loading, store.runAgent = true, obo
		store.mu.Unlock()
		store.isLoading.Store(true)
		store.cancelMu.Lock()
		store.cancel = func() { stopped++ }
		store.cancelMu.Unlock()
	}
	inFlight(&app.OnBehalfOf{Task: "t", Epoch: 1})
	raw, err := h.Snapshot().Query(opGetState, nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[PlayState](raw)
	require.NoError(t, err)
	assert.Equal(t, "task:t", st.Result.RunBy)
	require.ErrorAs(t, cancel("u"), &refusal)
	assert.Contains(t, refusal.Error(), "only the person")
	assert.Zero(t, stopped)
	require.NoError(t, cancel("t"))
	assert.Equal(t, 1, stopped)

	inFlight(nil)
	require.ErrorAs(t, cancel("t"), &refusal, "the person's run")
	require.NoError(t, cancel(""))
	assert.Equal(t, 2, stopped)
}

// The top bar's Cancel goes through cancel_run; without a host it applies
// directly.
func TestTheCancelButtonGoesThroughCancelRun(t *testing.T) {
	l, _ := opsLauncher(t)
	p := l.inner
	store := p.graph.mainLane
	stopped := 0
	store.cancelMu.Lock()
	store.cancel = func() { stopped++ }
	store.cancelMu.Unlock()
	p.personCancelRun()
	assert.Equal(t, 1, stopped)
}

// run's Statement ships statement n with the prelude, and the checks judge
// that text: the first statement of a buffer whose second writes runs.
func TestRunStatementShipsOneStatementAndIsCheckedOnIt(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	p.client = NewClient(ClientConfig{URL: "http://localhost:8123/"}, nil)
	p.SetSecurityCeiling(analysis.QuerySecurityMutating)
	p.swapSql("SET param_n = 3;\nSELECT 1;\nINSERT INTO t SELECT 1")

	stmt, err := p.statementBuffer(1)
	require.NoError(t, err)
	assert.Equal(t, "SET param_n = 3;\nSELECT 1", stmt)
	_, err = p.statementBuffer(3)
	assert.ErrorContains(t, err, "statements 1 to 2")

	obo := &app.OnBehalfOf{Task: "t", Epoch: 1, Destinations: []string{"clickhouse:localhost:8123"}}
	run := func(in RunArgs) error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: obo}, opRun, mustEncode(t, in))
		return err
	}
	var refusal *app.OperationRefusal
	require.ErrorAs(t, run(RunArgs{Statement: 2}), &refusal)
	assert.Contains(t, refusal.Error(), "mutating")
	assert.False(t, p.requestRun)
	require.ErrorAs(t, run(RunArgs{Statement: 3}), &refusal)
	require.ErrorAs(t, run(RunArgs{Statement: 1, Subquery: true}), &refusal)

	require.NoError(t, run(RunArgs{Statement: 1}))
	assert.True(t, p.requestRun)
	assert.Equal(t, 1, p.requestStatement)

	// A statement the buffer no longer holds when the run starts lands in
	// the status line.
	p.requestRun = false
	p.requestStatement = 5
	p.executeRun(false, false)
	assert.Contains(t, p.runBlockedReason, "statement 5")
	assert.Zero(t, p.requestStatement)

	// The person's Run goes back to the caret.
	p.requestStatement = 2
	p.applyRunShortcut(true, false)
	assert.Zero(t, p.requestStatement)
}

// A task stops only the projection it asked for; a compute not yet started
// is withdrawn.
func TestCancelProjectionStopsOnlyTheTasksOwnRun(t *testing.T) {
	l, h := opsLauncher(t)
	pj := l.inner.projector
	require.NotNil(t, pj)
	cancel := func(task string) error {
		call := app.OperationCall{Writer: "person"}
		if task != "" {
			call = app.OperationCall{Writer: "task:" + task, OnBehalfOf: &app.OnBehalfOf{Task: task, Epoch: 1}}
		}
		_, err := h.ApplyCommand(call, opCancelProjection, nil)
		return err
	}
	var refusal *app.OperationRefusal
	require.ErrorAs(t, cancel("t"), &refusal)
	assert.Contains(t, refusal.Error(), "no projection is running")

	pj.computeRequested, pj.runTask = true, "t"
	require.ErrorAs(t, cancel("u"), &refusal)
	assert.True(t, pj.computeRequested)
	require.NoError(t, cancel("t"))
	assert.False(t, pj.computeRequested)

	pj.mu.Lock()
	pj.status, pj.cancel = projectorStatusRunning, make(chan struct{})
	pj.mu.Unlock()
	raw, err := h.Snapshot().Query(opGetProjection, nil)
	require.NoError(t, err)
	ps, err := buscodec.Decode[ProjectionState](raw)
	require.NoError(t, err)
	assert.Equal(t, "task:t", ps.RunBy)
	require.ErrorAs(t, cancel("u"), &refusal)
	require.NoError(t, cancel("t"))
	assert.Equal(t, projectorStatusCancelling, pj.Snapshot().status)

	pj.mu.Lock()
	pj.status, pj.cancel, pj.runTask = projectorStatusRunning, make(chan struct{}), ""
	pj.mu.Unlock()
	require.ErrorAs(t, cancel("t"), &refusal, "the person's run")
	l.inner.personCancelProjection()
	assert.Equal(t, projectorStatusCancelling, pj.Snapshot().status, "the button applies directly without a host")
}

// publish_projection refuses until a finished run's layout is drawn, then
// starts a round that leaves the person's caret alone; get_projection
// reports how it went.
func TestPublishProjectionIsAnAgentsRoundWithoutTheScaffold(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	pj := p.projector
	publish := func() error {
		_, err := h.ApplyCommand(app.OperationCall{Writer: "task:t", OnBehalfOf: &app.OnBehalfOf{Task: "t", Epoch: 1}}, opPublishProjection, nil)
		return err
	}
	var refusal *app.OperationRefusal
	require.ErrorAs(t, publish(), &refusal)
	assert.Contains(t, refusal.Error(), "no bus")

	p.bus = &recordingBus{}
	require.ErrorAs(t, publish(), &refusal)
	assert.Contains(t, refusal.Error(), "no projection to publish")

	features, cl := twoClusterFeatures(60)
	ex := explainProjection(context.Background(), shapeFeatureDesc(features), cl)
	require.NoError(t, ex.err)
	rows := make([]int64, len(cl.Label))
	vals := make([]int64, len(rows))
	for i := range rows {
		rows[i], vals[i] = int64(i), int64(i)
	}
	cl.Probability = make([]float32, len(rows))
	res := &projectionResult{rows: rows, slotFeatures: features, clusters: cl, explanation: ex,
		params: projectionParams{K: 15, MinClusterSize: 10, FeatureSet: projectionFeatureShape}}
	pj.mu.Lock()
	pj.status, pj.result, pj.version = projectorStatusDone, res, 4
	pj.mu.Unlock()
	require.ErrorAs(t, publish(), &refusal)
	assert.Contains(t, refusal.Error(), "show_pane projection")

	pj.builtVersion = 4
	require.ErrorAs(t, publish(), &refusal)
	assert.Contains(t, refusal.Error(), "is gone", "no result fed to the pane")

	rec := int64Rec("n", vals...)
	p.graph.mainLane.finish("SELECT n", nil, time.Now(), rec, rec.Schema(), int64(len(vals)), Summary{}, nil, runstream.Terminal{})
	require.NoError(t, publish())
	assert.True(t, p.projPublishQuiet)
	require.Eventually(t, func() bool {
		publishing, _, _, _ := p.projPublish.status()
		return !publishing
	}, 5*time.Second, 10*time.Millisecond)
	// The fixture's run carries no graph for the pane's status line; the
	// reading's publish fields do not depend on it.
	pj.mu.Lock()
	pj.result = nil
	pj.mu.Unlock()
	raw, err := h.Snapshot().Query(opGetProjection, nil)
	require.NoError(t, err)
	ps, err := buscodec.Decode[ProjectionState](raw)
	require.NoError(t, err)
	assert.NotEmpty(t, ps.PublishError, "the recording bus refuses the publish, and the reading says so")
	assert.Contains(t, ps.PublishError, "no capability in this lane")
}

func TestDeleteSignalRemovesAHeldSignal(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	del := func(writer, name string) error {
		call := app.OperationCall{Writer: writer}
		if writer != "person" {
			call.OnBehalfOf = &app.OnBehalfOf{Task: "t", Epoch: 1}
		}
		_, err := h.ApplyCommand(call, opDeleteSignal, mustEncode(t, DeleteSignalArgs{Name: name}))
		return err
	}
	var refusal *app.OperationRefusal
	require.ErrorAs(t, del("task:t", "threshold"), &refusal)
	assert.Contains(t, refusal.Error(), "nothing holds")

	p.graph.setSignalRawFrom("threshold", "5", "task:t")
	before := h.ResourceValue(opsResSignals)
	require.NoError(t, del("task:t", "threshold"))
	_, held := p.graph.signals().Get("threshold")
	assert.False(t, held)
	assert.NotEqual(t, before, h.ResourceValue(opsResSignals))
	assert.NotNil(t, p.agentDriven, "a Live rerun after it carries the task's context")

	p.graph.setSignalRawFrom("tl_from", "2026-01-01", "timeline")
	require.ErrorAs(t, del("task:t", "tl_from"), &refusal)
	assert.Contains(t, refusal.Error(), "set_timeline_window")
	require.NoError(t, del("person", "tl_from"), "the person's × deletes anything")

	p.graph.setSignalRawFrom("x", "1", signalWriterEditor)
	p.personDeleteSignal("x")
	_, held = p.graph.signals().Get("x")
	assert.False(t, held, "the × applies directly without a host")
}

func TestSetRunOptionsTurnsTheConditionsRewrite(t *testing.T) {
	l, h := opsLauncher(t)
	p := l.inner
	on, off := true, false
	err := applyOp(t, h, opSetRunOptions, SetRunOptionsArgs{Conditions: &on})
	var refusal *app.OperationRefusal
	require.ErrorAs(t, err, &refusal)
	assert.Contains(t, refusal.Error(), "offers no conditions rewrite")
	require.NoError(t, applyOp(t, h, opSetRunOptions, SetRunOptionsArgs{}), "nothing to set")

	p.client = condTestClient(t)
	before := h.ResourceValue(opsResRunOptions)
	require.NoError(t, applyOp(t, h, opSetRunOptions, SetRunOptionsArgs{Conditions: &on}))
	assert.True(t, p.exposeConditions)
	assert.True(t, p.client.ExposeConditions())
	assert.NotEqual(t, before, h.ResourceValue(opsResRunOptions))
	raw, err := h.Snapshot().Query(opGetState, nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[PlayState](raw)
	require.NoError(t, err)
	assert.Equal(t, "on", st.Conditions)

	require.NoError(t, applyOp(t, h, opSetRunOptions, SetRunOptionsArgs{Conditions: &off}))
	assert.False(t, p.client.ExposeConditions())
}
