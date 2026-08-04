package view

import (
	"math"
	"strings"
	"testing"

	"github.com/stergiotis/boxer/public/keelson/designsystem/styletokens"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/icicle"
	"github.com/stergiotis/boxer/public/thestack/imzero2/egui2/widgets/implot"
)

// testTree is the same call tree the layout tests use: main(18) over
// parse(9) and eval(8), each with two children.
func testTree() icicle.Tree {
	return icicle.Tree{
		Labels:  []string{"main", "parse", "lex", "ast", "eval", "walk", "emit"},
		Parents: []int32{-1, 0, 1, 1, 0, 4, 4},
		Self:    []float64{1, 2, 4, 3, 1, 5, 2},
	}
}

func mustLayout(t *testing.T, o icicle.Options) *icicle.Layout {
	t.Helper()
	lay, err := icicle.Compute(testTree(), o)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return lay
}

// linFrame builds a frame with a linear value-to-pixel mapping, standing in
// for implot's transform: [xLo,xHi] and [yLo,yHi] in plot space map onto the
// area rect, with plot y up and pixel y down.
func linFrame(xLo, xHi, yLo, yHi float64, areaX, areaY, areaW, areaH float32) frame {
	return frame{
		areaX: areaX, areaY: areaY, areaW: areaW, areaH: areaH,
		pxX: func(v float64) float32 {
			return areaX + float32((v-xLo)/(xHi-xLo))*areaW
		},
		plotX: func(px float32) float64 {
			return xLo + float64((px-areaX)/areaW)*(xHi-xLo)
		},
		pxY: func(v float64) float32 {
			return areaY + areaH - float32((v-yLo)/(yHi-yLo))*areaH
		},
		plotY: func(px float32) float64 {
			return yLo + float64((areaY+areaH-px)/areaH)*(yHi-yLo)
		},
	}
}

func newState(t *testing.T, lay *icicle.Layout, o Opts) *state {
	t.Helper()
	r := &Renderer{}
	r.s.prepare(lay, o.withDefaults(lay))
	return &r.s
}

// The whole icicle in a 360×54 area: 18 value units across, 3 rows down.
func icicleFrame() frame { return linFrame(0, 18, -3, 0, 0, 0, 360, 54) }

func TestRectOfSnapsAndLeavesAGap(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	f := icicleFrame()

	find := func(label string) *icicle.Node {
		for i := range lay.Nodes {
			if lay.Nodes[i].Label == label {
				return &lay.Nodes[i]
			}
		}
		t.Fatalf("no node %q", label)
		return nil
	}

	// lex spans [0,4] of 18 over 360 px → [0,80]; the gap takes a pixel off
	// the right edge.
	x0, y0, x1, y1, ok := rectOf(f, find("lex"))
	if !ok {
		t.Fatal("lex was culled")
	}
	if x0 != 0 || x1 != 80-gapPx {
		t.Errorf("lex x = [%v,%v], want [0,%v]", x0, x1, 80-gapPx)
	}
	// Row 2 of 3 over 54 px: rows are 18 px, depth 2 is the bottom one.
	if y0 != 36 || y1 != 54-gapPx {
		t.Errorf("lex y = [%v,%v], want [36,%v]", y0, y1, 54-gapPx)
	}

	// The neighbour starts exactly where this one's unshrunk edge was, so
	// the gap between them is exactly gapPx and both edges are integral.
	ax0, _, _, _, ok := rectOf(f, find("ast"))
	if !ok {
		t.Fatal("ast was culled")
	}
	if got := ax0 - x1; math.Abs(float64(got-gapPx)) > 1e-6 {
		t.Errorf("gap between lex and ast = %v, want %v", got, gapPx)
	}
	if ax0 != float32(math.Round(float64(ax0))) {
		t.Errorf("ast x0 = %v, want a whole pixel", ax0)
	}
}

func TestRectOfCullsSubPixelFrames(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "big", "speck"},
		Parents: []int32{-1, 0, 0},
		Self:    []float64{0, 10000, 1},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	// 10001 units across 360 px: the speck is well under a pixel.
	f := linFrame(0, 10001, -2, 0, 0, 0, 360, 36)
	for i := range lay.Nodes {
		n := &lay.Nodes[i]
		_, _, _, _, ok := rectOf(f, n)
		if n.Label == "speck" && ok {
			t.Error("a sub-pixel frame was emitted")
		}
		if n.Label == "big" && !ok {
			t.Error("a full-width frame was culled")
		}
	}
}

// The cull is one predicate, and the numbers it turns on are worth pinning:
// the edges snap first, so the width that survives is a whole number of
// pixels and the real cut sits at 2 px of un-inset span, not at minRectPx.
func TestSnapXCutsAtTwoWholePixels(t *testing.T) {
	cases := []struct {
		px0, px1   float64
		wantX0, x1 float64
		ok         bool
	}{
		{0, 80, 0, 79, true},     // a wide frame keeps its gap
		{0.4, 79.6, 0, 79, true}, // both edges snap, and land where the last case did
		{10, 12, 10, 11, true},   // exactly 2 px un-inset: 1 px survives, which is minRectPx-wide enough
		{10, 11.6, 10, 11, true}, // 1.6 px, but it snaps up to 2
		{10, 11.4, 10, 10, false},
		{10, 11, 10, 10, false}, // 1 px un-inset: the gap takes all of it
		{10, 10, 10, 9, false},  // degenerate
		{10, 9, 10, 8, false},   // inverted, which the layout never produces
	}
	for _, tc := range cases {
		x0, x1, ok := snapX(tc.px0, tc.px1)
		if x0 != tc.wantX0 || x1 != tc.x1 || ok != tc.ok {
			t.Errorf("snapX(%v,%v) = (%v,%v,%v), want (%v,%v,%v)",
				tc.px0, tc.px1, x0, x1, ok, tc.wantX0, tc.x1, tc.ok)
		}
	}
}

// The pointer must not name a frame the renderer culled. Both go through
// snapX, so this pins that resolveHit really consults it — and that it stands
// down before the first render, when nothing has been culled yet.
func TestResolveHitDeclinesWhatWasNotDrawn(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "big", "speck"},
		Parents: []int32{-1, 0, 0},
		Self:    []float64{0, 10000, 1},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	speck, big := -1, -1
	for i := range lay.Nodes {
		switch lay.Nodes[i].Label {
		case "speck":
			speck = i
		case "big":
			big = i
		}
	}
	if speck < 0 || big < 0 {
		t.Fatal("the fixture lost a node")
	}
	// The projection the frame in TestRectOfCullsSubPixelFrames stands for:
	// 10001 units across 360 px, so the speck is well under a pixel.
	proj := xProj{originPx: 0, vMin: 0, perValue: 360.0 / 10001}
	inside := func(i int) (float64, float64) {
		n := &lay.Nodes[i]
		return (n.X0 + n.X1) / 2, (n.Y0 + n.Y1) / 2
	}

	x, y := inside(speck)
	if h := resolveHit(lay, proj, true, x, y); h.Ok {
		t.Errorf("a culled sliver was hittable: node %d", h.Node)
	}
	// The layout on its own still reports it — the filter is the view's, and
	// the layout stays pixel-free.
	if got := lay.NodeAt(x, y); got != speck {
		t.Errorf("NodeAt = %d, want the speck at %d: the cull leaked into the layout", got, speck)
	}
	// Before the first render there is no projection, and nothing has been
	// drawn for a hit to disagree with.
	if h := resolveHit(lay, xProj{}, false, x, y); !h.Ok || int(h.Node) != speck {
		t.Errorf("first-frame hit = %+v, want the speck at %d", h, speck)
	}
	// A frame that is drawn stays hittable, and empty space stays empty.
	x, y = inside(big)
	if h := resolveHit(lay, proj, true, x, y); !h.Ok || int(h.Node) != big {
		t.Errorf("hit on a drawn frame = %+v, want node %d", h, big)
	}
	if h := resolveHit(lay, proj, true, -1, y); h.Ok {
		t.Error("a point outside the tree reported a hit")
	}
}

// The projection is recovered from readbacks, so it has to decline while
// those are empty rather than invent a mapping.
func TestPlotXProjNeedsARenderedPlot(t *testing.T) {
	if _, ok := plotXProj(implot.NewDetached()); ok {
		t.Error("a plot that has never rendered offered a projection")
	}
	proj := xProj{originPx: 40, vMin: 5, perValue: 2}
	for _, tc := range []struct{ v, want float64 }{{5, 40}, {6, 42}, {0, 30}} {
		if got := proj.px(tc.v); got != tc.want {
			t.Errorf("px(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestVisibleRowsBothOrientations(t *testing.T) {
	cases := []struct {
		name   string
		orient icicle.OrientationE
		frame  frame
		lo, hi int
	}{
		// The whole tree in view.
		{"icicle all", icicle.OrientIcicle, linFrame(0, 18, -3, 0, 0, 0, 360, 54), 0, 2},
		{"flame all", icicle.OrientFlame, linFrame(0, 18, 0, 3, 0, 0, 360, 54), 0, 2},
		// Scrolled so only the deepest two rows show.
		{"icicle scrolled", icicle.OrientIcicle, linFrame(0, 18, -3, -1, 0, 0, 360, 36), 1, 2},
		{"flame scrolled", icicle.OrientFlame, linFrame(0, 18, 1, 3, 0, 0, 360, 36), 1, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lay := mustLayout(t, icicle.Options{Orientation: tc.orient})
			s := newState(t, lay, Opts{})
			lo, hi, ok := s.visibleRows(tc.frame)
			if !ok {
				t.Fatal("visibleRows reported nothing on screen")
			}
			if lo != tc.lo || hi != tc.hi {
				t.Errorf("rows = [%d,%d], want [%d,%d]", lo, hi, tc.lo, tc.hi)
			}
		})
	}
}

func TestVisibleRowsClampsAndRejectsOffScreen(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	// A window far deeper than the tree.
	if _, _, ok := s.visibleRows(linFrame(0, 18, -40, -30, 0, 0, 360, 54)); ok {
		t.Error("a window past the deepest row reported visible rows")
	}
	// A window straddling the top: clamped to row 0 rather than negative.
	lo, hi, ok := s.visibleRows(linFrame(0, 18, -1, 2, 0, 0, 360, 54))
	if !ok || lo != 0 {
		t.Errorf("rows = [%d,%d,%v], want lo 0", lo, hi, ok)
	}
}

// The row window comes out of the plot transform, and a degenerate one hands
// back non-finite coordinates. They are bounded before the conversion for the
// same reason Layout.DepthAt bounds its argument: int() of such a value is
// INT_MIN on amd64, which a clamp written for a row index reads as plausible.
func TestVisibleRowsRejectsNonFiniteTransform(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	// A frame whose inverse transform is degenerate: areaH of 0 divides by
	// zero in linFrame, which is exactly how one arises in practice.
	degenerate := func(v float64) frame {
		f := icicleFrame()
		f.plotY = func(float32) float64 { return v }
		return f
	}
	for _, v := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if lo, hi, ok := s.visibleRows(degenerate(v)); ok {
			t.Errorf("plotY = %v gave rows [%d,%d], want nothing visible", v, lo, hi)
		}
	}
	// One finite edge and one infinite one still resolves, clamped to the
	// tree, rather than collapsing to nothing.
	f := icicleFrame()
	f.plotY = func(px float32) float64 {
		if px == 0 {
			return 0
		}
		return math.Inf(-1) // an unbounded scroll downward, in icicle sign
	}
	if lo, hi, ok := s.visibleRows(f); !ok || lo != 0 || hi != len(lay.Rows)-1 {
		t.Errorf("rows = [%d,%d,%v], want the whole tree [0,%d]", lo, hi, ok, len(lay.Rows)-1)
	}
}

// Zooming the value axis must reduce the nodes considered, not just the ones
// drawn: that is what keeps frame cost tracking the view and not the tree.
func TestEachVisibleCullsByValueWindow(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})

	collect := func(f frame) []string {
		var got []string
		s.eachVisible(f, func(n *icicle.Node, _ int) { got = append(got, n.Label) })
		return got
	}
	all := collect(icicleFrame())
	if len(all) != 7 {
		t.Errorf("unzoomed view yielded %d nodes (%v), want all 7", len(all), all)
	}
	// Zoom onto eval's subtree, x in [9,17].
	zoomed := collect(linFrame(9, 17, -3, 0, 0, 0, 360, 54))
	joined := strings.Join(zoomed, ",")
	for _, want := range []string{"main", "eval", "walk", "emit"} {
		if !strings.Contains(joined, want) {
			t.Errorf("zoomed view is missing %s (got %v)", want, joined)
		}
	}
	for _, unwanted := range []string{"lex", "ast"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("zoomed view still visits %s, which is off screen (got %v)", unwanted, joined)
		}
	}
}

func TestCollectFramesBatchesOnlyVisibleRects(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	s.collectFrames(icicleFrame())
	if got := len(s.rMinX); got != 7 {
		t.Errorf("batched %d rects, want 7", got)
	}
	if len(s.rCols) != len(s.rMinX) || len(s.rMaxY) != len(s.rMinX) {
		t.Fatal("the batched rect columns disagree in length")
	}
	for i := range s.rMinX {
		if s.rMaxX[i] <= s.rMinX[i] || s.rMaxY[i] <= s.rMinY[i] {
			t.Errorf("rect %d is degenerate: [%v,%v]-[%v,%v]", i, s.rMinX[i], s.rMinY[i], s.rMaxX[i], s.rMaxY[i])
		}
	}
	// Re-emitting must reset the buffers rather than append to them.
	s.collectFrames(icicleFrame())
	if got := len(s.rMinX); got != 7 {
		t.Errorf("second emit batched %d rects, want 7 (buffers were not reset)", got)
	}
}

// The label pass reads the batch instead of projecting and hashing every node
// again, so the node column has to be there and has to line up with the rest.
func TestCollectFramesCarriesTheNodeAndFill(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	f := icicleFrame()
	s.collectFrames(f)
	if len(s.rNode) != len(s.rMinX) {
		t.Fatalf("the node column is %d long against %d rects", len(s.rNode), len(s.rMinX))
	}
	if !s.collected {
		t.Error("collectFrames did not mark the buffers as filled")
	}
	seen := map[string]bool{}
	for i, idx := range s.rNode {
		n := &lay.Nodes[idx]
		seen[n.Label] = true
		// The carried fill is the one the node would have got on its own.
		if got := s.fill(n); got != s.rCols[i] {
			t.Errorf("%s: carried fill %08x, want %08x", n.Label, s.rCols[i], got)
		}
		// And the carried rect is the one rectOf produces for it.
		x0, y0, x1, y1, ok := rectOf(f, n)
		if !ok || x0 != s.rMinX[i] || y0 != s.rMinY[i] || x1 != s.rMaxX[i] || y1 != s.rMaxY[i] {
			t.Errorf("%s: carried rect [%v,%v]-[%v,%v], want [%v,%v]-[%v,%v] (ok=%v)",
				n.Label, s.rMinX[i], s.rMinY[i], s.rMaxX[i], s.rMaxY[i], x0, y0, x1, y1, ok)
		}
		// The label the draw path would produce agrees with labelFor's.
		wantText, wantX, wantY, wantOK := s.labelFor(f, n)
		text, lx, ly, lok := s.labelIn(s.rMinX[i], s.rMinY[i], s.rMaxX[i], s.rMaxY[i], n.Label)
		if text != wantText || lx != wantX || ly != wantY || lok != wantOK {
			t.Errorf("%s: label from the batch = (%q,%v,%v,%v), from the node = (%q,%v,%v,%v)",
				n.Label, text, lx, ly, lok, wantText, wantX, wantY, wantOK)
		}
	}
	for _, want := range []string{"main", "parse", "eval", "lex", "ast", "walk", "emit"} {
		if !seen[want] {
			t.Errorf("the batch is missing %s", want)
		}
	}
	// prepare starts a new draw, so the buffers are no longer this frame's.
	s.prepare(lay, Opts{}.withDefaults(lay))
	if s.collected {
		t.Error("prepare left the previous draw's buffers marked as current")
	}
}

func TestLabelForFitsAndDeclines(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})
	s := newState(t, lay, Opts{})
	find := func(label string) *icicle.Node {
		for i := range lay.Nodes {
			if lay.Nodes[i].Label == label {
				return &lay.Nodes[i]
			}
		}
		t.Fatalf("no node %q", label)
		return nil
	}

	// A wide frame in a roomy area gets its label, inset from the left edge
	// and centred in its row.
	f := icicleFrame()
	text, x, y, ok := s.labelFor(f, find("main"))
	if !ok {
		t.Fatal("the root frame got no label")
	}
	if text != "main" {
		t.Errorf("label = %q, want the whole name", text)
	}
	if x != labelPadPx {
		t.Errorf("label x = %v, want the pad %v", x, labelPadPx)
	}
	// Row 0 covers pixels [0,18) less the gap, so its centre is 8.5.
	if y != 8.5 {
		t.Errorf("label y = %v, want the row centre 8.5", y)
	}

	// A label too long for its frame is elided rather than overflowing.
	long := icicle.Tree{
		Labels:  []string{"github.com/example/pkg.(*Server).ServeHTTP"},
		Parents: []int32{-1},
		Self:    []float64{1},
	}
	longLay, err := icicle.Compute(long, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	longState := newState(t, longLay, Opts{})
	text, _, _, ok = longState.labelFor(linFrame(0, 1, -1, 0, 0, 0, 120, 18), &longLay.Nodes[0])
	if !ok {
		t.Fatal("a 120 px frame got no label at all")
	}
	if !strings.HasSuffix(text, "…") {
		t.Errorf("label %q was not elided to fit 120 px", text)
	}
	if w := implot.EstimateTextWidth(text, DefaultFontSize); w > 120 {
		t.Errorf("elided label %q estimates at %v px, wider than its 120 px frame", text, w)
	}

	// Narrow still elides; narrower than one character plus an ellipsis
	// declines outright rather than drawing a bare "…".
	if got, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 30, 54), find("main")); !ok || !strings.HasSuffix(got, "…") {
		t.Errorf("in a 30 px frame the label was %q (ok=%v), want it elided", got, ok)
	}
	if _, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 15, 54), find("main")); ok {
		t.Error("a frame far too narrow still got a label")
	}
	// A row shorter than the glyphs gets no label however wide it is.
	if _, _, _, ok := s.labelFor(linFrame(0, 18, -3, 0, 0, 0, 1200, 12), find("main")); ok {
		t.Error("a row shorter than the font size still got a label")
	}
}

func TestContrastTextPicksTheReadableNeutral(t *testing.T) {
	mid := styletokens.Sequential(styletokens.SequentialLajolla, 0.6).AsHex() // a mid flame-band fill
	dark := styletokens.NeutralBgPanel.AsHex()
	if got := contrastText(0xffffffff); got != styletokens.NeutralBgExtreme.AsHex() {
		t.Errorf("ink on white = %08x, want the dark neutral", got)
	}
	if got := contrastText(0x000000ff); got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink on black = %08x, want the light neutral", got)
	}
	if got := contrastText(dark); got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink on the panel neutral = %08x, want the light neutral", got)
	}
	// Whatever it picks for a mid palette colour, it must be one of the two
	// tokens and never an invented value.
	got := contrastText(mid)
	if got != styletokens.NeutralBgExtreme.AsHex() && got != styletokens.NeutralTextExtreme.AsHex() {
		t.Errorf("ink = %08x, which is neither IDS neutral", got)
	}
}

// The switch point is derived from the two inks, so it cannot go stale when
// the palette is regenerated. This pins what it derives to today and, more to
// the point, that it really is the balance point rather than a rounded one.
func TestInkSwitchIsTheEqualContrastPoint(t *testing.T) {
	d := styletokens.NeutralBgExtreme.AsHex()
	l := styletokens.NeutralTextExtreme.AsHex()
	if math.Abs(inkSwitchL-0.173365) > 1e-5 {
		t.Errorf("inkSwitchL = %v, want 0.173365 for the palette as it stands", inkSwitchL)
	}
	// A fill exactly at the switch contrasts equally with either ink. The
	// identity is on the luminance, not on a colour: no byte triple lands
	// exactly on the switch, so quantising to one first would only measure
	// the rounding.
	toDark := (inkSwitchL + 0.05) / (implot.RelativeLuminance(d) + 0.05)
	toLite := (implot.RelativeLuminance(l) + 0.05) / (inkSwitchL + 0.05)
	if math.Abs(toDark-toLite) > 1e-9 {
		t.Errorf("at the switch the inks give %.6f:1 and %.6f:1, want them level", toDark, toLite)
	}
	// And the ink really does turn over there, in the direction claimed.
	if contrastText(0xffffffff) != d || contrastText(0x000000ff) != l {
		t.Error("the switch runs the wrong way: dark ink belongs on the brighter fill")
	}
	// And the whole flame band stays within a whisker of 4.5:1 under it. The
	// band is a dark-to-light ramp, so it crosses the switch, and at the
	// crossing the two inks meet at 4.45:1 — that is the floor pinned here;
	// everything off the crossing reads higher.
	for i := range 256 {
		t01 := flameBandLo + (flameBandHi-flameBandLo)*float32(i)/255
		fill := styletokens.Sequential(styletokens.SequentialLajolla, t01).AsHex()
		if cr := implot.ContrastRatio(fill, contrastText(fill)); cr < 4.4 {
			t.Errorf("flame band at t=%.3f (%08x) reads at only %.4f:1", t01, fill, cr)
		}
	}
}

// A frame's colour must depend only on its name, so the same function is the
// same colour in two captures and at two places in one tree.
func TestFillByLabelIsPositionIndependent(t *testing.T) {
	tr := icicle.Tree{
		Labels:  []string{"root", "a", "shared", "b", "shared"},
		Parents: []int32{-1, 0, 1, 0, 3},
		Self:    []float64{0, 1, 5, 1, 5},
	}
	lay, err := icicle.Compute(tr, icicle.Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	s := newState(t, lay, Opts{})
	var shared []uint32
	for i := range lay.Nodes {
		if lay.Nodes[i].Label == "shared" {
			shared = append(shared, s.fill(&lay.Nodes[i]))
		}
	}
	if len(shared) != 2 {
		t.Fatalf("found %d shared frames, want 2", len(shared))
	}
	if shared[0] != shared[1] {
		t.Errorf("the same label got two colours: %08x and %08x", shared[0], shared[1])
	}
	// And different names are told apart. Deterministic, so this is a real
	// pin, not a flake: it would catch the hash-to-band mapping collapsing.
	if a, b := s.fill(&lay.Nodes[0]), s.fill(&lay.Nodes[1]); a == b {
		t.Errorf("%q and %q share a colour: %08x", lay.Nodes[0].Label, lay.Nodes[1].Label, a)
	}
}

// The hash lands inside the flame band by construction: the band's ends are
// where Lajolla stops reading as fire, so a hash escaping them would draw a
// frame in a background colour.
func TestFlameTStaysInsideTheBand(t *testing.T) {
	// The interval is closed and carries one ulp of slack: float32 rounds the
	// topmost hashes onto the band ceiling exactly, which flameT documents.
	const ulp = 1e-6
	for _, h := range []uint32{0, 1, 0x40000000, 0x7fffffff} {
		got := flameT(h)
		if got < flameBandLo || got > flameBandHi+ulp {
			t.Errorf("flameT(%#x) = %v, outside [%v, %v]", h, got, flameBandLo, flameBandHi)
		}
	}
	if flameT(0) != flameBandLo {
		t.Errorf("flameT(0) = %v, want the band floor %v", flameT(0), flameBandLo)
	}
}

func TestFillModesAndOverride(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})

	byDepth := newState(t, lay, Opts{Color: ColorByDepth})
	root, leaf := &lay.Nodes[0], &lay.Nodes[2]
	if root.Depth == leaf.Depth {
		t.Fatal("the fixture lost its depth range")
	}
	if byDepth.fill(root) == byDepth.fill(leaf) {
		t.Error("ColorByDepth gave two different depths the same colour")
	}

	// The override wins, and ok=false falls through to the scheme.
	const sentinel = 0x1234abff
	sel := newState(t, lay, Opts{
		NodeColor: func(n *icicle.Node) (uint32, bool) {
			return sentinel, n.Label == "main"
		},
	})
	if got := sel.fill(root); got != sentinel {
		t.Errorf("override ignored: %08x", got)
	}
	if got := sel.fill(leaf); got == sentinel {
		t.Error("override applied to a node it declined")
	}
}

func TestWithDefaults(t *testing.T) {
	lay := mustLayout(t, icicle.Options{})

	got := Opts{}.withDefaults(lay)
	if got.RowPx != DefaultRowPx || got.FontSize != DefaultFontSize {
		t.Errorf("defaults not applied: RowPx=%v FontSize=%v", got.RowPx, got.FontSize)
	}
	// The zero Hit means nothing, which is the point of it being a struct:
	// node 0 stays addressable.
	if got.Hover.Ok || got.Selected.Ok {
		t.Error("the zero Opts highlighted a node")
	}
	withRoot := Opts{Selected: NodeHit(0)}.withDefaults(lay)
	if !withRoot.Selected.Ok || withRoot.Selected.Node != 0 {
		t.Error("node 0 could not be selected")
	}
	// A stale index from a previous, larger layout is folded away rather than
	// indexing out of range at draw time.
	stale := Opts{Hover: NodeHit(len(lay.Nodes) + 5)}.withDefaults(lay)
	if stale.Hover.Ok {
		t.Error("an out-of-range hit survived normalisation")
	}
	if neg := (Opts{Hover: Hit{Node: -2, Ok: true}}).withDefaults(lay); neg.Hover.Ok {
		t.Error("a negative hit survived normalisation")
	}
	// An explicit size is left alone.
	kept := Opts{RowPx: 30, FontSize: 9}.withDefaults(lay)
	if kept.RowPx != 30 || kept.FontSize != 9 {
		t.Errorf("explicit sizes were overwritten: %v / %v", kept.RowPx, kept.FontSize)
	}
}

// RowPx is a minimum, not a height: the span is the pane divided by it, but
// capped at the tree's own depth so a shallow tree gets taller rows rather
// than empty pane.
func TestDepthSpanIsDerivedAndCapped(t *testing.T) {
	const rowPx = 18
	cases := []struct {
		name   string
		rows   float64
		areaH  float32
		areaOk bool
		want   float64
	}{
		{"shallow tree in a tall pane", 3, 540, true, 3}, // 30 rows would fit; there are 3
		{"deep tree", 100, 540, true, 30},                // 540/18
		{"exactly full", 30, 540, true, 30},              // the cap and the fit agree
		{"first frame has no area", 100, 0, false, 100},  // nothing to divide yet
		{"a stale area of zero", 100, 0, true, 100},      // and a zero one is not a span
		{"a collapsed pane", 100, 9, true, 0.5},          // half a row, honestly reported
		{"one row deep", 1, 540, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := depthSpan(tc.rows, tc.areaH, rowPx, tc.areaOk); got != tc.want {
				t.Errorf("depthSpan(%v, %v, %v, %v) = %v, want %v",
					tc.rows, tc.areaH, rowPx, tc.areaOk, got, tc.want)
			}
		})
	}
	// A zero RowPx would divide by zero. withDefaults rules it out, but the
	// helper is the one that must not produce an infinity.
	if got := depthSpan(100, 540, 0, true); got != 100 {
		t.Errorf("depthSpan with no row height = %v, want the tree's depth", got)
	}
}

// The window and the condition are one answer: re-asserting the span every
// frame would pin the axis, so CondAlways has to be reserved for the frames
// where the window is genuinely wrong.
func TestDepthWindowKeepsTheRootEdgeAndLetsAPanStick(t *testing.T) {
	cases := []struct {
		name             string
		cur0, cur1, span float64
		flame            bool
		known, reset     bool
		lo, hi           float64
		cond             implot.Cond
	}{
		// First frame: nothing known yet, so seed the root edge once.
		{"icicle first frame", 0, 0, 5, false, false, false, -5, 0, implot.CondOnce},
		{"flame first frame", 0, 0, 5, true, false, false, 0, 5, implot.CondOnce},
		// Steady state: the span still holds, so a pan sticks.
		{"icicle steady, scrolled", -12, -7, 5, false, true, false, -5, 0, implot.CondOnce},
		{"flame steady, scrolled", 7, 12, 5, true, true, false, 0, 5, implot.CondOnce},
		// Resize: re-assert the span, keeping the edge nearest the root —
		// which is the far end of the window in each orientation.
		{"icicle resized", -12, -7, 8, false, true, false, -15, -7, implot.CondAlways},
		{"flame resized", 7, 12, 8, true, true, false, 7, 15, implot.CondAlways},
		// Reset outranks a resize and returns to the root either way.
		{"icicle reset", -12, -7, 8, false, true, true, -8, 0, implot.CondAlways},
		{"flame reset", 7, 12, 8, true, true, true, 0, 8, implot.CondAlways},
		// A difference below the epsilon is not a resize: at CondAlways a
		// drag would fight the widget every frame.
		{"sub-epsilon drift", -5, 0, 5 + spanEps/2, false, true, false, -5.0005, 0, implot.CondOnce},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lo, hi, cond := depthWindow(tc.cur0, tc.cur1, tc.span, tc.flame, tc.known, tc.reset)
			if math.Abs(lo-tc.lo) > 1e-9 || math.Abs(hi-tc.hi) > 1e-9 || cond != tc.cond {
				t.Errorf("depthWindow = (%v,%v,%v), want (%v,%v,%v)", lo, hi, cond, tc.lo, tc.hi, tc.cond)
			}
			// Whatever it returns spans exactly what was asked for.
			if got := hi - lo; math.Abs(got-tc.span) > 1e-9 {
				t.Errorf("window spans %v rows, want %v", got, tc.span)
			}
		})
	}
}

// A resize must re-scale the view without also scrolling it: the row at the
// root-side edge stays put.
func TestDepthWindowResizeDoesNotScroll(t *testing.T) {
	for _, flame := range []bool{false, true} {
		cur0, cur1 := -12.0, -7.0 // icicle: scrolled to rows 7..12
		if flame {
			cur0, cur1 = 7.0, 12.0
		}
		for _, span := range []float64{3, 8} { // shrink and grow
			lo, hi, cond := depthWindow(cur0, cur1, span, flame, true, false)
			if cond != implot.CondAlways {
				t.Errorf("flame=%v span=%v: cond = %v, want CondAlways", flame, span, cond)
			}
			// The root-side edge is the one nearer the root: cur0 growing up,
			// cur1 growing down.
			if flame && lo != cur0 {
				t.Errorf("flame span=%v: lower edge moved from %v to %v", span, cur0, lo)
			}
			if !flame && hi != cur1 {
				t.Errorf("icicle span=%v: upper edge moved from %v to %v", span, cur1, hi)
			}
		}
	}
}

func TestRootWindowRunsAwayFromZero(t *testing.T) {
	if lo, hi := rootWindow(4, true); lo != 0 || hi != 4 {
		t.Errorf("flame root window = (%v,%v), want (0,4)", lo, hi)
	}
	if lo, hi := rootWindow(4, false); lo != -4 || hi != 0 {
		t.Errorf("icicle root window = (%v,%v), want (-4,0)", lo, hi)
	}
}

// The entry points are called from render code where a nil layout is a normal
// transient state, so none of them may panic.
func TestNilSafety(t *testing.T) {
	if h, c, clicked := Probe(nil, nil); h.Ok || c.Ok || clicked {
		t.Error("Probe on a nil plot reported a hit")
	}
	Setup(nil, nil, Opts{})
	Draw(nil, nil, Opts{})
	ZoomTo(nil, nil, 0)
	var r Renderer
	if h, c, clicked := r.Show(nil, "t", 10, 10, nil, Opts{}); h.Ok || c.Ok || clicked {
		t.Error("Show on a nil layout reported a hit")
	}
}
