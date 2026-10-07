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
	assert.Equal(t, "proposed", held.Phase, "a consequential command is confirmed, not widened")
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

// approvedRig is a rig with a person-approved grant over window 7 in mode.
func approvedRig(t *testing.T, mode ModeE) (*rig, Grant) {
	r := coordinatorRig(t)
	got := make(chan Grant, 1)
	go func() {
		g, _ := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: mode}}})
		got <- g
	}()
	r.person(true, nil)
	return r, <-got
}

func (inst *rig) decideProposal(accept bool, confirm bool) {
	inst.t.Helper()
	inst.svc.mu.Lock()
	ps := inst.svc.proposals(0, confirm)
	inst.svc.mu.Unlock()
	require.NotEmpty(inst.t, ps)
	if accept {
		inst.svc.accept(ps[0])
	} else {
		inst.svc.rejectProposal(ps[0])
	}
}

func TestSuggestModeProposesAndThePersonAccepts(t *testing.T) {
	r, g := approvedRig(t, ModeSuggest)
	ctx := context.Background()
	r.call(g, "q", "get_text", "{}")
	out := r.call(g, "w", "set_text", `{"text":"suggested"}`)
	require.Equal(t, "proposed", out.Phase)
	assert.False(t, out.Final())
	r.host.frame(7)
	assert.Equal(t, "start", r.host.docs[7].text, "a proposal changes nothing")
	r.decideProposal(true, false)
	require.Eventually(t, func() bool {
		st, _ := r.cli.Status(ctx, g.Handle, "w", 0)
		return st.Phase == "accepted"
	}, 2*time.Second, 10*time.Millisecond)
	r.host.frame(7)
	assert.Equal(t, "suggested", r.host.docs[7].text)
}

func TestAProposalGoesStaleWhenItsResourceMoves(t *testing.T) {
	r, g := approvedRig(t, ModeSuggest)
	r.call(g, "q", "get_text", "{}")
	r.call(g, "w", "set_text", `{"text":"suggested"}`)
	r.host.person(7, func(d *doc) { d.text = "the person moved on" })
	r.host.frame(7)
	r.decideProposal(true, false)
	st, err := r.cli.Status(context.Background(), g.Handle, "w", 0)
	require.NoError(t, err)
	assert.Equal(t, "stale", st.Phase)
	assert.Equal(t, "the person moved on", r.host.docs[7].text)
}

func TestARejectedProposalIsFinal(t *testing.T) {
	r, g := approvedRig(t, ModeSuggest)
	r.call(g, "q", "get_text", "{}")
	r.call(g, "w", "set_text", `{"text":"suggested"}`)
	r.decideProposal(false, false)
	st, _ := r.cli.Status(context.Background(), g.Handle, "w", 0)
	assert.Equal(t, "rejected", st.Phase)
	assert.True(t, st.Final())
}

func TestAConsequentialCommandIsConfirmedEachTime(t *testing.T) {
	r, g := approvedRig(t, ModeAct)
	out := r.call(g, "x1", "export", "{}")
	require.Equal(t, "proposed", out.Phase, "act mode still asks for a consequential command")
	r.decideProposal(true, true)
	require.Eventually(t, func() bool {
		st, _ := r.cli.Status(context.Background(), g.Handle, "x1", 0)
		return st.Phase == "accepted"
	}, 2*time.Second, 10*time.Millisecond)
	out = r.call(g, "x2", "export", "{}")
	assert.Equal(t, "proposed", out.Phase, "every time")
}

// widen adds destinations to the rig's task, as the person approves them.
func (inst *rig) widen(g Grant, destinations ...string) {
	inst.t.Helper()
	got := make(chan Grant, 1)
	go func() {
		w, err := inst.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Plan: "publish", Destinations: destinations})
		assert.NoError(inst.t, err)
		got <- w
	}()
	inst.person(true, nil)
	<-got
}

// ADR-0288 (proposed) §SD4: a publish:<prefix> destination the person
// approved is standing consent for a consequential call whose argument
// starts with the prefix; the call applies as a document command would,
// and its record names the destination that admitted it.
func TestAPublishGrantIsStandingConsent(t *testing.T) {
	r, g := approvedRig(t, ModeAct)
	out := r.call(g, "p0", "publish_text", `{"name":"report_q3"}`)
	require.Equal(t, "proposed", out.Phase, "without the grant, the person confirms")
	assert.Contains(t, out.Reason, "unless the grant lists publish:<name prefix>", "the refusal says what would cover it")
	r.decideProposal(false, true)

	r.widen(g, "publish:report_", "publish:")
	out = r.call(g, "p1", "publish_text", `{"name":"report_q3"}`)
	assert.Contains(t, []string{"accepted", "applied"}, out.Phase, out.Reason)
	out = r.call(g, "p2", "publish_text", `{"name":"other"}`)
	assert.Equal(t, "proposed", out.Phase, "a name outside the prefix is confirmed; an empty prefix covers nothing")
	r.decideProposal(false, true)

	var admitted []string
	for _, a := range r.svc.Actions() {
		if a.Key == "p1" {
			admitted = append(admitted, a.Consent)
		}
		if a.Key == "p2" {
			assert.Empty(t, a.Consent)
		}
	}
	require.NotEmpty(t, admitted)
	assert.Equal(t, "publish:report_", admitted[0])

	// Suggest mode still proposes, but the person accepts rather than
	// confirms: consent waives the confirmation, not the mode.
	r.svc.mu.Lock()
	var t0 *task
	for _, cand := range r.svc.tasks {
		t0 = cand
	}
	r.svc.mu.Unlock()
	r.svc.setMode(t0, 7, ModeSuggest)
	out = r.call(g, "p3", "publish_text", `{"name":"report_q4"}`)
	require.Equal(t, "proposed", out.Phase)
	r.svc.mu.Lock()
	confirm := t0.keys["p3"].proposal.confirm
	r.svc.mu.Unlock()
	assert.False(t, confirm)
}

// A test grant has nobody to consent: a publish: destination does not let
// a consequential command through it.
func TestATestGrantNeverAppliesAConsequentialCommand(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	_, err := r.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Plan: "publish", Destinations: []string{"publish:report_"}})
	require.NoError(t, err)
	out := r.call(g, "p", "publish_text", `{"name":"report_q3"}`)
	assert.Equal(t, "input_required", out.Phase, out.Reason)
}

func TestLoweringToSuggestTurnsQueuedCommandsIntoProposals(t *testing.T) {
	r, g := approvedRig(t, ModeAct)
	r.call(g, "q", "get_text", "{}")
	require.Equal(t, "accepted", r.call(g, "w", "set_text", `{"text":"queued"}`).Phase)
	r.svc.mu.Lock()
	var t0 *task
	for _, cand := range r.svc.tasks {
		t0 = cand
	}
	r.svc.mu.Unlock()
	r.svc.setMode(t0, 7, ModeSuggest)
	r.host.frame(7)
	assert.Equal(t, "start", r.host.docs[7].text, "the queued command did not apply")
	st, _ := r.cli.Status(context.Background(), g.Handle, "w", 0)
	assert.Equal(t, "proposed", st.Phase)
}

// destinationsOf reads a task's destinations under the service's lock.
func (inst *rig) destinationsOf(handle string) (ds []string) {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()
	if t := inst.svc.tasks[handle]; t != nil {
		ds = append(ds, t.destinations...)
	}
	return
}

// A widening's destinations join the task when the person approves it.
func TestAnApprovedWideningAddsItsDestinations(t *testing.T) {
	r, g := approvedRig(t, ModeAct)
	got := make(chan Grant, 1)
	go func() {
		w, err := r.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Plan: "run it", Destinations: []string{"keelson:apps"}})
		assert.NoError(t, err)
		got <- w
	}()
	r.person(true, nil)
	assert.Equal(t, g.Handle, (<-got).Handle)
	assert.Contains(t, r.destinationsOf(g.Handle), "keelson:apps")
}

// ADR-0269 §SD6: under test grants a widening is approved at once, since
// nobody answers the dialog in a scene.
func TestATestGrantApprovesAWidening(t *testing.T) {
	r := newRig(t, true)
	g := r.grant(ModeAct)
	w, err := r.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Plan: "run it", Destinations: []string{"clickhouse:ch.example:8123"}})
	require.NoError(t, err)
	assert.Equal(t, g.Handle, w.Handle)
	assert.Equal(t, g.Task, w.Task)
	assert.Contains(t, r.destinationsOf(g.Handle), "clickhouse:ch.example:8123")
	_, err = r.cli.Request(context.Background(), GrantRequest{Handle: g.Handle, Entries: []GrantEntry{{Instance: 7, Mode: ModeSuggest}}})
	assert.Error(t, err, "suggest needs the person's proposal surface")
}
