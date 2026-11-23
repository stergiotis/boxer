package play

import (
	"errors"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/queryengine"
)

// A run on this process's plane reads a dataset by its handle for as long
// as the handle is registered, which outlasts the dataset's retraction by
// a grace. A statement that joined the task's own clear publication with a
// dataset a person published, and whose label is asked after the person's
// handle has left the plane, is labelled all the same: the label is judged
// by every handle the statement names, not by the ones still registered
// when the run ends.
func TestAHandleThatLeftThePlaneDuringTheRunStillLabelsIt(t *testing.T) {
	r := newOwnClearRig(t)
	handle := r.appPublishes("kernel_top")
	theirs := r.personPublishes("people")
	obo := r.attestedCall()
	ctx := t.Context()
	confined := queryengine.SensitivityConfined
	stmt := "SELECT 1 FROM keelson('" + handle + "') AS a JOIN keelson('" + theirs + "') AS b USING x"
	bare := "SELECT 1 FROM keelson('" + handle + "') AS a JOIN " + theirs + " AS b USING x"
	require.False(t, r.cl.ownClearRun(ctx, confined, stmt, obo, nil), "while both are registered the statement is labelled")

	// The run was placed and sent as confined, and is reading; the person
	// retracts the bundle and the grace passes before the run ends.
	require.NoError(t, adhocdata.RetractBundleRequest(ctx, r.person, "people", nil))
	r.adhoc.FlushRetracts()

	assert.False(t, r.cl.ownClearRun(ctx, confined, stmt, obo, nil), "the person's rows are in the result, whether or not their handle is still registered")
	assert.False(t, r.cl.ownClearRun(ctx, confined, bare, obo, nil), "named as a bare table too")
	dec := dispatchDecision{sensitivity: confined}
	assert.False(t, failedOwnClear(r.cl.markOwnClearFailure(ctx, errors.New("boom"), dec, stmt, obo, nil)), "and a failure of that run carries the label")
	assert.True(t, r.cl.ownClearRun(ctx, confined, "SELECT 1 FROM keelson('"+handle+"')", obo, nil), "the task's own publication alone still reads back unlabelled")
}

// A result that is unlabelled because it holds the own clear publications
// of the task that ran it is unlabelled for that task alone. The person,
// and any other task, that publishes it publishes rows derived from sealed
// data, and a check another caller rests on it is confined. The result
// here is read from a kept data set, so its statement names no dataset the
// dataset service could judge the publish by: the publisher's word is all
// the revision has.
func TestAResultIsUnlabelledOnlyForTheTaskWhoseWorkItHolds(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')", "keep_as": "mine"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, out.Confined)
	require.Empty(t, r.cl.inputHandlesOf(r.p.graph.MainSQL()), "the sample of the kept data set names no ad-hoc dataset")
	_, _, _, _, _, _, _, _, id := r.p.graph.MainSnapshot()

	confined, held := r.p.graph.MainLabelFor(id, r.g.Task)
	assert.True(t, held)
	assert.False(t, confined, "the task that ran it reads its own work")
	confined, _ = r.p.graph.MainLabelFor(id, "task-of-another-conversation")
	assert.True(t, confined, "another task reads a dataset it did not publish")
	confined, _ = r.p.graph.MainLabelFor(id, "")
	assert.True(t, confined, "the person publishes sealed data")
	_, clearFor := r.p.graph.MainLabel()
	assert.Equal(t, r.g.Task, clearFor)

	// The person publishes the result from the window: the revision says
	// its rows derive from sealed data.
	done := r.host.eng.Submit(t.Context(), opPublishResult, opwire.CallRequest{CallId: "person-1", Writer: opwire.WriterPerson, Key: "person-1",
		Args: mustEncode(t, PublishResultArgs{Bundle: "bythem"})})
	require.NotEqual(t, opwire.PhaseRefused, done.Phase, done.Reason)
	var pub adhocdata.BundleResult
	require.Eventually(t, func() bool {
		r.host.frame()
		var err error
		pub, err = r.adhoc.ResolveBundle(t.Context(), "bythem", adhocdata.Identity{}, nil)
		return err == nil
	}, 10*time.Second, 2*time.Millisecond)
	assert.False(t, pub.FromNothingSealed, "to the person the result is sealed data, whoever published what it read")
	require.Eventually(t, func() bool { r.host.frame(); return !r.host.l.lastPublish().Pending }, 10*time.Second, 2*time.Millisecond)

	// The task's own publish of the same result stays its own clear work.
	phase, reason := r.publishResult("final")
	require.Equal(t, "completed", phase, reason)
	pub, err := r.adhoc.ResolveBundle(t.Context(), "final", adhocdata.Identity{}, nil)
	require.NoError(t, err)
	assert.True(t, pub.FromNothingSealed)

	// A check rests on the result: for another caller the result is
	// confined, for the task whose work it holds it is not.
	src := snapshotCheck(r.p).checkSource
	assert.False(t, src.confined)
	assert.Equal(t, r.g.Task, src.clearFor)
}

// A second task that observes the window reads the result the first task's
// run left in it. The result holds the first task's own clear publication,
// which is sealed data to every other reader, so the observer's read is
// labelled.
func TestAnObservingTaskReadsAnotherTasksOwnClearResultLabelled(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, out.Confined, "the task's own read is unlabelled")

	observer, err := r.cli.Request(t.Context(), agent.GrantRequest{Plan: "watch the window",
		Entries: []agent.GrantEntry{{Instance: r.host.key, Mode: agent.ModeObserve}}})
	require.NoError(t, err)
	require.NotEqual(t, r.g.Task, observer.Task)
	got, err := r.cli.Call(t.Context(), agent.CallRequest{Handle: observer.Handle, Instance: r.host.key, Operation: opSampleRows, Args: `{}`,
		Key: "observer-1", Turn: "turn-1"})
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		if got.Final() {
			return true
		}
		r.host.frame()
		got, err = r.cli.Status(t.Context(), observer.Handle, "observer-1", 0)
		require.NoError(t, err)
		return got.Final()
	}, 10*time.Second, 2*time.Millisecond)
	require.Equal(t, "completed", got.Phase, got.Reason)
	assert.True(t, got.Confined, "the rows are another task's publication: sealed data to the observer")

	// The task whose work it is reads the same rows unlabelled, in the
	// same frame of the window.
	own := r.call(opSampleRows, `{}`)
	require.Equal(t, "completed", own.Phase, own.Reason)
	assert.False(t, own.Confined)
}

// follow follows a call of handle under key to its end, drawing frames.
func (inst *ownClearRig) follow(handle string, key string, got agent.Outcome) agent.Outcome {
	inst.t.Helper()
	require.Eventually(inst.t, func() bool {
		if got.Final() {
			return true
		}
		inst.host.frame()
		var err error
		got, err = inst.cli.Status(inst.t.Context(), handle, key, 0)
		require.NoError(inst.t, err)
		return got.Final()
	}, 10*time.Second, 2*time.Millisecond, "%s did not end: %s %s", key, got.Phase, got.Reason)
	return got
}

// A reader that is no call of the task the window is clear for takes the
// window as holding sealed data: a capture of it, by the observer and by
// the task itself, and its row in a listing, whose title is withheld.
func TestACaptureAndTheListingTakeTheWindowAsSealed(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, out.Confined)
	assert.True(t, r.host.eng.Confined(), "what the window host reports for captures, titles and the listing")

	observer, err := r.cli.Request(t.Context(), agent.GrantRequest{Plan: "watch the window",
		Entries: []agent.GrantEntry{{Instance: r.host.key, Mode: agent.ModeObserve}}})
	require.NoError(t, err)
	for _, who := range []struct {
		name   string
		handle string
	}{{"the observer", observer.Handle}, {"the task whose work it is", r.g.Handle}} {
		key := "capture-" + who.handle[:6]
		shot, cErr := r.cli.Capture(t.Context(), who.handle, r.host.key, agent.Asked{Key: key, Turn: "turn-1"})
		require.NoError(t, cErr, who.name)
		require.NotEqual(t, "refused", shot.Phase, "%s: %s", who.name, shot.Reason)
		assert.True(t, shot.Confined, "%s: a capture draws the window for whoever looks at it, so it carries the label for every reader", who.name)

		windows, lErr := r.cli.List(t.Context(), who.handle)
		require.NoError(t, lErr, who.name)
		require.Len(t, windows, 1)
		assert.True(t, windows[0].Confined, who.name)
		assert.Empty(t, windows[0].Title, "%s: a confined window's title is withheld", who.name)
	}

	// An ordinary result in the window: nothing is labelled for anyone.
	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT 1"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.False(t, r.host.eng.Confined())
}

// A lane keeps the result it last served. One made under the first task's
// mark, over that task's own clear publication, is unlabelled for that task
// alone: once the window acts for another task the memo is still served,
// without a run, and another task's read of the window is labelled.
func TestALaneMemoIsLabelledForTheTaskTheMarkMovedTo(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	stmt := "SELECT x FROM keelson('kernel_top__result')"
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": stmt}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	// The main result becomes ordinary: the lane's memo will be all the
	// window holds of sealed data.
	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT 1"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.False(t, r.p.windowConfined())

	// A bound lane runs under the window's mark, the first task's.
	require.NotNil(t, r.cl.agentMark.Load())
	require.Equal(t, r.g.Task, r.cl.agentMark.Load().Task)
	lane := newNodeLane(clientExecutor{client: r.cl}, memory.NewGoAllocator(), 0)
	t.Cleanup(lane.close)
	waitLaneReady(t, lane, stmt)
	r.p.tabBindings = map[string]NodeID{"chart": "memo"}
	r.p.boundLanes = map[NodeID]*nodeLane{"memo": lane}
	r.p.resolvedNodes = map[string]NodeID{"chart": "memo"}
	confined, clearFor := lane.servesLabel()
	require.False(t, confined)
	require.Equal(t, r.g.Task, clearFor, "the lane's result is the first task's own work")
	own := r.call(opGetState, `{}`)
	require.Equal(t, "completed", own.Phase, own.Reason)
	assert.False(t, own.Confined, "for the first task the window is unlabelled")
	require.Contains(t, r.p.boundLanes, NodeID("memo"), "the frames kept the lane")

	// The window now acts for another task: the mark moves, the memo
	// stays, and no run is made.
	runs := func() int {
		r.plane.mu.Lock()
		defer r.plane.mu.Unlock()
		return len(r.plane.runs)
	}
	before := runs()
	r.p.setAgentDriven(&app.OnBehalfOf{Task: "task-the-mark-moved-to", Epoch: 1, Call: "c1"})
	v := lane.demand(compiledNode{SQL: stmt})
	if v.rec != nil {
		v.rec.Release()
	}
	require.False(t, v.loading, "the memo is served, not run again")
	require.Equal(t, before, runs())
	confined, clearFor = r.p.windowLabel()
	assert.True(t, confined)
	assert.Equal(t, r.g.Task, clearFor, "the memo is still the first task's work and nobody else's")

	observer, err := r.cli.Request(t.Context(), agent.GrantRequest{Plan: "watch the window",
		Entries: []agent.GrantEntry{{Instance: r.host.key, Mode: agent.ModeObserve}}})
	require.NoError(t, err)
	r.host.frame()
	got, err := r.cli.Call(t.Context(), agent.CallRequest{Handle: observer.Handle, Instance: r.host.key, Operation: opGetState, Args: `{}`,
		Key: "after-the-mark-moved", Turn: "turn-1"})
	require.NoError(t, err)
	got = r.follow(observer.Handle, "after-the-mark-moved", got)
	require.Equal(t, "completed", got.Phase, got.Reason)
	assert.True(t, got.Confined, "another task reads a window that holds the first task's publication")
}

// The person at the window is answered as before the label had a reader in
// it: a read over a result of a task's own clear work is not labelled, one
// over a result labelled for everyone is.
func TestThePersonAtTheWindowKeepsTheLabelItHad(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishes("kernel_top")
	r.personPublishes("people")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	person := func(id string) opwire.Outcome {
		r.host.frame()
		return r.host.eng.Submit(t.Context(), opGetState, opwire.CallRequest{CallId: id, Writer: opwire.WriterPerson, Key: id, Args: mustEncode(t, struct{}{})})
	}
	got := person("p1")
	require.Equal(t, opwire.PhaseCompleted, got.Phase, got.Reason)
	assert.False(t, got.Confined, "as the person's reads of such a window were answered before")
	assert.True(t, r.host.eng.Confined())

	out = r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('people__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	require.True(t, out.Confined)
	got = person("p2")
	require.Equal(t, opwire.PhaseCompleted, got.Phase, got.Reason)
	assert.True(t, got.Confined, "a result labelled for everyone is labelled for the person too")
}

// An app that publishes on the task's behalf without saying what its rows
// derive from publishes sealed data, whatever tier its call declared: the
// task's read of it is labelled and the task does not expose it.
func TestACarelessAppsPublicationIsSealedToItsTask(t *testing.T) {
	r := newOwnClearRig(t)
	r.appPublishesCarelessly("kernel_top")
	out := r.round(opQuery, jsonArgs(t, map[string]any{"sql": "SELECT x FROM keelson('kernel_top__result')"}))
	require.Equal(t, "completed", out.Phase, out.Reason)
	assert.True(t, out.Confined, "nothing says the rows derive from nothing sealed")
	assert.True(t, r.p.graph.MainConfined())

	// Published on from play in the round the read arrived in, at
	// TLP:CLEAR: play asserts nothing of a confined result.
	r.tier = agentrun.TierClear
	phase, reason := r.publishResult("leak")
	require.Equal(t, "completed", phase, reason)
	exp := r.call(opExposeDataset, jsonArgs(t, map[string]any{"name": "leak__result"}))
	require.Equal(t, "refused", exp.Phase)
	assert.Contains(t, exp.Reason, "not known to derive from nothing sealed")
}
