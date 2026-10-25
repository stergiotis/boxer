package httpegress

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

type fakeDelegation struct {
	allow map[string]bool
	// tier and conversation are what the dispatcher attests for every
	// call: the run's tier the coordinator declared (ADR-0312 §SD4).
	tier         uint8
	conversation string
}

// CallContext attests every call but "made-up".
func (inst *fakeDelegation) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	if call == "made-up" {
		return cc, false, "the task has no call by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance, Tier: inst.tier, Conversation: inst.conversation}, true, ""
}

func (inst *fakeDelegation) AllowDestination(task string, epoch uint64, destination string) (bool, string) {
	if inst.allow[task+"|"+destination] {
		return true, ""
	}
	return false, "not in the grant"
}

// ADR-0269 §SD6: a fetch that is an agent's work reaches only a destination
// the task's grant lists, and is refused while no dispatcher checks it.
func TestAgentCausedFetchNeedsTheGrant(t *testing.T) {
	srv, _ := origin(t)
	cli, svc, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}})}, "tiles")
	ctx := context.Background()
	obo := &app.OnBehalfOf{Task: "task-1", Epoch: 1}
	var refused *RefusedError

	_, err := cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: obo})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "no dispatcher")

	svc.SetDelegation(&fakeDelegation{allow: map[string]bool{"task-1|" + DelegationDestination("tiles"): true}})
	res, err := cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: obo})
	require.NoError(t, err)
	assert.Equal(t, "hello", string(res.Body))

	_, err = cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: &app.OnBehalfOf{Task: "task-2", Epoch: 1}})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "not in the grant")

	_, err = cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a"})
	require.NoError(t, err, "the app's own fetch is not an agent's")
	calls := svc.Calls()
	require.NotEmpty(t, calls)
	assert.Equal(t, "task-2", calls[len(calls)-2].Task)

	// ADR-0277 §SD1: a call the dispatcher does not attest is refused, and
	// its row does not carry the task it claimed.
	_, err = cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "made-up"}})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "does not attest")
	calls = svc.Calls()
	assert.Empty(t, calls[len(calls)-1].Task)
}

// ADR-0312 §SD4: the run's tier on an agent-caused fetch is the
// dispatcher's word. A sender that declares CLEAR cannot lower what the
// coordinator declared on the call, so an AMBER run's fetch to a GREEN
// destination is refused, and the row carries the tier the wall saw and
// the conversation the dispatcher attested.
func TestAnAgentCausedFetchCarriesTheRunsAttestedTier(t *testing.T) {
	srv, _ := origin(t)
	remote := spec("remote", Destination{Prefixes: []string{"https://tiles.example/"}})
	cli, svc, _ := serve(t, []DestinationSpec{spec("tiles", Destination{Prefixes: []string{srv.URL + "/tiles/"}}), remote}, "tiles", "remote")
	ctx := context.Background()
	obo := &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "c1"}
	d := &fakeDelegation{allow: map[string]bool{"task-1|" + DelegationDestination("tiles"): true, "task-1|" + DelegationDestination("remote"): true},
		tier: uint8(agentrun.TierAmber), conversation: "conv-9"}
	svc.SetDelegation(d)

	_, err := cli.Fetch(ctx, "remote", Request{URL: "https://tiles.example/1", OnBehalfOf: obo})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "AMBER, above the clearance (GREEN) of destination remote")
	calls := svc.Calls()
	require.Len(t, calls, 1)
	assert.Equal(t, agentrun.TierAmber, calls[0].Tier, "the tier the wall saw is the attested one")
	assert.Equal(t, "conv-9", calls[0].Conversation)
	assert.Equal(t, "c1", calls[0].TaskCall)

	res, err := cli.Fetch(ctx, "tiles", Request{URL: srv.URL + "/tiles/a", OnBehalfOf: obo, Tier: uint8(agentrun.TierRed)})
	require.NoError(t, err, "inside the cell nothing is withheld")
	assert.Equal(t, "hello", string(res.Body))
	calls = svc.Calls()
	assert.Equal(t, agentrun.TierRed, calls[len(calls)-1].Tier, "the higher of the sender's and the attested tier")

	d.tier = uint8(agentrun.TierGreen)
	_, err = cli.Fetch(ctx, "remote", Request{URL: "https://tiles.example/1", OnBehalfOf: obo})
	if errors.As(err, &refused) {
		assert.NotContains(t, refused.Reason, "above the clearance", "a GREEN run passes the GREEN wall; what fails is the unreachable origin")
	}
}
