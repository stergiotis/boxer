//go:build llm_generated_opus47

package h3arrow_test

import (
	"context"
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/stergiotis/boxer/public/science/geo/h3"
	"github.com/stergiotis/boxer/public/science/geo/h3/h3arrow"
	"github.com/stretchr/testify/require"
)

// bridgeAvailable acquires a handle or skips the test if the embedded wasm
// has not been built. Mirrors the parent package's helper to keep tests
// runnable against the checked-in placeholder artifact.
func bridgeAvailable(tb testing.TB) (rt *h3.Runtime, handle *h3.Handle) {
	tb.Helper()
	var err error
	rt, err = h3.NewRuntime(context.Background(), h3.RuntimeConfig{PoolSize: 1})
	if err != nil {
		if errors.Is(err, h3.ErrExportNotFound) || errors.Is(err, h3.ErrNoWasmBytes) {
			tb.Skipf("h3 wasm bridge not built (%v)", err)
			return
		}
		require.NoError(tb, err)
	}
	tb.Cleanup(func() { _ = rt.Close() })
	handle, err = rt.AcquireE(context.Background())
	require.NoError(tb, err)
	tb.Cleanup(handle.Release)
	return
}

func TestCellsAsArrowUint64_RoundTripAndZeroCopy(t *testing.T) {
	cells := []uint64{0x8a2a1072b59ffff, 0x8f283470dc2fff9, 0xc0ffeec0ffee}
	arr := h3arrow.CellsAsArrowUint64(cells)
	require.NotNil(t, arr)
	defer arr.Release()

	require.Equal(t, arrow.UINT64, arr.DataType().ID())
	require.Equal(t, len(cells), arr.Len())
	for i, c := range cells {
		require.Equal(t, c, arr.Value(i), "idx=%d", i)
	}

	// Zero-copy: mutating the source slice must be visible through the
	// arrow view (until the array is Released). This asserts we're not
	// silently copying.
	cells[0] = 0xdeadbeef
	require.Equal(t, uint64(0xdeadbeef), arr.Value(0))
}

func TestFloat64sAsArrowFloat64_RoundTrip(t *testing.T) {
	vals := []float64{0, 1.5, -42.0, 1e300}
	arr := h3arrow.Float64sAsArrowFloat64(vals)
	require.NotNil(t, arr)
	defer arr.Release()

	require.Equal(t, arrow.FLOAT64, arr.DataType().ID())
	require.Equal(t, len(vals), arr.Len())
	for i, v := range vals {
		require.Equal(t, v, arr.Value(i))
	}
}

func TestCellsAsArrowUint64_Empty(t *testing.T) {
	arr := h3arrow.CellsAsArrowUint64(nil)
	require.NotNil(t, arr)
	defer arr.Release()
	require.Equal(t, 0, arr.Len())
}

func TestCSRAsArrowListUint64E_RoundTrip(t *testing.T) {
	// Three rows: [100, 101, 102], [200], [300, 301].
	values := []uint64{100, 101, 102, 200, 300, 301}
	offsets := []int32{0, 3, 4, 6}

	arr, err := h3arrow.CSRAsArrowListUint64E(values, offsets)
	require.NoError(t, err)
	defer arr.Release()

	require.Equal(t, 3, arr.Len())
	require.Equal(t, int32(0), arr.Offsets()[0])
	require.Equal(t, int32(3), arr.Offsets()[1])
	require.Equal(t, int32(4), arr.Offsets()[2])
	require.Equal(t, int32(6), arr.Offsets()[3])

	// Arrow's ListValues() is the flat value array; slice it using the
	// row offsets.
	valuesArr := arr.ListValues()
	require.Equal(t, arrow.UINT64, valuesArr.DataType().ID())

	require.Equal(t, len(values), valuesArr.Len())
}

func TestCSRAsArrowListFloat64E_RoundTrip(t *testing.T) {
	values := []float64{0, 0, 1, 0, 1, 1, 0, 1}
	offsets := []int32{0, 4, 8} // two 4-vertex rings

	arr, err := h3arrow.CSRAsArrowListFloat64E(values, offsets)
	require.NoError(t, err)
	defer arr.Release()

	require.Equal(t, 2, arr.Len())
}

func TestCSR_InvariantViolations(t *testing.T) {
	_, err := h3arrow.CSRAsArrowListUint64E(nil, nil)
	require.ErrorIs(t, err, h3arrow.ErrEmptyOffsets)

	_, err = h3arrow.CSRAsArrowListUint64E([]uint64{1, 2}, []int32{0, 5}) // offsets[1] > len(values)
	require.ErrorIs(t, err, h3arrow.ErrOffsetsOutOfRange)

	_, err = h3arrow.CSRAsArrowListFloat64E(nil, []int32{})
	require.ErrorIs(t, err, h3arrow.ErrEmptyOffsets)
}

// TestEndToEnd_H3ToArrow exercises the full pipeline: h3 bulk call →
// arrow adapter → arrow array reads. Verifies the adapter plays with real
// outputs from the wasm bridge, not just synthetic data.
func TestEndToEnd_H3ToArrow(t *testing.T) {
	_, h := bridgeAvailable(t)
	ctx := context.Background()

	lats := []float64{0, 37.7749, 48.8566, -33.8688}
	lngs := []float64{0, -122.4194, 2.3522, 151.2093}
	cells, _, err := h.LatLngsToCellsE(ctx, h3.ResolutionR7, lats, lngs, nil, nil)
	require.NoError(t, err)

	arr := h3arrow.CellsAsArrowUint64(cells)
	defer arr.Release()
	require.Equal(t, len(cells), arr.Len())
	for i, c := range cells {
		require.Equal(t, c, arr.Value(i))
	}

	// Variable-arity: children → List<Uint64>.
	children, childOffsets, _, err := h.CellsToChildrenE(ctx, h3.ResolutionR9, cells, nil, nil, nil)
	require.NoError(t, err)

	list, err := h3arrow.CSRAsArrowListUint64E(children, childOffsets)
	require.NoError(t, err)
	defer list.Release()
	require.Equal(t, len(cells), list.Len())
}
