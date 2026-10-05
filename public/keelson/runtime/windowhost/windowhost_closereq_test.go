package windowhost

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// Close may run off the render thread while the render loop checks
// the window's closeReq; the check must read it under inst.mu. Run
// with -race.
func TestCloseRequested_ConcurrentWithClose(t *testing.T) {
	reg := app.NewRegistry()
	require.NoError(t, reg.Register(&counterApp{manifest: mkManifest("test.closereq")}))
	h := NewInst(reg, zerolog.Nop())
	k, err := h.Open("test.closereq")
	require.NoError(t, err)
	h.mu.Lock()
	w := h.windows[0]
	h.mu.Unlock()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		h.Close(k, "test")
	}()
	for range 100 {
		_ = h.closeRequested(w)
	}
	wg.Wait()
	require.True(t, h.closeRequested(w))
}
