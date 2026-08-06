package implot

import "testing"

// newLayoutProbe is a plot bound to no canvas, carrying just the label choices
// and the box height layoutFrame reads. layoutFrame paints nothing, so this is
// enough to run the real arithmetic rather than a copy of it.
func newLayoutProbe(title, xLabel, yLabel string, h float32) *Plot {
	p := NewDetached()
	p.w, p.h = 600, h
	p.titleShown = title
	p.st.x.label, p.st.y.label = xLabel, yLabel
	p.st.x.rng, p.st.y.rng = Range{0, 100}, Range{0, 100}
	return p
}

// A plot's gutters come OUT of its box height: layoutFrame subtracts them and
// floors what is left at a minimum plot area (see areaH). So a box shorter
// than gutters+minimum does not shrink the plot — it makes the layout exceed
// the canvas, and the canvas clips. What gets clipped is the BOTTOM gutter,
// which is where the x tick labels are.
//
// That matters outside this package: a caller sizing a plot to its pane
// (play's Chart, Distribution and Series tabs — ADR-0172) needs a floor at or
// above this minimum, or its boxes lose the very labels the pane-following
// sizing exists to keep. MinBoxHeight is what those floors are checked
// against, and this test is the contract between the two.
func TestMinBoxHeightIsWhereTheLayoutStopsFitting(t *testing.T) {
	for _, tc := range []struct {
		name           string
		title          string
		xLabel, yLabel string
	}{
		{name: "bare"},
		{name: "y label only (Series, and the score plot under it)", yLabel: "bytes_per_min"},
		{name: "both labels (Distribution ECDF, Chart)", xLabel: "value", yLabel: "F(x)"},
		{name: "titled", title: "Rates", xLabel: "t", yLabel: "MiB/s"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			minH := MinBoxHeight(tc.title != "", tc.xLabel != "", tc.yLabel != "", 1)

			// At the minimum the layout fits the canvas exactly, and one point
			// under it no longer does. Below that the difference is what the
			// canvas clips off the bottom gutter.
			for _, probe := range []struct {
				h        float32
				wantFits bool
			}{{minH, true}, {minH + 40, true}, {minH - 1, false}, {minH / 2, false}} {
				p := newLayoutProbe(tc.title, tc.xLabel, tc.yLabel, probe.h)
				_, areaY, _, areaH := p.layoutFrame(1, p.maxBandLanes())
				drawn := areaY + areaH + bottomGutterFor(tc.xLabel != "", true, 1)
				if fits := drawn <= probe.h+1e-3; fits != probe.wantFits {
					t.Errorf("h=%v: drawn %v, fits=%v want %v (minimum %v)",
						probe.h, drawn, fits, probe.wantFits, minH)
				}
			}
		})
	}
}

// Every extra x-label lane deepens the bottom gutter, so a stacked axis needs
// a taller box. maxBandLanes already bounds stacking by a quarter of the
// canvas; this pins that the bound is tight enough that a stacked axis never
// asks for more than its box has.
func TestStackedLabelsStayInsideTheBox(t *testing.T) {
	for _, h := range []float32{80, 96, 120, 160, 240, 380, 600} {
		p := newLayoutProbe("", "x", "y", h)
		lanes := p.maxBandLanes()
		if got := MinBoxHeight(false, true, true, lanes); got > h {
			t.Errorf("h=%v: %d lane(s) need %v — stacking outgrew its own canvas", h, lanes, got)
		}
	}
}

// Callers floor their boxes at MinBoxHeight, so it must not depend on the
// caller having drawn anything yet — it is consulted BEFORE the plot exists,
// to pick the height the plot is then constructed with.
func TestMinBoxHeightNeedsNoPlot(t *testing.T) {
	if got := MinBoxHeight(false, false, true, 1); got <= 0 {
		t.Fatalf("MinBoxHeight must answer without a plot, got %v", got)
	}
	// Lane count is clamped, so a caller that has not measured its band yet
	// can pass 0 and still get the one-lane answer rather than a short one.
	if MinBoxHeight(false, true, true, 0) != MinBoxHeight(false, true, true, 1) {
		t.Error("an unmeasured lane count must read as one lane, not none")
	}
	// Deeper stacking costs more, monotonically — a floor computed for one
	// lane must never exceed one computed for two.
	if MinBoxHeight(false, true, true, 1) >= MinBoxHeight(false, true, true, 2) {
		t.Error("an extra label lane must deepen the minimum")
	}
}
