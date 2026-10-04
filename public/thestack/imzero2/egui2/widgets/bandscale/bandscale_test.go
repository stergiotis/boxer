package bandscale

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func ladder() []Band {
	return []Band{
		{Label: "talk", Tone: styletokens.ToneNeutral}, {Label: "read", Tone: styletokens.ToneSuccess},
		{Label: "edit", Tone: styletokens.ToneWarning}, {Label: "act", Tone: styletokens.ToneError},
	}
}

// One frame renders with no host behind it, from nothing but its input.
func TestRendersAFrame(t *testing.T) {
	t.Cleanup(scenetest.Install())
	ids := c.NewWidgetIdStack()
	res := Render(Input{Ids: ids, ScopeKey: "scale", Bands: ladder(),
		Markers: []Marker{{Position: 0.9, Label: "may", Hollow: true}, {Position: 0.3, Label: "now"}}})
	require.NoError(t, res.Err)
	assert.EqualValues(t, 320, res.Width)
	assert.Greater(t, res.Height, float32(30), "two marker rows, the bar and the band labels")

	compact := Render(Input{Ids: ids, ScopeKey: "bar", Bands: ladder(), Markers: []Marker{{Position: 0.5}}, Width: 120, Compact: true})
	require.NoError(t, compact.Err)
	assert.EqualValues(t, 120, compact.Width)
	assert.Less(t, compact.Height, res.Height, "the compact scale draws no label rows")
}

// A scale that cannot be drawn says so instead of drawing nothing.
func TestBrokenInputIsReported(t *testing.T) {
	t.Cleanup(scenetest.Install())
	assert.Error(t, Render(Input{Bands: ladder()}).Err, "no id stack")
	assert.Error(t, Render(Input{Ids: c.NewWidgetIdStack()}).Err, "no bands")
}

// A marker stays on the bar wherever its position falls.
func TestMarkersStayOnTheBar(t *testing.T) {
	l := Input{Width: 200}.layout()
	for _, p := range []float64{-1, 0, 0.5, 1, 7} {
		x := l.x(p)
		assert.GreaterOrEqual(t, x, l.pointer)
		assert.LessOrEqual(t, x, l.w-l.pointer)
	}
	assert.InDelta(t, 100, l.x(0.5), 0.01)
	assert.Equal(t, "bandscale", Input{}.scopeKey())
}
