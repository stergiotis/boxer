//go:build llm_generated_opus47

package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type parentRecord struct {
	Name   string `json:"name"`
	Cell   uint64 `json:"cell"`
	Res    uint8  `json:"res"`
	Parent uint64 `json:"parent"`
}

type childrenRecord struct {
	Name     string   `json:"name"`
	Cell     uint64   `json:"cell"`
	ChildRes uint8    `json:"child_res"`
	Children []uint64 `json:"children"`
}

func TestCellsToParents_Golden(t *testing.T) {
	recs := readNDJSON[parentRecord](t, "golden_parents.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	byRes := make(map[uint8][]parentRecord, 16)
	for _, r := range recs {
		byRes[r.Res] = append(byRes[r.Res], r)
	}
	for res, group := range byRes {
		cells := make([]uint64, len(group))
		for i, r := range group {
			cells[i] = r.Cell
		}
		parents, status, err := h.CellsToParentsE(ctx, ResolutionE(res), cells, nil, nil)
		require.NoError(t, err)
		for i, r := range group {
			require.Equal(t, StatusOk, status[i], "name=%s res=%d", r.Name, res)
			require.Equal(t, r.Parent, parents[i], "name=%s res=%d", r.Name, res)
		}
	}
}

func TestCellsToChildren_Golden(t *testing.T) {
	recs := readNDJSON[childrenRecord](t, "golden_children.ndjson")
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
	children, offsets, status, err := h.CellsToChildrenE(ctx, ResolutionR4, cells, nil, nil, nil)
	require.NoError(t, err)
	requireCSRInvariants(t, offsets, len(cells), len(children))
	for i, r := range recs {
		require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
		got := children[offsets[i]:offsets[i+1]]
		require.Equal(t, r.Children, got, "name=%s", r.Name)
	}
}

func TestCellsToChildren_GrowProtocol(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Use a resolution-0 cell so the child count is large.
	lats := []float64{37.7749}
	lngs := []float64{-122.4194}
	cells, _, err := h.LatLngsToCellsE(ctx, ResolutionR0, lats, lngs, nil, nil)
	require.NoError(t, err)

	// Pass an obviously-undersized dst so the grow protocol fires.
	undersized := make([]uint64, 0, 1)
	children, offsets, status, err := h.CellsToChildrenE(ctx, ResolutionR3, cells, undersized, nil, nil)
	require.NoError(t, err)
	require.Equal(t, StatusOk, status[0])
	require.GreaterOrEqual(t, len(children), 1)
	require.Equal(t, int32(len(children)), offsets[1])
	require.Equal(t, int32(0), offsets[0])
}

func TestParentOfChildIsIdentity(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	lats := []float64{0.0, 37.7749, 48.8566, -33.8688}
	lngs := []float64{0.0, -122.4194, 2.3522, 151.2093}

	parents, _, err := h.LatLngsToCellsE(ctx, ResolutionR3, lats, lngs, nil, nil)
	require.NoError(t, err)

	children, offsets, _, err := h.CellsToChildrenE(ctx, ResolutionR5, parents, nil, nil, nil)
	require.NoError(t, err)
	requireCSRInvariants(t, offsets, len(parents), len(children))

	// For each parent, every child's parent-at-r3 must equal the original.
	for i := range parents {
		row := children[offsets[i]:offsets[i+1]]
		require.NotEmpty(t, row, "parent idx=%d", i)
		computedParents, _, err := h.CellsToParentsE(ctx, ResolutionR3, row, nil, nil)
		require.NoError(t, err)
		for _, p := range computedParents {
			require.Equal(t, parents[i], p)
		}
	}
}

func requireCSRInvariants(t *testing.T, offsets []int32, n int, total int) {
	t.Helper()
	require.Len(t, offsets, n+1)
	require.Equal(t, int32(0), offsets[0])
	require.Equal(t, int32(total), offsets[n])
	for i := 0; i < n; i++ {
		require.LessOrEqual(t, offsets[i], offsets[i+1], "offsets not monotone at %d", i)
	}
}
