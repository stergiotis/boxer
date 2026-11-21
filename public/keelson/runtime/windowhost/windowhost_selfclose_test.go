package windowhost

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// A window's app may ask the host to close it (FrameContextI.CloseWindow): the
// request lands on that window alone, with the app's reason marked as its
// own on the lifecycle row.
func TestAWindowAsksToBeClosed(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&counterApp{manifest: mkManifest("test.selfclose")}))
	h := NewInst(t.Context(), reg, zerolog.Nop())
	_, err := h.Open("test.selfclose")
	require.NoError(t, err)
	_, err = h.Open("test.selfclose")
	require.NoError(t, err)
	h.mu.Lock()
	first, second := h.windows[0], h.windows[1]
	h.mu.Unlock()

	require.True(t, first.frameCtxApp.CloseWindow("the stop \"done\" fired"))

	require.True(t, h.closeRequested(first))
	require.False(t, h.closeRequested(second))
	h.mu.Lock()
	reason := first.stopReason
	h.mu.Unlock()
	require.Equal(t, "self-close: the stop \"done\" fired", reason)
}

// A context no host installed a close on says so, and the app falls back.
func TestAContextWithoutAWindowDoesNotClose(t *testing.T) {
	fc := app.NewStaticFrameContext(app.NewStaticMountContext("test.selfclose", zerolog.Nop(), nil, nil, nil))
	require.False(t, fc.CloseWindow("nothing to close"))
}
