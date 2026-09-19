package carrierclient

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/observability/eh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func f64(v float64) *float64 { return &v }

func TestBindMatchBindsNamedGroupsAndParsesNumbers(t *testing.T) {
	re, err := compilePattern(`centre (?P<lat>-?[\d.]+), (?P<lon>-?[\d.]+)\s+zoom (?P<z>[\d.]+) loading (?P<l>\w+)`)
	require.NoError(t, err)
	v := NewVars()
	require.True(t, v.bindMatch(re, "centre 51.09920, -17.03660   zoom 12.00 loading false"))

	lat, err := v.Number("lat")
	require.NoError(t, err)
	assert.InDelta(t, 51.0992, lat, 1e-9)
	lon, err := v.Number("lon")
	require.NoError(t, err)
	assert.InDelta(t, -17.0366, lon, 1e-9)

	// A capture that is not a number is still bound; only reading it as one fails.
	text, err := v.Text("l")
	require.NoError(t, err)
	assert.Equal(t, "false", text)
	_, err = v.Number("l")
	require.Error(t, err)

	assert.False(t, v.bindMatch(re, "no camera here"))
}

func TestCompilePatternInsistsOnANamedGroup(t *testing.T) {
	_, err := compilePattern(`zoom ([\d.]+)`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "named group")
	_, err = compilePattern("")
	require.Error(t, err)
	_, err = compilePattern(`zoom (?P<z>[`)
	require.Error(t, err)
}

func TestExpectNumericComparisons(t *testing.T) {
	v := NewVars()
	v.Bind("z0", "12.00")
	v.Bind("z1", "13.004")
	v.Bind("errors", "0")

	require.NoError(t, v.expect(Step{Of: "errors", Eq: f64(0)}))
	require.NoError(t, v.expect(Step{Of: "z1", Minus: "z0", Approx: f64(1), Tol: 0.011}))
	require.NoError(t, v.expect(Step{Of: "z1", Minus: "z0", Min: f64(0.55), Max: f64(1.2)}))

	err := v.expect(Step{Of: "z1", Minus: "z0", Approx: f64(1), Tol: 0.001})
	require.Error(t, err)
	// What was read and what was expected are in the text, not only in fields.
	var buf strings.Builder
	eh.FormatErrorPlain(&buf, err)
	human := buf.String()
	assert.Contains(t, human, "z1 - z0")
	assert.Contains(t, human, "1.004")
	assert.Contains(t, human, "1 +/- 0.001")
	err = v.expect(Step{Of: "z1", Max: f64(13)})
	require.Error(t, err)
	err = v.expect(Step{Of: "z1", Min: f64(14)})
	require.Error(t, err)
}

func TestExpectTextComparisons(t *testing.T) {
	v := NewVars()
	v.Bind("loading", "false")
	require.NoError(t, v.expect(Step{Of: "loading", Is: "false"}))
	require.NoError(t, v.expect(Step{Of: "loading", Matches: "^(true|false)$"}))
	require.Error(t, v.expect(Step{Of: "loading", Is: "true"}))
	require.Error(t, v.expect(Step{Of: "loading", Matches: "^t"}))
}

func TestExpectRejectsWhatItCannotMean(t *testing.T) {
	v := NewVars()
	v.Bind("a", "1")
	v.Bind("s", "text")
	require.Error(t, v.expect(Step{}), "no name")
	require.Error(t, v.expect(Step{Of: "a"}), "no comparison")
	require.Error(t, v.expect(Step{Of: "unbound", Eq: f64(1)}))
	require.Error(t, v.expect(Step{Of: "s", Eq: f64(1)}), "text compared as a number")
	require.Error(t, v.expect(Step{Of: "s", Minus: "a", Is: "text"}), "a difference of texts")
}

func TestOffsetAddsBoundNumbers(t *testing.T) {
	v := NewVars()
	v.Bind("ox", "190.5")
	v.Bind("oy", "230")
	x, y, err := v.offset(Step{XFrom: "ox", YFrom: "oy"}, 360, 230)
	require.NoError(t, err)
	assert.InDelta(t, 550.5, x, 1e-4)
	assert.InDelta(t, 460, y, 1e-4)

	x, y, err = v.offset(Step{}, 10, 20)
	require.NoError(t, err)
	assert.Equal(t, float32(10), x)
	assert.Equal(t, float32(20), y)

	_, _, err = v.offset(Step{XFrom: "missing"}, 0, 0)
	require.Error(t, err)
}
