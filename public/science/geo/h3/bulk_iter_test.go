//go:build llm_generated_opus47

package h3

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"
)

// latLngSlicesAsSeq returns an iter.Seq2[int, LatLng] yielded in ascending
// index order from parallel lat/lng slices — the canonical happy-path shape.
func latLngSlicesAsSeq(lats, lngs []float64) iter.Seq2[int, LatLng] {
	return func(yield func(int, LatLng) bool) {
		for i := range lats {
			if !yield(i, LatLng{LatDeg: lats[i], LngDeg: lngs[i]}) {
				return
			}
		}
	}
}

func TestLatLngsIterToCells_MatchesBatch(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0, 37.7749, 48.8566, -33.8688}
	lngs := []float64{0, -122.4194, 2.3522, 151.2093}

	batchCells, batchStatus, err := h.LatLngsToCellsE(ctx, ResolutionR9, lats, lngs, nil, nil)
	require.NoError(t, err)

	iterCells, iterStatus, err := h.LatLngsIterToCellsE(ctx, ResolutionR9,
		len(lats), latLngSlicesAsSeq(lats, lngs), nil, nil)
	require.NoError(t, err)

	require.Equal(t, batchCells, iterCells)
	require.Equal(t, batchStatus, iterStatus)
}

func TestLatLngsIterToCells_OutOfOrderIndices(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Feed (3,p3), (0,p0), (2,p2), (1,p1) — non-contiguous order should
	// still cover all indices and produce the same result as the in-order
	// variant.
	lats := []float64{0, 37.7749, 48.8566, -33.8688}
	lngs := []float64{0, -122.4194, 2.3522, 151.2093}
	order := []int{3, 0, 2, 1}

	shuffled := func(yield func(int, LatLng) bool) {
		for _, i := range order {
			if !yield(i, LatLng{LatDeg: lats[i], LngDeg: lngs[i]}) {
				return
			}
		}
	}

	want, _, err := h.LatLngsToCellsE(ctx, ResolutionR7, lats, lngs, nil, nil)
	require.NoError(t, err)
	got, _, err := h.LatLngsIterToCellsE(ctx, ResolutionR7, len(lats), shuffled, nil, nil)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestLatLngsIterToCells_DuplicateIndexRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	dup := func(yield func(int, LatLng) bool) {
		_ = yield(0, LatLng{})
		_ = yield(0, LatLng{}) // duplicate
	}
	_, _, err = h.LatLngsIterToCellsE(ctx, ResolutionR5, 2, dup, nil, nil)
	require.Error(t, err)
}

func TestLatLngsIterToCells_OutOfRangeIndex(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	oor := func(yield func(int, LatLng) bool) {
		_ = yield(5, LatLng{})
	}
	_, _, err = h.LatLngsIterToCellsE(ctx, ResolutionR5, 3, oor, nil, nil)
	require.Error(t, err)
}

func TestLatLngsIterToCells_IncompleteIterRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	short := func(yield func(int, LatLng) bool) {
		_ = yield(0, LatLng{})
		// never yields index 1
	}
	_, _, err = h.LatLngsIterToCellsE(ctx, ResolutionR5, 2, short, nil, nil)
	require.Error(t, err)
}

func TestLatLngsIterToCells_Empty(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	empty := func(yield func(int, LatLng) bool) {}
	cells, status, err := h.LatLngsIterToCellsE(ctx, ResolutionR5, 0, empty, nil, nil)
	require.NoError(t, err)
	require.Empty(t, cells)
	require.Empty(t, status)
}
