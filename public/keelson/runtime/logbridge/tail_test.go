//go:build llm_generated_opus47 && binary_log

package logbridge_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// TestSink_Tail_RetainsAfterFlush is the contract the logviewer widget
// depends on: the tail buffer keeps the most recent rows even after
// the flush ring has drained. Without this the widget would see
// nothing 99% of the time.
func TestSink_Tail_RetainsAfterFlush(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      16,
		FlushN:        1,                     // flush every event
		FlushInterval: 5 * time.Millisecond,  // and quickly
		TailCapacity:  64,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink)
	for i := 0; i < 5; i++ {
		logger.Info().Int("i", i).Msg(fmt.Sprintf("ev%d", i))
	}

	// Wait until the store has all 5 — proves the flush ring drained.
	waitFor(t, func() bool { return len(store.Logs()) >= 5 }, time.Second)

	// Tail must still hold the rows even though the flush ring is now
	// empty.
	rows := sink.Tail(0)
	require.Len(t, rows, 5, "tail buffer must outlive the flush ring")
	for i, r := range rows {
		assert.Equal(t, fmt.Sprintf("ev%d", i), r.Message,
			"tail rows must be in chronological order (newest last)")
	}
}

// TestSink_Tail_DropOldestOnOverflow covers the bounded contract: when
// more events arrive than TailCapacity, the oldest fall off.
func TestSink_Tail_DropOldestOnOverflow(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      64,
		FlushN:        100,
		FlushInterval: 24 * time.Hour, // keep flush quiet
		TailCapacity:  4,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink)
	for i := 0; i < 10; i++ {
		logger.Info().Msg(fmt.Sprintf("ev%d", i))
	}

	rows := sink.Tail(0)
	require.Len(t, rows, 4, "TailCapacity caps retention")
	assert.Equal(t, "ev6", rows[0].Message, "oldest retained must be ev6 (events 0-5 evicted)")
	assert.Equal(t, "ev9", rows[3].Message)
}

// TestSink_Tail_MaxClampsResult lets a UI ask for "last N" without
// receiving a giant slice when the buffer is full.
func TestSink_Tail_MaxClampsResult(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      64,
		FlushN:        100,
		FlushInterval: 24 * time.Hour,
		TailCapacity:  100,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink)
	for i := 0; i < 50; i++ {
		logger.Info().Msg(fmt.Sprintf("ev%d", i))
	}

	rows := sink.Tail(10)
	require.Len(t, rows, 10)
	assert.Equal(t, "ev40", rows[0].Message)
	assert.Equal(t, "ev49", rows[9].Message)

	assert.Equal(t, 50, sink.TailLen())
	assert.Equal(t, 100, sink.TailCapacity())
}

// TestSink_Tail_DisabledWhenCapacityNegative proves the documented
// opt-out: TailCapacity < 0 disables tail retention so high-throughput
// hosts that never read the buffer don't pay for the parallel writes.
func TestSink_Tail_DisabledWhenCapacityNegative(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      16,
		FlushN:        100,
		FlushInterval: 24 * time.Hour,
		TailCapacity:  -1, // explicit opt-out
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink)
	logger.Info().Msg("dropped from tail view")

	assert.Empty(t, sink.Tail(0))
	assert.Zero(t, sink.TailLen())
	assert.Zero(t, sink.TailCapacity())
}
