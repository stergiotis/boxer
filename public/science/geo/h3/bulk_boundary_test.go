//go:build llm_generated_opus47

package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type boundaryRecord struct {
	Name        string    `json:"name"`
	Cell        uint64    `json:"cell"`
	Res         uint8     `json:"res"`
	VertexCount int       `json:"vertex_count"`
	Lats        []float64 `json:"lats"`
	Lngs        []float64 `json:"lngs"`
}

func TestCellsToBoundaries_Golden(t *testing.T) {
	recs := readNDJSON[boundaryRecord](t, "golden_boundaries.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	cells := make([]uint64, len(recs))
	for i, r := range recs {
		cells[i] = r.Cell
	}
	lats, lngs, offsets, status, err := h.CellsToBoundariesE(ctx, cells, nil, nil, nil, nil)
	require.NoError(t, err)
	requireCSRInvariants(t, offsets, len(cells), len(lats))
	require.Equal(t, len(lats), len(lngs))

	for i, r := range recs {
		require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
		gotLats := lats[offsets[i]:offsets[i+1]]
		gotLngs := lngs[offsets[i]:offsets[i+1]]
		require.Equal(t, r.VertexCount, len(gotLats), "name=%s", r.Name)
		for j, wantLat := range r.Lats {
			require.InDelta(t, wantLat, gotLats[j], 1e-9,
				"name=%s vertex=%d", r.Name, j)
			require.InDelta(t, r.Lngs[j], gotLngs[j], 1e-9,
				"name=%s vertex=%d", r.Name, j)
		}
	}
}

func TestCellsToBoundaries_VertexCountBounds(t *testing.T) {
	// Every valid cell produces 5 <= vertex_count <= 10 vertices per h3o.
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0.0, 37.7749, 48.8566, -33.8688, 89.9, -89.9}
	lngs := []float64{0.0, -122.4194, 2.3522, 151.2093, 0.0, 0.0}
	for _, res := range []ResolutionE{ResolutionR0, ResolutionR5, ResolutionR10} {
		cells, _, err := h.LatLngsToCellsE(ctx, res, lats, lngs, nil, nil)
		require.NoError(t, err)
		_, _, offsets, status, err := h.CellsToBoundariesE(ctx, cells, nil, nil, nil, nil)
		require.NoError(t, err)
		for i, c := range cells {
			require.Equal(t, StatusOk, status[i], "cell=%d res=%d", c, res)
			verts := int(offsets[i+1] - offsets[i])
			require.GreaterOrEqual(t, verts, 5, "cell=%d res=%d", c, res)
			require.LessOrEqual(t, verts, 10, "cell=%d res=%d", c, res)
		}
	}
}

func TestCellsToBoundaries_InvalidCell(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	cells := []uint64{0, 0xdeadbeef_cafebabe}
	_, _, offsets, status, err := h.CellsToBoundariesE(ctx, cells, nil, nil, nil, nil)
	require.NoError(t, err)
	for i := range cells {
		require.Equal(t, StatusInvalidCell, status[i])
		require.Equal(t, offsets[i], offsets[i+1], "invalid cell %d has non-empty row", i)
	}
}

func TestCellsToBoundaries_GrowProtocol(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{37.7749, 48.8566}
	lngs := []float64{-122.4194, 2.3522}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR5, lats, lngs, nil, nil)
	require.NoError(t, err)
	undersizedLat := make([]float64, 0, 1)
	undersizedLng := make([]float64, 0, 1)
	outLats, outLngs, offsets, _, err := h.CellsToBoundariesE(ctx, cells,
		undersizedLat, undersizedLng, nil, nil)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(outLats), 10)
	require.Equal(t, len(outLats), len(outLngs))
	require.Equal(t, int32(len(outLats)), offsets[len(cells)])
}
