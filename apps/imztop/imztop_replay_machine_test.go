package imztop

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/appops/opfsm"
	"github.com/stergiotis/boxer/public/keelson/runtime/buscodec"
)

// The phase is the session's state; its details are not phases.
func TestObserveReplayFollowsTheSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		st   ReplayStatus
		want ReplayStateE
	}{
		{"off", ReplayStatus{}, ReplayOff},
		{"opening", ReplayStatus{State: ReplayOpening}, ReplayOpening},
		{"on", ReplayStatus{State: ReplayOn, Host: "h"}, ReplayOn},
		{"on with nothing recorded", ReplayStatus{State: ReplayOn, Host: "h", Empty: true}, ReplayOn},
		{"failed", ReplayStatus{State: ReplayFailed, Err: errors.New("no such table")}, ReplayFailed},
	} {
		assert.Equal(t, tc.want, observeReplay(tc.st), tc.name)
	}
}

// Every phase is reachable from off along the declared rules, and a mirror
// of an observed status lands on it.
func TestEveryReplayPhaseIsReachable(t *testing.T) {
	m := newReplayMachine()
	for _, ph := range allReplayStates[1:] {
		assert.True(t, m.CanReach(ReplayOff, ph), ph.String())
	}
	assert.True(t, m.CanReach(ReplayFailed, ReplayOpening), "a failed open may be retried")

	inst := newApp()
	inst.mirrorReplay(ReplayStatus{State: ReplayOpening})
	inst.mirrorReplay(ReplayStatus{State: ReplayOn})
	assert.Equal(t, ReplayOn, inst.replayMachine.Current())
	inst.mirrorReplay(ReplayStatus{})
	assert.Equal(t, ReplayOff, inst.replayMachine.Current())
}

// The phase is mounted: replay_machine is the chip's graph, replay_state the
// current phase and what moves it.
func TestTheReplayMachineIsMounted(t *testing.T) {
	require.NoError(t, manifest.Operations.Validate())
	opsFixture(t)
	h := newApp().Operations()
	raw, err := h.Snapshot().Query("replay_machine", nil)
	require.NoError(t, err)
	mach, err := buscodec.Decode[opfsm.Machine](raw)
	require.NoError(t, err)
	assert.Len(t, mach.States, len(allReplayStates))
	raw, err = h.Snapshot().Query("replay_state", nil)
	require.NoError(t, err)
	st, err := buscodec.Decode[opfsm.State](raw)
	require.NoError(t, err)
	assert.Equal(t, "off", st.Current)
	assert.Contains(t, st.Next, opfsm.Edge{From: "off", To: "opening", Label: "Replay / enter_replay"})
}

// enter_replay is asynchronous (ADR-0358): it stays running while the
// session opens, fails with the session's reason, and settles as off when a
// leave landed while it opened.
func TestEnterReplaySettlesFromTheSession(t *testing.T) {
	opsFixture(t)
	inst := newApp()
	inst.claimController()
	spec, ok := manifest.Operations.Lookup(opEnterReplay)
	require.True(t, ok)
	assert.True(t, spec.Async)
	h, ok := inst.Operations().(app.OperationsAsyncI)
	require.True(t, ok, "the handler settles its async command")
	t.Cleanup(func() { _ = LeaveReplay() })

	require.True(t, beginOpening())
	settled, _, err := h.SettleAsync(app.OperationCall{}, opEnterReplay, nil)
	require.NoError(t, err)
	assert.False(t, settled, "an open in flight is still running")

	failOpening(errors.New("no such table"))
	settled, _, err = h.SettleAsync(app.OperationCall{}, opEnterReplay, nil)
	assert.True(t, settled)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "replay could not open")

	require.NoError(t, LeaveReplay())
	settled, raw, err := h.SettleAsync(app.OperationCall{}, opEnterReplay, nil)
	require.NoError(t, err)
	assert.True(t, settled)
	out, err := buscodec.Decode[ReplayOutcome](raw)
	require.NoError(t, err)
	assert.Equal(t, "off", out.State)
}
