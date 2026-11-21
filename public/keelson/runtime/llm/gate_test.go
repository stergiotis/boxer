package llm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
	"github.com/stergiotis/boxer/public/keelson/runtime/agentrun"
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// An approved exit that a check below the tier wall refuses keeps its
// approval: the claim goes back, and the same request resent under the
// same key goes through once that check lets it (ADR-0312 §SD3). Here the
// later check is admission, a deny rule the moderator lifts.
func TestARefusalBelowTheWallGivesTheApprovalBack(t *testing.T) {
	msg := []openaichat.Message{{Role: openaichat.ChatRoleUser, Content: "twelve"}}
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	journal := &fakeApprovals{entries: map[string]agentrun.Entry{}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Approvals: journal})
	req := Request{Tier: uint8(agentrun.TierAmber), Messages: msg, Conversation: "chat-r", Run: 1}
	var refused *RefusedError
	_, err := cli.Complete(context.Background(), req)
	require.True(t, errors.As(err, &refused), "%v", err)
	require.True(t, refused.NeedsApproval())
	req.Approval = journal.plant(t, 1, 17, refused.CallId, refused.Sink, "person")

	require.NoError(t, svc.Ledger().Set(ration.Rule{Id: "stop", Select: ration.Selector{Kind: ration.AccountKindApp, Key: string(appId)}, Kind: ration.RuleKindDeny}))
	_, err = cli.Complete(context.Background(), req)
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Equal(t, "stop", refused.Rule, "admission refused the approved exit")
	assert.Zero(t, remote.calls.Load())

	require.True(t, svc.Ledger().Remove("stop"))
	res, err := cli.Complete(context.Background(), req)
	require.NoError(t, err, "the approval was given back, not spent")
	assert.Equal(t, "x", res.Content)
	assert.EqualValues(t, 1, remote.calls.Load())
	_, err = cli.Complete(context.Background(), req)
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "names no call this route refused", "and once it is spent it is gone")
}

// ctxProvider records the deadline of the context the call ran under.
type ctxProvider struct {
	deadline time.Time
	has      bool
}

func (f *ctxProvider) Complete(ctx context.Context, _ openaichat.CompletionRequest) (openaichat.CompletionResponse, error) {
	f.deadline, f.has = ctx.Deadline()
	return openaichat.CompletionResponse{Content: "x"}, nil
}
func (f *ctxProvider) Close() (err error) { return }

// The requester's deadline reaches the provider call when it is sooner
// than the service's bound; a later one leaves the service's bound.
func TestTheCallersDeadlineBoundsTheProviderCall(t *testing.T) {
	p := &ctxProvider{}
	cfg := Config{Endpoint: "http://127.0.0.1:1234/v1", Model: "m", Client: p, Timeout: time.Hour}
	cli, _, _ := serve(t, cfg)
	soon := time.Now().Add(30 * time.Second)
	ctx, cancel := context.WithDeadline(context.Background(), soon)
	defer cancel()
	_, err := cli.Complete(ctx, Request{Messages: userMessage()})
	require.NoError(t, err)
	require.True(t, p.has)
	assert.WithinDuration(t, soon, p.deadline, time.Second, "the caller's deadline, not the service's hour")

	late := time.Now().Add(3 * time.Hour)
	ctx2, cancel2 := context.WithDeadline(context.Background(), late)
	defer cancel2()
	_, err = cli.Complete(ctx2, Request{Messages: userMessage()})
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), p.deadline, 5*time.Second, "the service's bound is the tighter one")
}

// A call refused after admission — here by the write-ahead, a required
// trail on a host without a durable backend — sent nothing, and the exit
// budget is charged nothing for it.
func TestARefusalAfterAdmissionChargesNoBytes(t *testing.T) {
	envtest.Set(t, trail.RequiredEnv, "true")
	rec := trail.NewRecorder(nil, "run-test", zerolog.Nop())
	t.Cleanup(rec.Close)
	remote := &fakeProvider{resp: openaichat.CompletionResponse{Content: "x"}}
	cli, svc, _ := serve(t, Config{Endpoint: "https://api.example.net/v1", Model: "m", Client: remote, Trail: rec})
	_, err := cli.Complete(context.Background(), Request{Messages: userMessage()})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "audit trail")
	assert.Zero(t, remote.calls.Load())
	for _, a := range svc.Ledger().Accounts() {
		assert.Zero(t, a.Totals[ration.QuantityBytesSent], "account %s/%s: nothing left the cell", a.Account.Kind, a.Account.Key)
	}
}
