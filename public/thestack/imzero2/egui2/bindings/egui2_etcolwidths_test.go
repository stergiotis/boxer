package bindings

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/containers"
)

// newColWidthsSM builds just enough StateManager to exercise the R25
// unpacking. The drain is the only ragged-stride reader in Sync, so its
// arithmetic is worth testing away from the FFI.
func newColWidthsSM() *StateManager {
	return &StateManager{
		etColWidths: containers.NewBinarySearchGrowingKVOrdered[uint64, EtColWidthsValue](8),
	}
}

func TestApplyEtColWidths_SplitsByCounts(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths(
		[]uint64{10, 20},
		[]uint64{3, 2},
		slices.Values([]float32{1, 2, 3, 40, 50}),
	)

	v, ok := sm.etColWidths.Get(10)
	require.True(t, ok)
	assert.Equal(t, []float32{1, 2, 3}, v.Widths)

	v, ok = sm.etColWidths.Get(20)
	require.True(t, ok)
	assert.Equal(t, []float32{40, 50}, v.Widths)
}

func TestApplyEtColWidths_EmptyIsNoop(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths(nil, nil, slices.Values([]float32{}))
	_, ok := sm.etColWidths.Get(10)
	assert.False(t, ok)
}

// A table can legitimately report zero columns; it must still occupy its
// slot rather than shifting the following table's values onto it.
func TestApplyEtColWidths_ZeroColumnTableDoesNotShiftOthers(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths(
		[]uint64{10, 20},
		[]uint64{0, 2},
		slices.Values([]float32{7, 8}),
	)
	v, ok := sm.etColWidths.Get(10)
	require.True(t, ok)
	assert.Empty(t, v.Widths)
	v, ok = sm.etColWidths.Get(20)
	require.True(t, ok)
	assert.Equal(t, []float32{7, 8}, v.Widths)
}

// The backing array is reused across frames when the column count is
// stable — the steady state must not allocate.
func TestApplyEtColWidths_ReusesBackingArray(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths([]uint64{10}, []uint64{3}, slices.Values([]float32{1, 2, 3}))
	first, ok := sm.etColWidths.Get(10)
	require.True(t, ok)

	sm.applyEtColWidths([]uint64{10}, []uint64{3}, slices.Values([]float32{4, 5, 6}))
	second, ok := sm.etColWidths.Get(10)
	require.True(t, ok)

	assert.Equal(t, []float32{4, 5, 6}, second.Widths)
	require.NotEmpty(t, first.Widths)
	assert.Same(t, &first.Widths[0], &second.Widths[0],
		"a stable column count must not reallocate every frame")
}

func TestApplyEtColWidths_GrowsWhenColumnCountRises(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths([]uint64{10}, []uint64{2}, slices.Values([]float32{1, 2}))
	sm.applyEtColWidths([]uint64{10}, []uint64{4}, slices.Values([]float32{1, 2, 3, 4}))
	v, _ := sm.etColWidths.Get(10)
	assert.Equal(t, []float32{1, 2, 3, 4}, v.Widths)
}

func TestApplyEtColWidths_ShrinksWhenColumnCountFalls(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths([]uint64{10}, []uint64{4}, slices.Values([]float32{1, 2, 3, 4}))
	sm.applyEtColWidths([]uint64{10}, []uint64{2}, slices.Values([]float32{9, 8}))
	v, _ := sm.etColWidths.Get(10)
	assert.Equal(t, []float32{9, 8}, v.Widths, "a stale tail must not survive")
}

// A truncated payload must publish what arrived rather than zeros: a
// resolver reads zeros as "the user set this column to zero" and would
// capture them as overrides.
func TestApplyEtColWidths_TruncatedPayloadDoesNotPublishZeros(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths([]uint64{10}, []uint64{4}, slices.Values([]float32{1, 2}))
	v, ok := sm.etColWidths.Get(10)
	require.True(t, ok)
	assert.Equal(t, []float32{1, 2}, v.Widths)
}

// A counts slice shorter than ids must not panic; the unlisted tables
// simply report no columns.
func TestApplyEtColWidths_ShortCountsSlice(t *testing.T) {
	sm := newColWidthsSM()
	sm.applyEtColWidths([]uint64{10, 20}, []uint64{1}, slices.Values([]float32{5}))
	v, ok := sm.etColWidths.Get(10)
	require.True(t, ok)
	assert.Equal(t, []float32{5}, v.Widths)
	v, ok = sm.etColWidths.Get(20)
	require.True(t, ok)
	assert.Empty(t, v.Widths)
}
