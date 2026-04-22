//go:build llm_generated_opus47

package h3

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
)

type compactRecord struct {
	Name      string   `json:"name"`
	Cells     []uint64 `json:"cells"`
	Compacted []uint64 `json:"compacted"`
}

type uncompactRecord struct {
	Name     string   `json:"name"`
	Cells    []uint64 `json:"cells"`
	Res      uint8    `json:"res"`
	Expanded []uint64 `json:"expanded"`
}

func TestCompactCells_Golden(t *testing.T) {
	recs := readNDJSON[compactRecord](t, "golden_compact.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	for _, r := range recs {
		compacted, err := h.CompactCellsE(ctx, r.Cells, nil)
		require.NoError(t, err, "name=%s", r.Name)

		got := append([]uint64(nil), compacted...)
		want := append([]uint64(nil), r.Compacted...)
		sort.Slice(got, func(a, b int) bool { return got[a] < got[b] })
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		require.Equal(t, want, got, "name=%s", r.Name)
	}
}

func TestCompactCells_MixedResolutionRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Build a mixed-resolution set: two cells at res 3 and res 4.
	lats := []float64{37.7749, 37.7749}
	lngs := []float64{-122.4194, -122.4194}
	r3, _, err := h.LatLngsToCellsE(ctx, ResolutionR3, lats[:1], lngs[:1], nil, nil)
	require.NoError(t, err)
	r4, _, err := h.LatLngsToCellsE(ctx, ResolutionR4, lats[:1], lngs[:1], nil, nil)
	require.NoError(t, err)
	mixed := append(append([]uint64(nil), r3...), r4...)

	_, err = h.CompactCellsE(ctx, mixed, nil)
	require.ErrorIs(t, err, ErrCompactMixedResolution)
}

func TestCompactCells_DuplicateRejected(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR5, lats, lngs, nil, nil)
	require.NoError(t, err)
	dup := []uint64{cells[0], cells[0]}

	_, err = h.CompactCellsE(ctx, dup, nil)
	require.ErrorIs(t, err, ErrCompactDuplicateInput)
}

func TestUncompactCells_Golden(t *testing.T) {
	recs := readNDJSON[uncompactRecord](t, "golden_uncompact.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	for _, r := range recs {
		expanded, status, err := h.UncompactCellsE(ctx, ResolutionE(r.Res),
			r.Cells, nil, nil)
		require.NoError(t, err, "name=%s", r.Name)
		for i, s := range status {
			require.Equal(t, StatusOk, s, "name=%s idx=%d", r.Name, i)
		}
		got := append([]uint64(nil), expanded...)
		want := append([]uint64(nil), r.Expanded...)
		sort.Slice(got, func(a, b int) bool { return got[a] < got[b] })
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		require.Equal(t, want, got, "name=%s", r.Name)
	}
}

func TestCompactUncompact_RoundTrip(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Take a res-3 cell's res-5 children (a compactable set), compact, then
	// uncompact at res 5 — must recover the original set.
	lats := []float64{0.0, 37.7749, 48.8566}
	lngs := []float64{0.0, -122.4194, 2.3522}
	for i := range lats {
		baseCells, _, err := h.LatLngsToCellsE(ctx, ResolutionR3,
			lats[i:i+1], lngs[i:i+1], nil, nil)
		require.NoError(t, err)
		original, offsets, _, err := h.CellsToChildrenE(ctx, ResolutionR5,
			baseCells, nil, nil, nil)
		require.NoError(t, err)
		children := original[offsets[0]:offsets[1]]

		compacted, err := h.CompactCellsE(ctx, children, nil)
		require.NoError(t, err)
		require.LessOrEqual(t, len(compacted), len(children))

		expanded, status, err := h.UncompactCellsE(ctx, ResolutionR5, compacted, nil, nil)
		require.NoError(t, err)
		for _, s := range status {
			require.Equal(t, StatusOk, s)
		}

		got := append([]uint64(nil), expanded...)
		want := append([]uint64(nil), children...)
		sort.Slice(got, func(a, b int) bool { return got[a] < got[b] })
		sort.Slice(want, func(a, b int) bool { return want[a] < want[b] })
		require.Equal(t, want, got)
	}
}

func TestUncompactCells_InvalidResolutionFlagsStatus(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// A res-7 cell uncompacted to res 3 — finer-than-target — is invalid.
	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	fine, _, err := h.LatLngsToCellsE(ctx, ResolutionR7, lats, lngs, nil, nil)
	require.NoError(t, err)
	_, status, err := h.UncompactCellsE(ctx, ResolutionR3, fine, nil, nil)
	require.NoError(t, err)
	require.Equal(t, StatusInvalidResolution, status[0])
}

func TestUncompactCells_GrowProtocol(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR3, lats, lngs, nil, nil)
	require.NoError(t, err)

	undersized := make([]uint64, 0, 1)
	expanded, _, err := h.UncompactCellsE(ctx, ResolutionR5, cells, undersized, nil)
	require.NoError(t, err)
	require.Equal(t, 49, len(expanded)) // 7^(5-3) children
}
