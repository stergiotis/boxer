//go:build llm_generated_opus47

package h3

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConcurrentMixedBulkCalls(t *testing.T) {
	const poolSize = 4
	const workers = 8
	const opsPerWorker = 40

	rt := newTestRuntime(t, poolSize)
	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(workers)
	errCh := make(chan error, workers)

	for i := 0; i < workers; i++ {
		go func(id int) {
			defer wg.Done()
			lats := []float64{float64(id), float64(-id), 37.7749, 48.8566}
			lngs := []float64{float64(id), float64(id * 2), -122.4194, 2.3522}
			for j := 0; j < opsPerWorker; j++ {
				h, err := rt.AcquireE(ctx)
				if err != nil {
					errCh <- err
					return
				}
				cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR7, lats, lngs, nil, nil)
				if err != nil {
					h.Release()
					errCh <- err
					return
				}
				_, _, _, err = h.CellsToLatLngsE(ctx, cells, nil, nil, nil)
				if err != nil {
					h.Release()
					errCh <- err
					return
				}
				_, _, _, err = h.GridDisksE(ctx, 1, cells, nil, nil, nil)
				if err != nil {
					h.Release()
					errCh <- err
					return
				}
				h.Release()
			}
		}(i)
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}
