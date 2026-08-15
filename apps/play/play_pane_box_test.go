package play

import (
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
	"github.com/stretchr/testify/assert"
)

// The rule the fixed-box panels share: the box IS the pane, less the margin.
// Before it, each of them derived a height from its width through a fixed
// aspect measured off one tour capture — which left every other leaf either
// scrolling or part empty.
func TestPaneFillFillsThePane(t *testing.T) {
	f := paneFill{slack: 12, minW: 360, maxW: 1600, minH: 240, fallbackW: 760, fallbackH: 460}

	// Whatever the probe reports, both dimensions follow it. No height ceiling:
	// a taller leaf must get a taller box, which is the whole point.
	for _, pane := range []float32{300, 460, 720, 1100, 2000} {
		w, h := f.box(900, pane)
		assert.Equalf(t, pane-f.slack, h, "a %vpt pane must fill", pane)
		assert.Equal(t, float32(900-12), w)
	}

	// The floors hold for a leaf too small to hold a drawing; it scrolls.
	w, h := f.box(40, 40)
	assert.Equal(t, f.minW, w)
	assert.Equal(t, f.minH, h)

	// The width alone is capped — an ultrawide leaf must not stretch the
	// drawing across the screen, where its height is the leaf's own business.
	w, h = f.box(4000, 4000)
	assert.Equal(t, f.maxW, w)
	assert.Equal(t, float32(4000-12), h)

	// A dimension the probe has not answered for — the first frame, and the
	// frame a hidden tab comes back on — takes the fallback, per dimension.
	w, h = f.box(0, 0)
	assert.Equal(t, f.fallbackW-f.slack, w)
	assert.Equal(t, f.fallbackH-f.slack, h)
	w, _ = f.box(0, 900)
	assert.Equal(t, f.fallbackW-f.slack, w)
	_, h = f.box(900, 0)
	assert.Equal(t, f.fallbackH-f.slack, h)
}

// Every panel's bounds have to agree with themselves, and a fallback that lands
// on a floor would make the frame before the probe reports a jump rather than
// an approximation.
func TestPanelPaneFillsAreCoherent(t *testing.T) {
	for _, tc := range []struct {
		name string
		f    paneFill
	}{
		{"network", networkPaneFill},
		{"sankey", sankeyPaneFill},
		{"icicle", iciclePaneFill},
		{"treemap", treemapPaneFill},
		{"experiments-topology", experimentsTopoPaneFill},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Positive(t, tc.f.slack)
			assert.Positive(t, tc.f.minH)
			assert.Less(t, tc.f.minW, tc.f.maxW)
			w, h := tc.f.box(0, 0)
			assert.GreaterOrEqual(t, w, tc.f.minW, "the fallback width is not at the floor")
			assert.LessOrEqual(t, w, tc.f.maxW)
			assert.GreaterOrEqual(t, h, tc.f.minH, "the fallback height is not at the floor")
		})
	}
}

// The implot panels' readability floors must clear the height below which a
// plot clips its own x tick labels from the inside — the floor implot exports
// for exactly this decision. Checked against the most demanding label set, so
// the assertion holds however these plots come to be labelled.
func TestImplotPanelFloorsClearTheClipFloor(t *testing.T) {
	clip := implot.MinBoxHeight(true, true, true, 1)
	assert.GreaterOrEqual(t, iciclePaneFill.minH, clip)
	assert.GreaterOrEqual(t, sankeyPaneFill.minH, clip)
}

// A widget that draws its own chrome around the box it is handed — the
// Treemap's breadcrumb bar — is covered by the pane too, so the chrome comes
// off before the floor applies. The floor still holds under it: a leaf shorter
// than the chrome must get the floor, not the fallback.
func TestPaneFillCoversWidgetChrome(t *testing.T) {
	bare := paneFill{slack: 12, minW: 360, maxW: 1600, minH: 260, fallbackW: 760, fallbackH: 460}
	withChrome := bare
	withChrome.chrome = 48

	_, h := bare.box(900, 500)
	_, hc := withChrome.box(900, 500)
	assert.Equal(t, h-48, hc, "the chrome comes out of the same pane")

	_, hc = withChrome.box(900, 40)
	assert.Equal(t, withChrome.minH, hc, "a leaf shorter than the chrome takes the floor")

	// The fallback is a guess at the pane, so it carries the chrome too.
	_, hc = withChrome.box(0, 0)
	assert.Equal(t, withChrome.fallbackH-withChrome.slack-withChrome.chrome, hc)
}
