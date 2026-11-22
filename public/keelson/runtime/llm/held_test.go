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
	"github.com/stergiotis/boxer/public/keelson/runtime/llm/ration"
	"github.com/stergiotis/boxer/public/keelson/runtime/trail"
	"github.com/stergiotis/boxer/public/llm/openaichat"
)

// Over clickhouse-local, ADR-0408: with BOXER_TRAIL_REQUIRED, a host whose
// trail is held until its store opens refuses a model call with the host's
// account of the store and asks the provider nothing. The host rebuilds
// the ledger from the earlier run's calls before it releases the trail;
// after the release the next call is admitted, durable, and charged once.
func TestHeldTrailRefusesUntilReleasedAndTheLedgerIsRebuiltFirst(t *testing.T) {
	exec := localFacts(t)
	ctx := context.Background()
	p := &fakeProvider{resp: openaichat.CompletionResponse{Content: "ok", FinishReason: "stop"}}
	earlier := localCfg(p)
	earlier.Trail = trail.NewRecorder(exec, "run-earlier", zerolog.Nop())
	cli, _, _ := serve(t, earlier)
	for range 2 {
		_, err := cli.Complete(ctx, Request{Messages: hi(), Purpose: "book/greet"})
		require.NoError(t, err)
	}
	earlier.Trail.Close()

	envtest.Set(t, trail.RequiredEnv, "true")
	cfg := localCfg(p)
	cfg.Trail = trail.NewRecorder(exec, "run-held", zerolog.Nop(), trail.WithHeld("the host's store has not opened; the host retries it and no restart is needed"))
	t.Cleanup(cfg.Trail.Close)
	cli, svc, _ := serve(t, cfg)
	appCalls := func() int64 {
		for _, a := range svc.Ledger().Accounts() {
			if a.Account == (ration.Account{Kind: ration.AccountKindApp, Key: string(appId)}) {
				return a.Totals[ration.QuantityCalls]
			}
		}
		return 0
	}
	assert.Zero(t, appCalls(), "a held trail is not read at construction")

	p.seen = openaichat.CompletionRequest{}
	_, err := cli.Complete(ctx, Request{Messages: hi(), Purpose: "book/greet"})
	var refused *RefusedError
	require.True(t, errors.As(err, &refused), "%v", err)
	assert.Contains(t, refused.Reason, "BOXER_TRAIL_REQUIRED")
	assert.Contains(t, refused.Reason, "no restart is needed")
	assert.Empty(t, p.seen.Messages, "the provider was not asked")

	rows, err := svc.RebuildLedger(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, rows, "the earlier run's calls")
	assert.EqualValues(t, 2, appCalls())
	require.NoError(t, cfg.Trail.Release(ctx))
	rows, err = svc.RebuildLedger(ctx)
	require.NoError(t, err)
	assert.Zero(t, rows, "a released trail is not read again")

	_, err = cli.Complete(ctx, Request{Messages: hi(), Purpose: "book/greet"})
	require.NoError(t, err)
	calls := svc.Calls()
	last := calls[len(calls)-1]
	assert.True(t, last.Durable, "its rows were on the trail before it left")
	assert.EqualValues(t, 3, appCalls(), "two earlier calls and this one, each once")

	landed, err := svc.ScanCalls(ctx, time.Now().Add(-time.Hour), 10)
	require.NoError(t, err)
	onServer := map[string]bool{}
	for _, c := range landed {
		onServer[c.CallId] = c.Refused
	}
	require.Len(t, calls, 2)
	assert.True(t, calls[0].Refused)
	for _, c := range calls {
		wasRefused, ok := onServer[c.CallId]
		assert.True(t, ok, "call %s landed", c.CallId)
		assert.Equal(t, c.Refused, wasRefused)
	}
}
