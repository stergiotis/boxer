package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Without the boxer_unattended tag, Config.Unattended changes nothing: the
// request waits for the person (ADR-0298).
func TestUnattendedNeedsTheBuildTag(t *testing.T) {
	if Unattended {
		t.Skip("built with boxer_unattended")
	}
	r := newRigWith(t, func(cfg *Config) {
		cfg.Coordinators = []string{"test.coordinator"}
		cfg.Unattended = true
	})
	assert.False(t, r.svc.Unattended())
	go func() {
		_, _ = r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	}()
	r.person(true, nil)
}

// unattendedRig is a coordinator rig with the unattended mode on; it skips
// unless the binary carries the tag.
func unattendedRig(t *testing.T, configure func(cfg *Config)) *rig {
	t.Helper()
	if !Unattended {
		t.Skip("needs -tags boxer_unattended")
	}
	return newRigWith(t, func(cfg *Config) {
		cfg.Coordinators = []string{"test.coordinator"}
		cfg.Unattended = true
		if configure != nil {
			configure(cfg)
		}
	})
}

func (inst *rig) unattendedGrant(mode ModeE, calls uint32) Grant {
	inst.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ceiling := Unlimited()
	g, err := inst.cli.Request(ctx, GrantRequest{Plan: "work alone", Calls: calls, Entries: []GrantEntry{{Instance: 7, Mode: mode}},
		Ceiling: &ceiling})
	require.NoError(inst.t, err, "the host approves without the person")
	return g
}

func (inst *rig) eventually(g Grant, key string, phase string) {
	inst.t.Helper()
	require.Eventually(inst.t, func() bool {
		st, err := inst.cli.Status(context.Background(), g.Handle, key, 0)
		return err == nil && st.Phase == phase
	}, 2*time.Second, 10*time.Millisecond)
}

func (inst *rig) nothingPending() bool {
	inst.svc.mu.Lock()
	defer inst.svc.mu.Unlock()
	return len(inst.svc.pending()) == 0
}

func TestUnattendedApprovesAGrant(t *testing.T) {
	r := unattendedRig(t, nil)
	g := r.unattendedGrant(ModeAct, 0)
	assert.Equal(t, "completed", r.call(g, "q", "get_text", "{}").Phase)
	assert.True(t, r.nothingPending())
}

func TestUnattendedApprovesAWidening(t *testing.T) {
	r := unattendedRig(t, nil)
	g := r.unattendedGrant(ModeObserve, 0)
	r.call(g, "q", "get_text", "{}")
	held := r.call(g, "w", "set_text", `{"text":"agent"}`)
	require.Equal(t, "input_required", held.Phase)
	r.eventually(g, "w", "accepted")
	r.host.frame(7)
	assert.Equal(t, "agent", r.host.docs[7].text)
}

func TestUnattendedAcceptsASuggestProposal(t *testing.T) {
	r := unattendedRig(t, nil)
	g := r.unattendedGrant(ModeSuggest, 0)
	r.call(g, "q", "get_text", "{}")
	out := r.call(g, "w", "set_text", `{"text":"suggested"}`)
	assert.NotEqual(t, "proposed", out.Phase)
	r.eventually(g, "w", "accepted")
	r.host.frame(7)
	assert.Equal(t, "suggested", r.host.docs[7].text)
}

func TestUnattendedStillConfirmsAConsequentialCommand(t *testing.T) {
	r := unattendedRig(t, nil)
	g := r.unattendedGrant(ModeAct, 0)
	assert.Equal(t, "proposed", r.call(g, "x", "export", "{}").Phase, "act outside stays the person's")
}

func TestUnattendedLeavesASpentBudgetToThePerson(t *testing.T) {
	r := unattendedRig(t, func(cfg *Config) { cfg.CallsMin, cfg.CallsMax = 1, 1 })
	g := r.unattendedGrant(ModeAct, 1)
	r.call(g, "q", "get_text", "{}")
	out := r.call(g, "q2", "get_text", "{}")
	require.Equal(t, "input_required", out.Phase)
	time.Sleep(50 * time.Millisecond)
	assert.False(t, r.nothingPending(), "more calls wait for the person")
}

func TestUnattendedLeavesMoreTimeToThePerson(t *testing.T) {
	r := unattendedRig(t, func(cfg *Config) { cfg.Deadline = 50 * time.Millisecond })
	g := r.unattendedGrant(ModeAct, 0)
	time.Sleep(80 * time.Millisecond)
	out := r.call(g, "q", "get_text", "{}")
	require.Equal(t, "input_required", out.Phase)
	time.Sleep(50 * time.Millisecond)
	assert.False(t, r.nothingPending(), "more time waits for the person")
}

func TestUnattendedLeavesARequestWithoutACeilingToThePerson(t *testing.T) {
	r := unattendedRig(t, nil)
	go func() {
		_, _ = r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}}})
	}()
	r.person(true, nil)
}

func TestUnattendedKeepsTheCeiling(t *testing.T) {
	r := unattendedRig(t, nil)
	_, err := r.cli.Request(context.Background(), GrantRequest{Entries: []GrantEntry{{Instance: 7, Mode: ModeAct}},
		Ceiling: &Ceiling{Mode: ModeObserve}})
	require.Error(t, err, "above the ceiling is refused before anyone decides")
}
