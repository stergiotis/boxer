package imztop

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// beginTimePlot pins the y axis to the view talbotTicks returns, so that view
// has to contain the range it was asked for: a view that clipped the range
// would clip the plotted data with it. It also has to place a tick at each
// end, which is the whole reason the axis is pinned — implot drops ticks
// outside the visible range, so a data-fitted axis loses its top label.
func TestTalbotTicksViewContainsRange(t *testing.T) {
	for _, hi := range []float64{1.0, 1.1, 1.5, 3.3, 55, 100, 0.0007} {
		layout, ok := talbotTicks(0, hi, 5)
		require.True(t, ok, "hi=%v", hi)
		assert.LessOrEqual(t, layout.ViewMin, 0.0, "hi=%v: view floor above the range", hi)
		assert.GreaterOrEqual(t, layout.ViewMax, hi, "hi=%v: view ceiling below the range", hi)
		require.NotEmpty(t, layout.TickValues, "hi=%v", hi)
		require.Len(t, layout.TickLabels, len(layout.TickValues), "hi=%v", hi)
		assert.Equal(t, layout.ViewMin, layout.TickValues[0], "hi=%v: no tick at the axis floor", hi)
		assert.Equal(t, layout.ViewMax, layout.TickValues[len(layout.TickValues)-1], "hi=%v: no tick at the axis ceiling", hi)
	}
}

// A degenerate range has no layout to pin, and the caller falls back to
// refitting the axis to the data.
func TestTalbotTicksRejectsDegenerateRange(t *testing.T) {
	for _, tc := range []struct{ lo, hi float64 }{{0, 0}, {1, 0}, {0, math.NaN()}} {
		_, ok := talbotTicks(tc.lo, tc.hi, 5)
		assert.False(t, ok, "lo=%v hi=%v", tc.lo, tc.hi)
	}
}

// rateUpperBound pads the peak so the pinned axis top does not chase every
// wiggle, and never reports less than 1 MiB/s so an idle interface still
// gets a labelled axis rather than a degenerate one.
func TestRateUpperBound(t *testing.T) {
	assert.Equal(t, 1.0, rateUpperBound(nil))
	assert.Equal(t, 1.0, rateUpperBound([]float64{0, 0.2, 0.5}))
	assert.InDelta(t, 4.4, rateUpperBound([]float64{1, 4}, []float64{2, 3}), 1e-12)
}
