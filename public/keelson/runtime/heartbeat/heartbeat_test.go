package heartbeat

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

func TestStart_RejectsNilStore(t *testing.T) {
	_, err := Start(context.Background(), nil, "run-1", 100*time.Millisecond, zerolog.Nop())
	require.Error(t, err)
}

func TestStart_RejectsEmptyRunId(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	_, err := Start(context.Background(), store, "", 100*time.Millisecond, zerolog.Nop())
	require.Error(t, err)
}

func TestStart_TicksAtIntervalThenStops(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	inst, err := Start(context.Background(), store, "run-tick", 50*time.Millisecond, zerolog.Nop())
	require.NoError(t, err)
	// Two intervals should produce at least two ticks; the goroutine
	// takes a beat to start the time.Ticker, so 250ms gives the second
	// tick comfortable headroom before Stop drains. Three ticks (50ms,
	// 100ms, 150ms) is the expected modal case under CI scheduling
	// jitter.
	time.Sleep(250 * time.Millisecond)
	inst.Stop()
	rows := store.Heartbeats()
	assert.GreaterOrEqual(t, len(rows), 2, "expected ≥2 heartbeats over 250ms at 50ms interval")
	for _, r := range rows {
		assert.Equal(t, "run-tick", r.RunId)
		assert.False(t, r.Ts.IsZero())
	}
}

func TestStop_Idempotent(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	inst, err := Start(context.Background(), store, "run-x", 100*time.Millisecond, zerolog.Nop())
	require.NoError(t, err)
	inst.Stop()
	inst.Stop() // must not deadlock or panic
}

func TestStop_NilSafe(t *testing.T) {
	var inst *Inst
	inst.Stop() // nil receiver path
}

func TestStart_ContextCancelStopsTicker(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	ctx, cancel := context.WithCancel(context.Background())
	inst, err := Start(ctx, store, "run-ctx", 50*time.Millisecond, zerolog.Nop())
	require.NoError(t, err)
	cancel()
	// Stop waits for the goroutine to acknowledge the cancellation.
	inst.Stop()
	pre := len(store.Heartbeats())
	time.Sleep(120 * time.Millisecond)
	assert.Equal(t, pre, len(store.Heartbeats()), "no new ticks after context cancellation")
}

func TestStart_NormalisesSubMinInterval(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	inst, err := Start(context.Background(), store, "run-clamp", 1*time.Microsecond, zerolog.Nop())
	require.NoError(t, err)
	// minInterval = 100ms; over 250ms we expect ≤3 ticks, definitely not
	// the millions a 1µs interval would produce.
	time.Sleep(250 * time.Millisecond)
	inst.Stop()
	rows := store.Heartbeats()
	assert.LessOrEqual(t, len(rows), 4, "interval clamp must prevent runaway tick rate")
	assert.GreaterOrEqual(t, len(rows), 1)
}
