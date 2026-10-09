package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

type fakeDelegation struct{ allow bool }

// CallContext attests only the call "call-1" of task-1.
func (inst *fakeDelegation) CallContext(task string, epoch uint64, call string, sender app.AppIdT, senderInstance uint64) (cc app.CallContext, ok bool, reason string) {
	if task != "task-1" || call != "call-1" {
		return cc, false, "the task has no call by that id"
	}
	return app.CallContext{Task: task, Epoch: epoch, Call: call, App: sender, Instance: senderInstance}, true, ""
}

func (inst *fakeDelegation) AllowDestination(task string, epoch uint64, destination string) (bool, string) {
	if inst.allow && destination == DelegationDestination {
		return true, ""
	}
	return false, "not in the grant"
}

// ADR-0269 §SD6: a completion that is an agent's work reaches the model only
// when the task's grant lists the model service.
func TestAgentCausedCompletionNeedsTheGrant(t *testing.T) {
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok", FinishReason: "stop"}}
	cli, svc, _ := serve(t, localCfg(p))
	ctx := context.Background()
	msgs := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "hi"}}
	obo := &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "call-1"}
	var refused *RefusedError

	_, err := cli.Complete(ctx, Request{Messages: msgs, OnBehalfOf: obo})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "no dispatcher")

	svc.SetDelegation(&fakeDelegation{allow: false})
	_, err = cli.Complete(ctx, Request{Messages: msgs, OnBehalfOf: obo})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "not in the grant")

	svc.SetDelegation(&fakeDelegation{allow: true})
	res, err := cli.Complete(ctx, Request{Messages: msgs, OnBehalfOf: obo})
	require.NoError(t, err)
	assert.Equal(t, "ok", res.Content)
	calls := svc.Calls()
	assert.Equal(t, "task-1", calls[len(calls)-1].Task)
	assert.Equal(t, "call-1", calls[len(calls)-1].TaskCall)

	// ADR-0277 §SD1: a call the dispatcher does not attest is refused, and
	// its row does not carry the task it claimed.
	_, err = cli.Complete(ctx, Request{Messages: msgs, OnBehalfOf: &app.OnBehalfOf{Task: "task-1", Epoch: 1, Call: "made-up"}})
	require.True(t, errors.As(err, &refused))
	assert.Contains(t, refused.Reason, "does not attest")
	calls = svc.Calls()
	assert.Empty(t, calls[len(calls)-1].Task)

	_, err = cli.Complete(ctx, Request{Messages: msgs})
	require.NoError(t, err, "the app's own completion is not an agent's")
}
