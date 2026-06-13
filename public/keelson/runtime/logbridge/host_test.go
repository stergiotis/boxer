//go:build binary_log

package logbridge_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// TestNewLogger_FansOutToBaseAndSink wires the canonical host pattern —
// stdout + Sink — and asserts both writers receive every event.
func TestNewLogger_FansOutToBaseAndSink(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      16,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	var base bytes.Buffer
	logger := logbridge.NewLogger(&base, sink)
	logger.Info().Str("subject", "ch.query.boxer").Msg("ok")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)

	// Base writer received the marshalled bytes (CBOR or JSON depending
	// on build tags) — verify the message phrase is present so we know
	// the fan-out reached both writers.
	assert.Contains(t, base.String(), "ok",
		"base writer must receive every event in addition to the Sink")
	logs := store.Logs()
	require.Len(t, logs, 1)
	assert.Equal(t, "ok", logs[0].Message)
}

// TestNewLogger_NilBaseSinkOnly drops the operator-facing writer so only
// the facts capture runs. Useful when a host wants ClickHouse-only log
// retention without writing to disk.
func TestNewLogger_NilBaseSinkOnly(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      8,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := logbridge.NewLogger(nil, sink)
	logger.Info().Msg("captured")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	assert.Equal(t, "captured", store.Logs()[0].Message)
}

// TestNewLogger_NilSinkBaseOnly turns the helper into a thin convenience
// wrapper over zerolog.New — proves the helper degrades to "ordinary
// logger" when the host opts out of fact capture.
func TestNewLogger_NilSinkBaseOnly(t *testing.T) {
	var base bytes.Buffer
	logger := logbridge.NewLogger(&base, nil)
	logger.Info().Msg("stdout only")
	assert.Contains(t, base.String(), "stdout only")
}

// TestNewLogger_BothNil is the no-op case — the helper must not panic
// and must yield a logger callers can use safely. Hosts misconfigured
// at startup should at least not crash on the first log call.
func TestNewLogger_BothNil(t *testing.T) {
	logger := logbridge.NewLogger(nil, nil)
	// No assertions on output — Nop logger drops events on the floor.
	// The contract under test is "this call does not panic".
	logger.Info().Msg("dropped")
}

// TestAppLogger_RoundTripsAppId is the end-to-end check the user-visible
// contract relies on: a per-app logger built by app.AppLogger flows
// through the Sink and lands the AppId on the resulting LogRow without
// any extra wiring on the Sink itself.
func TestAppLogger_RoundTripsAppId(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		// Deliberately leave Config.AppId empty so a passing test
		// proves the event-encoded app_id is the source of truth.
		Capacity:      16,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	base := logbridge.NewLogger(nil, sink)
	playLogger := app.AppLogger(base, "github.com/example/play")
	imztopLogger := app.AppLogger(base, "github.com/example/imztop")

	playLogger.Info().Msg("from play")
	imztopLogger.Warn().Msg("from imztop")

	waitFor(t, func() bool { return len(store.Logs()) >= 2 }, time.Second)

	logs := store.Logs()
	byApp := map[string]factsstore.LogRow{}
	for _, r := range logs {
		byApp[string(r.AppId)] = r
	}
	require.Contains(t, byApp, "github.com/example/play")
	require.Contains(t, byApp, "github.com/example/imztop")
	assert.Equal(t, "from play", byApp["github.com/example/play"].Message)
	assert.Equal(t, "from imztop", byApp["github.com/example/imztop"].Message)
}

// TestAppLogger_FallbackToConfigAppId proves the precedence: when no
// app_id field is encoded into the event (caller used a raw base logger
// without AppLogger), the Sink's Config.AppId still applies.
func TestAppLogger_FallbackToConfigAppId(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		AppId:         "runtime", // fallback when event has no app_id
		Capacity:      8,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)
	defer sink.Close()

	logger := zerolog.New(sink)
	logger.Info().Msg("no app_id in event")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	assert.Equal(t, "runtime", string(store.Logs()[0].AppId))
}
