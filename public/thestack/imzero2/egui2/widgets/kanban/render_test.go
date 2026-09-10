package kanban

import (
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// The drag ghost's title wraps to the ghost's width rather than running out
// through its right edge, which is what one unwrapped paintText call did.
func TestGhostTitleLinesWrapToTheGhostWidth(t *testing.T) {
	const pt = 13
	// A 240 px lane less its padding, and a ghost tall enough for every line.
	const availW, availH = 236, 400
	title := "Regenerate the dispatch code and reconcile the drift test"
	lines := ghostTitleLines(title, availW, availH, pt)
	if len(lines) < 2 {
		t.Fatalf("ghostTitleLines(%q) = %q, want it broken over several lines", title, lines)
	}
	for _, line := range lines {
		if w := implot.EstimateTextWidth(line, pt); w > availW {
			t.Errorf("line %q is %v px wide, past the ghost's %v", line, w, availW)
		}
	}
}

// A title too tall for the ghost is cut to the lines that fit and the last one
// says so, so it cannot run out through the bottom edge either.
func TestGhostTitleLinesClampToTheGhostHeight(t *testing.T) {
	const pt = 13
	const availW = 100
	title := "a title long enough to need a good many lines at this width"
	full := ghostTitleLines(title, availW, 1000, pt)
	if len(full) < 4 {
		t.Fatalf("ghostTitleLines(%q) = %q, want at least four lines to clamp", title, full)
	}
	// Room for two lines only.
	availH := 2.5 * pt * ghostLineHeightRatio
	got := ghostTitleLines(title, availW, availH, pt)
	if len(got) != 2 {
		t.Fatalf("ghostTitleLines(%q, availH=%v) = %q, want 2 lines", title, availH, got)
	}
	if !strings.HasSuffix(got[len(got)-1], "…") {
		t.Errorf("last line %q does not say the title was cut", got[len(got)-1])
	}
	for _, line := range got {
		if w := implot.EstimateTextWidth(line, pt); w > availW {
			t.Errorf("line %q is %v px wide, past the ghost's %v", line, w, availW)
		}
	}
}

// The fallback ghost (no captured rect: 180 × 44) is one of the shapes a title
// has to survive — it must draw a line, and only lines that fit.
func TestGhostTitleLinesAlwaysDrawSomething(t *testing.T) {
	const pt = 13
	for _, tc := range []struct{ w, h float32 }{
		{180 - 4, 44 - 4}, // the w<60 / h<24 fallback, less PaddingTight
		{236, 400},
		{20, 8},  // narrower and shorter than one line
		{0, 100}, // degenerate width
	} {
		got := ghostTitleLines("Reconcile the drift test", tc.w, tc.h, pt)
		if tc.w <= 0 {
			if got != nil {
				t.Errorf("ghostTitleLines at width %v = %q, want nothing", tc.w, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("ghostTitleLines at %vx%v drew nothing", tc.w, tc.h)
		}
	}
}
