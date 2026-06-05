//go:build llm_generated_opus47 && binary_log

package logbridge_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stergiotis/boxer/public/observability/eh/eb"
)

// TestInstallGlobal_RewiresGlobalAndRestores guards the two contracts
// the host bootstrap relies on: (1) after Install, the package-level
// log.Logger emits to both the passthrough writer and the Sink;
// (2) calling the returned closer restores the prior global so a
// subsequent test (or graceful shutdown sequence) sees the original
// logger.
func TestInstallGlobal_RewiresGlobalAndRestores(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      16,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	prev := log.Logger
	var passthrough bytes.Buffer

	closer := logbridge.InstallGlobal(&passthrough, sink)

	// Emit through the package-level logger — Install should have
	// retargeted it.
	log.Info().Str("subject", "ch.query.boxer").Msg("hello")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)

	assert.Contains(t, passthrough.String(), "hello",
		"passthrough writer must see the same event as the Sink")
	logs := store.Logs()
	require.Len(t, logs, 1)
	assert.Equal(t, "hello", logs[0].Message)

	require.NoError(t, closer())
	assert.Equal(t, prev, log.Logger, "closer must restore the previous global")
}

// TestInstallGlobal_AppLoggerRoundTrip mirrors the realistic host
// pattern: install the bridge globally, then ask each app for a
// per-app logger via app.AppLogger(log.Logger, appId). The Sink must
// recover the AppId from the event-encoded `app_id` field.
func TestInstallGlobal_AppLoggerRoundTrip(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      16,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	closer := logbridge.InstallGlobal(&bytes.Buffer{}, sink)
	defer closer()

	playLogger := app.AppLogger(log.Logger, "github.com/example/play")
	playLogger.Info().Msg("from play")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	logs := store.Logs()
	require.Len(t, logs, 1)
	assert.Equal(t, "github.com/example/play", string(logs[0].AppId))
}

// TestNopCloser_NoPanic guards the "facts capture disabled" code path
// in the host bootstrap — every code site that uses the closer in a
// `defer` or App.After hook must be safe when the bridge wasn't
// installed.
func TestNopCloser_NoPanic(t *testing.T) {
	closer := logbridge.NopCloser()
	require.NoError(t, closer())
	require.NoError(t, closer(), "closer should be idempotent")
}

// sameFunc compares two function values by their reflect.Value.Pointer
// — Go forbids `==` on function values directly. Used by the
// marshaler-restoration test to confirm zerolog.ErrorMarshalFunc was
// reset to the prior global pointer.
func sameFunc(a, b func(error) interface{}) (eq bool) {
	eq = reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
	return
}

// TestInstallGlobal_SwapsAndRestoresErrorMarshaler covers the
// boxer-error integration: InstallGlobal must swap
// zerolog.ErrorMarshalFunc to eh.MarshalError (the structured
// marshaler that emits {streams:[...]}) so the Sink decoder can
// project the result into LogRow.ErrorContext for the detail-pane
// tree renderer. The closer must restore the previous marshaler so
// a subsequent test (or graceful shutdown) sees the original.
func TestInstallGlobal_SwapsAndRestoresErrorMarshaler(t *testing.T) {
	prev := zerolog.ErrorMarshalFunc

	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      8,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	closer := logbridge.InstallGlobal(&bytes.Buffer{}, sink)
	require.True(t, sameFunc(zerolog.ErrorMarshalFunc, eh.MarshalError),
		"InstallGlobal must point ErrorMarshalFunc at eh.MarshalError so .Err(boxerErr) emits the structured shape the logbridge decoder recognises")

	require.NoError(t, closer())
	assert.True(t, sameFunc(zerolog.ErrorMarshalFunc, prev),
		"closer must restore the previous ErrorMarshalFunc — otherwise repeated test setups leak the global mutation")
}

// TestInstallGlobal_BoxerErrorPopulatesErrorContext proves the
// Path B integration end-to-end: build a wrapped boxer error chain
// (cause + stack + structured CBOR data via eb.Build), emit via
// .Err, and confirm the resulting LogRow carries (a) a flat Error
// summary so table-column readers still work, and (b) a non-nil
// ErrorContext whose Streams enumerate the per-stack facts the
// detail-pane tree renderer walks. The structured-data leaf must
// surface its CBOR diagnostic so the operator can read the
// attached fields without manual decoding.
func TestInstallGlobal_BoxerErrorPopulatesErrorContext(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:     8,
		FlushN:       1,
		TailCapacity: 8,
	})
	require.NoError(t, err)
	defer sink.Close()

	closer := logbridge.InstallGlobal(&bytes.Buffer{}, sink)
	defer closer()

	// Three-level chain with structured data on the leaf — the same
	// pattern logdemo's "structured" boxer-err scenario uses.
	leaf := eb.Build().
		Str("op", "Sink.appendTail").
		Uint64("ring_capacity", 32).
		Errorf("ring full")
	mid := eh.Errorf("logbridge.flush: %w", leaf)
	wrapped := eh.Errorf("apply: %w", mid)

	log.Error().Err(wrapped).Msg("test boxer error")

	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)
	rows := sink.Tail(0)
	require.Len(t, rows, 1)
	got := rows[0]

	assert.Equal(t, "test boxer error", got.Message,
		"event message must round-trip unchanged through CBOR + decode")

	// Flat summary still populated — the chain's outermost message
	// (eh.Errorf("apply: %w", ...)) is what table-column readers see.
	assert.NotEmpty(t, got.Error, "Error envelope field must be populated even with the structured marshaler")
	assert.Contains(t, got.Error, "apply",
		"flat summary must carry the outermost wrap message")
	assert.NotContains(t, got.Error, "&{0x",
		"flat summary must not be a pointer-stringified blob — decoder fell back to fmt instead of structured decode")

	// Structured projection — the detail-pane walks this.
	require.NotNil(t, got.ErrorContext, "structured marshaler output must decode into ErrorContext")
	require.NotEmpty(t, got.ErrorContext.Streams,
		"chain must produce at least one stream — eh.MarshalError emits 'no-stack' or 'stack-N' per dedup'd stack")

	// Walk the chain looking for the markers we know must be there:
	// the innermost message, every wrap message, and the structured-
	// data diagnostic (CBOR diagnostic notation of the eb.Build
	// payload).
	var (
		sawApply     bool
		sawFlush     bool
		sawRingFull  bool
		sawDataDiag  bool
		sawStackName bool
	)
	for _, st := range got.ErrorContext.Streams {
		if strings.HasPrefix(st.Name, "stack-") {
			sawStackName = true
		}
		for _, f := range st.Facts {
			switch {
			case strings.Contains(f.Msg, "apply:"):
				sawApply = true
			case strings.Contains(f.Msg, "logbridge.flush:"):
				sawFlush = true
			case strings.Contains(f.Msg, "ring full"):
				sawRingFull = true
			}
			if strings.Contains(f.DataDiag, "ring_capacity") {
				sawDataDiag = true
			}
		}
	}
	assert.True(t, sawApply, "outermost wrap message must appear as a fact")
	assert.True(t, sawFlush, "middle wrap message must appear as a fact")
	assert.True(t, sawRingFull, "innermost leaf message must appear as a fact")
	assert.True(t, sawDataDiag,
		"eb.Build()-attached structured data must surface as cbor.Diagnose output on a fact's DataDiag — otherwise the detail pane has nothing to show for the structured payload")
	assert.True(t, sawStackName,
		"chain with stacks must produce at least one 'stack-N' stream — 'no-stack' alone means runtime.Callers returned empty for every wrap level")
}

// TestInstallGlobal_PreservesPriorLoggerContext guards the contract that
// InstallGlobal re-Outputs the *previous* logger onto the fan-out writer
// rather than building a fresh one — so the context logging.Apply
// attached (here a --logCorrelationId field) survives the bridge install.
// A fresh NewLogger would silently drop it.
func TestInstallGlobal_PreservesPriorLoggerContext(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:      8,
		FlushN:        1,
		FlushInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	// Stand in for logging.Apply having attached a correlation id to the
	// global logger before the bridge runs.
	prev := log.Logger
	t.Cleanup(func() { log.Logger = prev })
	log.Logger = log.Logger.With().Str("correlationId", "abc123").Logger()

	var passthrough bytes.Buffer
	closer := logbridge.InstallGlobal(&passthrough, sink)
	defer closer()

	log.Info().Msg("ctx-survives")
	waitFor(t, func() bool { return len(store.Logs()) >= 1 }, time.Second)

	assert.Contains(t, passthrough.String(), "correlationId",
		"InstallGlobal must preserve the prior logger's context field name")
	assert.Contains(t, passthrough.String(), "abc123",
		"InstallGlobal must preserve the prior logger's context value — a fresh NewLogger would drop --logCorrelationId")
}
