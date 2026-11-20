package windowhost

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
	"github.com/stergiotis/boxer/public/observability/ph"
)

// panickyApp panics in whichever of its calls the test asks for.
type panickyApp struct {
	manifest      app.Manifest
	mountPanics   bool
	framePanics   bool
	unmountPanics bool
	unmountCalls  int
}

func (inst *panickyApp) Manifest() (m app.Manifest) { return inst.manifest }
func (inst *panickyApp) Mount(app.MountContextI) (err error) {
	if inst.mountPanics {
		panic("mount boom")
	}
	return
}
func (inst *panickyApp) Frame(app.FrameContextI) (err error) {
	if inst.framePanics {
		panic("frame boom")
	}
	return
}
func (inst *panickyApp) Unmount(app.MountContextI) (err error) {
	inst.unmountCalls++
	if inst.unmountPanics {
		panic("unmount boom")
	}
	return
}

func openPanicky(t *testing.T, h *Inst) (w *window) {
	t.Helper()
	_, err := h.Open("test.panicky")
	require.NoError(t, err)
	h.mu.Lock()
	w = h.windows[len(h.windows)-1]
	h.mu.Unlock()
	return
}

// A Mount that panics before drawing anything is a failed Mount: the stream
// is as balanced as before the call, so the window can show the error.
func TestMountPanicBeforeDrawingIsAFailedMount(t *testing.T) {
	reg := app.NewRegistry()
	a := &panickyApp{manifest: mkManifest("test.panicky"), mountPanics: true}
	require.NoError(t, reg.Register(a))
	var logged strings.Builder
	h := NewInst(reg, zerolog.New(&logged))
	w := openPanicky(t, h)
	var err error
	assert.NotPanics(t, func() { err = mountGuarded(w, h.logger) })
	assert.ErrorIs(t, err, ph.ErrPanic)
	assert.Contains(t, logged.String(), "test.panicky")
}

// A Frame panic is logged with its window and re-raised: the stream it
// left cannot be drawn on.
func TestFramePanicIsLoggedAndReraised(t *testing.T) {
	reg := app.NewRegistry()
	a := &panickyApp{manifest: mkManifest("test.panicky"), framePanics: true}
	require.NoError(t, reg.Register(a))
	var logged strings.Builder
	h := NewInst(reg, zerolog.New(&logged))
	w := openPanicky(t, h)
	assert.PanicsWithValue(t, "frame boom", func() { _ = frameReraising(w, h.logger) })
	assert.Contains(t, logged.String(), "Frame panicked")
	assert.Contains(t, logged.String(), "test.panicky")
}

// One app's Unmount panic does not stop the reap of the other windows.
func TestReapAllGoesOnPastAPanickingUnmount(t *testing.T) {
	reg := app.NewRegistry()
	bad := &panickyApp{manifest: mkManifest("test.panicky"), unmountPanics: true}
	good := &panickyApp{manifest: mkManifest("test.good")}
	require.NoError(t, reg.Register(bad))
	require.NoError(t, reg.Register(good))
	h := NewInst(reg, zerolog.Nop())
	_, err := h.Open("test.panicky")
	require.NoError(t, err)
	_, err = h.Open("test.good")
	require.NoError(t, err)
	h.mu.Lock()
	for _, w := range h.windows {
		w.mount.mounted = true
		w.mount.mountCtx = w.mountCtx
	}
	h.mu.Unlock()
	assert.NotPanics(t, func() { h.ReapAll("shutdown") })
	assert.Equal(t, 1, bad.unmountCalls)
	assert.Equal(t, 1, good.unmountCalls)
	assert.Equal(t, 0, h.Len())
}
