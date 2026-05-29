//go:build llm_generated_opus47 && binary_log

package logdemo

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/keelson/runtime/factsstore"
	"github.com/stergiotis/boxer/public/keelson/runtime/logbridge"
)

// TestE2E_LogdemoEmitsIntoSink wires the actual host chain that the
// running carousel uses:
//
//   host logger ──► logbridge.Sink ──► tail buffer (+ factsstore)
//        │
//        └─► app.AppLogger(host logger, manifest.Id) ──► mountCtx.Log()
//                                                        │
//                                                        └─► inst.logger
//                                                            (via Mount)
//
// emit() through inst.logger must land a decoded LogRow in the
// Sink's tail buffer — that's exactly what the logviewer widget
// reads each frame.
//
// Reproduces the issue the operator hit ("ringbuffer empty after
// clicking emit") in a hermetic test: if the path is broken we
// catch it without a GUI.
func TestE2E_LogdemoEmitsIntoSink(t *testing.T) {
	store := factsstore.NewInMemoryFactsStore()
	sink, err := logbridge.NewSink(store, logbridge.Config{
		Capacity:     8,
		FlushN:       1,
		TailCapacity: 8,
	})
	require.NoError(t, err)
	defer sink.Close()

	// Build a host logger over the sink (multi-writer with no
	// passthrough, mirroring InstallGlobal(nil, sink)).
	hostLogger := logbridge.NewLogger(nil, sink)

	// App-scoped derivation, exactly the chain the windowhost uses.
	appLogger := runtimeapp.AppLogger(hostLogger, manifest.Id)
	mountCtx := runtimeapp.NewStaticMountContext(manifest.Id, appLogger, nil, nil, nil)

	// Build the AppI and run Mount → emit, same as a real Frame call.
	a := newApp()
	require.NoError(t, a.Mount(mountCtx))
	a.emit(zerolog.InfoLevel, "ringbuffer-smoke")

	// Drain triggered by FlushN=1; assert the tail buffer (what the
	// logviewer reads) has the event.
	rows := sink.Tail(0)
	require.Len(t, rows, 1, "sink tail must contain the emitted event — the logviewer reads from this buffer")
	got := rows[0]
	assert.Equal(t, "info", got.Level)
	assert.Equal(t, "ringbuffer-smoke", got.Message)
	assert.Equal(t, manifest.Id, got.AppId, "AppLogger must inject the manifest id field; Sink decode picks it up via AppIdFieldName")

	// The logdemo_inst custom field must survive as a typed LogField.
	var sawInst bool
	for _, f := range got.Fields {
		if f.Name == "logdemo_inst" {
			sawInst = true
			assert.True(t, f.Kind == factsstore.LogFieldKindUint || f.Kind == factsstore.LogFieldKindInt,
				"logdemo_inst is uint64 — Sink should decode it as Uint or Int")
		}
	}
	assert.True(t, sawInst, "logdemo_inst field missing — the logviewer can't tell windows apart without it")
}
