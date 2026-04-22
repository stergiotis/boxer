//go:build llm_generated_opus47

package h3

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type stringRecord struct {
	Name   string `json:"name"`
	Cell   uint64 `json:"cell"`
	Res    uint8  `json:"res"`
	String string `json:"string"`
}

func TestCellsToStrings_Golden(t *testing.T) {
	recs := readNDJSON[stringRecord](t, "golden_strings.ndjson")
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
	buf, offsets, status, err := h.CellsToStringsE(ctx, cells, nil, nil, nil)
	require.NoError(t, err)
	require.Equal(t, int32(0), offsets[0])
	require.Equal(t, int32(len(buf)), offsets[len(cells)])
	for i, r := range recs {
		require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
		got := string(buf[offsets[i]:offsets[i+1]])
		require.Equal(t, r.String, got, "name=%s", r.Name)
	}
}

func TestStringsToCells_RoundTrip(t *testing.T) {
	recs := readNDJSON[stringRecord](t, "golden_strings.ndjson")
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

	buf, offsets, _, err := h.CellsToStringsE(ctx, cells, nil, nil, nil)
	require.NoError(t, err)

	decoded, status, err := h.StringsToCellsE(ctx, buf, offsets, nil, nil)
	require.NoError(t, err)
	for i, r := range recs {
		require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
		require.Equal(t, r.Cell, decoded[i], "name=%s", r.Name)
	}
}

func TestStringsToCells_InvalidInputFlagsStatus(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	buf := []byte("garbage!")
	offsets := []int32{0, int32(len(buf))}
	_, status, err := h.StringsToCellsE(ctx, buf, offsets, nil, nil)
	require.NoError(t, err)
	require.Equal(t, StatusInvalidString, status[0])
}
