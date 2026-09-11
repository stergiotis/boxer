package legend

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/color"
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
