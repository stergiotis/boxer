package spectrumdisplay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPartitionBasic checks the region geometry with a colorbar and no line panel:
// gutters and colorbar abut the texture exactly, leaving the texture in the middle.
func TestPartitionBasic(t *testing.T) {
	r := partition(640, 400, layoutOpts{leftGutterW: 48, freqGutterH: 18, colorbarW: 60})
	require.Equal(t, rect{48, 0, 580, 382}, r.texture)
	require.Equal(t, rect{0, 0, 48, 382}, r.leftGutter)
	require.Equal(t, rect{48, 382, 580, 400}, r.freqGutter)
	require.Equal(t, rect{580, 0, 640, 382}, r.colorbar)
	require.False(t, r.linePanel.valid(), "no line panel requested")
	require.True(t, r.texture.valid() && r.colorbar.valid())
}

// TestPartitionLinePanel checks the vertical split: the line panel takes splitRatio of
// the data height above the texture, and the colorbar tracks the texture's y-range.
func TestPartitionLinePanel(t *testing.T) {
	r := partition(640, 400, layoutOpts{leftGutterW: 48, freqGutterH: 18, colorbarW: 60, showLine: true, splitRatio: 0.5, lineGapY: 2})
	require.Equal(t, rect{48, 0, 580, 191}, r.linePanel)
	require.Equal(t, rect{48, 193, 580, 382}, r.texture)
	require.Equal(t, rect{580, 193, 640, 382}, r.colorbar, "colorbar aligns to the texture, not the line panel")
}

// TestPartitionNoColorbar drops the colorbar column so the data area runs to the edge.
func TestPartitionNoColorbar(t *testing.T) {
	r := partition(400, 300, layoutOpts{leftGutterW: 40, freqGutterH: 16})
	require.Equal(t, rect{40, 0, 400, 284}, r.texture)
	require.False(t, r.colorbar.valid())
}

// TestFreqToPx maps and clamps the frequency axis onto pixels.
func TestFreqToPx(t *testing.T) {
	sd := &SpectrumDisplay{freqAxis: AxisSpec{Min: 0, Max: 100}}
	require.InDelta(t, 100, sd.freqToPx(50, 200), 1e-4)
	require.InDelta(t, 0, sd.freqToPx(-10, 200), 1e-4, "clamps below min")
	require.InDelta(t, 200, sd.freqToPx(150, 200), 1e-4, "clamps above max")
}

// TestDbToPx maps the dB axis with max at the top (y=0).
func TestDbToPx(t *testing.T) {
	sd := &SpectrumDisplay{powerAxis: AxisSpec{Min: -100, Max: 0}}
	require.InDelta(t, 0, sd.dbToPx(0, 100), 1e-4, "max at top")
	require.InDelta(t, 100, sd.dbToPx(-100, 100), 1e-4, "min at bottom")
	require.InDelta(t, 50, sd.dbToPx(-50, 100), 1e-4)
}

// TestGutterTicksMaxAtTop: the dB convention puts the maximum at the top, so
// ascending tick values map to descending y.
func TestGutterTicksMaxAtTop(t *testing.T) {
	sd := &SpectrumDisplay{}
	ticks := sd.gutterTicks(AxisSpec{Min: -100, Max: 0, Unit: AxisUnitDecibel}, 0, 100, true)
	require.NotEmpty(t, ticks)
	for i := 1; i < len(ticks); i++ {
		require.Less(t, ticks[i].Pos, ticks[i-1].Pos, "ascending dB ⇒ descending y (max at top)")
	}
}

// TestGutterTicksMinAtTop: the time-since convention puts the minimum at the top
// (newest row up), so ascending values map to ascending y, offset by top.
func TestGutterTicksMinAtTop(t *testing.T) {
	sd := &SpectrumDisplay{}
	ticks := sd.gutterTicks(AxisSpec{Min: 0, Max: 6, Unit: AxisUnitSeconds}, 10, 100, false)
	require.NotEmpty(t, ticks)
	for i := 1; i < len(ticks); i++ {
		require.Greater(t, ticks[i].Pos, ticks[i-1].Pos)
	}
	require.GreaterOrEqual(t, ticks[0].Pos, float32(10), "positions are offset by top")
}

// TestLeftGutterWidthFallback: with no axis to label, the gutter falls back to
// the documented default width.
func TestLeftGutterWidthFallback(t *testing.T) {
	sd := &SpectrumDisplay{fontSize: DefaultFontSize}
	require.Equal(t, DefaultLeftGutterW, sd.leftGutterWidth())
}

// TestLeftGutterWidthMeasured: a set power axis yields a measured width within
// the clamp bounds (the ADR-0091 §SD2 widest-label rule).
func TestLeftGutterWidthMeasured(t *testing.T) {
	sd := &SpectrumDisplay{
		fontSize:      DefaultFontSize,
		showLinePanel: true,
		powerAxis:     AxisSpec{Min: -110, Max: -20, Unit: AxisUnitDecibel},
	}
	w := sd.leftGutterWidth()
	require.GreaterOrEqual(t, w, minLeftGutterWPx)
	require.LessOrEqual(t, w, maxLeftGutterWPx)
}

// TestRegionBand maps placements to vertical bands.
func TestRegionBand(t *testing.T) {
	y0, y1 := regionBand(PlacementFull, 100)
	require.Equal(t, float32(0), y0)
	require.Equal(t, float32(100), y1)
	y0, y1 = regionBand(PlacementTop, 100)
	require.Equal(t, float32(0), y0)
	require.InDelta(t, 18, y1, 1e-4)
}
