//go:build llm_generated_opus47

package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// The scalar wrappers are thin shims over the bulk API; the bulk tests
// exercise correctness under every interesting input shape. These tests
// verify that the shim behaviour matches the "naturally ergonomic"
// expectation for one-element inputs:
//   - value-returns (not 1-element slices)
//   - status returned as its own value, not a 1-element slice
//   - GridDisk returns a flat slice, not CSR offsets+values
//   - PolygonToCellsSimple synthesises ringOffsets = {0, len(verts)}.
//
// We do not re-verify H3 correctness here — that's the bulk suite's job.

func TestLatLngToCellE_ReturnsScalar(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	cell, status, err := h.LatLngToCellE(ctx, ResolutionR9, 37.7749, -122.4194)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	require.NotZero(t, cell, "valid lat/lng should produce a non-zero cell index")

	// Cross-check against the bulk form.
	bulk, bulkStatus, err := h.LatLngsToCellsE(ctx, ResolutionR9,
		[]float64{37.7749}, []float64{-122.4194}, nil, nil)
	require.NoError(t, err)
	require.Len(t, bulk, 1)
	require.Equal(t, bulk[0], cell, "scalar result must match bulk[0]")
	require.Equal(t, bulkStatus[0], status)
}

func TestCellToLatLngE_RoundTrip(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	cell, status, err := h.LatLngToCellE(ctx, ResolutionR9, 37.7749, -122.4194)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)

	lat, lng, s, err := h.CellToLatLngE(ctx, cell)
	require.NoError(t, err)
	require.Equal(t, StatusOk, s)
	// Res 9 cell centroid is within ~200m of an interior point; loose tolerance.
	require.InDelta(t, 37.7749, lat, 0.005)
	require.InDelta(t, -122.4194, lng, 0.005)
}

func TestGridDiskE_FlatSliceForSingleInput(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	center, _, err := h.LatLngToCellE(ctx, ResolutionR7, 51.0992, 17.0366)
	require.NoError(t, err)

	// k=0 returns the cell itself.
	only, status, err := h.GridDiskE(ctx, 0, center)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	require.Equal(t, []uint64{center}, only)

	// k=2 returns the cell plus its 2-ring. For non-pentagon cells:
	// 1 + 6 + 12 = 19 cells.
	ring, status, err := h.GridDiskE(ctx, 2, center)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status)
	require.GreaterOrEqual(t, len(ring), 15, "k=2 ring is at least 15 cells (pentagon: 16, regular: 19)")
	require.LessOrEqual(t, len(ring), 19)
	require.Contains(t, ring, center, "k-ring must contain the input cell")
}

func TestPolygonToCellsSimpleE_OneRingNoHoles(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Small closed rectangle around Wrocław.
	lats := []float64{51.0, 51.2, 51.2, 51.0, 51.0}
	lngs := []float64{17.0, 17.0, 17.2, 17.2, 17.0}

	cells, err := h.PolygonToCellsSimpleE(ctx,
		ResolutionR7, ContainmentIntersectsBoundary, lats, lngs)
	require.NoError(t, err)
	require.NotEmpty(t, cells)

	// Cross-check: calling the bulk form with the synthesised ring
	// offsets must produce the same set.
	bulk, err := h.PolygonToCellsE(ctx,
		ResolutionR7, ContainmentIntersectsBoundary,
		lats, lngs, []int32{0, int32(len(lats))}, nil)
	require.NoError(t, err)
	require.ElementsMatch(t, bulk, cells)
}
