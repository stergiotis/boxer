package spectrumdisplay

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestEngFormatHertz pins the engineering-suffix selection: an axis around 870 MHz
// with a few-MHz span reads in MHz (the SDR convention), not GHz or Hz.
func TestEngFormatHertz(t *testing.T) {
	const mag = 871.59e6
	require.Equal(t, "870 MHz", engFormat(870e6, mag, AxisUnitHertz))
	require.Equal(t, "869.5 MHz", engFormat(869.5e6, mag, AxisUnitHertz))
	require.Equal(t, "2 GHz", engFormat(2e9, 2e9, AxisUnitHertz))
	require.Equal(t, "100 kHz", engFormat(100e3, 200e3, AxisUnitHertz))
}

// TestEngFormatSeconds pins the downward SI scale for a time axis.
func TestEngFormatSeconds(t *testing.T) {
	require.Equal(t, "2.5 s", engFormat(2.5, 2.5, AxisUnitSeconds))
	require.Equal(t, "5 ms", engFormat(0.005, 0.005, AxisUnitSeconds))
	require.Equal(t, "250 µs", engFormat(250e-6, 500e-6, AxisUnitSeconds))
}

// TestEngFormatDecibel keeps dB bare (the unit goes in the axis caption).
func TestEngFormatDecibel(t *testing.T) {
	require.Equal(t, "-100", engFormat(-100, 110, AxisUnitDecibel))
	require.Equal(t, "-37.5", engFormat(-37.5, 110, AxisUnitDecibel))
	require.Equal(t, "0", engFormat(0, 110, AxisUnitDecibel))
}

// TestAxisTicksHertzSharedSuffix checks that a frequency axis produces ascending
// ticks within range, all sharing the MHz suffix.
func TestAxisTicksHertzSharedSuffix(t *testing.T) {
	pos, lab := AxisTicks(AxisSpec{Min: 868.59e6, Max: 871.59e6, Unit: AxisUnitHertz})
	require.GreaterOrEqual(t, len(pos), 2)
	require.Equal(t, len(pos), len(lab))
	for i, p := range pos {
		require.GreaterOrEqual(t, p, 868.0e6)
		require.LessOrEqual(t, p, 872.0e6)
		require.True(t, strings.HasSuffix(lab[i], " MHz"), "label %q should be in MHz", lab[i])
		if i > 0 {
			require.Greater(t, p, pos[i-1], "ticks ascend")
		}
	}
}

// TestAxisTicksDegenerate returns the two endpoints for a zero/inverted span.
func TestAxisTicksDegenerate(t *testing.T) {
	pos, lab := AxisTicks(AxisSpec{Min: 5, Max: 5, Unit: AxisUnitGeneric})
	require.Equal(t, []float64{5, 5}, pos)
	require.Len(t, lab, 2)
}
