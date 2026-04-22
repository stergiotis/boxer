//go:build llm_generated_opus47

package h3

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type gridDiskRecord struct {
	Name       string   `json:"name"`
	Cell       uint64   `json:"cell"`
	K          uint32   `json:"k"`
	Neighbours []uint64 `json:"neighbours"`
}

func TestGridDisks_Golden(t *testing.T) {
	recs := readNDJSON[gridDiskRecord](t, "golden_grid_disk.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	byK := make(map[uint32][]gridDiskRecord, 4)
	for _, r := range recs {
		byK[r.K] = append(byK[r.K], r)
	}
	for k, group := range byK {
		cells := make([]uint64, len(group))
		for i, r := range group {
			cells[i] = r.Cell
		}
		out, offsets, status, err := h.GridDisksE(ctx, uint8(k), cells, nil, nil, nil)
		require.NoError(t, err)
		requireCSRInvariants(t, offsets, len(cells), len(out))
		for i, r := range group {
			require.Equal(t, StatusOk, status[i], "name=%s k=%d", r.Name, k)
			got := out[offsets[i]:offsets[i+1]]
			gotSorted := append([]uint64(nil), got...)
			wantSorted := append([]uint64(nil), r.Neighbours...)
			sort.Slice(gotSorted, func(a, b int) bool { return gotSorted[a] < gotSorted[b] })
			sort.Slice(wantSorted, func(a, b int) bool { return wantSorted[a] < wantSorted[b] })
			require.Equal(t, wantSorted, gotSorted, "name=%s k=%d", r.Name, k)
		}
	}
}

func TestGridDisks_KZeroReturnsCellItself(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0.0, 37.7749, 48.8566}
	lngs := []float64{0.0, -122.4194, 2.3522}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR5, lats, lngs, nil, nil)
	require.NoError(t, err)

	out, offsets, status, err := h.GridDisksE(ctx, 0, cells, nil, nil, nil)
	require.NoError(t, err)
	requireCSRInvariants(t, offsets, len(cells), len(out))
	for i, c := range cells {
		require.Equal(t, StatusOk, status[i])
		row := out[offsets[i]:offsets[i+1]]
		require.Equal(t, []uint64{c}, row)
	}
}

func TestGridDisks_GrowProtocol(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR5, lats, lngs, nil, nil)
	require.NoError(t, err)

	// Undersized dst forces a retry.
	undersized := make([]uint64, 0, 1)
	out, offsets, _, err := h.GridDisksE(ctx, 2, cells, undersized, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int32(len(out)), offsets[1])
	require.GreaterOrEqual(t, len(out), 7)
}
