package watchbilldemo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opwire"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/watchbill/watchbillstore"
)

// seeded is a window holding the tour's list and one row of another kind,
// without a bus or a poller.
func seeded() (inst *App) {
	inst = newApp()
	inst.jobs = append(append([]watchbillstore.Job(nil), tourJobs...),
		watchbillstore.Job{ID: "other-kind", Kind: "tender.download", Subject: "row-1", State: watchbillstore.StateQueued})
	inst.refreshed = time.Now()
	return
}

func query[In any, Out any](t *testing.T, inst *App, name string, in In) (out Out, err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	raw, err := inst.Operations().Snapshot().Query(name, args)
	if err != nil {
		return
	}
	out, decErr := buscodec.Decode[Out](raw)
	require.NoError(t, decErr)
	return
}

func command[In any](t *testing.T, inst *App, call app.OperationCall, name string, in In) (err error) {
	t.Helper()
	args, encErr := buscodec.Encode(in)
	require.NoError(t, encErr)
	_, err = inst.Operations().ApplyCommand(call, name, args)
	return
}

func refusal(err error) (r *app.OperationRefusal) {
	errors.As(err, &r)
	return
}

func TestOperationsCatalogRegisters(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	m, ok := app.LookupManifest(manifest.Id)
	require.True(t, ok)
	require.NotNil(t, m.Operations, app.DefaultRegistry.OperationsDiagnostic(manifest.Id))
	for _, name := range []string{opGetView, opListJobs, opSetControl, opEnqueue} {
		spec, found := m.Operations.Lookup(name)
		require.True(t, found, name)
		assert.True(t, spec.Agents, name)
	}
	enq, _ := m.Operations.Lookup(opEnqueue)
	assert.Equal(t, app.OperationEffectConsequential, enq.Effect, "a job row is written outside the window")
	assert.True(t, enq.Async, "the worker answers off the frame")
	set, _ := m.Operations.Lookup(opSetControl)
	assert.Equal(t, app.OperationEffectDocument, set.Effect)
}

func TestGetViewReadsTheControlsAndTheList(t *testing.T) {
	inst := seeded()
	inst.simulateFail = true
	v, err := query[struct{}, View](t, inst, opGetView, struct{}{})
	require.NoError(t, err)
	assert.Equal(t, Controls{DurationSec: 3, End: endFail, Attempts: 1, Backoff: watchbillstore.BackoffNone}, v.Controls)
	assert.Equal(t, len(tourJobs)+1, v.Jobs)
	assert.Equal(t, len(tourJobs), v.OwnJobs, "the other kind is counted among the rows, not among the demo's")
	assert.False(t, v.ListedAt.IsZero())
	assert.Zero(t, v.Inflight)
}

func TestListJobsIsTheDemosKindBounded(t *testing.T) {
	inst := seeded()
	out, err := query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{})
	require.NoError(t, err)
	assert.Equal(t, len(tourJobs), out.Matched, "the other kind is not listed")
	require.Len(t, out.Jobs, len(tourJobs))
	assert.Equal(t, rowOf(tourJobs[0]), out.Jobs[0])
	assert.False(t, out.Truncated)

	out, err = query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{States: []string{watchbillstore.StateDiscarded}})
	require.NoError(t, err)
	assert.Equal(t, 2, out.Matched)
	for _, j := range out.Jobs {
		assert.Equal(t, watchbillstore.StateDiscarded, j.State)
	}

	out, err = query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{Limit: 2})
	require.NoError(t, err)
	assert.Equal(t, len(tourJobs), out.Matched, "the limit bounds the rows, not the count")
	assert.Len(t, out.Jobs, 2)
	assert.True(t, out.Truncated)

	_, err = query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{States: []string{"done"}})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, "no state done")
	_, err = query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{Limit: -1})
	assert.NotNil(t, refusal(err))

	for i := range opsMaxJobs + 20 {
		inst.jobs = append(inst.jobs, watchbillstore.Job{ID: string(rune('a' + i%26)), Kind: KindSleep, State: watchbillstore.StateSucceeded})
	}
	out, err = query[ListJobsArgs, JobList](t, inst, opListJobs, ListJobsArgs{Limit: opsMaxJobs + 50})
	require.NoError(t, err)
	assert.Len(t, out.Jobs, opsMaxJobs)
	assert.True(t, out.Truncated)
}

func TestSetControlSetsNeverTogglesAndRefusesOutOfRange(t *testing.T) {
	inst := newApp()
	for range 2 {
		require.NoError(t, command(t, inst, app.OperationCall{}, opSetControl, SetControlArgs{End: endFail, Attempts: 3}))
		assert.True(t, inst.simulateFail)
		assert.EqualValues(t, 3, inst.maxAttempts)
	}
	assert.EqualValues(t, 3, inst.durationSec, "a field left empty keeps its control")
	require.NoError(t, command(t, inst, app.OperationCall{}, opSetControl, SetControlArgs{DurationSec: 0.5, Backoff: watchbillstore.BackoffExponential, End: endSucceed}))
	assert.Equal(t, controls{DurationSec: 0.5, Fail: false, Attempts: 3, Backoff: watchbillstore.BackoffExponential}, inst.controlsValue())
	assert.Equal(t, inst.controlsValue(), inst.Operations().ResourceValue(resControls))

	before := inst.controlsValue()
	for _, bad := range []SetControlArgs{{DurationSec: 0.1}, {DurationSec: 61}, {Attempts: 6}, {Attempts: -1}, {Backoff: "cubic"}, {End: "maybe"}} {
		err := command(t, inst, app.OperationCall{}, opSetControl, bad)
		require.NotNil(t, refusal(err), "%+v", bad)
		assert.Equal(t, before, inst.controlsValue(), "a refused command changes nothing: %+v", bad)
	}

	h := inst.Operations()
	assert.True(t, h.Restore(resControls, controls{DurationSec: 2, Fail: true, Attempts: 2, Backoff: watchbillstore.BackoffLinear}))
	assert.Equal(t, 1, inst.backoffIdx)
	assert.False(t, h.Restore(resControls, "3s"))
}

// Without a bus the enqueue is refused with where to open the window, and
// nothing is left pending.
func TestEnqueueWithoutBusIsRefused(t *testing.T) {
	inst := newApp()
	err := command(t, inst, app.OperationCall{Key: "c1"}, opEnqueue, appops.None{})
	require.NotNil(t, refusal(err))
	assert.Contains(t, refusal(err).Reason, "no bus")
	assert.Empty(t, inst.enqueues)
}

// The W3 copy: an on-behalf-of context on the call reaches the watchbill
// request, and through an attesting worker the row the store accepts
// carries the task and the call (ADR-0344 §SD2); the call settles with
// the job once the worker answered (ADR-0358).
func TestOnBehalfOfReachesTheRequest(t *testing.T) {
	obo := &app.OnBehalfOf{Task: "task-7", Epoch: 2, Call: "call-3"}
	inst := newApp()
	assert.Same(t, obo, inst.request(obo).OnBehalfOf)
	assert.Nil(t, inst.request(nil).OnBehalfOf, "the person's own path carries none")

	f := newFixture(t)
	f.a.durationSec = 0.2
	f.w.SetDelegation(&attester{task: "task-7", epoch: 2, call: "call-3", sender: app.AppIdT(manifest.Id)})
	call := app.OperationCall{Writer: "task:task-7", Key: "call-3", OnBehalfOf: obo}
	require.NoError(t, command(t, f.a, call, opEnqueue, appops.None{}))

	var out EnqueueOutcome
	require.Eventually(t, func() bool {
		settled, raw, err := f.a.Operations().(app.OperationsAsyncI).SettleAsync(call, opEnqueue, nil)
		require.NoError(t, err)
		if settled {
			var decErr error
			out, decErr = buscodec.Decode[EnqueueOutcome](raw)
			require.NoError(t, decErr)
		}
		return settled
	}, 3*time.Second, 10*time.Millisecond, "the enqueue settles once the worker answered")
	assert.True(t, out.AgentCaused)
	assert.Equal(t, "200ms", out.Subject)
	row, found, err := f.store.Get(context.Background(), out.Id)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "task-7", row.Task)
	assert.EqualValues(t, 2, row.TaskEpoch)
	assert.Equal(t, "call-3", row.Call)
	assert.Empty(t, f.a.enqueues, "a settled call is forgotten")

	// A second ask under a key nothing waits on is refused, not pending.
	settled, _, err := f.a.Operations().(app.OperationsAsyncI).SettleAsync(app.OperationCall{Key: "never"}, opEnqueue, nil)
	assert.True(t, settled)
	assert.NotNil(t, refusal(err))

	// An unattested context is the worker's refusal, and the call settles
	// with it rather than hanging.
	bad := app.OperationCall{Key: "call-9", OnBehalfOf: &app.OnBehalfOf{Task: "task-7", Epoch: 2, Call: "other"}}
	require.NoError(t, command(t, f.a, bad, opEnqueue, appops.None{}))
	require.Eventually(t, func() bool {
		settled, _, err := f.a.Operations().(app.OperationsAsyncI).SettleAsync(bad, opEnqueue, nil)
		return settled && err != nil && !errors.Is(err, context.Canceled)
	}, 3*time.Second, 10*time.Millisecond)
}

// attester is the dispatcher's seam the worker attests a delegation with:
// it knows one call of one task for one sender.
type attester struct {
	task   string
	epoch  uint64
	call   string
	sender app.AppIdT
}

func (a *attester) CallContext(task string, epoch uint64, call string, sender app.AppIdT, _ uint64) (cc app.CallContext, ok bool, reason string) {
	if task != a.task || epoch != a.epoch || call != a.call || sender != a.sender {
		return cc, false, "no such call for this sender"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender}, true, ""
}

// The Enqueue button goes through the catalog where a host serves it, so
// the command log records the person's click under the same operation an
// agent calls (A3).
func TestTheEnqueueButtonIsTheCommand(t *testing.T) {
	f := newFixture(t)
	f.a.durationSec = 0.2
	eng := opengine.New(ops.Catalog(), f.a.Operations())
	ctx := app.NewStaticFrameContext(nil, nil)
	ctx.SetOperationsGesture(eng.Gesture)

	eng.BeginFrame()
	eng.ApplyQueued()
	eng.TakeSnapshot()
	f.a.frameCtx = ctx
	f.a.enqueueGesture()
	f.a.frameCtx = nil
	eng.EndFrame()

	log := eng.Log()
	require.NotEmpty(t, log)
	assert.Equal(t, opEnqueue, log[len(log)-1].Op)
	assert.Equal(t, opwire.WriterPerson, log[len(log)-1].Writer)
	assert.Equal(t, opwire.PhaseApplied, log[len(log)-1].Phase, log[len(log)-1].Reason)
	job := f.eventuallyState(t, watchbillstore.StateSucceeded)
	assert.False(t, job.AgentCaused(), "the person's click carries no delegation")

	// With no host serving the catalog the same button enqueues directly.
	g := newFixture(t)
	g.a.durationSec = 0.2
	g.a.enqueueGesture()
	g.eventuallyState(t, watchbillstore.StateSucceeded)
}
