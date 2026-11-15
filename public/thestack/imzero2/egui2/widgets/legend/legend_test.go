package legend

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
)

func TestMeasureFollowsWidestLabelAndRowCount(t *testing.T) {
	st := Style{FontSize: 10, RowHeight: 16, Padding: 6, Swatch: 10, TextWidth: func(s string, _ float32) float32 { return float32(len(s)) * 5 }}
	w, h := Measure([]Item{{Label: "ab"}, {Label: "abcd"}}, st)
	require.Equal(t, float32(6*3+10+20), w)
	require.Equal(t, float32(6*2+2*16), h)
	w, h = Measure(nil, st)
	require.Zero(t, w)
	require.Zero(t, h)
}

func TestDimmedKeepsColourDropsAlpha(t *testing.T) {
	require.Equal(t, uint32(0x11223340), dimmed(color.Hex(0x112233ff)).Literal())
	var none color.Color
	require.Equal(t, color.ColorKindNone, dimmed(none).Kind())
}

func TestItemKeyDefaultsToLabel(t *testing.T) {
	require.Equal(t, "L", Item{Label: "L"}.key())
	require.Equal(t, "K", Item{Key: "K", Label: "L"}.key())
}

// Every labelled row is named — a hit region beside its sense region — and a
// row with no label has nothing to be named by.
func TestEmitSenseNamesLabelledRows(t *testing.T) {
	messages, zero, reset := scenetest.InstallCounting()
	t.Cleanup(reset)
	ids := c.NewWidgetIdStack()
	zero()
	EmitSense(ids, "legend-", []Item{{Label: "a"}, {Label: "b", Hidden: true}, {Key: "k"}}, 0, 0, Style{})
	if n := messages(); n != 5 {
		t.Errorf("three rows, two labelled, sent %d messages, want 5 (three sense regions, two names)", n)
	}
	require.NotEqual(t, rowTip(false), rowTip(true))
}

// A horizontal legend puts items side by side, each as wide as its label,
// and wraps at MaxWidth; an item wider than the bound gets a line of its own.
func TestHorizontalLayoutWraps(t *testing.T) {
	st := Style{FontSize: 10, RowHeight: 16, Padding: 6, Swatch: 10, Horizontal: true,
		TextWidth: func(s string, _ float32) float32 { return float32(len(s)) * 5 }}
	items := []Item{{Label: "ab"}, {Label: "abcd"}, {Label: "abcdef"}}
	type cell struct{ x, y, w float32 }
	cells := func(st Style) (out []cell, w, h float32) {
		w, h = layout(items, st.withDefaults(), func(_ int, x, y, cw float32) { out = append(out, cell{x, y, cw}) })
		return
	}
	// Cell = padding, swatch, padding, text: 6+10+6+5·len.
	got, w, h := cells(st)
	require.Equal(t, []cell{{0, 6, 32}, {44, 6, 42}, {98, 6, 52}}, got)
	require.Equal(t, float32(98+52+6), w)
	require.Equal(t, float32(6*2+16), h)
	vw, vh := Measure(items, st)
	require.Equal(t, [2]float32{w, h}, [2]float32{vw, vh})

	st.MaxWidth = 100
	got, w, h = cells(st)
	require.Equal(t, []cell{{0, 6, 32}, {44, 6, 42}, {0, 22, 52}}, got)
	require.Equal(t, float32(44+42+6), w)
	require.Equal(t, float32(6*2+2*16), h)

	st.MaxWidth = 20
	got, _, h = cells(st)
	require.Equal(t, float32(0), got[1].x)
	require.Equal(t, float32(6*2+3*16), h)
}
