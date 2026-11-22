package trail

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/config/env/envtest"
)

// A trail held until the host's store opens keeps what it is given and is
// not durable; under BOXER_TRAIL_REQUIRED a write-ahead and an admission
// are refused with the host's account of the store, counted. Once released
// the held rows land, sealed, and work is admitted again.
func TestHeldTrailRefusesWithTheHostsReasonAndLandsOnRelease(t *testing.T) {
	envtest.Set(t, RequiredEnv, "true")
	exec := &gateExec{open: true}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithHeld("the store has not opened"))
	t.Cleanup(rec.Close)
	assert.True(t, rec.Held())
	assert.False(t, rec.Durable())
	assert.False(t, rec.HeldSince().IsZero())
	assert.Equal(t, "the store has not opened", rec.NotDurable())

	rec.SetHeldWhy("the store has not opened since 10:00:00 UTC; the host retries it")
	require.NoError(t, rec.HttpFetch(t.Context(), time.Now(), Context{}, HttpFetch{}))
	durable, refuse := rec.WriteAhead(t.Context())
	assert.False(t, durable)
	require.Error(t, refuse)
	assert.Contains(t, refuse.Error(), "BOXER_TRAIL_REQUIRED is set: the store has not opened since 10:00:00 UTC; the host retries it")
	refuse = rec.Admit()
	require.Error(t, refuse)
	assert.Contains(t, refuse.Error(), "the host retries it")
	assert.EqualValues(t, 2, rec.Counts().Refused)
	assert.Zero(t, exec.landed(), "nothing is sent while held")

	require.NoError(t, rec.Release(t.Context()))
	assert.False(t, rec.Held())
	assert.True(t, rec.Durable())
	assert.Empty(t, rec.NotDurable())
	assert.EqualValues(t, 2, exec.landed(), "the held row and its seal")
	assert.NoError(t, rec.Admit())
	durable, refuse = rec.WriteAhead(t.Context())
	require.NoError(t, refuse)
	assert.True(t, durable)
}

// A release that does not reach the server leaves the trail held.
func TestHeldTrailStaysHeldWhileTheServerIsDown(t *testing.T) {
	exec := &gateExec{}
	rec := NewRecorder(exec, "run-1", zerolog.Nop(), WithHeld(""))
	t.Cleanup(rec.Close)
	assert.Equal(t, notDurableHeld, rec.NotDurable())
	require.NoError(t, rec.HttpFetch(t.Context(), time.Now(), Context{}, HttpFetch{}))
	require.Error(t, rec.Release(context.Background()))
	assert.True(t, rec.Held())
	exec.mu.Lock()
	exec.open = true
	exec.mu.Unlock()
	require.NoError(t, rec.Release(context.Background()))
	assert.True(t, rec.Durable())
}

// A recorder without an executor reports the reason the host gave for it
// and has nothing to release.
func TestNoBackendReason(t *testing.T) {
	rec := NewRecorder(nil, "run-1", zerolog.Nop(), WithNoBackendReason("ClickHouse did not answer at boot"), WithHeld("ignored"))
	t.Cleanup(rec.Close)
	assert.Equal(t, "ClickHouse did not answer at boot", rec.NotDurable())
	assert.False(t, rec.Held())
	assert.Error(t, rec.Release(context.Background()))
	plain := NewRecorder(nil, "run-2", zerolog.Nop())
	t.Cleanup(plain.Close)
	assert.Equal(t, notDurableNoBackend, plain.NotDurable())
}
