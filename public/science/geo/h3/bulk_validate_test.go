//go:build llm_generated_opus47

package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type validateRecord struct {
	Name  string `json:"name"`
	Cell  uint64 `json:"cell"`
	Res   uint8  `json:"res"`
	Valid bool   `json:"valid"`
}

func TestAreValidCells_Golden(t *testing.T) {
	recs := readNDJSON[validateRecord](t, "golden_validate.ndjson")
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
	valid, err := h.AreValidCellsE(ctx, cells, nil)
	require.NoError(t, err)
	for i, r := range recs {
		require.Equal(t, r.Valid, valid[i], "name=%s", r.Name)
	}
}

func TestGetResolutions_Golden(t *testing.T) {
	recs := readNDJSON[validateRecord](t, "golden_validate.ndjson")
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
	res, status, err := h.GetResolutionsE(ctx, cells, nil, nil)
	require.NoError(t, err)
	for i, r := range recs {
		if r.Valid {
			require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
			require.Equal(t, ResolutionE(r.Res), res[i], "name=%s", r.Name)
		} else {
			require.Equal(t, StatusInvalidCell, status[i], "name=%s", r.Name)
		}
	}
}
