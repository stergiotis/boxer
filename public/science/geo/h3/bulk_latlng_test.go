//go:build llm_generated_opus47

package h3

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type latLngToCellRecord struct {
	Name string  `json:"name"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
	Res  uint8   `json:"res"`
	Cell uint64  `json:"cell"`
}

type cellToLatLngRecord struct {
	Name      string  `json:"name"`
	Cell      uint64  `json:"cell"`
	Res       uint8   `json:"res"`
	CenterLat float64 `json:"center_lat"`
	CenterLng float64 `json:"center_lng"`
}

func TestLatLngsToCells_Golden(t *testing.T) {
	recs := readNDJSON[latLngToCellRecord](t, "golden_latlng_to_cell.ndjson")
	require.NotEmpty(t, recs)

	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// Group by resolution so each batch is homogeneous.
	byRes := make(map[uint8][]latLngToCellRecord, 16)
	for _, r := range recs {
		byRes[r.Res] = append(byRes[r.Res], r)
	}

	for res, group := range byRes {
		lats := make([]float64, len(group))
		lngs := make([]float64, len(group))
		for i, r := range group {
			lats[i] = r.Lat
			lngs[i] = r.Lng
		}

		cells, status, err := h.LatLngsToCellsE(ctx, ResolutionE(res), lats, lngs, nil, nil)
		require.NoError(t, err, "res=%d", res)
		require.Len(t, cells, len(group))
		require.Len(t, status, len(group))

		for i, r := range group {
			require.Equal(t, StatusOk, status[i], "res=%d name=%s", res, r.Name)
			require.Equal(t, r.Cell, cells[i], "res=%d name=%s", res, r.Name)
		}
	}
}

func TestCellsToLatLngs_Golden(t *testing.T) {
	recs := readNDJSON[cellToLatLngRecord](t, "golden_cell_to_latlng.ndjson")
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

	lats, lngs, status, err := h.CellsToLatLngsE(ctx, cells, nil, nil, nil)
	require.NoError(t, err)
	require.Len(t, lats, len(recs))
	require.Len(t, lngs, len(recs))
	require.Len(t, status, len(recs))

	for i, r := range recs {
		require.Equal(t, StatusOk, status[i], "name=%s", r.Name)
		require.InDelta(t, r.CenterLat, lats[i], 1e-9, "name=%s", r.Name)
		require.InDelta(t, r.CenterLng, lngs[i], 1e-9, "name=%s", r.Name)
	}
}

func TestLatLngsToCells_RoundTrip(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// A mixed grid of coordinates across latitude bands.
	var lats, lngs []float64
	for lat := -80.0; lat <= 80.0; lat += 10.0 {
		for lng := -170.0; lng <= 170.0; lng += 10.0 {
			lats = append(lats, lat)
			lngs = append(lngs, lng)
		}
	}

	// Round-trip invariant: the cell whose center we recover must round-trip
	// back to the same cell id. This is stronger than a degree-based
	// tolerance check and doesn't depend on the shape of the Earth.
	for _, res := range []ResolutionE{ResolutionR0, ResolutionR3, ResolutionR6, ResolutionR9, ResolutionR12} {
		cells, status, err := h.LatLngsToCellsE(ctx, res, lats, lngs, nil, nil)
		require.NoError(t, err)
		for _, s := range status {
			require.Equal(t, StatusOk, s)
		}
		latsBack, lngsBack, status2, err := h.CellsToLatLngsE(ctx, cells, nil, nil, nil)
		require.NoError(t, err)
		for _, s := range status2 {
			require.Equal(t, StatusOk, s)
		}
		cells2, status3, err := h.LatLngsToCellsE(ctx, res, latsBack, lngsBack, nil, nil)
		require.NoError(t, err)
		for i := range cells {
			require.Equal(t, StatusOk, status3[i])
			require.Equal(t, cells[i], cells2[i],
				"res=%d idx=%d lat=%g lng=%g", res, i, lats[i], lngs[i])
		}
	}
}

func TestLatLngsToCells_EmptyInput(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	cells, status, err := h.LatLngsToCellsE(ctx, ResolutionR9, nil, nil, nil, nil)
	require.NoError(t, err)
	require.Empty(t, cells)
	require.Empty(t, status)
}

func TestLatLngsToCells_LengthMismatch(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	_, _, err = h.LatLngsToCellsE(ctx, ResolutionR9,
		[]float64{1, 2, 3}, []float64{1, 2}, nil, nil)
	require.Error(t, err)
}

func TestLatLngsToCells_InvalidInputFlagsStatus(t *testing.T) {
	rt := newTestRuntime(t, 1)
	ctx := context.Background()
	h, err := rt.AcquireE(ctx)
	require.NoError(t, err)
	defer h.Release()

	// NaN is the canonical invalid latitude/longitude — h3o's LatLng::new
	// accepts out-of-range-but-finite values without error, so we use a
	// non-finite value to exercise the invalid-latlng path deterministically.
	lats := []float64{math.NaN(), 37.7749}
	lngs := []float64{0.0, -122.4194}
	_, status, err := h.LatLngsToCellsE(ctx, ResolutionR9, lats, lngs, nil, nil)
	require.NoError(t, err)
	require.Equal(t, StatusInvalidLatLng, status[0])
	require.Equal(t, StatusOk, status[1])
}
