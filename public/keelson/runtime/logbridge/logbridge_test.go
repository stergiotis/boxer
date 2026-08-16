//go:build binary_log

package logbridge_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// waitFor polls until cond returns true or the timeout elapses. Helps
// avoid sleeping a fixed duration after we trigger the async flusher.
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("waitFor: condition not satisfied within %s", timeout)
}

// TestSink_EndToEnd_ZerologToFacts feeds a zerolog event through the
// Sink and asserts that the structured envelope (level, message,
// arbitrary string field) round-trips into a LogRow. Uses the project's
// in-memory FactsStoreI shim to stay hermetic.
func TestSink_EndToEnd_ZerologToFacts(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		AppId:         "play",
		Capacity:      32,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink).With().Timestamp().Logger()
	logger.Info().Str("subject", "ch.query.boxer").Int("latency_ms", 7).Msg("query ok")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)

	logs := store.Logs()
	require.Len(t, logs, 1)
	got := logs[0]
	assert.Equal(t, "info", got.Level)
	assert.Equal(t, "query ok", got.Message)
	assert.Equal(t, "play", string(got.AppId))
	assert.False(t, got.Ts.IsZero(), "Ts must be set from CBOR or default to now()")

	// Field fan-out: one string + one int.
	var sawSubject, sawLatency bool
	for _, f := range got.Fields {
		switch f.Name {
		case "subject":
			sawSubject = true
			assert.Equal(t, factsstore.LogFieldKindString, f.Kind)
			assert.Equal(t, "ch.query.boxer", f.Str)
		case "latency_ms":
			sawLatency = true
			assert.True(t, f.Kind == factsstore.LogFieldKindInt || f.Kind == factsstore.LogFieldKindUint,
				"numeric field should decode as Int or Uint, got Kind=%d", f.Kind)
			if f.Kind == factsstore.LogFieldKindInt {
				assert.Equal(t, int64(7), f.Int)
			} else {
				assert.Equal(t, uint64(7), f.Uint)
			}
		}
	}
	assert.True(t, sawSubject, "subject field missing — fan-out lost it")
	assert.True(t, sawLatency, "latency_ms field missing — fan-out lost it")
}

// TestSink_LiftsInstanceKeyToItsOwnColumn is ADR-0191 §SD4 on the log path.
//
// windowhost pre-tags every per-window logger with `instance_id`, so these
// events have carried the window since ADR-0188 — inside the field lane,
// where filtering on it means unpacking a mixed membership per row. The
// promotion moves it to a column and, crucially, takes it OUT of Fields: a
// value written to both places would be filterable two ways with no rule for
// which wins, which is exactly how app_id is already handled.
func TestSink_LiftsInstanceKeyToItsOwnColumn(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		AppId: "play", Capacity: 32, FlushN: 1, FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	// The shape windowhost builds: app_id from AppLogger, instance_id added
	// per window.
	logger := zerolog.New(sink).With().Timestamp().
		Uint64(logbridge.InstanceKeyFieldName, 12).Logger()
	logger.Info().Str("subject", "ch.query.boxer").Msg("query ok")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	got := store.Logs()[0]

	assert.Equal(t, uint64(12), got.InstanceKey,
		"the window must reach the column, or a per-window filter has to unpack the field lane")
	for _, f := range got.Fields {
		assert.NotEqual(t, logbridge.InstanceKeyFieldName, f.Name,
			"lifted, not copied — a value in both places is filterable two ways")
	}
}

// TestSink_KeepsANonNumericInstanceIdAsAField pins the coercion boundary. A
// logger that means something else by the same name — a string tag, say — is
// not this vocabulary's window key, and silently coercing it would put a
// wrong number in a column readers filter on. It stays a field.
func TestSink_KeepsANonNumericInstanceIdAsAField(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		AppId: "play", Capacity: 32, FlushN: 1, FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink).With().Timestamp().
		Str(logbridge.InstanceKeyFieldName, "not-a-number").Logger()
	logger.Info().Msg("hello")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	got := store.Logs()[0]

	assert.Zero(t, got.InstanceKey)
	var kept bool
	for _, f := range got.Fields {
		if f.Name == logbridge.InstanceKeyFieldName {
			kept = true
			assert.Equal(t, "not-a-number", f.Str)
		}
	}
	assert.True(t, kept, "an unrecognised shape must survive as a field, not vanish")
}

// TestSink_RingOverflow_DropsOldest exhausts the ring without giving the
// flusher a chance to run, then asserts the dropped counter reports the
// oversupply. We pause the flusher by setting FlushInterval far in the
// future and never tripping FlushN before bursting past Capacity.
func TestSink_RingOverflow_DropsOldest(t *testing.T) {
	// Stub store with a blocking WriteLog so the flusher cannot drain
	// while we burst — guarantees overflow occurs in the ring rather
	// than in transit to the store.
	gate := make(chan struct{})
	store := &blockingStore{gate: gate}
	sink, err := logbridge.NewSink(store, logbridge.Config{
		AppId:         "play",
		Capacity:      4,
		FlushN:        4,
		FlushInterval: time.Hour,
	})
	require.NoError(t, err)
	defer func() {
		close(gate)
		sink.Close()
	}()

	logger := zerolog.New(sink)
	// 4 fit; 6 more are over capacity. The flusher *may* wake when
	// FlushN=4 is reached and consume one before the burst completes,
	// but it cannot drain past 1 because gate is unbuffered.
	for i := 0; i < 10; i++ {
		logger.Info().Int("i", i).Msg("burst")
	}

	// Settling: give the flusher a brief moment to attempt drain (it'll
	// block on the gate after at most one write) so the dropped counter
	// stabilises before we read it.
	time.Sleep(50 * time.Millisecond)
	assert.GreaterOrEqual(t, sink.Dropped(), uint64(1), "expected at least one ring overflow drop")
}

// blockingStore satisfies FactsStoreI but parks WriteLog on a gate so the
// test can keep the ring full. The non-Log methods are unused but must
// exist to satisfy the interface.
//
// This file is behind `binary_log`, so the default `go test ./...` never
// compiles it and the stub drifted: the state verbs left the interface with
// ADR-0105 D3a and the column-width verbs joined it with ADR-0151, neither of
// which went red here. Anyone adding a verb to FactsStoreI has to remember
// this lane exists.
type blockingStore struct {
	gate chan struct{}
	n    atomic.Uint64
}

var _ factsstore.FactsStoreI = (*blockingStore)(nil)

func (s *blockingStore) WriteGrant(_ factsstore.GrantRow) (uint64, error) { return 0, nil }
func (s *blockingStore) WriteAudit(_ factsstore.AuditRow) (uint64, error) { return 0, nil }
func (s *blockingStore) WriteRuntimeStart(_ factsstore.RuntimeStartRow) (uint64, error) {
	return 0, nil
}
func (s *blockingStore) WriteRuntimeHeartbeat(_ factsstore.HeartbeatRow) (uint64, error) {
	return 0, nil
}
func (s *blockingStore) WriteAppLifecycle(_ factsstore.AppLifecycleRow) (uint64, error) {
	return 0, nil
}
func (s *blockingStore) WriteLaunch(_ factsstore.LaunchRow) (uint64, error) { return 0, nil }
func (s *blockingStore) WriteLog(_ factsstore.LogRow) (id uint64, err error) {
	<-s.gate
	id = s.n.Add(1)
	return
}
func (s *blockingStore) WriteLogs(rows []factsstore.LogRow) (ids []uint64, err error) {
	// Park the whole batch on the gate, matching WriteLog, so the flusher
	// stalls and the ring overflows during the burst.
	<-s.gate
	ids = make([]uint64, len(rows))
	for i := range rows {
		ids[i] = s.n.Add(1)
	}
	return
}
func (s *blockingStore) WriteWorkingset(_ factsstore.WorkingsetRow) (uint64, error) {
	return 0, nil
}
func (s *blockingStore) LatestWorkingset(_ app.AppIdT, _ string) (cfg []byte, kind string, found bool, err error) {
	return
}
func (s *blockingStore) ListWorkingsets() (rows []factsstore.WorkingsetRow, err error) {
	return
}
func (s *blockingStore) DeleteWorkingset(_ app.AppIdT, _ string) (err error) { return }
func (s *blockingStore) WriteColumnWidth(_ factsstore.ColumnWidthRow) (uint64, error) {
	return 0, nil
}
func (s *blockingStore) ListColumnWidths(_ app.AppIdT) (rows []factsstore.ColumnWidthRow, err error) {
	return
}
func (s *blockingStore) DeleteColumnWidth(_ app.AppIdT, _ string, _ string, _ string) (err error) {
	return
}

// TestSink_Close_DrainsPending guarantees the close path flushes any
// buffered rows synchronously so a process exit does not lose log data
// already accepted on the writer.
func TestSink_Close_DrainsPending(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      64,
		FlushN:        100,            // never reach
		FlushInterval: 24 * time.Hour, // never tick
	})
	require.NoError(t, err)

	logger := zerolog.New(sink)
	for i := 0; i < 5; i++ {
		logger.Info().Int("i", i).Msg("pending")
	}
	require.NoError(t, sink.Close())
	assert.Len(t, store.Logs(), 5)
}

// TestSink_NilStore rejects construction without a store — eh.Errorf
// path matches the rest of the runtime.
func TestSink_NilStore(t *testing.T) {
	_, err := logbridge.NewSink(nil, logbridge.Config{})
	require.Error(t, err)
}
