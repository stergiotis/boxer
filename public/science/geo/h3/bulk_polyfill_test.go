//go:build llm_generated_opus47

package h3

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type polyfillRecord struct {
	Name        string    `json:"name"`
	Res         uint8     `json:"res"`
	Mode        uint8     `json:"mode"`
	VertsLat    []float64 `json:"verts_lat"`
	VertsLng    []float64 `json:"verts_lng"`
	RingOffsets []int32   `json:"ring_offsets"`
	Cells       []uint64  `json:"cells"`
}

func TestPolygonToCells_Golden(t *testing.T) {
	recs := readNDJSON[polyfillRecord](t, "golden_polyfill.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	for _, r := range recs {
		cells, err := h.PolygonToCellsE(ctx, ResolutionE(r.Res), ContainmentModeE(r.Mode),
			r.VertsLat, r.VertsLng, r.RingOffsets, nil)
		require.NoError(t, err, "name=%s res=%d mode=%d", r.Name, r.Res, r.Mode)

		got := append([]uint64(nil), cells...)
		want := append([]uint64(nil), r.Cells...)
		sort.Slice(got, func(a, b int) bool { return got[a] < got[b] })
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		require.Equal(t, want, got, "name=%s res=%d mode=%d", r.Name, r.Res, r.Mode)
	}
}

func TestPolygonToCells_EmptyRingsRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	_, err = h.PolygonToCellsE(ctx, ResolutionR5, ContainmentContainsCentroid,
		nil, nil, []int32{0}, nil)
	require.Error(t, err)
}

func TestPolygonToCells_BadMode(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0, 0, 1, 1, 0}
	lngs := []float64{0, 1, 1, 0, 0}
	_, err = h.PolygonToCellsE(ctx, ResolutionR5, ContainmentModeE(99),
		lats, lngs, []int32{0, 5}, nil)
	require.ErrorIs(t, err, ErrBadContainmentMode)
}

func TestPolygonToCells_GrowProtocol(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0, 0, 1, 1, 0}
	lngs := []float64{0, 1, 1, 0, 0}
	// Passing an undersized dst forces the grow protocol.
	undersized := make([]uint64, 0, 1)
	cells, err := h.PolygonToCellsE(ctx, ResolutionR7, ContainmentCovers,
		lats, lngs, []int32{0, 5}, undersized)
	require.NoError(t, err)
	require.NotEmpty(t, cells)
}

func TestPolygonToCells_CentroidsInside(t *testing.T) {
	// Sampled-check the Centroid mode invariant: every returned cell's
	// center is inside the polygon. We use a convex polygon (unit square)
	// so point-in-polygon is a trivial axis-aligned check.
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0, 0, 1, 1, 0}
	lngs := []float64{0, 1, 1, 0, 0}
	cells, err := h.PolygonToCellsE(ctx, ResolutionR6,
		ContainmentContainsCentroid, lats, lngs, []int32{0, 5}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, cells)

	centreLats, centreLngs, _, err := h.CellsToLatLngsE(ctx, cells, nil, nil, nil)
	require.NoError(t, err)
	for i := range cells {
		require.GreaterOrEqual(t, centreLats[i], 0.0, "cell %d lat=%g", i, centreLats[i])
		require.LessOrEqual(t, centreLats[i], 1.0, "cell %d lat=%g", i, centreLats[i])
		require.GreaterOrEqual(t, centreLngs[i], 0.0, "cell %d lng=%g", i, centreLngs[i])
		require.LessOrEqual(t, centreLngs[i], 1.0, "cell %d lng=%g", i, centreLngs[i])
	}
}
