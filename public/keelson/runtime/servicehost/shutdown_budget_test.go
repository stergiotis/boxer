package servicehost

import (
	"context"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// stalledStore is a facts store whose lifecycle writes block until their
// ctx ends, as a server that stopped answering does.
type stalledStore struct {
	*factsstore.InMemoryFactsStore
	stall chan struct{}
}

func (inst *stalledStore) WriteAppLifecycle(ctx context.Context, row factsstore.AppLifecycleRow) (id uint64, err error) {
	select {
	case <-inst.stall:
		return inst.InMemoryFactsStore.WriteAppLifecycle(ctx, row)
	case <-ctx.Done():
		err = ctx.Err()
		return
	}
}

// A store that does not answer costs the reap at most the shutdown ctx's
// deadline: the "stopped" rows are written under that ctx.
func TestReapAll_AStalledStoreCannotHoldTheShutdownPastItsCtx(t *testing.T) {
	a := &recorder{manifest: headless("test.a")}
	f := newFixture(t, a)
	store := &stalledStore{InMemoryFactsStore: factsstore.NewInMemoryFactsStore(), stall: make(chan struct{})}
	close(store.stall) // the "started" row lands
	h := New(Config{Registry: f.reg, Bus: f.bus, Facts: store, Log: zerolog.Nop(), ShutdownDrain: time.Second})
	require.NoError(t, h.Start(t.Context(), []app.AppIdT{"test.a"}, nil))
	require.NoError(t, h.Wait(t.Context()))

	store.stall = make(chan struct{}) // from here on the server stalls
	budget := 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start := time.Now()
	h.ReapAll(ctx, StopReasonShutdown)
	assert.Less(t, time.Since(start), budget+time.Second, "the reap ended near the shutdown's deadline")
	assert.Equal(t, StateStopped, rowOf(t, h, "test.a").State)
	assert.Len(t, store.Lifecycles(), 1, "the stalled stop row did not land")
}
