package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opengine"
)

// expire moves a task's deadline into the past.
func (inst *rig) expire(g Grant) {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()
	inst.svc.tasks[g.Handle].deadline = time.Now().Add(-time.Second)
}

// Past its deadline a task is not gone: its calls wait for the person to
// give it more time, and the approval lets the held call through.
func TestALateTasksCallWaitsForMoreTime(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(ctx, GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	r.expire(g)

	held := r.call(g, "q", "get_text", "{}")
	require.Equal(t, "input_required", held.Phase)
	assert.True(t, held.Held)
	assert.Contains(t, held.Reason, "deadline passed")
	r.svc.mu.Lock()
	open := r.svc.pending()
	require.Len(t, open, 1)
	assert.Contains(t, needText(open[0].held), "Approving gives it another 30m0s")
	r.svc.mu.Unlock()
	polled, err := r.cli.Status(ctx, g.Handle, "q", 0)
	require.NoError(t, err)
	assert.Equal(t, "input_required", polled.Phase, "status reports the hold, not a denial of the late task")
	assert.True(t, polled.Held)

	r.person(true, nil)
	require.Eventually(t, func() bool {
		out, err := r.cli.Status(ctx, g.Handle, "q", 0)
		return err == nil && out.Phase == "completed"
	}, 2*time.Second, 10*time.Millisecond)
	r.svc.mu.Lock()
	assert.True(t, r.svc.tasks[g.Handle].deadline.After(time.Now()), "the approval gave the task more time")
	r.svc.mu.Unlock()
}

// Under test grants nobody answers a hold, so the call says how to extend
// the task; request_access on the late task does, and calls go through.
func TestATestTaskIsExtendedByAskingAgain(t *testing.T) {
	r := newRigWith(t, func(cfg *Config) { cfg.TestGrants, cfg.Deadline = true, time.Hour })
	g := r.grant(ModeAct)
	r.svc.mu.Lock()
	assert.True(t, r.svc.tasks[g.Handle].deadline.After(time.Now().Add(59*time.Minute)), "the configured deadline")
	r.svc.mu.Unlock()
	r.expire(g)

	late := r.call(g, "q", "get_text", "{}")
	require.Equal(t, "input_required", late.Phase)
	assert.False(t, late.Held)
	assert.Contains(t, late.Reason, "request_access extends it")
	_, err := r.cli.List(context.Background(), g.Handle)
	require.Error(t, err, "other services still deny a late task")

	_, err = r.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Plan: "more time"})
	require.NoError(t, err)
	again := r.call(g, "q2", "get_text", "{}")
	assert.Equal(t, "completed", again.Phase)
}

func TestTaskGoneTellsAnEndedTaskFromALateOne(t *testing.T) {
	assert.True(t, TaskGone("agent: refused: the task ended: stopped"))
	assert.True(t, TaskGone(reasonHandleInvalid))
	assert.False(t, TaskGone(reasonDeadline+"; request_access extends it"))
}

// A late call is checked as any other first: a refusal stands, and a call
// outside the grant asks for that widening, never for more time alone, so
// approving "more time" cannot share a window the dialog did not show.
func TestALateCallIsCheckedBeforeItWaitsForTime(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	d := &doc{text: "other"}
	r.host.mu.Lock()
	r.host.engines[9] = opengine.New(docOps.Catalog(), docOps.Bind(d))
	r.host.docs[9] = d
	r.host.mu.Unlock()
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(ctx, GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	r.expire(g)

	denied := r.call(g, "w", "wipe", "{}")
	assert.Equal(t, "denied", denied.Phase, "an operation not exposed to agents is denied, late or not")

	other, err := r.cli.Call(ctx, CallRequest{Handle: g.Handle, Instance: 9, Operation: "get_text", Args: "{}", Key: "o"})
	require.NoError(t, err)
	require.Equal(t, "input_required", other.Phase)
	assert.True(t, other.Held)
	r.svc.mu.Lock()
	open := r.svc.pending()
	require.Len(t, open, 1)
	assert.Equal(t, needInstance, open[0].held.need, "the person is asked to share window 9")
	r.svc.mu.Unlock()
}

// A coordinator that stops waiting withdraws its held call: the dialog
// closes, and nothing is left for the person to approve later.
func TestCancelWithdrawsAHeldCall(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(ctx, GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	r.expire(g)

	held := r.call(g, "q", "get_text", "{}")
	require.True(t, held.Held)
	out, err := r.cli.Cancel(ctx, g.Handle, "q")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", out.Phase)
	assert.False(t, out.Held)
	r.svc.mu.Lock()
	assert.Empty(t, r.svc.pending(), "the widening's dialog closed")
	r.svc.mu.Unlock()
}
