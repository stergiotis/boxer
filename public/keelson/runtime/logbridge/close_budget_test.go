package logbridge

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
)

// stalledLogStore blocks every log write until its ctx ends, as a server
// that stopped answering does.
type stalledLogStore struct {
	*factsstore.InMemoryFactsStore
}

func (inst stalledLogStore) WriteLogs(ctx context.Context, rows []factsstore.LogRow) (ids []uint64, err error) {
	<-ctx.Done()
	err = ctx.Err()
	return
}

// Close's final drain runs under Close's ctx: a stalled store costs the
// shutdown at most that ctx's deadline, and the rows it held are counted as
// write errors rather than waited for.
func TestSink_Close_AStalledStoreCannotHoldItPastItsCtx(t *testing.T) {
	sink, err := NewSink(t.Context(), stalledLogStore{factsstore.NewInMemoryFactsStore()}, Config{
		Capacity: 8, FlushN: 8, FlushInterval: time.Hour,
	})
	require.NoError(t, err)
	sink.enqueue(factsstore.LogRow{Level: "info", Message: "pending"})

	budget := 200 * time.Millisecond
	ctx, cancel := context.WithTimeout(t.Context(), budget)
	defer cancel()
	start := time.Now()
	_ = sink.Close(ctx)
	assert.Less(t, time.Since(start), budget+time.Second)
	assert.Equal(t, uint64(1), sink.WriteErrors())
	assert.Equal(t, uint64(0), sink.Written())
}
