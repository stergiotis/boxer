package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
	"github.com/stergiotis/boxer/public/keelson/runtime/codec/instanceclosed"
)

// person stands in for the dialog: it decides the oldest pending request.
func (inst *rig) person(approve bool, configure func(r *request)) {
	inst.t.Helper()
	svc := inst.svc
	deadline := time.Now().Add(2 * time.Second)
	for {
		svc.mu.Lock()
		open := svc.pending()
		if len(open) > 0 {
			r := open[0]
			if configure != nil {
				configure(r)
			}
			var route *held
			if approve {
				route = svc.approve(r)
			} else {
				svc.reject(r)
			}
			svc.mu.Unlock()
			if route != nil {
				svc.routeHeld(route)
			}
			return
		}
		svc.mu.Unlock()
		require.True(inst.t, time.Now().Before(deadline), "no request reached the person")
		time.Sleep(10 * time.Millisecond)
	}
}

func coordinatorRig(t *testing.T) *rig {
	return newRigWith(t, func(cfg *Config) { cfg.Coordinators = []string{"test.coordinator"} })
}

func TestOnlyARegisteredCoordinatorMayAsk(t *testing.T) {
	r := newRig(t, false)
	_, _, err := r.cli.RequestKey(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "coordinator")
}

func TestThePersonApprovesAGrant(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	got := make(chan Grant, 1)
	go func() {
		g, err := r.cli.Request(ctx, GrantRequest{Plan: "tidy the doc", Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		assert.NoError(t, err)
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	require.NotEmpty(t, g.Handle)
	assert.Equal(t, "completed", r.call(g, "q", "get_text", "{}").Phase)
}

func TestThePersonSharesInTheModeTheyPick(t *testing.T) {
	r := coordinatorRig(t)
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		got <- g
	}()
	r.person(true, func(req *request) { req.mode[7] = ModeObserve })
	g := <-got
	r.call(g, "q", "get_text", "{}")
	out := r.call(g, "w", "set_text", `{"text":"x"}`)
	assert.Equal(t, "input_required", out.Phase)
	assert.True(t, out.Held, "the call waits on the person")
}

func TestThePersonDeclines(t *testing.T) {
	r := coordinatorRig(t)
	errc := make(chan error, 1)
	go func() {
		_, err := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
		errc <- err
	}()
	r.person(false, nil)
	var refused *RefusedError
	require.True(t, errors.As(<-errc, &refused))
	assert.Contains(t, refused.Reason, "rejected")
}

func TestACallOutsideTheGrantWaitsForAWidening(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(ctx, GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeObserve}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	r.call(g, "q", "get_text", "{}")
	held := r.call(g, "w", "set_text", `{"text":"agent"}`)
	require.Equal(t, "input_required", held.Phase)
	require.True(t, held.Held)

	r.person(true, nil) // the widening: act in window 7
	var out Outcome
	require.Eventually(t, func() bool {
		var err error
		out, err = r.cli.Status(ctx, g.Handle, "w", 0)
		return err == nil && out.Phase == "accepted"
	}, 2*time.Second, 10*time.Millisecond)
	r.host.frame(7)
	assert.Equal(t, "agent", r.host.docs[7].text)

	r.call(g, "q2", "get_text", "{}")
	// A declined widening ends the call as rejected.
	held = r.call(g, "e", "export", "{}")
	assert.Equal(t, "input_required", held.Phase, "a consequential command is not a widening")
	assert.False(t, held.Held)
}

func TestADeclinedWideningRejectsTheCall(t *testing.T) {
	r := coordinatorRig(t)
	ctx := context.Background()
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(ctx, GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeObserve}}})
		got <- g
	}()
	r.person(true, nil)
	g := <-got
	r.call(g, "w", "set_text", `{"text":"agent"}`)
	r.person(false, nil)
	out, err := r.cli.Status(ctx, g.Handle, "w", 0)
	require.NoError(t, err)
	assert.Equal(t, "rejected", out.Phase)
	assert.True(t, out.Final())
}

func TestTheTaskEndsWithItsCoordinator(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	payload, err := buscodec.Encode(instanceclosed.InstanceClosed{AppId: "test.coordinator", InstanceKey: 0})
	require.NoError(t, err)
	closer := r.bus.NewClient("test.closer", []app.SubjectFilter{{Pattern: app.SubjectInstanceClosed, Direction: app.CapDirectionPub}})
	require.NoError(t, closer.Publish(app.SubjectInstanceClosed, payload))
	assert.Equal(t, "denied", r.call(g, "q", "get_text", "{}").Phase)
}
