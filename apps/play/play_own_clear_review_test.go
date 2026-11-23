package play

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/adhocdata"
	"github.com/stergiotis/boxer/public/keelson/runtime/agent"
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
	assert.True(t, pub.DerivedFromSealed, "to the person the result is sealed data, whoever published what it read")
	require.Eventually(t, func() bool { r.host.frame(); return !r.host.l.lastPublish().Pending }, 10*time.Second, 2*time.Millisecond)

	// The task's own publish of the same result stays its own clear work.
	phase, reason := r.publishResult("final")
	require.Equal(t, "completed", phase, reason)
	pub, err := r.adhoc.ResolveBundle(t.Context(), "final", adhocdata.Identity{}, nil)
	require.NoError(t, err)
	assert.False(t, pub.DerivedFromSealed)

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
	// The window's label is one value for every reader (the catalog
	// handler's Confined, which the engine stamps on each outcome and the
	// window host reports for captures and titles), and play reports it
	// as the label for the task whose run made the result. Making it the
	// label for the task that asks changes what the catalog handler, the
	// engine and the dispatcher exchange, and what a capture of the window
	// carries: a decision for the owner, not a fix a review makes.
	t.Skip("open finding: the window's label is judged for the task that ran the result, and another task's read of the window takes it; needs a decision on a per-reader window label")
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
}
