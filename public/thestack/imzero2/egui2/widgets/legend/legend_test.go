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

func TestRowAtMapsPointerToRow(t *testing.T) {
	st := Style{FontSize: 10, RowHeight: 16, Padding: 6, Swatch: 10, TextWidth: func(s string, _ float32) float32 { return 20 }}
	items := []Item{{Label: "a"}, {Label: "b"}, {Label: "c"}}
	w, h := Measure(items, st)
	require.Equal(t, 0, RowAt(items, 10, 10, 12, 10+6+1, st))
	require.Equal(t, 2, RowAt(items, 10, 10, 12, 10+6+2*16+3, st))
	require.Equal(t, -1, RowAt(items, 10, 10, 12, 10+h+1, st), "below the box")
	require.Equal(t, -1, RowAt(items, 10, 10, 10+w+1, 20, st), "right of the box")
	require.Equal(t, -1, RowAt(items, 10, 10, 12, 10+2, st), "in the top padding")
}

func TestDimmedKeepsColourDropsAlpha(t *testing.T) {
	require.Equal(t, uint32(0x11223340), Dimmed(color.Hex(0x112233ff)).Literal())
	var none color.Color
	require.Equal(t, color.ColorKindNone, Dimmed(none).Kind())
}

func TestItemKeyDefaultsToLabel(t *testing.T) {
	require.Equal(t, "L", Item{Label: "L"}.key())
	require.Equal(t, "K", Item{Key: "K", Label: "L"}.key())
}
