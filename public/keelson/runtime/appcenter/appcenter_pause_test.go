package appcenter

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/runtime/app"
)

// countingReader counts reads, from the poller's goroutine.
type countingReader struct{ n atomic.Int64 }

func (inst *countingReader) read(context.Context, string, string, string, any) error {
	inst.n.Add(1)
	return nil
}

// While the window is paused the poller reads nothing; the resume reads at
// once (ADR-0354 §SD2).
func TestThePollerStopsWhilePaused(t *testing.T) {
	r := &countingReader{}
	inst := newApp()
	inst.reader = r
	ctx := app.NewStaticMountContext(AppId, zerolog.Nop(), nil, nil, nil)
	require.NoError(t, inst.Mount(ctx))
	t.Cleanup(func() { _ = inst.Unmount(ctx) })
	require.Eventually(t, func() bool { return r.n.Load() > 0 }, 5*time.Second, 10*time.Millisecond, "the first read")

	inst.SetPaused(true)
	time.Sleep(50 * time.Millisecond) // a read in flight ends
	before := r.n.Load()
	inst.markDirty()
	require.Never(t, func() bool { return r.n.Load() != before }, 300*time.Millisecond, 20*time.Millisecond, "no read while paused")

	inst.SetPaused(false)
	require.Eventually(t, func() bool { return r.n.Load() > before }, 5*time.Second, 10*time.Millisecond, "the resume reads")
}
