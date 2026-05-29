//go:build llm_generated_opus47

package logdemo

import (
	"bytes"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	runtimeapp "github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// TestApp_InstancesAreIndependent guards per-window state isolation:
// two Open() calls must yield independent *App values with their own
// emit counter, stream toggle, and custom-message buffer.
func TestApp_InstancesAreIndependent(t *testing.T) {
	a1, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo")
	require.NoError(t, err)
	a2, err := runtimeapp.DefaultRegistry.Open("github.com/stergiotis/boxer/public/thestack/imzero2/egui2/demo/apps/logdemo")
	require.NoError(t, err)

	app1 := a1.(*App)
	app2 := a2.(*App)

	assert.NotSame(t, app1, app2, "factory must allocate a fresh App per Open")
	assert.NotEqual(t, app1.instNum, app2.instNum, "instance numbers must differ — that's what the logviewer uses to tell windows apart")

	app1.customMessage = "alpha"
	app1.streamEnabled = true
	app1.streamEveryN = 7
	assert.Equal(t, "hello from logdemo", app2.customMessage, "customMessage must not leak between windows")
	assert.False(t, app2.streamEnabled, "streamEnabled must not leak between windows")
	assert.Equal(t, uint64(30), app2.streamEveryN, "streamEveryN must not leak between windows")
}

// TestEmit_WritesThroughLogger: the emit path must go through the
// per-instance logger so structured fields (logdemo_inst) and the
// message land on every event. Build tag `binary_log` is set in this
// repo, which encodes zerolog as CBOR — the field names and message
// text are still embedded as plain text labels, so substring asserts
// work without decoding the whole map.
func TestEmit_WritesThroughLogger(t *testing.T) {
	var buf bytes.Buffer
	logger := zerolog.New(&buf).With().Uint64("logdemo_inst", 42).Logger()

	inst := &App{logger: logger, loggerInit: true, instNum: 42}
	inst.emit(zerolog.InfoLevel, "hello tail")

	out := buf.String()
	assert.Contains(t, out, "logdemo_inst", "structured field label must survive emit")
	assert.Contains(t, out, "hello tail", "message must survive emit")
	assert.Contains(t, out, "info", "level must survive emit")
	assert.Equal(t, uint64(1), inst.emitted.Load(), "counter must advance")
}

// TestEmit_BeforeMountFallsBackToGlobal: emit must not nil-deref if
// Frame is reached before Mount (e.g., a test path that drives Frame
// directly). The fallback to log.Logger keeps the demo crash-free in
// that pathology.
func TestEmit_BeforeMountFallsBackToGlobal(t *testing.T) {
	inst := &App{} // loggerInit = false
	// Must not panic.
	inst.emit(zerolog.InfoLevel, "no mount")
	assert.Equal(t, uint64(1), inst.emitted.Load())
}
