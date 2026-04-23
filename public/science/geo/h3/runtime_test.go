//go:build llm_generated_opus47

package h3

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRuntime_NewAndClose(t *testing.T) {
	rt := newTestRuntime(t, 2)
	require.NotNil(t, rt)
	// Close is called via t.Cleanup; calling it explicitly here exercises
	// idempotency.
	require.NoError(t, rt.Close())
	require.NoError(t, rt.Close())
}

func TestRuntime_AcquireRelease(t *testing.T) {
	rt := newTestRuntime(t, 2)
	ctx := context.Background()

	h1, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	require.NotNil(t, h1)
	h2, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	require.NotNil(t, h2)

	h1.Release()
	h1.Release() // double release is a no-op

	h3, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	require.NotNil(t, h3)
	h2.Release()
	h3.Release()
}

func TestRuntime_AcquireAfterCloseFails(t *testing.T) {
	rt := newTestRuntime(t, 1)
	require.NoError(t, rt.Close())
	_, err := rt.AcquireE(context.Background())
	require.ErrorIs(t, err, ErrClosed)
}

func TestRuntime_AcquireRespectsContext(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()

	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Pool is now empty. A cancelled context must unblock the acquire.
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	_, err = rt.AcquireE(cctx)
	require.Error(t, err)
}

func TestHandle_UseAfterReleaseFails(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	h.Release()

	// Any bulk call on a released handle must surface ErrHandleReleased
	// rather than corrupting guest memory or trapping. ensureScratchE is
	// the single gate.
	_, _, err = h.LatLngsToCellsE(ctx, ResolutionR9,
		[]float64{37.7749}, []float64{-122.4194}, nil, nil)
	require.ErrorIs(t, err, ErrHandleReleased)
}

func TestRuntime_ConcurrentAcquire(t *testing.T) {
	const poolSize = 4
	const workers = 16
	const opsPerWorker = 50

	rt := newTestRuntime(t, poolSize)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < opsPerWorker; j++ {
				h, err := rt.AcquireE(ctx)
				if err != nil {
					t.Errorf("AcquireE: %v", err)
					return
				}
				h.Release()
			}
		}()
	}
	wg.Wait()
}
