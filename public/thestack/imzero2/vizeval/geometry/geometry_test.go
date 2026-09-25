package geometry

import (
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantedSVG is the exporter's shape with one planted case per metric:
// two labels overlapping, one cut by a cell's clip, one cut by the area's
// bottom edge, one elided, one grey-on-grey, and a right-aligned column of
// numbers. The grey panel is painted before the text it sits under.
const plantedSVG = `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 300" width="400" height="300">
  <defs>
    <clipPath id="c0"><rect x="0" y="0" width="400" height="200"/></clipPath>
    <clipPath id="c1"><rect x="200" y="10" width="40" height="20"/></clipPath>
    <style>@font-face{}</style>
  </defs>
  <rect x="0" y="0" width="400" height="300" fill="#000000"/>
  <g clip-path="url(#c0)">
  <rect x="0" y="0" width="400" height="200" fill="#1e1e1e" fill-opacity="1.000"/>
  <rect x="300" y="100" width="80" height="40" fill="#777777" fill-opacity="1.000"/>
  <g class="imz-text" data-text="alpha" data-bbox="10.00 10.00 50.00 16.00" data-size="13.00">
  <text x="10" y="22" font-size="13" fill="#e5e8eb" fill-opacity="1.000">a</text>
  </g>
  <g class="imz-text" data-text="beta" data-bbox="40.00 12.00 50.00 16.00" data-size="13.00">
  <text x="40" y="24" font-size="13" fill="#e5e8eb" fill-opacity="1.000">b</text>
  </g>
  <g clip-path="url(#c1)">
  <g class="imz-text" data-text="too long for its cell" data-bbox="200.00 12.00 90.00 16.00" data-size="13.00">
  <text x="200" y="24" font-size="13" fill="#e5e8eb" fill-opacity="1.000">t</text>
  </g>
  </g>
  <g class="imz-text" data-text="bottom" data-bbox="10.00 190.00 40.00 16.00" data-size="13.00">
  <text x="10" y="202" font-size="13" fill="#e5e8eb" fill-opacity="1.000">b</text>
  </g>
  <g class="imz-text" data-text="shortened" data-bbox="10.00 60.00 60.00 16.00" data-size="9.00" data-elided="1">
  <text x="10" y="72" font-size="9" fill="#e5e8eb" fill-opacity="1.000">s</text>
  </g>
  <g class="imz-text" data-text="| cut by the sink… |" data-bbox="10.00 80.00 60.00 16.00" data-size="13.00">
  <text x="10" y="92" font-size="13" fill="#e5e8eb" fill-opacity="1.000">|</text>
  </g>
  <g class="imz-text" data-text="faint" data-bbox="310.00 110.00 40.00 16.00" data-size="13.00">
  <text x="310" y="122" font-size="13" fill="#888888" fill-opacity="1.000">f</text>
  </g>
  <g class="imz-text" data-text="1.5" data-bbox="140.00 100.00 20.00 16.00" data-size="13.00"><text x="140" y="112" fill="#e5e8eb">1</text></g>
  <g class="imz-text" data-text="22.75" data-bbox="126.00 120.00 34.00 16.00" data-size="13.00"><text x="126" y="132" fill="#e5e8eb">2</text></g>
  <g class="imz-text" data-text="-3" data-bbox="146.00 140.00 14.00 16.00" data-size="13.00"><text x="146" y="152" fill="#e5e8eb">-</text></g>
  <rect x="200" y="150" width="10" height="10" fill="#d62728" fill-opacity="1.000"/>
  <polygon points="220,150 230,150 225,160" fill="#1f77b4" fill-opacity="1.000"/>
  </g>
</svg>`

func TestReadSVG(t *testing.T) {
	d, err := ReadSVG(strings.NewReader(plantedSVG))
	require.NoError(t, err)
	assert.Equal(t, Rect{0, 0, 400, 300}, d.Viewport)
	require.Len(t, d.Runs, 10)
	assert.Equal(t, "alpha", d.Runs[0].Text)
	assert.Equal(t, Rect{0, 0, 400, 200}, d.Runs[0].Clip, "a run inherits its group's clip")
	assert.Equal(t, Rect{200, 10, 240, 30}, d.Runs[2].Clip, "nested clips intersect")
	assert.True(t, d.Runs[4].Elided)
	assert.InDelta(t, 0x88/255.0, d.Runs[6].Fill.R, 1e-9, "a run takes its first glyph's fill")
	assert.Len(t, d.Marks, 5, "clip path rects are not marks")
}

func TestMeasurePlanted(t *testing.T) {
	d, err := ReadSVG(strings.NewReader(plantedSVG))
	require.NoError(t, err)
	area := VisibleArea(d, Rect{0, 0, 400, 200})
	assert.Equal(t, Rect{0, 0, 400, 200}, area)
	m := Measure(d, area, nil)
	assert.Equal(t, 10.0, m[MetricTextRuns])
	assert.Equal(t, 1.0, m[MetricTextOverlapPairs], "alpha and beta")
	assert.Equal(t, 1.0, m[MetricTextClipped], "the cell's text")
	assert.Equal(t, 1.0, m[MetricTextCutAtEdge], "the run past the bottom")
	assert.Equal(t, 2.0, m[MetricTextElided], "one elided by egui, one cut by its sink")
	assert.Equal(t, 9.0, m[MetricTextMinSize])
	assert.Equal(t, 1.0, m[MetricTextLowContrast], "grey on grey")
	assert.Less(t, m[MetricTextMinContrast], 2.0)
	assert.Equal(t, 1.0, m[MetricTableNumericColumns])
	assert.Equal(t, 1.0, m[MetricTableNumericRightAligned])
	assert.Equal(t, 2.0, m[MetricColorDistinct], "red and blue; the greys are not colours")
	assert.Greater(t, m[MetricColorMinDeltaE], 20.0)
	_, has := m[MetricInkRatio]
	assert.False(t, has, "no capture, no ink ratio")
}

func TestInkRatio(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			c := color.RGBA{20, 20, 20, 255}
			if x < 25 {
				c = color.RGBA{230, 230, 230, 255}
			}
			img.Set(x, y, c)
		}
	}
	d := Drawing{Viewport: Rect{0, 0, 50, 50}} // a 2x capture
	m := Measure(d, Rect{0, 0, 50, 50}, img)
	assert.InDelta(t, 0.25, m[MetricInkRatio], 1e-9)
}

func TestColorReferences(t *testing.T) {
	white, black := RGBA{1, 1, 1, 1}, RGBA{0, 0, 0, 1}
	assert.InDelta(t, 21.0, Contrast(white, black), 1e-9)
	assert.InDelta(t, 1.0, Contrast(white, white), 1e-9)
	// Sharma, Wu, Dalal (2005), test pair 1.
	assert.InDelta(t, 2.0425, DeltaE2000(Lab{50, 2.6772, -79.7751}, Lab{50, 0, -82.7485}), 1e-4)
	// Test pair 7: achromatic pair.
	assert.InDelta(t, 0.0, DeltaE2000(Lab{50, 0, 0}, Lab{50, 0, 0}), 1e-9)
	assert.InDelta(t, 100.0, white.Lab().L, 1e-3)
}
