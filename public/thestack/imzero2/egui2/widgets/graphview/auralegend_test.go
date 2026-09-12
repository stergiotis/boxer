package graphview

import (
	"testing"

	"github.com/stretchr/testify/require"

	c "github.com/stergiotis/boxer/public/thestack/imzero2/egui2/bindings"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/graphview/scenetest"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/legend"
)

// auraView declares three auras over three nodes, one opted out of the
// legend, and runs the row build for the given params.
func auraView(t *testing.T, ap AuraParams) (*View, AuraParams) {
	t.Helper()
	// AuraLegendInside paints, which needs a channel behind the lane.
	t.Cleanup(scenetest.Install())
	v := New(c.NewWidgetIdStack(), "t", Options{})
	v.style = DefaultStyle()
	nodes := []NodeSpec{
		{Id: 1, Auras: []string{"alpha"}},
		{Id: 2, Auras: []string{"beta"}},
		{Id: 3, Auras: []string{"gamma"}},
	}
	v.g.reconcile(nodes, nil)
	v.auraSet.build(nodes, &v.g)
	return v, ap.withDefaults()
}

func keys(items []legend.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Key)
	}
	return out
}

func TestAuraLegendRowsAreBuiltInEveryMode(t *testing.T) {
	// The mode chooses who paints the rows, not whether they exist — which
	// is what lets a caller draw them outside the view (ADR-0224 §SD15).
	for _, mode := range []AuraLegendModeE{AuraLegendOff, AuraLegendInside, AuraLegendExternal} {
		v, ap := auraView(t, AuraParams{Enabled: true, Legend: mode})
		v.emitAuraLegend(ap, 800, 600, true)
		require.Equal(t, []string{"alpha", "beta", "gamma"}, keys(v.AuraLegendItems()), "mode %d", mode)
	}
	// Auras off is the case that reports nothing at all.
	v, ap := auraView(t, AuraParams{Legend: AuraLegendExternal})
	v.emitAuraLegend(ap, 800, 600, true)
	require.Empty(t, v.AuraLegendItems())
}

func TestAuraLegendRowsCarryLabelAndHiddenState(t *testing.T) {
	v, ap := auraView(t, AuraParams{
		Enabled: true, Legend: AuraLegendExternal,
		Styles: map[string]AuraStyle{
			"beta":  {Label: "Beta group"},
			"gamma": {NoLegend: true},
		},
	})
	v.HideAura("alpha")
	v.emitAuraLegend(ap, 800, 600, true)
	items := v.AuraLegendItems()
	require.Equal(t, []string{"alpha", "beta"}, keys(items), "NoLegend keeps gamma out")
	require.Equal(t, "alpha", items[0].Label, "an aura without a label is its id")
	require.Equal(t, "Beta group", items[1].Label)
	require.True(t, items[0].Hidden)
	require.False(t, items[1].Hidden)
}

func TestAuraLegendCornersPlaceTheBoxInside(t *testing.T) {
	const w, h = 800, 600
	v, ap := auraView(t, AuraParams{Enabled: true, Legend: AuraLegendInside})
	v.emitAuraLegend(ap, w, h, true)
	items := v.AuraLegendItems()
	bw, bh := legend.Measure(items, ap.LegendStyle)
	require.Greater(t, bw, float32(0))

	for _, tc := range []struct {
		corner CornerE
		wantX  float32
		wantY  float32
	}{
		{CornerTopLeft, 8, 8},
		{CornerTopRight, w - bw - 8, 8},
		{CornerBottomLeft, 8, h - bh - 8},
		{CornerBottomRight, w - bw - 8, h - bh - 8},
	} {
		p := ap
		p.LegendCorner = tc.corner
		x, y := legendOrigin(items, p, w, h)
		require.Equal(t, tc.wantX, x, "corner %d x", tc.corner)
		require.Equal(t, tc.wantY, y, "corner %d y", tc.corner)
	}

	// A custom inset moves both edges.
	p := ap
	p.LegendCorner, p.LegendInset = CornerBottomRight, 20
	x, y := legendOrigin(items, p, w, h)
	require.Equal(t, w-bw-20, x)
	require.Equal(t, h-bh-20, y)
}

func TestAuraLegendStaysReachableOnATinyCanvas(t *testing.T) {
	// A canvas narrower than the box would push a right-anchored legend off
	// the left edge; the near edge wins so the rows can still be clicked.
	v, ap := auraView(t, AuraParams{Enabled: true, Legend: AuraLegendInside})
	v.emitAuraLegend(ap, 800, 600, true)
	items := v.AuraLegendItems()
	p := ap
	p.LegendCorner = CornerBottomRight
	x, y := legendOrigin(items, p, 10, 10)
	require.Equal(t, float32(8), x)
	require.Equal(t, float32(8), y)
}
